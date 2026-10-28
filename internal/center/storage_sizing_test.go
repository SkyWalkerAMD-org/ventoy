package center

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCapacityTierLayout(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("executes the installer POSIX shell in an isolated sysfs fixture")
	}
	for _, tc := range []struct {
		first, second int64
		want          string
	}{
		{120000000000, 0, "1280 8192 16384 32768"},
		{128000000000, 0, "1280 8192 16384 32768"},
		{240000000000, 0, "2560 16384 32768 65536"},
		{256000000000, 0, "2560 16384 32768 65536"},
		{480000000000, 0, "5120 32768 65536 131072"},
		{512000000000, 0, "5120 32768 65536 131072"},
		{960000000000, 0, "10240 65536 131072 262144"},
		{1024000000000, 0, "10240 65536 131072 262144"},
		{1920000000000, 0, "20480 131072 262144 524288"},
		{2000000000000, 0, "20480 131072 262144 524288"},
		{3840000000000, 0, "40960 262144 524288 1048576"},
		{4000000000000, 0, "40960 262144 524288 1048576"},
		{7680000000000, 0, "81920 524288 1048576 2097152"},
		{480000000000 - 512, 0, "2560 16384 32768 65536"},
		{512000000000, 480000000000, "5120 32768 65536 131072"},
		{960000000000, 240000000000, "2560 16384 32768 65536"},
		{240000000000, 960000000000, "2560 16384 32768 65536"},
		{120000000000 - 512, 0, ""},
		{480000000000, 64000000000, ""},
		{0, 0, ""},
	} {
		t.Run(fmt.Sprintf("%d-%d", tc.first, tc.second), func(t *testing.T) {
			root := t.TempDir()
			for name, capacity := range map[string]int64{"sda": tc.first, "nvme0n1": tc.second} {
				dir := filepath.Join(root, name)
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "size"), []byte(fmt.Sprint(capacity/512)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			d := renderData{StorageGuard: "verified storage scope"}
			if tc.second != 0 {
				d.RAIDSetup = "verified RAID pair"
			}
			source, err := render("storage-sizing.sh", d)
			if err != nil {
				t.Fatal(err)
			}
			source = strings.ReplaceAll(source, "/sys/class/block", root)
			cmd := exec.Command("/bin/sh", "-eu")
			cmd.Stdin = strings.NewReader("disk=sda; disk2=nvme0n1\n" + source + "\nprintf 'SIZES=%s\\n' \"$VC_STORAGE_SIZES_MIB\"\n")
			out, err := cmd.CombinedOutput()
			if tc.want == "" {
				if err == nil || strings.Contains(string(out), "SIZES=") {
					t.Fatalf("unsafe capacity accepted: %s", out)
				}
			} else if err != nil || !strings.HasSuffix(string(out), "SIZES="+tc.want+"\n") {
				t.Fatalf("wrong capacity tier: %v %s", err, out)
			}
		})
	}
}

// Save every native selected-install hook for executable, capacity-dependent
// validation. These fixtures never contain production credentials or devices.
func TestLinuxCapacityLayoutFiles(t *testing.T) {
	for _, p := range profiles {
		if p.OS == "windows" {
			continue
		}
		for _, firmware := range []string{"uefi", "bios"} {
			s := &Server{config: Config{PublicURL: "https://controller.invalid"}}
			job := PrivateJob{Job: Job{ID: "storage-fixture", OS: p.OS, Profile: p.ID, PXEEntryID: "test", Machine: Machine{Name: "QA-STORAGE", MAC: "52:54:00:25:01:01", DiskSerial: "QA-FIRST", Firmware: firmware}}, Request: JobRequest{Username: "deployadmin", PasswordHash: "$6$test$" + strings.Repeat("a", 86)}, Token: "fixture"}
			files, err := s.renderFiles(job)
			if err != nil {
				t.Fatal(p.ID, firmware, err)
			}
			guard := files["preinstall.sh"]
			capacity := strings.Index(guard, "vc_storage_sizes \"$disk\"")
			claim := strings.Index(guard, `"state":"installing"`)
			if capacity < 0 || claim < capacity {
				t.Fatal(p.ID, "capacity must be validated before claiming installation")
			}
			if strings.Contains(files["kickstart.cfg"], "autopart --") {
				t.Fatal(p.ID, "installer defaults would override the capacity layout")
			}
			if out := os.Getenv("VENTOY_TEST_OUTPUT"); out != "" {
				dir := filepath.Join(out, "storage-layout", p.ID, firmware)
				if err = os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"preinstall.sh", "user-data", "kickstart.cfg", "preseed.cfg"} {
					if body, ok := files[name]; ok {
						if err = os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		}
	}
}
