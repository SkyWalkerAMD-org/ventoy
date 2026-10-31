package center

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

const managedISO = "iso/ventoy-center"
const managedScripts = "user/scripts/ventoy-center"

var jobIDRE = regexp.MustCompile(`^[a-f0-9]{24}$`)

type Preparation struct {
	Release        string            `json:"release,omitempty"`
	SetupSHA256    string            `json:"setup_sha256,omitempty"`
	PreparedAt     time.Time         `json:"prepared_at"`
	Root           string            `json:"root"`
	ISOPath        string            `json:"iso_path"`
	ScriptPath     string            `json:"script_path"`
	SourceSize     int64             `json:"source_size"`
	SourceModified time.Time         `json:"source_modified"`
	Files          map[string]string `json:"files"`
}

type PreparationPlan struct {
	JobID              string `json:"job_id"`
	MAC                string `json:"mac"`
	ISOPath            string `json:"iso_path"`
	ScriptPath         string `json:"script_path"`
	ImageSelection     string `json:"image_selection"`
	ScriptSelection    string `json:"script_selection"`
	InjectionSelection string `json:"injection_selection,omitempty"`
	ManualBinding      bool   `json:"manual_binding"`
	Prepared           bool   `json:"prepared"`
	CanPrepare         bool   `json:"can_prepare"`
	CanCleanup         bool   `json:"can_cleanup"`
	Reason             string `json:"reason,omitempty"`
}

func managedPaths(j PrivateJob) (isoDir, scriptDir, script string, err error) {
	if !jobIDRE.MatchString(j.ID) {
		return "", "", "", errors.New("任务编号无效")
	}
	p, err := resolveProfile(j.Profile, j.OS)
	if err != nil {
		return "", "", "", err
	}
	if p.OS == "windows" {
		return "", "", "", errors.New("当前自动准备仅支持 Linux；Windows 请使用安装包")
	}
	script = map[string]string{"kickstart": "kickstart.cfg", "autoinstall": "user-data", "preseed": "preseed.cfg", "ubiquity": "preseed.cfg"}[p.Installer]
	if script == "" {
		return "", "", "", errors.New("未知安装模板")
	}
	return path.Join(managedISO, j.ID), path.Join(managedScripts, j.ID), script, nil
}

// Use only a configured local installation; never infer a server from a URL.
func (s *Server) openIVentoyRoot() (*os.Root, error) {
	if s.config.IVentoyRoot == "" {
		return nil, errors.New("请先配置同机 iVentoy 的 iventoy_root，并将 iso_dir 指向其 iso 目录")
	}
	r, err := os.OpenRoot(s.config.IVentoyRoot)
	if err != nil {
		return nil, fmt.Errorf("无法打开 iVentoy 目录：%w", err)
	}
	ok := false
	defer func() {
		if !ok {
			r.Close()
		}
	}()
	for _, name := range []string{"iso", "user", "user/scripts", "data"} {
		info, e := r.Lstat(name)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("iVentoy 缺少真实目录 %s", name)
		}
	}
	info, e := r.Lstat("data/iventoy.dat")
	if e != nil || !info.Mode().IsRegular() {
		return nil, errors.New("未找到 iVentoy data/iventoy.dat，请核对安装目录")
	}
	actual, e := r.Stat("iso")
	source, se := os.Stat(s.config.ISODir)
	if e != nil || se != nil || !os.SameFile(actual, source) {
		return nil, errors.New("自动准备要求 iso_dir 指向 iventoy_root/iso 的同一真实目录")
	}
	ok = true
	return r, nil
}

func ensureManagedDir(r *os.Root, name string) error {
	if err := r.Mkdir(name, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	info, err := r.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("受管路径不是普通目录")
	}
	return nil
}

