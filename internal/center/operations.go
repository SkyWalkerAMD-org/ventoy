package center

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func publicOperationFile(dir, name string) map[string]any {
	out := map[string]any{}
	f, err := safeOpen(dir, name, ".json")
	if err != nil {
		return out
	}
	defer f.Close()
	if json.NewDecoder(io.LimitReader(f, 256<<10)).Decode(&out) != nil {
		return map[string]any{}
	}
	return out
}

func (s *Server) cacheWarmEvidence() []map[string]any {
	rows := []map[string]any{}
	dir := filepath.Join(s.config.DataDir, "cache-warm")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		source := publicOperationFile(dir, e.Name())
		if len(source) == 0 {
			continue
		}
		row := map[string]any{}
		for _, key := range []string{"origin", "suite", "architecture", "packages", "time", "status", "downloaded_packages", "signatures"} {
			row[key] = source[key]
		}
		rows = append(rows, row)
		if len(rows) == 100 {
			break
		}
	}
	return rows
}

func fileDigest(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	return fmt.Sprintf("%x", h.Sum(nil)), err
}

// Report only public release metadata, never the staging manifest with private
// entry capabilities or config snapshots. Publication itself is an OS admin CLI.
func (s *Server) operations(w http.ResponseWriter, r *http.Request) {
	public := map[string]any{}
	f, err := safeOpen(s.config.DataDir, "release-status.json", ".json")
	if err == nil {
		json.NewDecoder(io.LimitReader(f, 16<<10)).Decode(&public)
		f.Close()
	}
	active := []string{}
	for _, j := range s.store.snapshot().Jobs {
		if !terminal(j.State) {
			active = append(active, j.ID)
		}
	}
	rows := []map[string]any{}
	for _, e := range s.store.snapshot().PXEEntries {
		version := "unknown"
		var hash string
		if e.Preparation != nil {
			version = e.Preparation.Release
			hash = e.Preparation.SetupSHA256
		}
		rows = append(rows, map[string]any{"id": e.ID, "iso": e.ISO, "release": version, "setup_sha256": hash, "current": version == Version})
	}
	respond(w, 200, map[string]any{"version": Version, "release": public, "entries": rows, "active_tasks": active, "network_media_check": "release CLI checks live sockets immediately before activation", "cache": s.config.InstallerCacheDir != ""})
}

func (s *Server) auditRelease(w http.ResponseWriter, r *http.Request) {
	s.preparationMu.Lock()
	defer s.preparationMu.Unlock()
	root, err := s.openIVentoyRoot()
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	defer root.Close()
	bundle, err := s.manualSetupBundle()
	if err != nil {
		fail(w, 409, "完整配置包不可用")
		return
	}
	expected := fmt.Sprintf("%x", sha256.Sum256(manualSetupLauncher(bundle)))
	rows := []map[string]any{}
	for _, e := range s.store.snapshot().PXEEntries {
		problems := []string{}
		p := e.Preparation
		if p == nil {
			problems = append(problems, "入口未准备")
		} else {
			if p.Release != Version || p.SetupSHA256 != expected {
				problems = append(problems, "预置包版本或校验值不是当前版本")
			}
			for name, hash := range p.Files {
				f, er := root.Open(filepath.Join(filepath.Dir(p.ScriptPath), name))
				if er != nil {
					problems = append(problems, "文件不可读: "+name)
					continue
				}
				h := sha256.New()
				_, er = io.Copy(h, f)
				f.Close()
				if er != nil || fmt.Sprintf("%x", h.Sum(nil)) != hash {
					problems = append(problems, "文件校验不匹配: "+name)
				}
			}
		}
		rows = append(rows, map[string]any{"iso": e.ISO, "passed": len(problems) == 0, "problems": problems})
	}
	diskHash, er := fileDigest(filepath.Join(s.config.SoftwareDir, "ventoy-linux-setup.run"))
	readError := ""
	if er != nil {
		readError = "文件缺失或管理服务无权读取"
	}
	respond(w, 200, map[string]any{"version": Version, "setup_sha256": expected, "tools_setup_matches": er == nil && diskHash == expected, "tools_setup_error": readError, "entries": rows})
}

func (s *Server) diskUsage(w http.ResponseWriter, r *http.Request) {
	rows := []map[string]any{}
	for _, x := range [][2]string{{"ISO（含硬链接，目录逻辑大小）", s.config.ISODir}, {"安装依赖缓存", s.config.InstallerCacheDir}, {"软件与驱动包", s.config.PackageDir}, {"tools 软件", s.config.SoftwareDir}, {"管理数据与报告", s.config.DataDir}, {"入口脚本与预置包", filepath.Join(s.config.IVentoyRoot, "user/scripts/ventoy-center")}} {
		if x[1] == "" {
			continue
		}
		var total int64
		count, errors := 0, 0
		filepath.WalkDir(x[1], func(p string, d fs.DirEntry, err error) error {
			if r.Context().Err() != nil {
				return r.Context().Err()
			}
			if err != nil {
				errors++
				return nil
			}
			if d.Type().IsRegular() {
				st, e := d.Info()
				if e != nil {
					errors++
				} else {
					total += st.Size()
					count++
				}
			}
			return nil
		})
		rows = append(rows, map[string]any{"category": x[0], "bytes": total, "files": count, "unreadable": errors})
	}
	respond(w, 200, map[string]any{"directories": rows, "snapshot": publicOperationFile(s.config.DataDir, "storage-status.json"), "note": "目录大小可能包含硬链接。磁盘余量和备份清理候选是注明时间的快照；原始 ISO 不会自动删除。"})
}

func (s *Server) setupRelease(w http.ResponseWriter, r *http.Request) {
	b, err := s.manualSetupBundle()
	if err != nil {
		fail(w, 503, "配置包尚未准备")
		return
	}
	run := manualSetupLauncher(b)
	if strings.HasSuffix(r.URL.Path, "/download") {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-SHA256", fmt.Sprintf("%x", sha256.Sum256(run)))
		w.Write(run)
		return
	}
	respond(w, 200, map[string]any{"version": Version, "sha256": fmt.Sprintf("%x", sha256.Sum256(run)), "bytes": len(run)})
}
