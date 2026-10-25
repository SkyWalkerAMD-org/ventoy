package center

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupPreloadPreservesBootAndDriverHooks(t *testing.T) {
	base, _ := ubuntuInjection(renderData{})
	driver, _ := packInjection(map[string]injectionFile{liveInjectionHook: {[]byte("echo E610"), 0755}, "ventoy-e610/verify.sh": {[]byte("driver check"), 0755}})
	withDriver, err := mergeMediaInjection([]byte(base), driver)
	if err != nil {
		t.Fatal(err)
	}
	preload, _ := manualPreloadInjection([]byte("inert setup"))
	merged, err := mergeInjectionHooks(withDriver, preload, "ventoy-preload-chain/boot-hook.sh", "ventoy-preload-chain/setup-hook.sh")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := readInjection(withDriver)
	after, _ := readInjection(merged)
	for name, original := range before {
		if name != liveInjectionHook && !bytes.Equal(after[name].Data, original.Data) {
			t.Fatal("original member changed", name)
		}
	}
	if !bytes.Equal(after["ventoy-preload-chain/boot-hook.sh"].Data, before[liveInjectionHook].Data) {
		t.Fatal("boot/driver chain lost")
	}
	if string(after["ventoy-manual-preload/ventoy-linux-setup.run"].Data) != "inert setup" {
		t.Fatal("setup missing")
	}
	if _, err = mergeInjectionHooks(merged, preload, "ventoy-preload-chain/boot-hook.sh", "ventoy-preload-chain/setup-hook.sh"); err == nil {
		t.Fatal("double injection accepted")
	}
}

func TestEveryLinuxProfilePreloadsCurrentSetup(t *testing.T) {
	for _, p := range profiles {
		if p.OS == "windows" {
			continue
		}
		t.Run(p.ID, func(t *testing.T) {
			s, e := pxeFixture(t, p.ID)
			s.ca = []byte("fixture CA, not valid for deployment")
			s.config.LinuxSetupPreload = true
			s.config.SoftwareDir = t.TempDir()
			files := map[string][]byte{"install.sh": []byte("#!/bin/sh\necho inert test package\n"), "VERSION": []byte(Version + "\n")}
			s.addSoftwareClient(files)
			var err error
			s.manualBundle, err = packManualSetup(files)
			if err != nil {
				t.Fatal(err)
			}
			expected := manualSetupLauncher(s.manualBundle)
			j := injectionFixtureJob(t, s, p.ID)
			job, err := s.renderFiles(j)
			if err != nil {
				t.Fatal(err)
			}
			menu, err := s.renderPXEFiles(e)
			if err != nil {
				t.Fatal(err)
			}
			for mode, output := range map[string]map[string]string{"job": job, "pxe": menu} {
				if dir := os.Getenv("VENTOY_TEST_OUTPUT"); dir != "" {
					for name, body := range output {
						path := filepath.Join(dir, "setup-preload", p.ID, mode, name)
						if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, []byte(body), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				archive, err := readInjection([]byte(output[bootInjectionName(p.ID)]))
				if err != nil {
					t.Fatal(mode, err)
				}
				if !bytes.Equal(archive["ventoy-manual-preload/ventoy-linux-setup.run"].Data, expected) {
					t.Fatal(mode, "preload differs from download")
				}
				if bytes.Contains(expected, []byte(s.admin)) || bytes.Contains(expected, []byte(e.Token)) {
					t.Fatal("privileged credential in setup")
				}
			}
			main := map[string]string{"kickstart": "kickstart.cfg", "autoinstall": "user-data", "preseed": "preseed.cfg", "ubiquity": "preseed.cfg"}[p.Installer]
			if p.Installer != "ubiquity" && !strings.Contains(job[main], "copy-to-target.sh") {
				t.Fatal("completion does not require the preload")
			}
			if p.Installer == "ubiquity" {
				late, err := render("ubiquity-late.sh", renderData{SetupPreload: true})
				if err != nil || !strings.Contains(late, "copy-to-target.sh") {
					t.Fatal("Desktop completion does not require preload", err)
				}
			}
			// Debian 10/11 must retain BusyBox printf encoding for their late script.
			if p.OS == "debian" && strings.Contains(job[main], base64.StdEncoding.EncodeToString(expected)) {
				t.Fatal("large setup embedded as a command argument")
			}
		})
	}
}

func TestRequiredSetupPreloadFailsBeforePublishing(t *testing.T) {
	s, e := pxeFixture(t, "ubuntu-24.04")
	s.config.LinuxSetupPreload = true
	if _, err := s.renderPXEFiles(e); err == nil {
		t.Fatal("missing checked driver archive silently omitted setup")
	}
}
