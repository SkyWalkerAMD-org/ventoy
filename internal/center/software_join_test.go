package center

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func joinFixture() nodeJoinConfig {
	return nodeJoinConfig{Schema: 1, Nodes: []nodeJoinCredential{{Program: "fyhocsys", URL: "https://node-center.invalid", Pin: strings.Repeat("a", 64), Key: strings.Repeat("ABCD", 8)}}}
}

func TestNodeJoinCredentialIsolationAndRotation(t *testing.T) {
	s := softwareFixture(t)
	endpoint := "/software/v1/node-join"
	for _, token := range []string{"", "wrong", s.admin} {
		mustCode(t, request(s, "GET", endpoint, token, nil), 401)
	}
	w := request(s, "GET", endpoint, s.softwareToken(), nil)
	mustCode(t, w, 200)
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal("join must be explicitly configured")
	}
	s.config.NodeJoinConfigFile = filepath.Join(t.TempDir(), "node-join.json")
	config := joinFixture()
	for _, key := range []string{strings.Repeat("ABCD", 8), strings.Repeat("WXYZ", 8)} {
		config.Nodes[0].Key = key
		b, _ := json.Marshal(config)
		if err := os.WriteFile(s.config.NodeJoinConfigFile, b, 0600); err != nil {
			t.Fatal(err)
		}
		w = request(s, "GET", endpoint, s.softwareToken(), nil)
		mustCode(t, w, 200)
		if !strings.Contains(w.Body.String(), key) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("rotation or cache control failed")
		}
		for _, path := range []string{"/api/overview", "/api/software", "/software/v1/manifest?os=ubuntu&version=22.04&arch=amd64"} {
			token := s.admin
			if strings.HasPrefix(path, "/software/") {
				token = s.softwareToken()
			}
			got := request(s, "GET", path, token, nil)
			mustCode(t, got, 200)
			if strings.Contains(got.Body.String(), key) {
				t.Fatal("join key disclosed by a public API")
			}
		}
		files := map[string][]byte{}
		s.addSoftwareClient(files)
		if !strings.Contains(string(files["software-client.json"]), `"node_join": true`) {
			t.Fatal("new client does not discover join support")
		}
		for _, data := range files {
			if strings.Contains(string(data), key) || strings.Contains(string(data), s.config.NodeJoinConfigFile) {
				t.Fatal("join secret or private path entered setup bundle")
			}
		}
	}
	if runtime.GOOS != "windows" {
		os.Chmod(s.config.NodeJoinConfigFile, 0644)
		mustCode(t, request(s, "GET", endpoint, s.softwareToken(), nil), 503)
		os.Chmod(s.config.NodeJoinConfigFile, 0600)
		link := filepath.Join(t.TempDir(), "link.json")
		if err := os.Symlink(s.config.NodeJoinConfigFile, link); err != nil {
			t.Fatal(err)
		}
		s.config.NodeJoinConfigFile = link
		mustCode(t, request(s, "GET", endpoint, s.softwareToken(), nil), 503)
	}
}

func TestNodeJoinConfigValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "join.json")
	for _, mutation := range []func(*nodeJoinConfig){
		func(c *nodeJoinConfig) { c.Schema = 2 },
		func(c *nodeJoinConfig) { c.Nodes = nil },
		func(c *nodeJoinConfig) { c.Nodes = append(c.Nodes, c.Nodes[0]) },
		func(c *nodeJoinConfig) { c.Nodes[0].Program = "shell" },
		func(c *nodeJoinConfig) { c.Nodes[0].URL = "http://center.invalid" },
		func(c *nodeJoinConfig) { c.Nodes[0].URL = "https://user:secret@center.invalid" },
		func(c *nodeJoinConfig) { c.Nodes[0].URL = "https://center.invalid/join" },
		func(c *nodeJoinConfig) { c.Nodes[0].URL = "https://center.invalid?" },
		func(c *nodeJoinConfig) { c.Nodes[0].URL = "https://center.invalid#fragment" },
		func(c *nodeJoinConfig) { c.Nodes[0].Pin = "invalid" },
		func(c *nodeJoinConfig) { c.Nodes[0].Key = strings.Repeat("0", 32) },
		func(c *nodeJoinConfig) { c.Nodes[0].Key = "short" },
	} {
		config := joinFixture()
		mutation(&config)
		data, _ := json.Marshal(config)
		os.WriteFile(path, data, 0600)
		if _, err := readNodeJoinConfig(path); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	for _, data := range []string{`{"schema":1,"unknown":"test-secret"}`, strings.Repeat("x", 16385), `{"schema":1} {}`} {
		os.WriteFile(path, []byte(data), 0600)
		if _, err := readNodeJoinConfig(path); err == nil || strings.Contains(err.Error(), "test-secret") {
			t.Fatal("invalid configuration or parser content disclosure")
		}
	}
	config := joinFixture()
	config.Nodes = append(config.Nodes, config.Nodes[0])
	config.Nodes[1].Program = "sckocstp"
	config.Nodes[1].Key = strings.TrimSuffix(strings.Repeat("abcd-", 8), "-")
	config.Nodes[1].Pin = "sha256:" + strings.Repeat("B", 64)
	data, _ := json.Marshal(config)
	os.WriteFile(path, data, 0600)
	got, err := readNodeJoinConfig(path)
	if err != nil || got.Nodes[1].Key != strings.Repeat("ABCD", 8) || got.Nodes[1].Pin != strings.Repeat("b", 64) {
		t.Fatal("normalization or dual program configuration failed")
	}
}

func TestNodeJoinConfigPath(t *testing.T) {
	dir := t.TempDir()
	if err := Initialize(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	data, _ := os.ReadFile(path)
	var config Config
	json.Unmarshal(data, &config)
	config.NodeJoinConfigFile = "join.json"
	write := func() {
		data, _ := json.Marshal(config)
		os.WriteFile(path, data, 0600)
	}
	write()
	if _, err := loadConfig(path); err == nil {
		t.Fatal("configured join without authenticated software library")
	}
	config.SoftwareDir = filepath.Join(dir, "tools")
	write()
	got, err := loadConfig(path)
	if err != nil || got.NodeJoinConfigFile != filepath.Join(dir, "join.json") {
		t.Fatal("relative private configuration path failed", err)
	}
	config.NodeJoinConfigFile = filepath.Join(config.SoftwareDir, "join.json")
	write()
	if _, err := loadConfig(path); err == nil {
		t.Fatal("credential under distributed tools accepted")
	}
	if runtime.GOOS != "windows" {
		os.MkdirAll(config.SoftwareDir, 0700)
		os.WriteFile(config.NodeJoinConfigFile, []byte("fixture"), 0600)
		alias := filepath.Join(dir, "alias")
		if err := os.Symlink(config.SoftwareDir, alias); err != nil {
			t.Fatal(err)
		}
		if nodeJoinFileOutsideSoftware(filepath.Join(alias, "join.json"), config.SoftwareDir) {
			t.Fatal("directory alias bypassed private-file isolation")
		}
	}
}
