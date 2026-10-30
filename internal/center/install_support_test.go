package center

import (
	"encoding/json"
	"errors"
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

type repositoryTransport func(*http.Request) (*http.Response, error)

func TestInstallerCacheConfigAndInstallerRouting(t *testing.T) {
	s := fixture(t)
	valid := s.config
	valid.InstallerCacheDir = t.TempDir()
	valid.InstallerCacheListen = "127.0.0.1:26081"
	valid.InstallerCacheURL = "http://127.0.0.1:26081"
	path := filepath.Join(t.TempDir(), "config.json")
	for _, raw := range []string{valid.InstallerCacheURL, "https://127.0.0.1:26081", "http://127.0.0.2:26081", "http://127.0.0.1:26082", "http://127.0.0.1:26081/path", "http://user@127.0.0.1:26081", "%"} {
		c := valid
		c.InstallerCacheURL = raw
		b, _ := json.Marshal(c)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadConfig(path)
		if (err == nil) != (raw == valid.InstallerCacheURL) {
			t.Fatalf("cache origin %q: %v", raw, err)
		}
	}
	s.config = valid
	j := createFixtureJob(t, s, "ubuntu", false)
	for _, tc := range []struct{ profile, os, file, want string }{
		{"ubuntu-22.04", "ubuntu", "user-data", "/installer-cache/ubuntu"},
		{"ubuntu-24.10", "ubuntu", "user-data", "/installer-cache/ubuntu-archive"},
		{"debian-12", "debian", "preseed.cfg", "/installer-cache/debian"},
		{"debian-10", "debian", "preseed.cfg", "/installer-cache/debian-archive"},
	} {
		j.Profile, j.OS = tc.profile, tc.os
		files, err := s.renderFiles(j)
		if err != nil {
			t.Fatal(tc.profile, err)
		}
		if !strings.Contains(files[tc.file], tc.want) || !strings.Contains(files[tc.file], "127.0.0.1:26081") {
			t.Fatal("missing installer cache route", tc.profile)
		}
		if strings.Contains(files[tc.file], "trusted=yes") || strings.Contains(files[tc.file], "AllowInsecureRepositories \"true\"") {
			t.Fatal("signature validation bypassed", tc.profile)
		}
	}
}

func TestRevalidateManagedPreloadLargerThanTextLimit(t *testing.T) {
	dir := t.TempDir()
	data := strings.Repeat("x", 3<<20)
	for _, name := range []string{"setup.tar.gz", "answer.cfg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	b, err := readManagedFile(root, "setup.tar.gz")
	if err != nil || string(b) != data {
		t.Fatal("real preload could not be verified", err)
	}
	if _, err := readManagedFile(root, "answer.cfg"); err == nil {
		t.Fatal("text size limit removed")
	}
}

func (f repositoryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestInstallerCacheConcurrentMissesWaitAndCoalesce(t *testing.T) {
	s := fixture(t)
	s.config.InstallerCacheDir = t.TempDir()
	var mu sync.Mutex
	counts := map[string]int{}
	s.cacheClient = &http.Client{Transport: repositoryTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		counts[r.URL.Path]++
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 3, Body: io.NopCloser(strings.NewReader("abc"))}, nil
	})}
	var group sync.WaitGroup
	for _, name := range []string{"a", "a", "b", "c", "b", "c"} {
		group.Add(1)
		go func(name string) {
			defer group.Done()
			w := request(s, "GET", "/installer-cache/ubuntu/pool/"+name+".deb", "", nil)
			if w.Code != 200 || w.Body.String() != "abc" {
				t.Errorf("concurrent request failed: %d %s", w.Code, w.Body.String())
			}
		}(name)
	}
	group.Wait()
	if len(counts) != 3 {
		t.Fatal(counts)
	}
	for _, count := range counts {
		if count != 1 {
			t.Fatal("duplicate cache download", counts)
		}
	}
}

