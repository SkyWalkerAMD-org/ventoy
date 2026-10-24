package center

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"reflect"
	"strings"
	"time"
)

func (s *Server) renderPXEFiles(e PrivatePXEEntry) (map[string]string, error) {
	p, err := pxeProfile(e.Profile)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(s.config.PublicURL, "https://") {
		return nil, errors.New("PXE 菜单要求 HTTPS 管理端")
	}
	policy, err := pxeDiskPolicy(e)
	if err != nil {
		return nil, err
	}
	settings, _ := json.Marshal(map[string]string{"url": s.config.PublicURL + "/pxe/" + e.ID, "token": e.Token, "ca": string(s.ca), "iso": path.Base(e.ISO), "installer": p.Installer, "disk_policy": policy})
	d := renderData{Target: p, PXEMenu: true, PXEISO: path.Base(e.ISO), PXESettings: base64.StdEncoding.EncodeToString(settings), OfflineMedia: debianDVD(p, e.ISO)}
	if p.Installer == "preseed" {
		d.Base = s.config.PublicURL + "/pxe/" + e.ID
		d.Token = e.Token
		d.CA = string(s.ca)
		client, err := render("pxe-debian.sh", d)
		if err != nil {
			return nil, err
		}
		d.PXECommand = client
		config, err := render("pxe-preseed.cfg", d)
		if err != nil {
			return nil, err
		}
		empty, err := packInjection(nil)
		if err != nil {
			return nil, err
		}
		files := map[string]string{"preseed.cfg": config, debianInjectionName: string(empty)}
		if err = s.addE610Media(e.ISO, p.ID, files); err != nil {
			return nil, err
		}
		return files, nil
	}
	client, err := render("pxe-client.py", d)
	if err != nil {
		return nil, err
	}
	d.PXECommand = "import base64;exec(compile(base64.b64decode('" + base64.StdEncoding.EncodeToString([]byte(client)) + "'),'<ventoy-pxe>','exec'))"
	name := "kickstart.cfg"
	tpl := "pxe-kickstart.cfg"
	if p.Installer == "autoinstall" {
		name = "user-data"
		tpl = "pxe-user-data"
	} else if p.Installer == "ubiquity" {
		name = "preseed.cfg"
		tpl = "pxe-ubiquity.cfg"
	}
	config, err := render(tpl, d)
	if err != nil {
		return nil, err
	}
	files := map[string]string{name: config}
	if inject := bootInjectionName(p.ID); inject != "" {
		var content string
		if p.OS == "ubuntu" {
			d.UserDataSHA512 = fmt.Sprintf("%x", sha512.Sum512([]byte(config)))
			content, err = ubuntuInjection(d)
		} else {
			content, err = kickstartInjection()
		}
		if err != nil {
			return nil, err
		}
		files[inject] = content
	}
	if err = s.addE610Media(e.ISO, p.ID, files); err != nil {
		return nil, err
	}
	return files, nil
}
func (s *Server) preparePXEEntry(w http.ResponseWriter, r *http.Request) {
	s.preparationMu.Lock()
	defer s.preparationMu.Unlock()
	var result PXEEntry
	step := func(write bool) error {
		return s.store.update(func(st *State) error {
			e := findPXEEntry(st, r.PathValue("id"))
			if e == nil {
				return errors.New("入口不存在")
			}
			root, err := s.openIVentoyRoot()
			if err != nil {
				return err
			}
			defer root.Close()
			p, err := s.preparePXEFiles(root, *e, write)
			if err != nil {
				return err
			}
			e.Preparation = p
			result = e.PXEEntry
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
func (s *Server) preparePXEFiles(root *os.Root, e PrivatePXEEntry, write bool) (*Preparation, error) {
	if !jobIDRE.MatchString(e.ID) {
		return nil, errors.New("入口编号无效")
	}
	if e.Preparation != nil && e.Preparation.Root != s.config.IVentoyRoot {
		return nil, errors.New("iVentoy 目录发生变化")
	}
	isoDir, scriptDir := pxePaths(e)
	script := "kickstart.cfg"
	if p, _ := pxeProfile(e.Profile); p.Installer == "ubiquity" {
		script = "preseed.cfg"
	} else if e.Request.OS == "ubuntu" {
		script = "user-data"
	} else if e.Request.OS == "debian" {
		script = "preseed.cfg"
	}
	source, err := safeOpen(s.config.ISODir, e.ISO, ".iso")
	if err != nil {
		return nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}
	plain, err := root.Lstat(path.Join("iso", e.ISO))
	if err != nil || !plain.Mode().IsRegular() || !os.SameFile(info, plain) {
		return nil, errors.New("原始镜像必须是普通文件")
	}
	files, err := s.renderPXEFiles(e)
	if err != nil {
		return nil, err
	}
	p := &Preparation{Root: s.config.IVentoyRoot, ISOPath: path.Join(isoDir, "image.iso"), ScriptPath: path.Join(scriptDir, script), SourceSize: info.Size(), SourceModified: info.ModTime(), Files: map[string]string{}}
	for name, content := range files {
		p.Files[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	}
	if old := e.Preparation; old != nil {
		if old.SourceSize != p.SourceSize || !old.SourceModified.Equal(p.SourceModified) || !reflect.DeepEqual(old.Files, p.Files) {
			return nil, errors.New("已准备的菜单内容改变，请停用入口并核对，未覆盖现有文件")
		}
		p.PreparedAt = old.PreparedAt
	}
	if !write {
		return p, nil
	}
	for _, dir := range []string{managedISO, managedScripts, path.Join(managedISO, "PXE-Auto"), path.Join(managedScripts, "pxe-menu"), isoDir, scriptDir} {
		if err = ensureManagedDir(root, dir); err != nil {
			return nil, err
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
	for _, name := range sortedFiles(files) {
		if !fs.ValidPath(name) || strings.Contains(name, "/") {
			return nil, errors.New("无效的安装文件名")
		}
		if old, er := readManagedFile(scripts, name); er == nil {
			if string(old) != files[name] {
				return nil, errors.New("安装文件内容已改变，未覆盖")
			}
			continue
		} else if !errors.Is(er, fs.ErrNotExist) {
			return nil, er
		}
		f, er := scripts.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if er != nil {
			return nil, er
		}
		_, er = f.WriteString(files[name])
		if er == nil {
			er = f.Sync()
		}
		closeErr := f.Close()
		if er != nil {
			return nil, er
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
	if alias, er := root.Lstat(p.ISOPath); er == nil {
		if !alias.Mode().IsRegular() || !os.SameFile(info, alias) {
			return nil, errors.New("镜像入口已存在且来源不同")
		}
	} else if !errors.Is(er, fs.ErrNotExist) {
		return nil, er
	} else if er = root.Link(path.Join("iso", e.ISO), p.ISOPath); er != nil {
		return nil, er
	}
	current, err := root.Stat(path.Join("iso", e.ISO))
	alias, ae := root.Lstat(p.ISOPath)
	if err != nil || ae != nil || !alias.Mode().IsRegular() || !os.SameFile(info, current) || !os.SameFile(info, alias) || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) {
		return nil, errors.New("准备过程中原始镜像发生变化")
	}
	if p.PreparedAt.IsZero() {
		p.PreparedAt = time.Now().UTC()
	}
	return p, nil
}
