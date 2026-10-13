package center

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Execute only the authorization hook with synthetic sysfs, devices and commands.
// No installer, real block device or external server is accessed by this test.
func TestDebianGuardAuthorizesOnlyAfterChecks(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux /bin/sh")
	}
	for _, mode := range []string{"allowed", "wrong-mac", "wrong-serial", "duplicate-serial", "wrong-firmware", "usb", "raid", "no-wget-udeb", "claim-rejected"} {
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
			write("sys/class/net/eth0/address", mac+"\n", 0600)
			write("sys/block/sda/device/serial", serial+"\n", 0600)
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
			write("bin/debconf-set", "#!/bin/sh\nprintf 'SET %s %s\\n' \"$1\" \"$2\" >> \"$GUARD_TEST_LOG\"\n", 0700)
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
			script = strings.NewReplacer("/sys/", root+"/sys/", "/dev/", root+"/dev/", "/tmp/", root+"/tmp/", "[ -b ", "[ -f ", "while :; do sleep 60; done", "exit 77").Replace(script)
			cmd := exec.Command("/bin/sh")
			cmd.Stdin = strings.NewReader(script)
			logPath := filepath.Join(root, "operations.log")
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(root, "bin")+":/usr/bin:/bin", "GUARD_TEST_LOG="+logPath)
			output, err := cmd.CombinedOutput()
			log, _ := os.ReadFile(logPath)
			if mode == "allowed" {
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
