package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
)

//go:embed assets/manual-preload/*.sh assets/manual-preload/*.py assets/manual-preload/finish-* assets/manual-preload/*.txt
var manualPreloadAssets embed.FS

// manualPreloadInjection retains interactive installation and stages setup.
// APT installers also receive the independently requested update policy.
func manualPreloadInjection(launcher []byte, settings ...map[string][]byte) ([]byte, error) {
	reboot, err := templates.ReadFile("templates/ubuntu-reboot.sh")
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{
		"ventoy-manual-preload/ventoy-linux-setup.run": launcher,
		"ventoy-manual-preload/setup.sha256":           []byte(fmt.Sprintf("%x  ventoy-linux-setup.run\n", sha256.Sum256(launcher))),
		"ventoy-manual-preload/ubuntu-reboot.sh":       bytes.ReplaceAll(reboot, []byte("\r\n"), []byte("\n")),
	}
	if err := addInstallSupport(files, "ventoy-manual-preload/"); err != nil {
		return nil, err
	}
	for _, configured := range settings {
		for name, value := range configured {
			files["ventoy-manual-preload/"+name] = value
		}
	}
	for _, name := range []string{"linux-update-policy.sh", "ubuntu-kernel.py"} {
		body, err := render(name, renderData{KernelPackages: kernelPackagePatterns()})
		if err != nil {
			return nil, err
		}
		files["ventoy-manual-preload/"+name] = []byte(body)
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
		case "finish-debian-policy":
			name = "usr/lib/post-base-installer.d/93ventoy-update-policy"
		case "finish-debian-cache":
			name = "usr/lib/pre-pkgsel.d/93ventoy-installer-cache"
		case "start-debian-watch.sh":
			name = "lib/debian-installer-startup.d/S99ventoy-diagnostics"
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
		if strings.HasSuffix(name, "support-curl.conf") || strings.HasSuffix(name, "support-wget.conf") {
			mode = 0600
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

// The automatic answer only copies this file; setup/software installation is
// still explicitly invoked by the operator after booting the installed system.
func (s *Server) addLinuxSetupPreload(profile string, files map[string]string) error {
	if !s.config.LinuxSetupPreload {
		return nil
	}
	p, err := resolveProfile(profile, "")
	if err != nil || p.OS == "windows" {
		return err
	}
	name := bootInjectionName(profile)
	if name == "" {
		return errors.New("Linux setup preload is not supported for this installer")
	}
	bundle, err := s.manualSetupBundle()
	if err != nil {
		return fmt.Errorf("Linux setup preload is required but unavailable: %w", err)
	}
	preload, err := manualPreloadInjection(manualSetupLauncher(bundle), s.supportSettings())
	if err != nil {
		return err
	}
	merged, err := mergeInjectionHooks([]byte(files[name]), preload, "ventoy-preload-chain/boot-hook.sh", "ventoy-preload-chain/setup-hook.sh")
	if err != nil {
		return err
	}
	files[name] = string(merged)
	return nil
}
