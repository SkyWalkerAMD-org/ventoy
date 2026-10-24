package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed assets/manual-preload/*
var manualPreloadAssets embed.FS

// manualPreloadInjection adds installer completion hooks, without supplying
// unattended answers or executing the contained system configuration package.
func manualPreloadInjection(launcher []byte) ([]byte, error) {
	files := map[string][]byte{
		"ventoy-manual-preload/ventoy-linux-setup.run": launcher,
		"ventoy-manual-preload/setup.sha256":           []byte(fmt.Sprintf("%x  ventoy-linux-setup.run\n", sha256.Sum256(launcher))),
	}
	entries, err := manualPreloadAssets.ReadDir("assets/manual-preload")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		b, err := manualPreloadAssets.ReadFile("assets/manual-preload/" + entry.Name())
		if err != nil {
			return nil, err
		}
		name := "ventoy-manual-preload/" + entry.Name()
		switch entry.Name() {
		case "live-hook.sh":
			name = "live_injection_7ed136ec_7a61_4b54_adc3_ae494d5106ea/hook.sh"
		case "dracut-hook.sh":
			// EL installers use /lib -> usr/lib. Do not replace that symlink
			// with an archive directory when the initramfs is unpacked.
			name = "usr/lib/dracut/hooks/pre-pivot/99-ventoy-manual-preload.sh"
		case "finish-debian":
			name = "usr/lib/finish-install.d/93ventoy-manual-preload"
		}
		files[name] = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	}
	var buf bytes.Buffer
	z := gzip.NewWriter(&buf)
	t := tar.NewWriter(z)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		mode := int64(0755)
		if strings.HasSuffix(name, ".run") || strings.HasSuffix(name, ".sha256") || strings.HasSuffix(name, ".txt") {
			mode = 0644
		}
		if err := t.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(files[name])), Format: tar.FormatUSTAR}); err != nil {
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
	return buf.Bytes(), nil
}
