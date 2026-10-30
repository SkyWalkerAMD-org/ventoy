package center

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"fmt"
)

//go:embed assets/install-support/*
var installSupportAssets embed.FS

// This capability can only submit bounded diagnostic reports. It cannot read
// reports, change tasks, fetch private software credentials or authorize disks.
func (s *Server) diagnosticToken() string {
	m := hmac.New(sha256.New, []byte(s.admin))
	m.Write([]byte("ventoy-installer-diagnostic-upload-v1"))
	return fmt.Sprintf("%x", m.Sum(nil))
}

func (s *Server) supportSettings() map[string][]byte {
	files := map[string][]byte{
		"support-version":   []byte(Version + "\n"),
		"support-url":       []byte(s.config.PublicURL + "/diagnostics/v1/report\n"),
		"support-ca.crt":    s.ca,
		"support-curl.conf": []byte("header = \"Authorization: Bearer " + s.diagnosticToken() + "\"\n"),
		"support-wget.conf": []byte("header = Authorization: Bearer " + s.diagnosticToken() + "\n"),
		"support-cache-url": []byte("\n"),
	}
	if s.config.InstallerCacheDir != "" && s.config.InstallerCacheURL != "" {
		files["support-cache-url"] = []byte(s.config.InstallerCacheURL + "/installer-cache\n")
	}
	return files
}

func addInstallSupport(files map[string][]byte, prefix string) error {
	entries, err := installSupportAssets.ReadDir("assets/install-support")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		b, err := installSupportAssets.ReadFile("assets/install-support/" + entry.Name())
		if err != nil {
			return err
		}
		files[prefix+entry.Name()] = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	}
	files[prefix+"support-version"] = []byte(Version + "\n")
	return nil
}
