package center

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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

func TestStagingPreservesCacheURLWithoutProductionListener(t *testing.T) {
	c := Config{Listen: "127.0.0.1:26190", InstallerCacheListen: "192.168.1.2:26081", InstallerCacheURL: "http://192.168.1.2:26081"}
	staged, err := stagingConfig(c)
	if err != nil || staged.InstallerCacheListen != "" || staged.InstallerCacheURL != c.InstallerCacheURL {
		t.Fatalf("staging cache configuration: %#v %v", staged, err)
	}
	for _, listen := range []string{"0.0.0.0:26190", "192.168.1.2:26190", "localhost:26190", "bad"} {
		c.Listen = listen
		if _, err := stagingConfig(c); err == nil {
			t.Fatalf("staging accepted non-loopback bind %s", listen)
		}
	}
}

func TestCacheConcurrencyBoundAndCanceledWaiter(t *testing.T) {
	s := fixture(t)
	s.config.InstallerCacheDir = t.TempDir()
	s.config.InstallerCacheConcurrency = 2
	started := make(chan string, 8)
	gate := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(gate) })
	s.cacheClient = &http.Client{Transport: repositoryTransport(func(r *http.Request) (*http.Response, error) {
		started <- r.URL.Path
		select {
		case <-gate:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 3, Body: io.NopCloser(strings.NewReader("abc"))}, nil
	})}
	var group sync.WaitGroup
	for _, name := range []string{"a", "b"} {
		group.Add(1)
		go func(name string) {
			defer group.Done()
			w := request(s, "GET", "/installer-cache/ubuntu/pool/"+name, "", nil)
			if w.Code != 200 {
				t.Errorf("download failed: %s", w.Body.String())
			}
		}(name)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("different files did not start concurrently")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest("GET", "/installer-cache/ubuntu/pool/c", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { s.Handler().ServeHTTP(httptest.NewRecorder(), req); close(done) }()
	select {
	case <-started:
		t.Fatal("exceeded concurrency limit")
	case <-time.After(150 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waiter ignored cancellation")
	}
	once.Do(func() { close(gate) })
	group.Wait()
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if len(s.cacheFlights) != 0 || s.cacheReserved != 0 || s.cacheDownloads != 2 {
		t.Fatalf("flight/reservation leak: %v %d %d", s.cacheFlights, s.cacheReserved, s.cacheDownloads)
	}
}

type heldCacheBody struct {
	gate   <-chan struct{}
	reader io.Reader
}

func (b heldCacheBody) Read(p []byte) (int, error) { <-b.gate; return b.reader.Read(p) }
func (b heldCacheBody) Close() error               { return nil }

func TestCacheReservationsBoundTemporaryBytes(t *testing.T) {
	s := fixture(t)
	s.config.InstallerCacheDir = t.TempDir()
	s.config.InstallerCacheMaxBytes = 100
	gate := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(gate) })
	s.cacheClient = &http.Client{Transport: repositoryTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 80, Body: heldCacheBody{gate, strings.NewReader(strings.Repeat("x", 80))}}, nil
	})}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(s, "GET", "/installer-cache/ubuntu/pool/a", "", nil) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.cacheMu.Lock()
		n := s.cacheReserved
		s.cacheMu.Unlock()
		if n == 80 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no reservation")
		}
		time.Sleep(time.Millisecond)
	}
	w := request(s, "GET", "/installer-cache/ubuntu/pool/b", "", nil)
	mustCode(t, w, 503)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry hint")
	}
	once.Do(func() { close(gate) })
	mustCode(t, <-done, 200)
	_, size := cacheFiles(s.config.InstallerCacheDir)
	if size > 100 {
		t.Fatal("cache oversubscribed", size)
	}
}

