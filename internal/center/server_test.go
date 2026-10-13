package center

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	if err := Initialize(dir); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(c.ISODir, "test.iso"), filepath.Join(c.PackageDir, "fyhocsys-node-test.rpm"), filepath.Join(c.PackageDir, "fyhocsys-node-test.deb")} {
		if err = os.WriteFile(path, []byte("test fixture, not a deployable image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := NewServer(c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func request(s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func mustCode(t *testing.T, w *httptest.ResponseRecorder, code int) {
	t.Helper()
	if w.Code != code {
		t.Fatalf("got %d, want %d: %s", w.Code, code, w.Body.String())
	}
}
func createFixtureJob(t *testing.T, s *Server, kind string, enroll bool) PrivateJob {
	t.Helper()
	w := request(s, "POST", "/api/machines", s.admin, Machine{Name: "LAB-001", MAC: "02:00:00:00:01:01", Serial: "ASSET-DEMO-001", DiskSerial: "DISK-DEMO-001", Firmware: "uefi"})
	mustCode(t, w, 201)
	var m Machine
	json.Unmarshal(w.Body.Bytes(), &m)
	req := JobRequest{MachineID: m.ID, OS: kind, ISO: "test.iso", Username: "deployadmin", Password: "A-long<&password123", PasswordHash: "$6$example$" + strings.Repeat("a", 86), WindowsImage: 1, Locale: "zh-CN", Enroll: enroll}
	if enroll {
		req.Package = "fyhocsys-node-test.rpm"
		if kind == "ubuntu" {
			req.Package = "fyhocsys-node-test.deb"
		}
		req.Connection = json.RawMessage(`{"node":"LAB-001","token":"PRIVATE-FYH-SECRET"}`)
	}
	createReq := req
	createReq.Enroll = false
	w = request(s, "POST", "/api/jobs", s.admin, createReq)
	mustCode(t, w, 201)
	var j Job
	json.Unmarshal(w.Body.Bytes(), &j)
	if enroll {
		// Preserve coverage of existing alpha.1 tasks. New tasks cannot enroll.
		if err := s.store.update(func(st *State) error {
			old := findJob(st, j.ID)
			old.Enroll, old.Request = true, req
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	state := s.store.snapshot()
	return *findJob(&state, j.ID)
}
func arm(t *testing.T, s *Server, j PrivateJob) PrivateJob {
	t.Helper()
	w := request(s, "POST", "/api/jobs/"+j.ID+"/arm", s.admin, map[string]any{"name": j.Machine.Name, "disk_serial": j.Machine.DiskSerial, "erase": true})
	mustCode(t, w, 200)
	state := s.store.snapshot()
	return *findJob(&state, j.ID)
}

func TestAuthorizationAndSecretIsolation(t *testing.T) {
	s := fixture(t)
	j := createFixtureJob(t, s, "rocky", true)
	mustCode(t, request(s, "GET", "/api/overview", "", nil), 401)
	mustCode(t, request(s, "GET", "/api/jobs/"+j.ID+"/bundle", s.admin, nil), 409)
	mustCode(t, request(s, "POST", "/api/jobs/"+j.ID+"/arm", s.admin, map[string]any{"name": j.Machine.Name, "disk_serial": "WRONG", "erase": true}), 409)
	j = arm(t, s, j)
	overview := request(s, "GET", "/api/overview", s.admin, nil)
	for _, secret := range []string{j.Token, "PRIVATE-FYH-SECRET", j.Request.PasswordHash, j.Request.Password} {
		if strings.Contains(overview.Body.String(), secret) {
			t.Fatal("secret exposed in public job view")
		}
	}
	mustCode(t, request(s, "GET", "/bootstrap/"+j.ID+"/connection", j.Token, nil), 409)
	r := httptest.NewRequest("GET", "/api/overview", nil)
	r.Header.Set("Authorization", "Bearer "+s.admin)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	mustCode(t, w, 403)
}

func TestSingleUseClaimSurvivesRestart(t *testing.T) {
	s := fixture(t)
	j := arm(t, s, createFixtureJob(t, s, "rocky", true))
	var wg sync.WaitGroup
	codes := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installing"}).Code
		}()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatalf("unexpected code %d", code)
		}
	}
	if success != 1 {
		t.Fatalf("claims accepted: %d", success)
	}
	newServer, err := NewServer(s.config)
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, request(newServer, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installing"}), 409)
	mustCode(t, request(s, "POST", "/api/jobs/"+j.ID+"/cancel", s.admin, nil), 409)
	mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "enrolled"}), 409)
	mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "enrolling"}), 200)
	w := request(s, "GET", "/bootstrap/"+j.ID+"/connection", j.Token, nil)
	mustCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "PRIVATE-FYH-SECRET") {
		t.Fatal("missing private connection")
	}
	mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "enrolled"}), 200)
	mustCode(t, request(s, "GET", "/bootstrap/"+j.ID+"/connection", j.Token, nil), 401)
	state := s.store.snapshot()
	done := findJob(&state, j.ID)
	if done.Token != "" || len(done.Request.Connection) != 0 || done.Request.Password != "" || done.Request.PasswordHash != "" {
		t.Fatal("terminal task retained provisioning secrets")
	}
}