func TestInstallerCacheAuthenticBytesOfflineHitsAndBoundaries(t *testing.T) {
	s := fixture(t)
	s.config.InstallerCacheDir = t.TempDir()
	s.config.InstallerCacheMaxBytes = 4096
	count := 0
	s.cacheClient = &http.Client{Transport: repositoryTransport(func(r *http.Request) (*http.Response, error) {
		count++
		if r.URL.Scheme != "https" || r.URL.Host != "archive.ubuntu.com" {
			t.Fatalf("unexpected upstream %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 13, Body: io.NopCloser(strings.NewReader("signed-object"))}, nil
	})}
	path := "/installer-cache/ubuntu/pool/main/g/grub/grub-test.deb"
	mustCode(t, request(s, "GET", path, "", nil), 200)
	s.cacheClient = &http.Client{Transport: repositoryTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	hit := request(s, "GET", path, "", nil)
	mustCode(t, hit, 200)
	if hit.Body.String() != "signed-object" || count != 1 || hit.Header().Get("X-Ventoy-Cache") != "HIT" {
		t.Fatal("cache changed bytes or fetched a cached package")
	}
	for _, p := range []string{"/installer-cache/ubuntu/pool/another.deb", "/installer-cache/ubuntu/dists/jammy/InRelease"} {
		mustCode(t, request(s, "GET", p, "", nil), 502)
	}
	for _, p := range []string{"/installer-cache/arbitrary/pool/a.deb", "/installer-cache/ubuntu/etc/passwd", "/installer-cache/ubuntu/pool/a.deb?url=http://127.0.0.1"} {
		mustCode(t, request(s, "GET", p, "", nil), 400)
	}
	for _, p := range []string{"pool/../secret", "../pool/a", "pool/a%2f..", "pool/a\\b", "pool/a?x"} {
		if _, err := installerUpstream("ubuntu", p); err == nil {
			t.Fatalf("allowed %s", p)
		}
	}
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("Range", "bytes=0-5")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	mustCode(t, w, 206)
	if w.Body.String() != "signed" {
		t.Fatal("range response corrupted")
	}
	mustCode(t, request(s, "GET", "/api/installer-cache", "", nil), 401)
	mustCode(t, request(s, "GET", "/api/installer-cache", s.admin, nil), 200)
}

func TestInstallerCacheRejectsPartialDownloadsAndExternalRedirects(t *testing.T) {
	s := fixture(t)
	s.config.InstallerCacheDir = t.TempDir()
	s.config.InstallerCacheMaxBytes = 100
	s.cacheClient = &http.Client{Transport: repositoryTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 80, Body: io.NopCloser(strings.NewReader("partial"))}, nil
	})}
	mustCode(t, request(s, "GET", "/installer-cache/debian/pool/main/test.deb", "", nil), 502)
	files, _ := os.ReadDir(s.config.InstallerCacheDir)
	if len(files) != 0 {
		t.Fatal("partial download became a cache object")
	}
	for _, raw := range []string{"http://archive.ubuntu.com/ubuntu/a", "https://localhost/a", "https://192.168.1.2/a", "https://archive.ubuntu.com:444/a", "https://user@archive.ubuntu.com/a"} {
		req, _ := http.NewRequest("GET", raw, nil)
		if installerHTTPClient().CheckRedirect(req, nil) == nil {
			t.Fatalf("unsafe redirect allowed %s", raw)
		}
	}
	// Only owned hash-named regular files count toward eviction; never foreign files.
	os.WriteFile(filepath.Join(s.config.InstallerCacheDir, strings.Repeat("z", 64)+".data"), []byte("keep"), 0600)
	items, total := cacheFiles(s.config.InstallerCacheDir)
	if len(items) != 0 || total != 0 {
		t.Fatal("foreign file included in eviction")
	}
}

func TestInstallerCacheMetadataExpiresButPackagesDoNot(t *testing.T) {
	s := fixture(t)
	s.config.InstallerCacheDir = t.TempDir()
	s.cacheClient = &http.Client{Transport: repositoryTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: 3, Body: io.NopCloser(strings.NewReader("abc"))}, nil
	})}
	for _, p := range []string{"dists/jammy/InRelease", "pool/a.deb"} {
		mustCode(t, request(s, "GET", "/installer-cache/ubuntu/"+p, "", nil), 200)
	}
	entries, _ := os.ReadDir(s.config.InstallerCacheDir)
	for _, e := range entries {
		os.Chtimes(filepath.Join(s.config.InstallerCacheDir, e.Name()), time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	}
	s.cacheClient = &http.Client{Transport: repositoryTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	mustCode(t, request(s, "GET", "/installer-cache/ubuntu/dists/jammy/InRelease", "", nil), 502)
	mustCode(t, request(s, "GET", "/installer-cache/ubuntu/pool/a.deb", "", nil), 200)
}

func TestDiagnosticCapabilityRedactionAndObservationDoesNotChangeJob(t *testing.T) {
	s := fixture(t)
	j := createFixtureJob(t, s, "ubuntu", false)
	before, _ := json.Marshal(s.store.snapshot())
	submit := func(token, kind, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/diagnostics/v1/report", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Ventoy-Report-Kind", kind)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	mustCode(t, submit(s.admin, "failure", "error"), 401)
	cap := s.diagnosticToken()
	mustCode(t, request(s, "GET", "/api/overview", cap, nil), 401)
	mustCode(t, submit(cap, "unknown", "error"), 400)
	mustCode(t, submit(cap, "failure", strings.Repeat("x", 65537)), 400)
	body := "Existing LVM members require manual storage preparation\nAuthorization: Bearer SECRET\n" + s.admin + "\n" + cap + "\n" + j.Request.Password + "\nPane is dead\n"
	w := submit(cap, "observed", body)
	mustCode(t, w, 201)
	var receipt map[string]string
	json.Unmarshal(w.Body.Bytes(), &receipt)
	path := "/api/installation-reports/" + receipt["id"]
	mustCode(t, request(s, "GET", path, cap, nil), 401)
	report := request(s, "GET", path, s.admin, nil)
	mustCode(t, report, 200)
	for _, secret := range []string{"SECRET", s.admin, cap, j.Request.Password} {
		if strings.Contains(report.Body.String(), secret) {
			t.Fatal("secret leaked")
		}
	}
	if !strings.Contains(report.Body.String(), "Existing LVM") || receipt["category"] != "storage" {
		t.Fatal("original reason lost")
	}
	after, _ := json.Marshal(s.store.snapshot())
	if string(before) != string(after) {
		t.Fatal("observation changed installation authority or state")
	}
	mustCode(t, request(s, "GET", "/api/installation-reports", s.admin, nil), 200)
	mustCode(t, submit(cap, "result", "result"), 429)
	category, _ := classifyReport("Could not resolve archive.ubuntu.com\nlsblk sda2 LVM2_member")
	if category != "network" {
		t.Fatal("ordinary storage inventory masked real failure")
	}
}
