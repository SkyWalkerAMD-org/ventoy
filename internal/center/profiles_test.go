package center

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func profileRequest(p Profile, machine string) JobRequest {
	return JobRequest{Profile: p.ID, MachineID: machine, ISO: "test.iso", Username: "deployadmin", PasswordHash: "$6$example$" + strings.Repeat("a", 86), Password: "Example-Password-123!", WindowsImage: 1, Locale: "en-US"}
}

func TestProfileSelectionAndCompatibility(t *testing.T) {
	for _, old := range []struct{ os, id string }{{"rocky", "rocky-9"}, {"ubuntu", "ubuntu-24.04"}, {"windows", "windows-server-2022"}} {
		p, err := resolveProfile("", old.os)
		if err != nil || p.ID != old.id {
			t.Fatalf("legacy %s: %+v, %v", old.os, p, err)
		}
	}
	for _, bad := range [][2]string{{"ubuntu-25.10", "ubuntu"}, {"rocky-10", "ubuntu"}, {"", "debian"}} {
		if _, err := resolveProfile(bad[0], bad[1]); err == nil {
			t.Fatal("invalid profile accepted", bad)
		}
	}
	for _, test := range []struct{ path, id string }{
		{"Rocky/Rocky-10.2-x86_64-dvd1.iso", "rocky-10"},
		{"CentOS/CentOS-7.9-x86_64-Everything-2009.iso", "centos-7.9"},
		{"CentOS/CentOS-7-x86_64-DVD-2009.iso", "centos-7.9"},
		{"CentOS/CentOS-7-x86_64-Everything-2009.iso", "centos-7.9"},
		{"CentOS/CentOS-7-x86_64-DVD-1810.iso", ""},
		{"CentOS/CentOS-7.90-x86_64-DVD.iso", ""},
		{"CentOS/CentOS-8.5.2111-x86_64-dvd1.iso", "centos-8.5"},
		{"CentOS/CentOS-Stream-9-latest-x86_64-dvd1.iso", "centos-stream-9"},
		{"CentOS/CentOS-Stream-10-latest-x86_64-dvd1.iso", "centos-stream-10"},
		{"CentOS/CentOS-Stream-100-x86_64-dvd1.iso", ""},
		{"CentOS/CentOS-8.50-x86_64-dvd1.iso", ""},
		{"Ubuntu/ubuntu-20.04.6-live-server-amd64.iso", "ubuntu-20.04"},
		{"Debian/debian-13.1.0-amd64-netinst.iso", "debian-13"},
		{"custom-linux.iso", ""}, {"rocky-100-x86_64.iso", ""},
	} {
		if got := suggestedProfile(test.path); got != test.id {
			t.Errorf("%s: got %s", test.path, got)
		}
	}
	for _, test := range [][2]string{{"rocky-9", "Rocky-8.10-x86_64-dvd1.iso"}, {"ubuntu-24.04", "ubuntu-24.04-desktop-amd64.iso"}, {"debian-13", "debian-live-13-amd64.iso"}, {"rocky-10", "Rocky-10.2-aarch64-dvd1.iso"}, {"centos-7.9", "CentOS-8.5.2111-x86_64-dvd1.iso"}, {"centos-stream-9", "CentOS-Stream-10-latest-x86_64-dvd1.iso"}, {"centos-8.5", "CentOS-8.5.2111-x86_64-boot.iso"}, {"centos-stream-10", "CentOS-Stream-10-latest-aarch64-dvd1.iso"}} {
		p, _ := findProfile(test[0])
		if validateMediaProfile(p, test[1]) == nil {
			t.Fatal("incompatible ISO accepted", test)
		}
	}
}

func TestCentOS79MediaSelection(t *testing.T) {
	p, _ := findProfile("centos-7.9")
	for _, name := range []string{"CentOS-7-x86_64-DVD-2009.iso", "CentOS-7-x86_64-Everything-2009.iso", "CentOS-7.9-x86_64-DVD-2009.iso"} {
		if err := validateMediaProfile(p, name); err != nil {
			t.Errorf("full installation media %s rejected: %v", name, err)
		}
		other, _ := findProfile("rocky-9")
		if err := validateMediaProfile(other, name); err == nil {
			t.Errorf("CentOS media %s accepted for Rocky", name)
		}
	}
	for _, name := range []string{"CentOS-7-x86_64-NetInstall-2009.iso", "CentOS-7-x86_64-Minimal-2009.iso", "CentOS-7.9-live-x86_64.iso"} {
		if err := validateMediaProfile(p, name); err == nil {
			t.Errorf("incomplete media %s accepted for DVD-only template", name)
		}
	}
}

