package center

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func passwordPolicyHash() string { return "$6$policy$" + strings.Repeat("b", 86) }

func TestLinuxInitialPasswordTemplates(t *testing.T) {
	for _, p := range profiles {
		t.Run(p.ID, func(t *testing.T) {
			s := fixture(t)
			s.ca = []byte("fixture CA")
			s.config.LinuxInitialPasswordHash = passwordPolicyHash()
			s.config.LinuxAdminUsername = "admin"
			m := Machine{Name: "LAB-PASSWORD", MAC: "02:00:00:00:01:01", Serial: "ASSET", DiskSerial: "DISK", Firmware: "uefi"}
			w := request(s, "POST", "/api/machines", s.admin, m)
			mustCode(t, w, 201)
			json.Unmarshal(w.Body.Bytes(), &m)
			r := profileRequest(p, m.ID)
			r.PasswordHash = "" // No per-task hash required with the configured policy.
			w = request(s, "POST", "/api/jobs", s.admin, r)
			mustCode(t, w, 201)
			j := s.store.snapshot().Jobs[0]
			if p.OS == "windows" {
				if j.Request.Password != r.Password || j.Request.RootPasswordHash != "" || j.Request.Username != r.Username {
					t.Fatal("Linux policy changed Windows credentials")
				}
				return
			}
			if j.Request.PasswordHash != passwordPolicyHash() || j.Request.RootPasswordHash != passwordPolicyHash() || j.Request.Username != "admin" {
				t.Fatal("policy not snapshotted")
			}
			// Subsequent service configuration changes cannot rewrite an issued job.
			s.config.LinuxInitialPasswordHash = "$6$newpolicy$" + strings.Repeat("c", 86)
			s.config.LinuxAdminUsername = "futureadmin"
			files, err := s.renderFiles(j)
			if err != nil {
				t.Fatal(err)
			}
			main := map[string]string{"kickstart": "kickstart.cfg", "autoinstall": "user-data", "preseed": "preseed.cfg", "ubiquity": "preseed.cfg"}[p.Installer]
			configText := files[main]
			if p.Installer == "ubiquity" {
				for _, m := range regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)'`).FindAllStringSubmatch(configText, -1) {
					configText += decodeTemplateScript(t, m[1])
				}
			}
			if strings.Count(configText, passwordPolicyHash()) != 2 {
				t.Fatal("administrator and root did not receive the snapshotted hash")
			}
			if p.Installer == "kickstart" && !strings.Contains(files[main], "rootpw --iscrypted "+passwordPolicyHash()) {
				t.Fatal("missing root password")
			}
			if p.Installer == "preseed" && !strings.Contains(files[main], "passwd/root-login boolean true") {
				t.Fatal("root still locked")
			}
			if p.OS == "ubuntu" && !strings.Contains(configText, "usermod --password '"+passwordPolicyHash()+"' root") {
				t.Fatal("root password not applied to target")
			}
			if p.OS != "windows" {
				if !strings.Contains(files["root-ssh-policy.sh"], "'PermitRootLogin yes' 'PasswordAuthentication yes'") {
					t.Fatal("missing root password SSH policy")
				}
				if !strings.Contains(configText, "ventoy-center-root-ssh") && (p.Installer == "autoinstall" || p.Installer == "kickstart") {
					t.Fatal("SSH policy not installed")
				}
				if p.Installer == "kickstart" && (!strings.Contains(files["root-ssh-policy.sh"], "systemctl enable sshd.service") || strings.Contains(configText, "PermitRootLogin prohibit-password")) {
					t.Fatal("EL root SSH password policy not applied")
				}
			}
			if files["safe-server-init.sh"] != serverInit || !strings.Contains(serverInit, "set-hostname()") || !strings.Contains(serverInit, "set-network()") {
				t.Fatal("initialization helper not bundled")
			}
			view := request(s, "GET", "/api/overview", s.admin, nil).Body.String()
			if strings.Contains(view, passwordPolicyHash()) || strings.Contains(view, s.config.LinuxInitialPasswordHash) {
				t.Fatal("password hash leaked")
			}
			if !strings.Contains(view, `"linux_initial_password_configured":true`) {
				t.Fatal("UI policy indicator missing")
			}
			if out := os.Getenv("VENTOY_TEST_OUTPUT"); out != "" {
				dir := filepath.Join(out, "password-policy", p.ID)
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, main), []byte(files[main]), 0600); err != nil {
					t.Fatal(err)
				}
			}
			addEvent(&j, "installed", "")
			if j.Request.RootPasswordHash != "" || j.Request.PasswordHash != "" {
				t.Fatal("terminal job retained password hashes")
			}
		})
	}
}

func TestExistingPXEUsesCurrentPasswordPolicy(t *testing.T) {
	s, e := pxeFixture(t, "ubuntu-22.04")
	s.config.LinuxInitialPasswordHash = passwordPolicyHash()
	s.config.LinuxAdminUsername = "admin"
	ticket := pxeTicket(t, s, e, pxeInventory())
	w := request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, map[string]string{"ticket": ticket["ticket"], "disk_serial": "DISK-DEMO-001", "confirmation": "ERASE DISK-DEMO-001"})
	mustCode(t, w, 200)
	j := s.store.snapshot().Jobs[0]
	if j.Request.PasswordHash != passwordPolicyHash() || j.Request.RootPasswordHash != passwordPolicyHash() || j.Request.Username != "admin" {
		t.Fatal("existing menu kept old password")
	}
	files, err := s.renderPXEFiles(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range files {
		if strings.Contains(content, passwordPolicyHash()) {
			t.Fatal("shared menu leaked the password hash")
		}
	}
}

func TestPasswordPolicyValidationAndMenuCreation(t *testing.T) {
	s := fixture(t)
	c := s.config
	for _, value := range []string{"plain-password", "$6$bad", "$6$x$" + strings.Repeat("a", 86) + "\n"} {
		c.LinuxInitialPasswordHash = value
		b, _ := json.Marshal(c)
		path := filepath.Join(t.TempDir(), "config.json")
		os.WriteFile(path, b, 0600)
		if _, err := loadConfig(path); err == nil {
			t.Fatal("invalid policy hash accepted")
		}
	}
	s.config.LinuxInitialPasswordHash = passwordPolicyHash()
	s.config.LinuxAdminUsername = "admin"
	w := request(s, "POST", "/api/pxe/entries", s.admin, map[string]string{"label": "Ubuntu 22.04.3", "profile": "ubuntu-22.04", "iso": "test.iso", "username": "deployadmin"})
	mustCode(t, w, 201)
	e := s.store.snapshot().PXEEntries[0]
	if e.Request.RootPasswordHash != passwordPolicyHash() || e.Request.Username != "admin" {
		t.Fatal("new menu policy missing")
	}
	p, _ := findProfile("ubuntu-22.04")
	r := profileRequest(p, "machine")
	r.OS = p.OS
	r.RootPasswordHash = "invalid; command"
	if validateJob(r) == nil {
		t.Fatal("invalid root password hash accepted")
	}
}

func TestLinuxAdminPolicyValidation(t *testing.T) {
	s := fixture(t)
	for _, name := range []string{"admin", "", "root", "operator", "Admin", "a;id", "admin\n"} {
		c := s.config
		c.LinuxAdminUsername = name
		b, _ := json.Marshal(c)
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadConfig(path)
		valid := name == "admin" || name == ""
		if (err == nil) != valid {
			t.Fatalf("username policy %q: %v", name, err)
		}
	}
}
