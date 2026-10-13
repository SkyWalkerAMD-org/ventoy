package center

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type Server struct {
	config Config
	store  *Store
	admin  string
	ca     []byte
}
type Media struct {
	Path             string `json:"path"`
	Size             int64  `json:"size"`
	SuggestedProfile string `json:"suggested_profile,omitempty"`
}

func NewServer(c Config) (*Server, error) {
	b, err := os.ReadFile(c.AdminTokenFile)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(string(b))
	if len(token) < 32 {
		return nil, errors.New("admin token must contain at least 32 characters")
	}
	s, err := openStore(c.DataDir)
	if err != nil {
		return nil, err
	}
	server := &Server{config: c, store: s, admin: token}
	if c.BootstrapCA != "" {
		server.ca, err = os.ReadFile(c.BootstrapCA)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(server.ca), "-----BEGIN CERTIFICATE-----") {
			return nil, errors.New("bootstrap CA must be a PEM certificate")
		}
	}
	return server, nil
}

func Serve(path string) error {
	c, err := loadConfig(path)
	if err != nil {
		return err
	}
	s, err := NewServer(c)
	if err != nil {
		return err
	}
	h := &http.Server{Addr: c.Listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		h.Shutdown(shutdown)
	}()
	log.Printf("Ventoy Center %s listening on %s", Version, c.Listen)
	if c.TLSCert != "" {
		err = h.ListenAndServeTLS(c.TLSCert, c.TLSKey)
	} else {
		err = h.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]string{"status": "ok", "version": Version})
	})
	m.HandleFunc("GET /api/overview", s.auth(s.overview))
	m.HandleFunc("POST /api/machines", s.auth(s.createMachine))
	m.HandleFunc("DELETE /api/machines/{id}", s.auth(s.deleteMachine))
	m.HandleFunc("POST /api/jobs", s.auth(s.createJob))
	m.HandleFunc("POST /api/jobs/{id}/arm", s.auth(s.armJob))
	m.HandleFunc("POST /api/jobs/{id}/cancel", s.auth(s.cancelJob))
	m.HandleFunc("POST /api/jobs/{id}/close", s.auth(s.closeJob))
	m.HandleFunc("GET /api/jobs/{id}/bundle", s.auth(s.bundle))
	m.HandleFunc("POST /bootstrap/{id}/event", s.bootEvent)
	m.HandleFunc("GET /bootstrap/{id}/connection", s.connection)
	m.HandleFunc("GET /bootstrap/{id}/package", s.nodePackage)
	f, _ := fs.Sub(webFiles, "web")
	m.Handle("GET /", http.FileServer(http.FS(f)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.config.PublicURL {
			fail(w, 403, "请求来源不匹配")
			return
		}
		m.ServeHTTP(w, r)
	})
}

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "请求 JSON 无效或过大")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		fail(w, 400, "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}
func sameToken(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !sameToken(bearer(r), s.admin) {
			fail(w, 401, "请输入管理口令")
			return
		}
		next(w, r)
	}
}

func media(dir string, extensions ...string) ([]Media, error) {
	items := []Media{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		for _, ext := range extensions {
			if strings.EqualFold(filepath.Ext(path), ext) {
				info, e := d.Info()
				if e != nil {
					return e
				}
				rel, e := filepath.Rel(dir, path)
				if e != nil {
					return e
				}
				items = append(items, Media{Path: filepath.ToSlash(rel), Size: info.Size(), SuggestedProfile: suggestedProfile(rel)})
				break
			}
		}
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, err
}

func safeOpen(dir, name string, extensions ...string) (*os.File, error) {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:\x00") {
		return nil, errors.New("invalid relative file path")
	}
	valid := false
	for _, ext := range extensions {
		if strings.EqualFold(filepath.Ext(name), ext) {
			valid = true
		}
	}
	if !valid {
		return nil, errors.New("unsupported file type")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("not a regular file")
	}
	return f, nil
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	state := s.store.snapshot()
	jobs := make([]Job, 0, len(state.Jobs))
	for i := len(state.Jobs) - 1; i >= 0; i-- {
		jobs = append(jobs, state.Jobs[i].Job)
	}
	isos, ie := media(s.config.ISODir, ".iso")
	packages, pe := media(s.config.PackageDir, ".rpm", ".deb")
	warnings := []string{}
	if ie != nil {
		warnings = append(warnings, "镜像目录无法读取，请检查服务配置和权限")
	}
	if pe != nil {
		warnings = append(warnings, "节点包目录无法读取，请检查服务配置和权限")
	}
	respond(w, 200, map[string]any{"version": Version, "machines": state.Machines, "jobs": jobs, "images": isos, "packages": packages, "warnings": warnings, "profiles": profiles, "iventoy_url": s.config.IVentoyURL, "fyhocsys_url": s.config.FYHOCSYSURL, "public_url": s.config.PublicURL})
}