func TestLinuxAdministratorName(t *testing.T) {
	for _, p := range profiles {
		r := profileRequest(p, "fixture-machine")
		r.OS = p.OS
		if err := validateJob(r); err != nil {
			t.Fatalf("default administrator rejected for %s: %v", p.ID, err)
		}
		r.Username = "operator"
		err := validateJob(r)
		if p.OS == "windows" {
			if err != nil {
				t.Fatalf("Windows account unexpectedly rejected: %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "内置系统账户") {
			t.Fatalf("reserved Linux account accepted for %s: %v", p.ID, err)
		}
	}
}

func TestInstallationOnlyAndDebianCA(t *testing.T) {
	s := fixture(t)
	for _, p := range profiles {
		r := profileRequest(p, "missing")
		r.Enroll = true
		w := request(s, "POST", "/api/jobs", s.admin, r)
		mustCode(t, w, 400)
		if !strings.Contains(w.Body.String(), "当前阶段仅提供系统安装") {
			t.Fatal(w.Body.String())
		}
	}
	p, _ := findProfile("debian-12")
	w := request(s, "POST", "/api/jobs", s.admin, profileRequest(p, "missing"))
	mustCode(t, w, 400)
	if !strings.Contains(w.Body.String(), "bootstrap_ca_file") {
		t.Fatal("missing CA accepted")
	}
	s.ca = []byte("fixture CA")
	w = request(s, "POST", "/api/jobs", s.admin, profileRequest(p, "missing"))
	mustCode(t, w, 409)
	if len(s.store.snapshot().Jobs) != 0 {
		t.Fatal("rejected requests created tasks")
	}
}

func TestEveryProfileProducesInstallationBundle(t *testing.T) {
	for _, p := range profiles {
		for _, firmware := range []string{"uefi", "bios"} {
			t.Run(p.ID+"/"+firmware, func(t *testing.T) {
				s := fixture(t)
				s.ca = []byte("fixture CA, not valid for deployment")
				w := request(s, "POST", "/api/machines", s.admin, Machine{Name: "LAB-001", MAC: "02:00:00:00:01:01", Serial: "ASSET-001", DiskSerial: "DISK-001", Firmware: firmware})
				mustCode(t, w, 201)
				var m Machine
				json.Unmarshal(w.Body.Bytes(), &m)
				w = request(s, "POST", "/api/jobs", s.admin, profileRequest(p, m.ID))
				if p.ID == "windows-11" && firmware == "bios" {
					mustCode(t, w, 409)
					return
				}
				mustCode(t, w, 201)
				var public Job
				json.Unmarshal(w.Body.Bytes(), &public)
				st := s.store.snapshot()
				j := arm(t, s, *findJob(&st, public.ID))
				if j.Profile != p.ID || j.OS != p.OS || j.Enroll {
					t.Fatalf("wrong profile snapshot: %+v", j.Job)
				}
				files, err := s.renderFiles(j)
				if err != nil {
					t.Fatal(err)
				}
				main := map[string]string{"kickstart": "kickstart.cfg", "autoinstall": "user-data", "preseed": "preseed.cfg", "unattend": "autounattend.xml"}[p.Installer]
				if files[main] == "" {
					t.Fatal("missing installation entry", main)
				}
				if (files[centosInjectionName] != "") != (p.ID == "centos-7.9") {
					t.Fatal("CentOS 7 compatibility archive must be limited to that profile")
				}
				if (files[rockyInjectionName] != "") != (p.ID == "rocky-8" || p.ID == "rocky-9") {
					t.Fatal("Rocky compatibility archive must be limited to verified profiles")
				}
				if (files[ubuntuInjectionName] != "") != (p.ID == "ubuntu-22.04") {
					t.Fatal("Ubuntu compatibility archive must be limited to 22.04")
				}
				if p.ID == "centos-7.9" && (!strings.Contains(files[main], "\n@^minimal\n@core\n") || strings.Contains(files[main], "@^minimal-environment")) {
					t.Fatal("package environment does not exist in the CentOS 7.9 DVD comps metadata")
				}
				for name, content := range files {
					if name != "SHA256SUMS" && !strings.Contains(files["SHA256SUMS"], fmt.Sprintf("%x  %s", sha256.Sum256([]byte(content)), name)) {
						t.Fatal("bad checksum", name)
					}
				}
				if p.OS != "windows" {
					d := renderData{PrivateJob: j, Target: p, Base: s.config.PublicURL + "/bootstrap/" + j.ID, CA: string(s.ca)}
					d.VersionCheck, err = render("version-check.sh", d)
					if err != nil {
						t.Fatal(err)
					}
					guard := "guard.sh"
					if p.OS == "debian" {
						guard = "debian-guard.sh"
					}
					for _, name := range []string{guard, "bootstrap.sh"} {
						content, e := render(name, d)
						if e != nil {
							t.Fatal(e)
						}
						if strings.Contains(content, "fyhocsys-node") || strings.Contains(content, "PRIVATE-FYH") {
							t.Fatal("OS-only bundle includes integration")
						}
						checkShell(t, name, content)
					}
					if p.OS == "debian" {
						if !strings.Contains(files[main], "mirror/suite string "+p.Suite) || strings.Contains(files[main], "partman/confirm boolean true") {
							t.Fatal("unconditional erase or wrong suite")
						}
						// Check the actual embedded hooks, including their nested first-boot script.
						encoded := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)' \| base64 -d`).FindAllStringSubmatch(files[main], -1)
						if len(encoded) != 2 {
							t.Fatal("missing preseed hooks")
						}
						for _, match := range encoded {
							b, e := base64.StdEncoding.DecodeString(match[1])
							if e != nil {
								t.Fatal(e)
							}
							checkShell(t, "preseed-hook.sh", string(b))
						}
					}
				}
				if output := os.Getenv("VENTOY_TEST_OUTPUT"); output != "" {
					for name, content := range files {
						path := filepath.Join(output, p.ID, firmware, filepath.FromSlash(name))
						if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, []byte(content), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
				mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installing"}), 200)
				mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installed"}), 200)
			})
		}
	}
}

func checkShell(t *testing.T, name, content string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s syntax: %v: %s", name, err, out)
	}
}