func TestSetupReleaseCapabilityAndAudit(t *testing.T) {
	s, e := pxeFixture(t, "ubuntu-22.04")
	s.config.SoftwareDir = t.TempDir()
	s.manualBundle, _ = packManualSetup(map[string][]byte{"VERSION": []byte(Version), "install.sh": []byte("echo fixture")})
	run := manualSetupLauncher(s.manualBundle)
	expected := fmt.Sprintf("%x", sha256.Sum256(run))
	mustCode(t, request(s, "GET", "/software/v1/setup-release", "", nil), 401)
	mustCode(t, request(s, "GET", "/software/v1/setup-release", s.admin, nil), 401)
	mustCode(t, request(s, "GET", "/api/operations", s.softwareToken(), nil), 401)
	w := request(s, "GET", "/software/v1/setup-release", s.softwareToken(), nil)
	mustCode(t, w, 200)
	if !strings.Contains(w.Body.String(), expected) {
		t.Fatal("manifest differs from runner")
	}
	w = request(s, "GET", "/software/v1/setup-release/download", s.softwareToken(), nil)
	mustCode(t, w, 200)
	if string(run) != w.Body.String() {
		t.Fatal("download differs")
	}
	os.WriteFile(filepath.Join(s.config.SoftwareDir, "ventoy-linux-setup.run"), run, 0700)
	s.store.update(func(st *State) error {
		p := findPXEEntry(st, e.ID).Preparation
		p.Release = Version
		p.SetupSHA256 = expected
		return nil
	})
	audit := func() map[string]any {
		w := request(s, "POST", "/api/operations/audit", s.admin, nil)
		mustCode(t, w, 200)
		var a map[string]any
		json.Unmarshal(w.Body.Bytes(), &a)
		return a
	}
	a := audit()
	if a["tools_setup_matches"] != true || a["entries"].([]any)[0].(map[string]any)["passed"] != true {
		t.Fatal(a)
	}
	os.WriteFile(filepath.Join(s.config.IVentoyRoot, e.Preparation.ScriptPath), []byte("tampered"), 0600)
	if audit()["entries"].([]any)[0].(map[string]any)["passed"] != false {
		t.Fatal("tampered entry accepted")
	}
}

func TestAcceptanceRequiresExplicitScopeAndAllChecks(t *testing.T) {
	s, e := pxeFixture(t, "rocky-9")
	a := AcceptanceRecord{EntryID: e.ID, Release: Version, Mode: "automatic", Platform: "AMD", Storage: "single", Node: "test-node", Result: "passed", Checks: map[string]bool{}}
	mustCode(t, request(s, "POST", "/api/acceptance", "", a), 401)
	mustCode(t, request(s, "POST", "/api/acceptance", s.admin, a), 400)
	for _, k := range acceptanceChecks {
		a.Checks[k] = true
	}
	a.Release = ""
	mustCode(t, request(s, "POST", "/api/acceptance", s.admin, a), 400)
	a.Release = Version
	a.Reports = []string{strings.Repeat("a", 32)}
	mustCode(t, request(s, "POST", "/api/acceptance", s.admin, a), 400)
	a.Reports = nil
	mustCode(t, request(s, "POST", "/api/acceptance", s.admin, a), 201)
	records := s.acceptanceRecords()
	if len(records) != 1 || records[0].Basis != "operator-attested" || records[0].ISO != e.ISO {
		t.Fatal(records)
	}
	before := len(s.acceptanceRecords())
	req := httptest.NewRequest("POST", "/diagnostics/v1/report", strings.NewReader("VC_META release="+Version+"\nVC_META cpu_vendor=AuthenticAMD\nVC_META password=ignored\nVERIFIED"))
	req.Header.Set("Authorization", "Bearer "+s.diagnosticToken())
	req.Header.Set("X-Ventoy-Report-Kind", "result")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code >= 300 {
		t.Fatal(w.Body.String())
	}
	if len(s.acceptanceRecords()) != before {
		t.Fatal("observation silently became hardware acceptance")
	}
	meta := reportMetadata("VC_META release=" + Version + "\nVC_META password=ignored\nVC_META cpu_vendor=AuthenticAMD")
	if len(meta) != 2 || meta["cpu_vendor"] != "AuthenticAMD" {
		t.Fatal(meta)
	}
}