func (s *Server) plan(j PrivateJob) PreparationPlan {
	p := PreparationPlan{JobID: j.ID, MAC: j.Machine.MAC, ManualBinding: true, Prepared: j.Preparation != nil && !j.Preparation.PreparedAt.IsZero()}
	i, d, script, err := managedPaths(j)
	if err != nil {
		p.Reason = err.Error()
		return p
	}
	p.ISOPath = path.Join(i, "image.iso")
	p.ScriptPath = path.Join(d, script)
	p.ImageSelection = strings.TrimPrefix(p.ISOPath, "iso/")
	p.ScriptSelection = filepath.Join(s.config.IVentoyRoot, filepath.FromSlash(p.ScriptPath))
	injectionName := bootInjectionName(j.Profile)
	if injectionName != "" {
		p.InjectionSelection = filepath.Join(s.config.IVentoyRoot, filepath.FromSlash(d), injectionName)
	}
	r, err := s.openIVentoyRoot()
	if err != nil {
		p.Reason = err.Error()
		return p
	}
	r.Close()
	if j.Preparation != nil && j.Preparation.Root != s.config.IVentoyRoot {
		p.Reason = "iVentoy 路径已更改，请恢复原路径后清理旧文件"
		return p
	}
	p.CanCleanup = j.Preparation != nil && terminal(j.State)
	p.CanPrepare = j.State == "ready" && time.Now().Before(j.Expires)
	if j.Preparation != nil && p.InjectionSelection != "" && j.Preparation.Files[injectionName] == "" {
		p.InjectionSelection = ""
		p.CanPrepare = false
		p.Reason = "此旧任务未包含所需的启动兼容注入包；请取消并清理旧任务后重新创建"
		return p
	}
	if !p.CanPrepare && !p.CanCleanup {
		p.Reason = "准备文件需要有效的待启动任务；清理前须完成或取消任务"
	}
	return p
}

func (s *Server) iventoyStatus(w http.ResponseWriter, r *http.Request) {
	root, err := s.openIVentoyRoot()
	message := "可准备 Linux 安装文件；镜像、脚本和 MAC 规则需在 iVentoy 中关联"
	if err != nil {
		message = err.Error()
	} else {
		root.Close()
	}
	respond(w, 200, map[string]any{"configured": s.config.IVentoyRoot != "", "available": err == nil, "root": s.config.IVentoyRoot, "message": message, "manual_binding": true})
}

func (s *Server) preparationPlan(w http.ResponseWriter, r *http.Request) {
	st := s.store.snapshot()
	j := findJob(&st, r.PathValue("id"))
	if j == nil {
		fail(w, 404, "任务不存在")
		return
	}
	respond(w, 200, s.plan(*j))
}

func (s *Server) prepareJob(w http.ResponseWriter, r *http.Request) {
	s.preparationMu.Lock()
	defer s.preparationMu.Unlock()
	var result PreparationPlan
	// Persist the exact cleanup inventory BEFORE any external filesystem mutation.
	// A failed write or process restart leaves a recoverable pending preparation.
	step := func(write bool) error {
		return s.store.update(func(st *State) error {
			j := findJob(st, r.PathValue("id"))
			if j == nil {
				return errors.New("任务不存在")
			}
			if j.State != "ready" || time.Now().After(j.Expires) {
				return errors.New("只能准备已授权且未过期的待启动任务")
			}
			root, err := s.openIVentoyRoot()
			if err != nil {
				return err
			}
			defer root.Close()
			prepared, err := s.prepareFiles(root, *j, write)
			if err != nil {
				return err
			}
			j.Preparation = prepared
			result = s.plan(*j)
			return nil
		})
	}
	err := step(false)
	if err == nil {
		err = step(true)
	}
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, result)
}

