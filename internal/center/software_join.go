package center

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Node enrollment keys are separate from FYHOC's boot unlock password. They
// never enter manifests, setup bundles, installer media or public overview APIs.
type nodeJoinCredential struct {
	Program string `json:"program"`
	URL     string `json:"url"`
	Pin     string `json:"pin"`
	Key     string `json:"key"`
}

type nodeJoinConfig struct {
	Schema int                  `json:"schema"`
	Nodes  []nodeJoinCredential `json:"nodes"`
}

var nodeJoinKeyRE = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{32}$`)
var nodeJoinPinRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func nodeJoinFileOutsideSoftware(name, directory string) bool {
	outside := func(name, directory string) bool {
		rel, err := filepath.Rel(directory, name)
		return err == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel))
	}
	if !outside(name, directory) {
		return false
	}
	// Check existing directory aliases as well as the configured paths. Missing
	// credentials can be supplied after startup; requests fail until available.
	resolvedName, nameErr := filepath.EvalSymlinks(name)
	resolvedDir, dirErr := filepath.EvalSymlinks(directory)
	return nameErr != nil || dirErr != nil || outside(resolvedName, resolvedDir)
}

func readNodeJoinConfig(name string) (nodeJoinConfig, error) {
	var config nodeJoinConfig
	invalid := errors.New("node join configuration is unavailable, invalid or not private")
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		return config, invalid
	}
	f, err := os.Open(name)
	if err != nil {
		return config, invalid
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || (runtime.GOOS != "windows" && opened.Mode().Perm() != 0600) {
		return config, invalid
	}
	b, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(b) > 16384 {
		return config, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil || decoder.Decode(new(any)) != io.EOF || config.Schema != 1 || len(config.Nodes) < 1 || len(config.Nodes) > 2 {
		return config, invalid
	}
	seen := map[string]bool{}
	for i := range config.Nodes {
		node := &config.Nodes[i]
		if (node.Program != "sckocstp" && node.Program != "fyhocsys") || seen[node.Program] {
			return config, invalid
		}
		seen[node.Program] = true
		node.URL = strings.TrimRight(node.URL, "/")
		u, err := url.Parse(node.URL)
		if err != nil || len(node.URL) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(node.URL, "?# \t\r\n") {
			return config, invalid
		}
		node.Pin = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(node.Pin)), "sha256:")
		node.Key = strings.NewReplacer("-", "", " ", "").Replace(strings.ToUpper(strings.TrimSpace(node.Key)))
		if !nodeJoinPinRE.MatchString(node.Pin) || !nodeJoinKeyRE.MatchString(node.Key) {
			return config, invalid
		}
	}
	return config, nil
}

func (s *Server) softwareNodeJoin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s.config.NodeJoinConfigFile == "" {
		respond(w, 200, map[string]any{"enabled": false})
		return
	}
	if !nodeJoinFileOutsideSoftware(s.config.NodeJoinConfigFile, s.config.SoftwareDir) {
		fail(w, 503, "Node join configuration must be outside software_dir")
		return
	}
	config, err := readNodeJoinConfig(s.config.NodeJoinConfigFile)
	if err != nil {
		// Never return parser input, file contents or secret values on errors.
		fail(w, 503, "Node join configuration is unavailable, invalid or not private")
		return
	}
	respond(w, 200, map[string]any{"enabled": true, "schema": config.Schema, "nodes": config.Nodes})
}
