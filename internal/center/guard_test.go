package center

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBootstrapRetriesOnlyConnectionFailures(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /bin/bash")
	}
	for _, tc := range []struct {
		name, code, failures, calls string
		ok                          bool
	}{
		{"dhcp-pending", "7", "2", "3", true},
		{"dns-pending", "6", "1", "2", true},
		{"offline-bounded", "7", "99", "12", false},
		{"unauthorized", "22", "99", "1", false},
		{"untrusted-ca", "60", "99", "1", false},
		{"ambiguous-timeout", "28", "99", "1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			curl := "#!/bin/bash\nn=$(cat \"$CALLS\" 2>/dev/null || echo 0); n=$((n+1)); echo \"$n\" > \"$CALLS\"\n[ \"$n\" -gt \"$FAILURES\" ] || exit \"$CODE\"\n"
			for name, content := range map[string]string{"curl": curl, "sleep": "#!/bin/sh\nexit 0\n"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			script, err := render("bootstrap.sh", renderData{Base: "https://controller.invalid/bootstrap/test"})
			if err != nil {
				t.Fatal(err)
			}
			start, end := strings.Index(script, "request() {"), strings.Index(script, "\nevent()")
			if start < 0 || end <= start {
				t.Fatal("request function missing")
			}
			cmd := exec.Command("/bin/bash")
			cmd.Stdin = strings.NewReader("set -eu\n" + script[start:end] + "\nrequest https://controller.invalid/event\n")
			cmd.Env = append(os.Environ(), "PATH="+root+":/usr/bin:/bin", "CALLS="+filepath.Join(root, "calls"), "CODE="+tc.code, "FAILURES="+tc.failures)
			output, err := cmd.CombinedOutput()
			calls, _ := os.ReadFile(filepath.Join(root, "calls"))
			if (err == nil) != tc.ok || strings.TrimSpace(string(calls)) != tc.calls {
				t.Fatalf("wrong retry behavior: %v, calls=%q, output=%s", err, calls, output)
			}
		})
	}
}

