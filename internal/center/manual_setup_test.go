package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise the actual launcher and installer argument parsers together, stopping
// before either can extract files or change the test host's configuration.
func TestManualSetupDefaultAction(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /bin/bash")
	}
	launcher := strings.ReplaceAll(manualLauncher, "\r\n", "\n")
	installerBytes, err := manualAssets.ReadFile("assets/manual/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	installer := strings.ReplaceAll(string(installerBytes), "\r\n", "\n")
	cut := func(source string) string {
		i := strings.Index(source, "\n[[ $EUID == 0")
		if i < 0 {
			t.Fatal("missing privilege boundary")
		}
		return source[:i] + "\n"
	}
	root := t.TempDir()
	installPath := filepath.Join(root, "install.sh")
	if err := os.WriteFile(installPath, []byte(cut(installer)+`printf '%s|%s|%s|%s\n' "$mode" "$name" "$offline" "$keep_passwords"`+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	launchPath := filepath.Join(root, "ventoy-linux-setup.run")
	if err := os.WriteFile(launchPath, []byte(cut(launcher)+"exec /bin/bash \"$DISPATCH_INSTALLER\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
		ok   bool
	}{
		{"default-full", nil, "--apply||0|0", true},
		{"legacy-full", []string{"--apply"}, "--apply||0|0", true},
		{"default-options", []string{"--hostname", "node01", "--offline", "--keep-passwords"}, "--apply|node01|1|1", true},
		{"software-only", []string{"--software"}, "--software||0|0", true},
		{"check-only", []string{"--check"}, "--check||0|0", true},
		{"list-only", []string{"--list-software"}, "--list-software||0|0", true},
		{"check-software-only", []string{"--check-software"}, "--check-software||0|0", true},
		{"unknown-option", []string{"--unknown"}, "", false},
		{"missing-hostname", []string{"--hostname"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(launchPath, tc.args...)
			cmd.Env = append(os.Environ(), "DISPATCH_INSTALLER="+installPath)
			output, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok || (tc.ok && strings.TrimSpace(string(output)) != tc.want) {
				t.Fatalf("dispatch: err=%v output=%q want=%q", err, output, tc.want)
			}
		})
	}
}

func TestManualSetupDownload(t *testing.T) {
	s := fixture(t)
	mustCode(t, request(s, "GET", "/api/linux-manual-setup", "", nil), http.StatusUnauthorized)
	mustCode(t, request(s, "GET", "/api/linux-manual-setup", s.admin, nil), http.StatusConflict)
	if err := os.WriteFile(filepath.Join(s.config.PackageDir, e610ArchiveName), []byte("corrupt archive"), 0600); err != nil {
		t.Fatal(err)
	}
	mustCode(t, request(s, "GET", "/api/linux-manual-setup", s.admin, nil), http.StatusInternalServerError)
	// Optional integration uses the real pinned offline release without checking
	// binaries into Git. CI still tests authentication and all archive boundaries.
	if source := os.Getenv("VENTOY_E610_ARCHIVE"); source != "" {
		b, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(s.config.PackageDir, e610ArchiveName), b, 0600); err != nil {
			t.Fatal(err)
		}
		w := request(s, "GET", "/api/linux-manual-setup", s.admin, nil)
		mustCode(t, w, http.StatusOK)
		if w.Header().Get("X-Content-SHA256") != fmt.Sprintf("%x", sha256.Sum256(w.Body.Bytes())) {
			t.Fatal("download checksum mismatch")
		}
		z, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		defer z.Close()
		files := map[string][]byte{}
		tr := tar.NewReader(z)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(h.Name, "__pycache__") || strings.HasSuffix(h.Name, ".pyc") {
				t.Fatal("development bytecode in download", h.Name)
			}
			content, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(content, []byte(s.admin)) {
				t.Fatal("administrator token in download")
			}
			files[strings.TrimPrefix(h.Name, "ventoy-manual-setup/")] = content
		}
		for _, name := range []string{"install.sh", "manual-ssh.sh", "manual-network.sh", "e610-driver.sh", "e610-install-template.sh", "safe-server-init.sh", "linux-update-policy.sh", "ventoy-manual-init.service", "SHA256SUMS", "e610/install.sh", "e610/targets.tsv"} {
			if len(files[name]) == 0 {
				t.Fatal("missing component", name)
			}
		}
		for _, line := range strings.Split(strings.TrimSpace(string(files["SHA256SUMS"])), "\n") {
			parts := strings.SplitN(line, "  ", 2)
			if len(parts) != 2 || fmt.Sprintf("%x", sha256.Sum256(files[parts[1]])) != parts[0] {
				t.Fatal("bad package member checksum", line)
			}
		}
		if out := os.Getenv("VENTOY_MANUAL_OUTPUT"); out != "" {
			if err := os.WriteFile(out, w.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(strings.TrimSuffix(out, ".tar.gz")+".run", manualSetupLauncher(w.Body.Bytes()), 0600); err != nil {
				t.Fatal(err)
			}
		}
		run := request(s, "GET", "/api/linux-manual-setup?format=run", s.admin, nil)
		mustCode(t, run, http.StatusOK)
		if !bytes.Equal(run.Body.Bytes(), manualSetupLauncher(w.Body.Bytes())) || !strings.Contains(run.Header().Get("Content-Disposition"), ".run") {
			t.Fatal("self-extracting download differs from verified archive")
		}
		injection := request(s, "GET", "/api/linux-manual-setup?format=injection", s.admin, nil)
		mustCode(t, injection, http.StatusOK)
		want, err := manualPreloadInjection(run.Body.Bytes())
		if err != nil || !bytes.Equal(want, injection.Body.Bytes()) {
			t.Fatal("preload download differs from verified package", err)
		}
		if out := os.Getenv("VENTOY_MANUAL_OUTPUT"); out != "" {
			if err := os.WriteFile(strings.TrimSuffix(out, ".tar.gz")+"-injection.tar.gz", want, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestManualDriverArchiveBoundaries(t *testing.T) {
	for _, header := range []*tar.Header{
		{Name: "E610-MultiOS-6.4.5-r2/../../escape", Typeflag: tar.TypeReg},
		{Name: "E610-MultiOS-6.4.5-r2/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"},
		{Name: "/etc/passwd", Typeflag: tar.TypeReg},
	} {
		var b bytes.Buffer
		z := gzip.NewWriter(&b)
		tw := tar.NewWriter(z)
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		tw.Close()
		z.Close()
		if addManualDrivers(map[string][]byte{}, b.Bytes(), fmt.Sprintf("%x", sha256.Sum256(b.Bytes()))) == nil {
			t.Fatal("unsafe archive accepted", header.Name)
		}
	}
}
