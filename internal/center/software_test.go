package center

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func softwareFixture(t *testing.T) *Server {
	t.Helper()
	s := fixture(t)
	s.config.SoftwareDir = t.TempDir()
	return s
}
func addSoftware(t *testing.T, s *Server, name, data string) {
	t.Helper()
	dest := filepath.Join(s.config.SoftwareDir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestSoftwareIsolationAndSelection(t *testing.T) {
	s := softwareFixture(t)
	addSoftware(t, s, "ubuntu/22.04/amd64/default/app.deb", "deb fixture")
	addSoftware(t, s, "ubuntu/24.04/amd64/default/app.deb", "different release")
	addSoftware(t, s, "centos/7/x86_64/default/app.rpm", "rpm fixture")
	addSoftware(t, s, "unclassified bad.deb", "not published")
	endpoint := "/software/v1/manifest?os=ubuntu&version=22.04&arch=amd64"
	for _, token := range []string{"", s.admin, "incorrect"} {
		mustCode(t, request(s, "GET", endpoint, token, nil), 401)
	}
	mustCode(t, request(s, "GET", "/api/overview", s.softwareToken(), nil), 401)
	mustCode(t, request(s, "GET", "/api/software", s.softwareToken(), nil), 401)
	w := request(s, "GET", endpoint, s.softwareToken(), nil)
	mustCode(t, w, 200)
	var m struct {
		Group SoftwareGroup `json:"group"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Group.ID != "ubuntu/22.04/amd64/default" || len(m.Group.Files) != 1 || len(m.Group.Files[0].SHA256) != 64 {
		t.Fatal(w.Body.String())
	}
	file := m.Group.Files[0]
	url := "/software/v1/package?path=" + file.Path + "&sha256=" + file.SHA256
	download := request(s, "GET", url, s.softwareToken(), nil)
	mustCode(t, download, 200)
	if download.Body.String() != "deb fixture" {
		t.Fatal("wrong bytes")
	}
	addSoftware(t, s, file.Path, "changed contents")
	mustCode(t, request(s, "GET", url, s.softwareToken(), nil), 409)
	missing := request(s, "GET", "/software/v1/manifest?os=ubuntu&version=22.10&arch=amd64", s.softwareToken(), nil)
	mustCode(t, missing, 200)
	if !strings.Contains(missing.Body.String(), `"group":null`) {
		t.Fatal("borrowed another release")
	}
	w = request(s, "GET", "/software/v1/manifest?os=centos&version=7.9&arch=x86_64", s.softwareToken(), nil)
	mustCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `centos/7/x86_64/default`) {
		t.Fatal("missing major fallback")
	}
	for _, path := range []string{"../../secret.deb", "ubuntu/22.04/amd64/default/../../secret.deb", "ubuntu/22.04/amd64/default/secret.rpm"} {
		mustCode(t, request(s, "GET", "/software/v1/package?path="+path+"&sha256="+file.SHA256, s.softwareToken(), nil), 400)
	}
	overview := request(s, "GET", "/api/software", s.admin, nil)
	mustCode(t, overview, 200)
	if !strings.Contains(overview.Body.String(), "unclassified bad.deb") {
		t.Fatal("unclassified file not reported")
	}
	addSoftware(t, s, "ubuntu/22.04/amd64/default/wrong.rpm", "wrong type")
	mustCode(t, request(s, "GET", endpoint, s.softwareToken(), nil), 409)
}

func TestSoftwareClientCredentialScope(t *testing.T) {
	s := softwareFixture(t)
	files := map[string][]byte{}
	s.addSoftwareClient(files)
	var c map[string]any
	if err := json.Unmarshal(files["software-client.json"], &c); err != nil {
		t.Fatal(err)
	}
	if c["token"] == s.admin || c["token"] != s.softwareToken() || c["enabled"] != true {
		t.Fatal("invalid client settings")
	}
	for _, b := range files {
		if strings.Contains(string(b), s.admin) {
			t.Fatal("admin token leaked")
		}
	}
	old := s.softwareToken()
	s.admin = strings.Repeat("b", 64)
	if old == s.softwareToken() {
		t.Fatal("credential did not rotate")
	}
	s.config.SoftwareDir = ""
	s.addSoftwareClient(files)
	if strings.Contains(string(files["software-client.json"]), "token") {
		t.Fatal("disabled client contains credential")
	}
}

func TestSoftwareFlatDefaultAndExplicitCompatibility(t *testing.T) {
	s := softwareFixture(t)
	s.config.SoftwareRPMMinEL = map[string]int{"fyhocsys-node": 8}
	names := []string{"fyhoc_1_amd64.deb", "fyhocsys-node_1_amd64.deb", "sckoct-fyh_1_amd64.deb", "fyhoc-1.x86_64.rpm", "fyhocsys-node-1.el8.x86_64.rpm", "sckoct-fyh-1.el8.x86_64.rpm", "sckoct-fyh-1.el9.x86_64.rpm", "sckoct-fyh-1.el10.x86_64.rpm"}
	for _, name := range names {
		addSoftware(t, s, name, "package "+name)
	}
	for _, tc := range []struct {
		id, version, arch string
		count             int
	}{{"ubuntu", "22.04", "amd64", 3}, {"debian", "12", "amd64", 3}, {"centos", "7", "x86_64", 1}, {"rocky", "8.10", "x86_64", 3}, {"rocky", "9.2", "x86_64", 3}, {"almalinux", "10", "x86_64", 3}, {"ubuntu", "22.04", "arm64", 0}} {
		w := request(s, "GET", "/software/v1/manifest?os="+tc.id+"&version="+tc.version+"&arch="+tc.arch, s.softwareToken(), nil)
		mustCode(t, w, 200)
		var m struct {
			Group *SoftwareGroup `json:"group"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if tc.count == 0 {
			if m.Group != nil {
				t.Fatal("wrong architecture selected")
			}
			continue
		}
		if m.Group == nil || len(m.Group.Files) != tc.count {
			t.Fatal(w.Body.String())
		}
		if tc.id == "rocky" && tc.version == "9.2" {
			if !strings.Contains(w.Body.String(), "fyhocsys-node-1.el8") || strings.Contains(w.Body.String(), "sckoct-fyh-1.el8") {
				t.Fatal("cross-EL rule leaked to a different package")
			}
		}
		file := m.Group.Files[0]
		mustCode(t, request(s, "GET", "/software/v1/package?path="+file.Path+"&sha256="+file.SHA256, s.softwareToken(), nil), 200)
	}
}

func TestSoftwareSymlinkEscape(t *testing.T) {
	s := softwareFixture(t)
	outside := filepath.Join(t.TempDir(), "secret.deb")
	os.WriteFile(outside, []byte("secret"), 0600)
	dir := filepath.Join(s.config.SoftwareDir, "ubuntu/22.04/amd64/default")
	os.MkdirAll(dir, 0700)
	if err := os.Symlink(outside, filepath.Join(dir, "secret.deb")); err != nil {
		t.Skip("symlink unavailable")
	}
	mustCode(t, request(s, "GET", "/software/v1/manifest?os=ubuntu&version=22.04&arch=amd64", s.softwareToken(), nil), 409)
	mustCode(t, request(s, "GET", "/software/v1/package?path=ubuntu/22.04/amd64/default/secret.deb&sha256="+strings.Repeat("0", 64), s.softwareToken(), nil), 404)
}
