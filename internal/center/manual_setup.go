package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//go:embed assets/manual
var manualAssets embed.FS

//go:embed assets/manual-launcher.sh
var manualLauncher string

const e610ArchiveName = "E610-MultiOS-6.4.5-r2.tar.gz"
const e610ArchiveSHA = "8a3b65589e4b55afef48f6fd797f8c8b08bdb2ec342c3954adfc45d44b686c94"

func manualSetupFiles(driverArchive []byte) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(manualAssets, "assets/manual", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := manualAssets.ReadFile(name)
		files[strings.TrimPrefix(name, "assets/manual/")] = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
		return err
	})
	if err != nil {
		return nil, err
	}
	policy, err := render("linux-update-policy.sh", renderData{KernelPackages: kernelPackagePatterns()})
	if err != nil {
		return nil, err
	}
	files["linux-update-policy.sh"] = []byte(policy)
	files["safe-server-init.sh"] = []byte(serverInit)
	files["VERSION"] = []byte(Version + "\n")
	files["software-client.json"] = []byte("{\"schema\":1,\"enabled\":false}\n")
	if err := addManualDrivers(files, driverArchive, e610ArchiveSHA); err != nil {
		return nil, err
	}
	return files, nil
}

func addManualDrivers(files map[string][]byte, archive []byte, expected string) error {
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != expected {
		return fmt.Errorf("E610 driver archive checksum mismatch")
	}
	z, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer z.Close()
	t := tar.NewReader(z)
	var total int64
	for {
		h, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name != path.Clean(name) || strings.Contains(name, "\\") || (name != "E610-MultiOS-6.4.5-r2" && !strings.HasPrefix(name, "E610-MultiOS-6.4.5-r2/")) {
			return fmt.Errorf("unexpected E610 archive member")
		}
		if h.Typeflag == tar.TypeDir {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > 64<<20 || total+h.Size > 256<<20 {
			return fmt.Errorf("invalid E610 archive member type or size")
		}
		total += h.Size
		name = "e610/" + strings.TrimPrefix(name, "E610-MultiOS-6.4.5-r2/")
		if _, exists := files[name]; exists {
			return fmt.Errorf("duplicate E610 archive member")
		}
		files[name], err = io.ReadAll(t)
		if err != nil {
			return err
		}
	}
	for _, name := range []string{"install.sh", "targets.tsv", "SHA256SUMS", "source/ixgbe-6.4.5.tar.gz"} {
		if len(files["e610/"+name]) == 0 {
			return fmt.Errorf("missing E610 component: %s", name)
		}
	}
	return nil
}

func packManualSetup(files map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var checks strings.Builder
	for _, name := range names {
		fmt.Fprintf(&checks, "%x  %s\n", sha256.Sum256(files[name]), name)
	}
	files["SHA256SUMS"] = []byte(checks.String())
	names = append(names, "SHA256SUMS")
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	t := tar.NewWriter(z)
	for _, name := range names {
		mode := int64(0644)
		if strings.HasSuffix(name, ".sh") || strings.HasSuffix(name, ".run") {
			mode = 0755
		}
		if err := t.WriteHeader(&tar.Header{Name: "ventoy-manual-setup/" + name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(files[name])), ModTime: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}); err != nil {
			return nil, err
		}
		if _, err := t.Write(files[name]); err != nil {
			return nil, err
		}
	}
	if err := t.Close(); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func manualSetupLauncher(bundle []byte) []byte {
	header := strings.ReplaceAll(manualLauncher, "\r\n", "\n")
	header = strings.ReplaceAll(header, "@ARCHIVE_SHA256@", fmt.Sprintf("%x", sha256.Sum256(bundle)))
	return append([]byte(strings.TrimRight(header, "\n")+"\n"), bundle...)
}

func (s *Server) linuxManualSetup(w http.ResponseWriter, r *http.Request) {
	// Only the release-pinned driver archive and narrowly scoped software client
	// settings enter the bundle. Never include administrator or job credentials.
	file, err := os.Open(filepath.Join(s.config.PackageDir, e610ArchiveName))
	if err != nil {
		http.Error(w, "完整配置包尚未准备好：服务器缺少已校验的 E610 驱动包", http.StatusConflict)
		return
	}
	defer file.Close()
	archive, err := io.ReadAll(io.LimitReader(file, 40<<20))
	if err != nil {
		http.Error(w, "Cannot read E610 driver archive", 500)
		return
	}
	files, err := manualSetupFiles(archive)
	if err != nil {
		http.Error(w, "Manual setup bundle validation failed", 500)
		return
	}
	s.addSoftwareClient(files)
	bundle, err := packManualSetup(files)
	if err != nil {
		http.Error(w, "Cannot build manual setup bundle", 500)
		return
	}
	if r.URL.Query().Get("format") == "injection" {
		bundle, err = manualPreloadInjection(manualSetupLauncher(bundle))
		if err != nil {
			http.Error(w, "Cannot build installer preload hooks", 500)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="ventoy-manual-preload.tar.gz"`)
		if iso := r.URL.Query().Get("iso"); iso != "" {
			driver, er := s.e610Media(iso)
			if er == nil && len(driver) != 0 {
				bundle, er = mergeMediaInjection(bundle, driver)
			}
			if er != nil {
				http.Error(w, "E610 media injection validation failed", 409)
				return
			}
		}
	} else if r.URL.Query().Get("format") == "run" {
		bundle = manualSetupLauncher(bundle)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="ventoy-linux-setup.run"`)
	} else {
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="ventoy-manual-setup.tar.gz"`)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-SHA256", fmt.Sprintf("%x", sha256.Sum256(bundle)))
	w.Write(bundle)
}