func TestManualRecoveryRequiresStoppedConfirmation(t *testing.T) {
	s := fixture(t)
	j := arm(t, s, createFixtureJob(t, s, "rocky", false))
	mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installing"}), 200)
	path := "/api/jobs/" + j.ID + "/close"
	mustCode(t, request(s, "POST", path, s.admin, map[string]any{"name": j.Machine.Name, "stopped": false, "reason": "test interruption"}), 400)
	mustCode(t, request(s, "POST", path, s.admin, map[string]any{"name": "WRONG", "stopped": true, "reason": "test interruption"}), 409)
	mustCode(t, request(s, "POST", path, s.admin, map[string]any{"name": j.Machine.Name, "stopped": true, "reason": "test interruption"}), 200)
	mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installed"}), 401)
	mustCode(t, request(s, "DELETE", "/api/machines/"+j.Machine.ID, s.admin, nil), 200)
}

func TestCancellationAndExpiryPreventClaims(t *testing.T) {
	for _, mode := range []string{"cancel", "expire"} {
		t.Run(mode, func(t *testing.T) {
			s := fixture(t)
			j := arm(t, s, createFixtureJob(t, s, "rocky", false))
			if mode == "cancel" {
				mustCode(t, request(s, "POST", "/api/jobs/"+j.ID+"/cancel", s.admin, nil), 200)
			} else {
				s.store.update(func(st *State) error { findJob(st, j.ID).Expires = time.Now().Add(-time.Hour); return nil })
			}
			w := request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installing"})
			if w.Code == 200 {
				t.Fatal("invalid task accepted")
			}
		})
	}
}

func TestBundlesAndWindowsGuard(t *testing.T) {
	for _, kind := range []string{"rocky", "ubuntu", "windows"} {
		t.Run(kind, func(t *testing.T) {
			s := fixture(t)
			j := arm(t, s, createFixtureJob(t, s, kind, kind != "windows"))
			w := request(s, "GET", "/api/jobs/"+j.ID+"/bundle", s.admin, nil)
			mustCode(t, w, 200)
			z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
			if err != nil {
				t.Fatal(err)
			}
			files := map[string]string{}
			for _, f := range z.File {
				r, _ := f.Open()
				b, _ := io.ReadAll(r)
				r.Close()
				files[f.Name] = string(b)
				if strings.Contains(string(b), "PRIVATE-FYH-SECRET") {
					t.Fatal("connection secret embedded in boot bundle")
				}
			}
			if files["SHA256SUMS"] == "" || files["manifest.json"] == "" {
				t.Fatal("incomplete bundle")
			}
			if kind == "windows" {
				d := xml.NewDecoder(strings.NewReader(files["autounattend.xml"]))
				for {
					_, err = d.Token()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				guard := files["injection/ventoy-center/guard.ps1"]
				if strings.Index(guard, "SerialNumber") > strings.Index(guard, "clean") || strings.Index(guard, "Invoke-RestMethod") > strings.Index(guard, "clean") {
					t.Fatal("guard runs after erasure")
				}
				if strings.Contains(files["autounattend.xml"], "WillWipeDisk") {
					t.Fatal("XML bypasses guarded partitioning")
				}
			}
			if output := os.Getenv("VENTOY_TEST_OUTPUT"); output != "" {
				for name, content := range files {
					path := filepath.Join(output, kind, filepath.FromSlash(name))
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestInputAndFileBoundaries(t *testing.T) {
	s := fixture(t)
	for _, path := range []string{"../admin.token", "/test.iso", "C:/test.iso", "x\\test.iso", "test.iso/../../admin.token"} {
		f, err := safeOpen(s.config.ISODir, path, ".iso")
		if err == nil {
			f.Close()
			t.Fatal("unsafe path accepted:", path)
		}
	}
	m := Machine{Name: "LAB-001", MAC: "ff:ff:ff:ff:ff:ff", Serial: "ASSET", DiskSerial: "disk"}
	if validateMachine(&m) == nil {
		t.Fatal("multicast MAC accepted")
	}
	m.MAC = "02:00:00:00:00:01"
	m.DiskSerial = "disk;touch /tmp/pwn"
	if validateMachine(&m) == nil {
		t.Fatal("shell input accepted")
	}
	mustCode(t, request(s, "POST", "/api/jobs", s.admin, JobRequest{OS: "windows", Username: "operator", Password: "LongValidPassword123", Locale: "en-US", WindowsImage: 1, Enroll: true}), 400)
	r := httptest.NewRequest("POST", "/api/machines", strings.NewReader(`{} {}`))
	r.Header.Set("Authorization", "Bearer "+s.admin)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	mustCode(t, w, 400)
}

func TestLocalOnlyHTTPAndInitialization(t *testing.T) {
	dir := t.TempDir()
	if err := Initialize(dir); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(dir); err == nil {
		t.Fatal("initialization overwrote configuration")
	}
	path := filepath.Join(dir, "config.json")
	b, _ := os.ReadFile(path)
	var c Config
	json.Unmarshal(b, &c)
	c.Listen = "0.0.0.0:26080"
	b, _ = json.Marshal(c)
	os.WriteFile(path, b, 0600)
	if _, err := loadConfig(path); err == nil {
		t.Fatal("public plain HTTP accepted")
	}
}

func TestStaticPagesAndHeaders(t *testing.T) {
	s := fixture(t)
	for _, path := range []string{"/", "/app.js", "/style.css", "/healthz"} {
		w := request(s, http.MethodGet, path, "", nil)
		mustCode(t, w, 200)
		if w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing CSP")
		}
	}
}