func (s *Server) createMachine(w http.ResponseWriter, r *http.Request) {
	var m Machine
	if !decode(w, r, &m) {
		return
	}
	if err := validateMachine(&m); err != nil {
		fail(w, 400, err.Error())
		return
	}
	m.ID = randomID(8)
	err := s.store.update(func(state *State) error {
		for _, a := range state.Machines {
			if a.MAC == m.MAC || strings.EqualFold(a.Name, m.Name) || a.Serial == m.Serial {
				return errors.New("节点名、MAC 或资产编号已登记")
			}
		}
		state.Machines = append(state.Machines, m)
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 201, m)
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var req JobRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Enroll {
		fail(w, 400, "当前阶段仅提供系统安装，fyhocsys 接入后续开放")
		return
	}
	p, profileErr := resolveProfile(req.Profile, req.OS)
	if profileErr != nil {
		fail(w, 400, profileErr.Error())
		return
	}
	req.Profile, req.OS = p.ID, p.OS
	if err := validateMediaProfile(p, req.ISO); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if p.OS == "debian" && len(s.ca) == 0 {
		fail(w, 400, "Debian 安装环境需要显式 CA；请先配置 bootstrap_ca_file 后重启服务")
		return
	}
	if err := validateJob(req); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if strings.ContainsAny(req.ISO, " \t\r\n") {
		fail(w, 400, "iVentoy 镜像路径不能包含空格")
		return
	}
	f, err := safeOpen(s.config.ISODir, req.ISO, ".iso")
	if err != nil {
		fail(w, 400, "镜像不存在或路径无效")
		return
	}
	f.Close()
	digest := ""
	if req.Enroll {
		ext := ".rpm"
		if req.OS == "ubuntu" {
			ext = ".deb"
		}
		if !strings.HasPrefix(filepath.Base(req.Package), "fyhocsys-node") {
			fail(w, 400, "请选择 fyhocsys-node 节点包")
			return
		}
		f, err = safeOpen(s.config.PackageDir, req.Package, ext)
		if err != nil {
			fail(w, 400, "节点包格式与目标系统不匹配，或文件不存在")
			return
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			fail(w, 500, "节点包校验失败")
			return
		}
		digest = hex.EncodeToString(h.Sum(nil))
	}
	j := PrivateJob{Job: Job{ID: randomID(12), OS: req.OS, Profile: req.Profile, ISO: req.ISO, State: "draft", Created: time.Now().UTC(), Enroll: req.Enroll, Package: req.Package, PackageSHA256: digest, Events: []Event{}}, Request: req}
	err = s.store.update(func(state *State) error {
		found := false
		for _, m := range state.Machines {
			if m.ID == req.MachineID {
				j.Machine = m
				found = true
				break
			}
		}
		if !found {
			return errors.New("节点不存在")
		}
		if p.ID == "windows-11" && j.Machine.Firmware != "uefi" {
			return errors.New("Windows 11 安装任务需要 UEFI 启动模式")
		}
		for _, old := range state.Jobs {
			if old.Machine.ID == req.MachineID && !terminal(old.State) {
				return errors.New("该节点已有未结束的任务")
			}
		}
		addEvent(&j, "draft", "任务草稿已创建")
		state.Jobs = append(state.Jobs, j)
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 201, j.Job)
}

func (s *Server) deleteMachine(w http.ResponseWriter, r *http.Request) {
	err := s.store.update(func(state *State) error {
		for _, j := range state.Jobs {
			if j.Machine.ID == r.PathValue("id") && !terminal(j.State) {
				return errors.New("请先结束这台机器未完成的安装任务")
			}
		}
		for i, m := range state.Machines {
			if m.ID == r.PathValue("id") {
				state.Machines = append(state.Machines[:i], state.Machines[i+1:]...)
				return nil
			}
		}
		return errors.New("机器不存在")
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) armJob(w http.ResponseWriter, r *http.Request) {
	var confirmation struct {
		Name       string `json:"name"`
		DiskSerial string `json:"disk_serial"`
		Erase      bool   `json:"erase"`
	}
	if !decode(w, r, &confirmation) {
		return
	}
	var result Job
	err := s.store.update(func(state *State) error {
		j := findJob(state, r.PathValue("id"))
		if j == nil {
			return errors.New("任务不存在")
		}
		if j.State != "draft" {
			return errors.New("只能授权草稿任务")
		}
		if !confirmation.Erase || confirmation.Name != j.Machine.Name || confirmation.DiskSerial != j.Machine.DiskSerial {
			return errors.New("请核对节点名和磁盘序列号，并明确同意清空目标盘")
		}
		j.Token = randomID(32)
		j.Expires = time.Now().UTC().Add(24 * time.Hour)
		addEvent(j, "ready", "已授权，有效期 24 小时；等待 iVentoy 启动目标机器")
		result = j.Job
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, result)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	err := s.store.update(func(state *State) error {
		j := findJob(state, r.PathValue("id"))
		if j == nil {
			return errors.New("任务不存在")
		}
		if j.State != "draft" && j.State != "ready" {
			return errors.New("已开始写盘的任务不能通过取消撤回")
		}
		j.Token = ""
		addEvent(j, "canceled", "操作员取消")
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) closeJob(w http.ResponseWriter, r *http.Request) {
	var confirmation struct {
		Name    string `json:"name"`
		Stopped bool   `json:"stopped"`
		Reason  string `json:"reason"`
	}
	if !decode(w, r, &confirmation) {
		return
	}
	if !confirmation.Stopped || len(strings.TrimSpace(confirmation.Reason)) == 0 || len(confirmation.Reason) > 500 {
		fail(w, 400, "请确认客户端已停止安装，并填写结单原因（最多 500 字节）")
		return
	}
	err := s.store.update(func(state *State) error {
		j := findJob(state, r.PathValue("id"))
		if j == nil {
			return errors.New("任务不存在")
		}
		if confirmation.Name != j.Machine.Name {
			return errors.New("节点名不匹配")
		}
		if j.State != "installing" && j.State != "enrolling" {
			return errors.New("只有执行中的任务需要人工结单")
		}
		addEvent(j, "failed", "操作员确认安装已停止并结单："+confirmation.Reason)
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (s *Server) bootstrapJob(w http.ResponseWriter, r *http.Request) *PrivateJob {
	state := s.store.snapshot()
	j := findJob(&state, r.PathValue("id"))
	if j == nil || j.Token == "" || !sameToken(j.Token, bearer(r)) {
		fail(w, 401, "任务凭据无效")
		return nil
	}
	if time.Now().After(j.Expires) || terminal(j.State) || j.State == "draft" {
		fail(w, 410, "任务已结束或已过期")
		return nil
	}
	return j
}
func (s *Server) bootEvent(w http.ResponseWriter, r *http.Request) {
	if s.bootstrapJob(w, r) == nil {
		return
	}
	var e struct {
		State string `json:"state"`
	}
	if !decode(w, r, &e) {
		return
	}
	err := s.store.update(func(state *State) error {
		j := findJob(state, r.PathValue("id"))
		if j == nil || !sameToken(j.Token, bearer(r)) || time.Now().After(j.Expires) {
			return errors.New("任务已失效")
		}
		return transition(j, e.State)
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) connection(w http.ResponseWriter, r *http.Request) {
	j := s.bootstrapJob(w, r)
	if j == nil {
		return
	}
	if !j.Enroll || j.State != "enrolling" {
		fail(w, 409, "任务尚未进入接入阶段")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(j.Request.Connection)
}
func (s *Server) nodePackage(w http.ResponseWriter, r *http.Request) {
	j := s.bootstrapJob(w, r)
	if j == nil {
		return
	}
	if !j.Enroll || j.State != "enrolling" {
		fail(w, 409, "任务尚未进入接入阶段")
		return
	}
	f, err := safeOpen(s.config.PackageDir, j.Package, ".rpm", ".deb")
	if err != nil {
		fail(w, 404, "节点包不可读取")
		return
	}
	defer f.Close()
	info, _ := f.Stat()
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, filepath.Base(j.Package), info.ModTime(), f)
}
func (s *Server) bundle(w http.ResponseWriter, r *http.Request) {
	state := s.store.snapshot()
	j := findJob(&state, r.PathValue("id"))
	if j == nil {
		fail(w, 404, "任务不存在")
		return
	}
	if j.State != "ready" || time.Now().After(j.Expires) {
		fail(w, 409, "请先授权一个有效的待启动任务")
		return
	}
	b, err := s.renderBundle(*j)
	if err != nil {
		fail(w, 500, "安装包生成失败")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=ventoy-%s-%s.zip", j.Machine.Name, j.ID))
	w.Write(b)
}