func TestBootstrapRequiresHumanLogin(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /bin/bash")
	}
	for _, mode := range []string{"valid", "system-uid", "missing-user", "nologin", "missing-policy", "policy-failed"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			bin := filepath.Join(root, "bin")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			commands := map[string]string{
				"hostname":  "echo LAB-001",
				"id":        "case \"$BOOT_TEST_MODE\" in system-uid) echo 11;; missing-user) exit 1;; *) echo 1000;; esac",
				"getent":    "case \"$BOOT_TEST_MODE\" in nologin) echo 'deployadmin:x:1000:1000::/home/deployadmin:/sbin/nologin';; *) echo 'deployadmin:x:1000:1000::/home/deployadmin:/bin/bash';; esac",
				"curl":      "printf '%s\\n' \"$@\" >> \"$BOOT_TEST_LOG\"",
				"systemctl": "exit 0",
			}
			for name, body := range commands {
				if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "missing-policy" {
				policy := filepath.Join(root, "usr/local/sbin/ventoy-center-update-policy")
				if err := os.MkdirAll(filepath.Dir(policy), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(policy, []byte("#!/bin/sh\n[ \"$1\" = --check ] && [ \"$BOOT_TEST_MODE\" != policy-failed ]\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			d := renderData{Base: "https://controller.invalid/bootstrap/test", VersionCheck: ":", PrivateJob: PrivateJob{Job: Job{Machine: Machine{Name: "LAB-001"}}, Request: JobRequest{Username: "deployadmin"}, Token: "fixture-token"}}
			script, err := render("bootstrap.sh", d)
			if err != nil {
				t.Fatal(err)
			}
			// Redirect all writable script paths into the fixture directory.
			script = strings.NewReplacer("/var/lib/", root+"/var/lib/", "/usr/local/", root+"/usr/local/").Replace(script)
			logPath := filepath.Join(root, "events.log")
			cmd := exec.Command("/bin/bash")
			cmd.Stdin = strings.NewReader(script)
			cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "BOOT_TEST_MODE="+mode, "BOOT_TEST_LOG="+logPath)
			output, err := cmd.CombinedOutput()
			log, _ := os.ReadFile(logPath)
			installed := strings.Contains(string(log), `{"state":"installed"}`)
			failed := strings.Contains(string(log), `{"state":"failed"}`)
			if mode == "valid" {
				if err != nil || !installed || failed {
					t.Fatalf("valid account: %v, %s, %s", err, log, output)
				}
			} else if err == nil || installed || !failed {
				t.Fatalf("invalid account reported success: %v, %s, %s", err, log, output)
			}
		})
	}
}

// CentOS 7.9 util-linux 2.23 omits virtio SERIAL, although the kernel exposes it.
// Run the real hook against fixture devices, without touching disks or networking.
func TestLinuxGuardDiskIdentity(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /bin/bash")
	}
	for _, mode := range []string{"legacy-virtio", "modern-virtio", "sata", "missing-serial", "wrong-serial", "conflicting-serial", "duplicate-mixed", "duplicate-virtio", "wrong-mac", "wrong-version", "wrong-firmware", "usb", "raid", "claim-rejected", "ubuntu-sata", "ubuntu-binding-failed", "ubuntu-kernel-failed", "ubuntu-duplicate", "ubuntu-raid"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			write := func(name, content string, perm os.FileMode) {
				t.Helper()
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), perm); err != nil {
					t.Fatal(err)
				}
			}
			mac := "02:00:00:00:01:01"
			if mode == "wrong-mac" {
				mac = "02:00:00:00:02:02"
			}
			write("sys/class/net/eth0/address", mac+"\n", 0600)
			if mode != "wrong-firmware" {
				if err := os.MkdirAll(filepath.Join(root, "sys/firmware/efi"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			serial := "DISK-001"
			if mode == "wrong-serial" || mode == "sata" || strings.HasPrefix(mode, "ubuntu-") && mode != "ubuntu-duplicate" {
				serial = "OTHER-DISK"
			}
			if mode != "missing-serial" {
				write("sys/block/vda/serial", "  "+serial+"  \n", 0600)
			}
			if mode == "duplicate-virtio" {
				write("sys/block/vdb/serial", "DISK-001\n", 0600)
			}
			for _, disk := range []string{"sda", "vda", "vdb"} {
				write("dev/"+disk, "fixture disk", 0600)
			}
			write("etc/os-release", "ID=centos\nVERSION_ID=7\n", 0600)
			release := "CentOS Linux release 7.9.2009 (Core)"
			if mode == "wrong-version" {
				release = "CentOS Linux release 7.6.1810 (Core)"
			}
			write("etc/centos-release", release+"\n", 0600)
			if err := os.MkdirAll(filepath.Join(root, "tmp"), 0700); err != nil {
				t.Fatal(err)
			}
			write("bin/udevadm", "#!/bin/sh\nexit 0\n", 0700)
			write("bin/base64", "#!/bin/sh\necho 'base64 unavailable in CentOS 7 installer' >&2\nexit 127\n", 0700)
			// Exercise the shell's binder/claim ordering; the validator executes
			// the real embedded Python against temporary autoinstall documents.
			write("bin/python3", "#!/bin/sh\nif [ \"$#\" -eq 1 ]; then\n echo KERNEL >> \"$GUARD_TEST_LOG\"\n [ \"$GUARD_TEST_MODE\" != ubuntu-kernel-failed ]\nelse\n printf 'BIND %s\\n' \"$2\" >> \"$GUARD_TEST_LOG\"\n [ \"$GUARD_TEST_MODE\" != ubuntu-binding-failed ]\nfi\n", 0700)
			write("bin/lsblk", `#!/bin/sh
case "$3" in
 NAME,TYPE) printf 'sda disk\nvda disk\nvdb disk\nsda1 part\nsr0 rom\n' ;;
 SERIAL)
  case "${4##*/}" in
   sda) case "$GUARD_TEST_MODE" in sata|duplicate-mixed|ubuntu-*) echo DISK-001;; *) echo OTHER-DISK;; esac ;;
   vda) case "$GUARD_TEST_MODE" in modern-virtio) echo DISK-001;; conflicting-serial) echo DIFFERENT;; esac ;;
  esac ;;
 TYPE) echo disk ;;
 TRAN) if [ "$GUARD_TEST_MODE" = usb ]; then echo usb; fi ;;
 FSTYPE) case "$GUARD_TEST_MODE" in raid|ubuntu-raid) echo linux_raid_member;; esac ;;
 *) exit 2 ;;
esac
exit 0
`, 0700)
			write("bin/curl", "#!/bin/sh\necho CLAIM >> \"$GUARD_TEST_LOG\"\n[ \"$GUARD_TEST_MODE\" != claim-rejected ]\n", 0700)
			p, _ := findProfile("centos-7.9")
			if strings.HasPrefix(mode, "ubuntu-") {
				p, _ = findProfile("ubuntu-22.04")
				write("etc/os-release", "ID=ubuntu\nVERSION_ID=22.04\n", 0600)
			}
			d := renderData{Target: p, CA: "fixture CA\nquoted 'value'\n", Base: "https://controller.invalid/bootstrap/test", PrivateJob: PrivateJob{Job: Job{Machine: Machine{MAC: "02:00:00:00:01:01", DiskSerial: "DISK-001", Firmware: "uefi"}}, Token: "fixture-token"}}
			var err error
			d.VersionCheck, err = render("version-check.sh", d)
			if err != nil {
				t.Fatal(err)
			}
			script, err := render("guard.sh", d)
			if err != nil {
				t.Fatal(err)
			}
			script = strings.NewReplacer("/sys/", root+"/sys/", "/dev/", root+"/dev/", "/etc/", root+"/etc/", "/tmp/", root+"/tmp/", "[ -b ", "[ -f ").Replace(script)
			script += "\nprintf 'SELECTED %s\\n' \"$disk\" >> \"$GUARD_TEST_LOG\"\n"
			logPath := filepath.Join(root, "operations.log")
			cmd := exec.Command("/bin/bash")
			cmd.Stdin = strings.NewReader(script)
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "bin")+":/usr/bin:/bin", "GUARD_TEST_LOG="+logPath, "GUARD_TEST_MODE="+mode)
			output, err := cmd.CombinedOutput()
			log, _ := os.ReadFile(logPath)
			allowed := mode == "legacy-virtio" || mode == "modern-virtio" || mode == "sata" || mode == "ubuntu-sata"
			if allowed {
				want := "vda"
				if mode == "sata" || mode == "ubuntu-sata" {
					want = "sda"
				}
				operations := "CLAIM\nSELECTED " + want + "\n"
				if mode == "ubuntu-sata" {
					operations = "KERNEL\nBIND sda\n" + operations
				}
				if err != nil || string(log) != operations {
					t.Fatalf("identity check: %v, %s, %s", err, log, output)
				}
				ca, err := os.ReadFile(filepath.Join(root, "tmp/ventoy-center-ca.crt"))
				if err != nil || string(ca) != d.CA {
					t.Fatalf("CA was not preserved without base64: %v", err)
				}
			} else {
				if err == nil || strings.Contains(string(log), "SELECTED") {
					t.Fatalf("unsafe selection: %v, %s, %s", err, log, output)
				}
				if mode != "claim-rejected" && strings.Contains(string(log), "CLAIM") {
					t.Fatal("claimed before validating disk identity")
				}
			}
		})
	}
}