func (s *Server) prepareFiles(root *os.Root, j PrivateJob, write bool) (*Preparation, error) {
	isoDir, scriptDir, script, err := managedPaths(j)
	if err != nil {
		return nil, err
	}
	if j.Preparation != nil && j.Preparation.Root != s.config.IVentoyRoot {
		return nil, errors.New("iVentoy 路径与已准备记录不一致")
	}
	if strings.HasPrefix(j.ISO, "ventoy-center/") {
		return nil, errors.New("请选择原始镜像，不能把其他任务的临时镜像作为源")
	}
	source, err := safeOpen(s.config.ISODir, j.ISO, ".iso")
	if err != nil {
		return nil, errors.New("源镜像不可读取")
	}
	defer source.Close()
	sourceInfo, err := source.Stat()
	if err != nil {
		return nil, err
	}
	plain, err := root.Lstat(path.Join("iso", j.ISO))
	if err != nil || !plain.Mode().IsRegular() || !os.SameFile(sourceInfo, plain) {
		return nil, errors.New("源镜像必须是普通文件，不能是符号链接")
	}
	if j.Preparation != nil && (j.Preparation.SourceSize != sourceInfo.Size() || !j.Preparation.SourceModified.Equal(sourceInfo.ModTime())) {
		return nil, errors.New("源镜像在文件准备后发生变化，请取消任务并核对镜像")
	}
	copyJob := j
	copyJob.Preparation = nil
	files, err := s.renderFiles(copyJob)
	if err != nil {
		return nil, err
	}
	isoPath := path.Join(isoDir, "image.iso")
	result := &Preparation{Root: s.config.IVentoyRoot, ISOPath: isoPath, ScriptPath: path.Join(scriptDir, script), SourceSize: sourceInfo.Size(), SourceModified: sourceInfo.ModTime(), Files: map[string]string{}}
	result.Release = Version
	if s.config.LinuxSetupPreload && j.OS != "windows" {
		bundle, er := s.manualSetupBundle()
		if er != nil {
			return nil, er
		}
		result.SetupSHA256 = fmt.Sprintf("%x", sha256.Sum256(manualSetupLauncher(bundle)))
	}
	for name, content := range files {
		result.Files[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	}
	if j.Preparation != nil {
		result.PreparedAt = j.Preparation.PreparedAt
		if !reflect.DeepEqual(result.Files, j.Preparation.Files) {
			return nil, errors.New("安装配置自上次准备后已改变，请取消旧任务并重新创建")
		}
	}
	if !write {
		return result, nil
	}
	for _, dir := range []string{managedISO, managedScripts, isoDir, scriptDir} {
		if err = ensureManagedDir(root, dir); err != nil {
			return nil, fmt.Errorf("无法创建受管目录 %s：%w", dir, err)
		}
	}
	scripts, err := root.OpenRoot(scriptDir)
	if err != nil {
		return nil, err
	}
	defer scripts.Close()
	if err = checkEntries(scripts, files); err != nil {
		return nil, err
	}
	keys := sortedFiles(files)
	for _, name := range keys {
		if !fs.ValidPath(name) || strings.Contains(name, "/") {
			return nil, errors.New("安装文件名不符合 Linux 暂存规则")
		}
		if old, e := readManagedFile(scripts, name); e == nil {
			if string(old) != files[name] {
				return nil, fmt.Errorf("已有文件内容不一致，保留现场：%s", name)
			}
			continue
		} else if !errors.Is(e, fs.ErrNotExist) {
			return nil, e
		}
		f, e := scripts.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		_, e = f.WriteString(files[name])
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	iso, err := root.OpenRoot(isoDir)
	if err != nil {
		return nil, err
	}
	defer iso.Close()
	if err = checkEntries(iso, map[string]string{"image.iso": ""}); err != nil {
		return nil, err
	}
	if info, e := root.Lstat(isoPath); e == nil {
		if !info.Mode().IsRegular() || !os.SameFile(sourceInfo, info) {
			return nil, errors.New("镜像别名已存在且不是此源镜像的硬链接，未覆盖")
		}
	} else if !errors.Is(e, fs.ErrNotExist) {
		return nil, e
	} else if e = root.Link(path.Join("iso", j.ISO), isoPath); e != nil {
		return nil, fmt.Errorf("无法创建镜像硬链接；请检查同一文件系统和目录权限：%w", e)
	}
	// Check the final inode, including the source path, after link creation.
	alias, err := root.Lstat(isoPath)
	current, e := root.Stat(path.Join("iso", j.ISO))
	if err != nil || e != nil || !alias.Mode().IsRegular() || !os.SameFile(sourceInfo, alias) || !os.SameFile(sourceInfo, current) || sourceInfo.Size() != current.Size() || !sourceInfo.ModTime().Equal(current.ModTime()) {
		return nil, errors.New("源镜像在准备期间发生变化，未确认准备成功")
	}
	if result.PreparedAt.IsZero() {
		result.PreparedAt = time.Now().UTC()
	}
	return result, nil
}

func readManagedFile(root *os.Root, name string) ([]byte, error) {
	limit := int64(2 << 20)
	// Injected driver/setup bundles are larger than text answers. Retain a
	// bounded read while allowing an already prepared real preload to be checked.
	if strings.HasSuffix(name, ".tar.gz") {
		limit = 256 << 20
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("安装文件类型或大小发生变化，保留现场")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, errors.New("安装文件超过大小限制")
	}
	return b, err
}

func sortedFiles(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for name := range files {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	return keys
}

func checkEntries(root *os.Root, expected map[string]string) error {
	f, err := root.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok || !entry.Type().IsRegular() {
			return errors.New("受管目录包含未知文件或链接，未覆盖或删除")
		}
	}
	return nil
}

func (s *Server) cleanupJob(w http.ResponseWriter, r *http.Request) {
	s.preparationMu.Lock()
	defer s.preparationMu.Unlock()
	err := s.store.update(func(st *State) error {
		j := findJob(st, r.PathValue("id"))
		if j == nil {
			return errors.New("任务不存在")
		}
		if !terminal(j.State) {
			return errors.New("文件清理前请先完成或取消任务；安装中需先确认客户端停止并结单")
		}
		if j.Preparation == nil {
			return nil
		}
		if j.Preparation.Root != s.config.IVentoyRoot {
			return errors.New("请恢复任务准备时的 iVentoy 路径，再清理文件")
		}
		root, err := s.openIVentoyRoot()
		if err != nil {
			return err
		}
		defer root.Close()
		if err = cleanupFiles(root, *j); err != nil {
			return err
		}
		j.Preparation = nil
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func cleanupFiles(root *os.Root, j PrivateJob) error {
	isoDir, scriptDir, _, err := managedPaths(j)
	if err != nil {
		return err
	}
	for _, dir := range []string{managedISO, managedScripts} {
		info, e := root.Lstat(dir)
		if errors.Is(e, fs.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("受管目录已被替换，拒绝清理")
		}
	}
	// Validate every surviving file before deleting any of them. Missing files are
	// tolerated so an interrupted cleanup can be retried. Never recurse on delete.
	var dirs []*os.Root
	defer func() {
		for _, d := range dirs {
			if d != nil {
				d.Close()
			}
		}
	}()
	for i, dir := range []string{scriptDir, isoDir} {
		info, e := root.Lstat(dir)
		if errors.Is(e, fs.ErrNotExist) {
			dirs = append(dirs, nil)
			continue
		}
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("受管任务目录已被替换，拒绝清理")
		}
		d, e := root.OpenRoot(dir)
		if e != nil {
			return e
		}
		dirs = append(dirs, d)
		expected := j.Preparation.Files
		if i == 1 {
			expected = map[string]string{"image.iso": ""}
		}
		if e = checkEntries(d, expected); e != nil {
			return e
		}
		for name, digest := range expected {
			if !fs.ValidPath(name) || strings.Contains(name, "/") {
				return errors.New("清理记录包含无效文件名")
			}
			f, e := d.Open(name)
			if errors.Is(e, fs.ErrNotExist) {
				continue
			}
			if e != nil {
				return e
			}
			if i == 0 {
				f.Close()
				b, e := readManagedFile(d, name)
				if e != nil {
					return e
				}
				if fmt.Sprintf("%x", sha256.Sum256(b)) != digest {
					return errors.New("安装文件已被人工修改，保留现场并拒绝自动清理")
				}
			} else {
				info, e := f.Stat()
				f.Close()
				if e != nil {
					return e
				}
				if info.Size() != j.Preparation.SourceSize || !info.ModTime().Equal(j.Preparation.SourceModified) {
					return errors.New("镜像别名发生变化，保留现场并拒绝清理")
				}
				source, e := root.Lstat(path.Join("iso", j.ISO))
				if e != nil || !source.Mode().IsRegular() || !os.SameFile(info, source) {
					return errors.New("源镜像已移走或替换，保留别名供人工核对")
				}
			}
		}
	}
	for i, d := range dirs {
		if d == nil {
			continue
		}
		names := j.Preparation.Files
		if i == 1 {
			names = map[string]string{"image.iso": ""}
		}
		for name := range names {
			if err = d.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		d.Close()
		dirs[i] = nil
	}
	for _, dir := range []string{scriptDir, isoDir} {
		if err = root.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
