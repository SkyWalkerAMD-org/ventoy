package center

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const Version = "0.2.0-alpha.1"

type Config struct {
	Listen         string `json:"listen"`
	PublicURL      string `json:"public_url"`
	DataDir        string `json:"data_dir"`
	ISODir         string `json:"iso_dir"`
	PackageDir     string `json:"package_dir"`
	AdminTokenFile string `json:"admin_token_file"`
	TLSCert        string `json:"tls_cert_file"`
	TLSKey         string `json:"tls_key_file"`
	BootstrapCA    string `json:"bootstrap_ca_file"`
	IVentoyURL     string `json:"iventoy_url"`
	FYHOCSYSURL    string `json:"fyhocsys_url"`
}

func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func Initialize(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return err
	}
	path := filepath.Join(abs, "config.json")
	if _, err = os.Stat(path); err == nil {
		return errors.New("configuration already exists; retained")
	}
	tokenPath := filepath.Join(abs, "admin.token")
	if err = writeNew(tokenPath, []byte(randomID(32)+"\n")); err != nil {
		return err
	}
	c := Config{Listen: "127.0.0.1:26080", PublicURL: "http://127.0.0.1:26080", DataDir: filepath.Join(abs, "data"), ISODir: filepath.Join(abs, "iso"), PackageDir: filepath.Join(abs, "packages"), AdminTokenFile: tokenPath, IVentoyURL: "http://127.0.0.1:26000"}
	for _, p := range []string{c.DataDir, c.ISODir, c.PackageDir} {
		if err = os.MkdirAll(p, 0700); err != nil {
			return err
		}
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return writeNew(path, append(b, '\n'))
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	e := f.Close()
	if err != nil {
		return err
	}
	return e
}

func loadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	base, _ := filepath.Abs(filepath.Dir(path))
	for _, p := range []*string{&c.DataDir, &c.ISODir, &c.PackageDir, &c.AdminTokenFile, &c.TLSCert, &c.TLSKey, &c.BootstrapCA} {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Join(base, *p)
		}
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return c, errors.New("public_url must be an HTTP(S) origin without a path or credentials")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return c, fmt.Errorf("listen: %w", err)
	}
	loopback := func(h string) bool { ip := net.ParseIP(h); return ip != nil && ip.IsLoopback() }
	if u.Scheme != "https" && !(loopback(host) && loopback(u.Hostname())) {
		return c, errors.New("LAN deployment requires HTTPS; HTTP is only allowed on loopback")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return c, errors.New("both TLS certificate and key are required")
	}
	if !loopback(host) && c.TLSCert == "" {
		return c, errors.New("non-loopback listener requires a TLS certificate and key")
	}
	if c.DataDir == "" || c.ISODir == "" || c.PackageDir == "" || c.AdminTokenFile == "" {
		return c, errors.New("data, ISO, package and admin-token paths are required")
	}
	for _, v := range []string{c.IVentoyURL, c.FYHOCSYSURL} {
		if v == "" {
			continue
		}
		p, e := url.Parse(v)
		if e != nil || p.Host == "" || p.User != nil || (p.Scheme != "http" && p.Scheme != "https") {
			return c, errors.New("invalid service URL")
		}
	}
	return c, nil
}