// Execute only the authorization hook with synthetic sysfs, devices and commands.
// No installer, real block device or external server is accessed by this test.
func TestDebianGuardAuthorizesOnlyAfterChecks(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /bin/sh")
	}
	for _, mode := range []string{"allowed", "virtio-serial", "wrong-release", "wrong-mac", "wrong-serial", "duplicate-serial", "wrong-firmware", "usb", "readonly", "mounted", "raid", "no-wget-udeb", "claim-rejected"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			write := func(name, content string, perm os.FileMode) {
				t.Helper()
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), perm); err != nil {
					t.Fatal(err)
				}
			}
			mac := "02:00:00:00:01:01"
			if mode == "wrong-mac" {
				mac = "02:00:00:00:02:02"
			}
			serial := "DISK-001"
			if mode == "wrong-serial" {
				serial = "DISK-WRONG"
			}
			release := "13 (trixie) - installer build fixture"
			if mode == "wrong-release" {
				release = "12 (bookworm) - installer build fixture"
			}
			write("etc/lsb-release", "DISTRIB_ID=Debian\nDISTRIB_RELEASE='"+release+"'\n", 0600)
			write("sys/class/net/eth0/address", mac+"\n", 0600)
			serialPath := "sys/block/sda/device/serial"
			if mode == "virtio-serial" {
				serialPath = "sys/block/sda/serial"
			}
			write(serialPath, serial+"\n", 0600)
			readOnly := "0\n"
			if mode == "readonly" {
				readOnly = "1\n"
			}
			write("sys/block/sda/ro", readOnly, 0600)
			write("sys/block/sda/removable", "0\n", 0600)
			write("sys/block/sda/dev", "8:0\n", 0600)
			mounts := ""
			if mode == "mounted" {
				mounts = "1 2 8:0 / /target rw - ext4 /dev/sda rw\n"
			}
			write("proc/self/mountinfo", mounts, 0600)
			write("proc/swaps", "Filename Type Size Used Priority\n", 0600)
			write("dev/sda", "fake disk", 0600)
			if mode == "duplicate-serial" {
				write("sys/block/sdb/device/serial", serial+"\n", 0600)
				write("dev/sdb", "fake disk", 0600)
			}
			if mode != "wrong-firmware" {
				if err := os.MkdirAll(filepath.Join(root, "sys/firmware/efi"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(root, "tmp"), 0700); err != nil {
				t.Fatal(err)
			}
			write("bin/udevadm", "#!/bin/sh\nexit 0\n", 0700)
			// Early partman hooks run before every question template is registered.
			write("bin/debconf-set", "#!/bin/sh\nexit 10\n", 0700)
			write("bin/debconf-set-selections", "#!/bin/sh\nawk '{print \"SET \" $2 \" \" $4}' \"$1\" >> \"$GUARD_TEST_LOG\"\n", 0700)
			fsType := ""
			if mode == "raid" {
				fsType = "linux_raid_member"
			}
			write("bin/blkid", "#!/bin/sh\necho '"+fsType+"'\n", 0700)
			annaExit := "0"
			if mode == "no-wget-udeb" {
				annaExit = "1"
			}
			write("bin/anna-install", "#!/bin/sh\nexit "+annaExit+"\n", 0700)
			claimExit := "0"
			if mode == "claim-rejected" {
				claimExit = "1"
			}
			write("bin/wget", "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'GNU Wget'; exit 0; fi\necho CLAIM >> \"$GUARD_TEST_LOG\"\nexit "+claimExit+"\n", 0700)
			if mode == "usb" {
				write("bin/readlink", "#!/bin/sh\necho /devices/usb1/storage\n", 0700)
			}
			p, _ := findProfile("debian-13")
			d := renderData{Target: p, CA: "fixture CA", Base: "https://controller.invalid/bootstrap/test", PrivateJob: PrivateJob{Job: Job{Machine: Machine{MAC: "02:00:00:00:01:01", DiskSerial: "DISK-001", Firmware: "uefi"}}, Token: "fixture-token"}}
			script, err := render("debian-guard.sh", d)
			if err != nil {
				t.Fatal(err)
			}
			// Substitute the failure hold with a test exit, and block tests with files.
			script = strings.NewReplacer("/etc/lsb-release", root+"/etc/lsb-release", "/proc/", root+"/proc/", "/sys/", root+"/sys/", "/dev/", root+"/dev/", "/tmp/", root+"/tmp/", "[ -b ", "[ -f ", "while :; do sleep 60; done", "exit 77").Replace(script)
			cmd := exec.Command("/bin/sh")
			cmd.Stdin = strings.NewReader(script)
			logPath := filepath.Join(root, "operations.log")
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "bin")+":/usr/bin:/bin", "GUARD_TEST_LOG="+logPath)
			output, err := cmd.CombinedOutput()
			log, _ := os.ReadFile(logPath)
			if mode == "allowed" || mode == "virtio-serial" {
				if err != nil {
					t.Fatalf("guard failed: %v: %s", err, output)
				}
				if !strings.HasPrefix(string(log), "CLAIM\nSET partman-auto/disk ") || !strings.Contains(string(log), "SET partman/confirm true") {
					t.Fatalf("wrong order: %s", log)
				}
			} else {
				if err == nil || strings.Contains(string(log), "SET ") {
					t.Fatalf("failed check authorized partitioning: %v, %s, %s", err, log, output)
				}
				if mode != "claim-rejected" && strings.Contains(string(log), "CLAIM") {
					t.Fatal("claimed before validating hardware and dependencies")
				}
			}
		})
	}
}
