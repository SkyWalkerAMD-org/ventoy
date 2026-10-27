package center

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestDebianLateCommandArgumentBudget(t *testing.T) {
	// preseed_command logs the entire hook as a single logger argument before
	// execution. RAID + root SSH + the real CA crossed Linux's 128 KiB limit.
	for _, profile := range []string{"debian-10", "debian-11", "debian-12", "debian-13"} {
		for _, fs := range []string{"ext4", "xfs"} {
			s := &Server{config: Config{PublicURL: "https://controller.invalid"}, ca: []byte(strings.Repeat("certificate-fixture\n", 140))}
			hash := "$6$test$" + strings.Repeat("a", 86)
			j := PrivateJob{Job: Job{ID: strings.Repeat("a", 24), OS: "debian", Profile: profile, PXEEntryID: "test", Machine: Machine{Name: "QA-FS", MAC: "52:54:00:25:01:01", DiskSerial: "QA-FIRST", Firmware: "uefi"}}, Request: JobRequest{Username: "user", PasswordHash: hash, RootPasswordHash: hash, Filesystem: fs, StoragePlan: &StoragePlan{Mode: storageRAID1LVM, DiskSerials: []string{"QA-FIRST", "QA-SECOND"}}}, Token: strings.Repeat("b", 64)}
			files, err := s.renderFiles(j)
			if err != nil {
				t.Fatal(profile, fs, err)
			}
			for _, line := range strings.Split(strings.ReplaceAll(files["preseed.cfg"], "\\\n", " "), "\n") {
				if strings.HasPrefix(line, "d-i preseed/late_command string ") && len(line) >= 120*1024 {
					t.Fatalf("%s %s: preseed logger/exec argument too large: %d", profile, fs, len(line))
				}
			}
		}
	}
	// Preserve the complete payload, including literal shell metacharacters.
	script := "#!/bin/sh\n" + strings.Repeat("printf '%s\\n' 'literal $HOME and 中文'\n", 5000)
	for _, profile := range []string{"debian-12", "debian-13"} {
		text, err := render("preseed.cfg", renderData{Target: Profile{ID: profile}, DebianLate: script})
		if err != nil {
			t.Fatal(err)
		}
		match := regexp.MustCompile(`printf '%s' '([A-Za-z0-9+/=]+)' \| base64 -d \| gunzip -c`).FindStringSubmatch(text)
		if len(match) != 2 || decodeTemplateScript(t, match[1]) != script {
			t.Fatal("compressed late hook did not preserve its payload", profile)
		}
	}
}

func TestPXEFilesystemSelection(t *testing.T) {
	for _, fs := range []string{"", "ext4", "xfs"} {
		t.Run("selected-"+fs, func(t *testing.T) {
			s, e := pxeFixture(t, "ubuntu-22.04")
			inv := pxeInventory()
			ticket := pxeTicket(t, s, e, inv)
			if ticket["filesystem"] != "ext4" {
				t.Fatal("legacy entry must default to ext4")
			}
			body := map[string]string{"ticket": ticket["ticket"], "disk_serial": inv.Disks[0].Serial, "filesystem": fs}
			for _, invalid := range []string{"XFS", "ntfs", "ext4;reboot", " xfs", "btrfs"} {
				body["filesystem"] = invalid
				mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 400)
				if len(s.store.snapshot().Jobs) != 0 {
					t.Fatal("invalid filesystem issued a destructive task")
				}
			}
			body["filesystem"] = fs
			mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 200)
			want, _ := linuxFilesystem(fs)
			j := s.store.snapshot().Jobs[0]
			if j.Filesystem != want || j.Request.Filesystem != want {
				t.Fatal("selection was not persisted")
			}
			restarted, err := NewServer(s.config)
			if err != nil {
				t.Fatal(err)
			}
			if restarted.store.snapshot().Jobs[0].Filesystem != want {
				t.Fatal("selection lost after restart")
			}
		})
	}
}

func TestNewInstallationFilesystemDefaults(t *testing.T) {
	for _, tc := range []struct{ profile, os, want string }{
		{"rocky-9", "rocky", "xfs"}, {"centos-7.9", "centos", "xfs"},
		{"rhel-8", "rhel", "xfs"}, {"almalinux-9", "almalinux", "xfs"},
		{"ubuntu-22.04", "ubuntu", "ext4"}, {"debian-12", "debian", "ext4"},
	} {
		t.Run(tc.os, func(t *testing.T) {
			for _, explicit := range []string{"", "ext4", "xfs"} {
				s, e := pxeFixture(t, tc.profile)
				info := pxeTicket(t, s, e, pxeInventory())
				if info["filesystem"] != tc.want {
					t.Fatalf("menu default = %q; want %q", info["filesystem"], tc.want)
				}
				body := map[string]string{"ticket": info["ticket"], "disk_serial": "DISK-DEMO-001"}
				want := tc.want
				if explicit != "" {
					body["filesystem"], want = explicit, explicit
				}
				mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 200)
				j := s.store.snapshot().Jobs[0]
				if j.Filesystem != want || j.Request.Filesystem != want {
					t.Fatal("PXE did not snapshot the effective filesystem", j.Filesystem)
				}
			}
			s, _ := pxeFixture(t, tc.profile)
			s.ca = []byte("fixture CA")
			j := injectionFixtureJob(t, s, tc.profile)
			if j.Filesystem != tc.want || j.Request.Filesystem != tc.want {
				t.Fatal("management draft did not snapshot its family default")
			}
		})
	}
}

func TestLegacyFilesystemAuthorizationSurvivesNewDefault(t *testing.T) {
	for _, sent := range []string{"", "ext4"} {
		s, e := pxeFixture(t, "rocky-9")
		j := createFixtureJob(t, s, "rocky", false)
		// Simulate a persisted pre-alpha.40 draft whose omitted value meant ext4.
		if err := s.store.update(func(st *State) error {
			old := findJob(st, j.ID)
			old.Request.Filesystem, old.Filesystem = "", ""
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		j = arm(t, s, j)
		info := pxeTicket(t, s, e, pxeInventory())
		if info["filesystem"] != "xfs" || info["preset_filesystem"] != "ext4" {
			t.Fatal("new default changed an existing authorization", info)
		}
		body := map[string]string{"ticket": info["ticket"], "disk_serial": j.Machine.DiskSerial, "filesystem": "xfs"}
		mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
		body["filesystem"] = sent
		mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 200)
		if s.store.snapshot().Jobs[0].Filesystem != "ext4" {
			t.Fatal("legacy authorized filesystem changed")
		}
	}
}

func TestPXEEntryFilesystemOverride(t *testing.T) {
	s, e := pxeFixture(t, "rocky-9")
	if err := s.store.update(func(st *State) error {
		findPXEEntry(st, e.ID).Request.Filesystem = "ext4"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	info := pxeTicket(t, s, e, pxeInventory())
	if info["filesystem"] != "ext4" {
		t.Fatal("explicit entry setting lost")
	}
	mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, map[string]string{"ticket": info["ticket"], "disk_serial": "DISK-DEMO-001"}), 200)
	if s.store.snapshot().Jobs[0].Request.Filesystem != "ext4" {
		t.Fatal("omitted client selection ignored entry override")
	}
}

func TestPresetFilesystemCannotChangeAtPXE(t *testing.T) {
	s, e := pxeFixture(t, "rocky-9", pxeConsole)
	j := createFixtureJob(t, s, "rocky", false)
	if err := s.store.update(func(st *State) error {
		for i := range st.Jobs {
			if st.Jobs[i].ID == j.ID {
				st.Jobs[i].Request.Filesystem = "xfs"
				st.Jobs[i].Filesystem = "xfs"
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	j = arm(t, s, j)
	info := pxeTicket(t, s, e, pxeInventory())
	if info["preset_filesystem"] != "xfs" {
		t.Fatal("preset filesystem not advertised")
	}
	body := map[string]string{"ticket": info["ticket"], "disk_serial": j.Machine.DiskSerial, "filesystem": "ext4"}
	mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
	body["filesystem"] = "xfs"
	mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 200)
}

func TestFilesystemInstallerFiles(t *testing.T) {
	for _, p := range profiles {
		if p.OS == "windows" {
			continue
		}
		for _, fs := range []string{"ext4", "xfs"} {
			for _, mode := range []string{"single", "raid"} {
				if mode == "raid" && !raidInstaller(p) {
					continue
				}
				for _, firmware := range []string{"uefi", "bios"} {
					s := &Server{config: Config{PublicURL: "https://controller.invalid"}}
					j := PrivateJob{Job: Job{ID: "fs-fixture", OS: p.OS, Profile: p.ID, PXEEntryID: "test", Machine: Machine{Name: "QA-FS", MAC: "52:54:00:25:01:01", DiskSerial: "QA-FIRST", Firmware: firmware}}, Request: JobRequest{Username: "user", PasswordHash: "$6$test$" + strings.Repeat("a", 86), Filesystem: fs}, Token: "fixture"}
					if mode == "raid" {
						j.Request.StoragePlan = &StoragePlan{Mode: storageRAID1LVM, DiskSerials: []string{"QA-FIRST", "QA-SECOND"}}
					}
					files, err := s.renderFiles(j)
					if err != nil {
						t.Fatal(p.ID, fs, mode, firmware, err)
					}
					guard := files["preinstall.sh"]
					if fs == "xfs" {
						check := strings.Index(guard, "command -v mkfs.xfs")
						claim := strings.Index(guard, `"state":"installing"`)
						if check < 0 || check >= claim {
							t.Fatal("formatter support must be checked before claim", p.ID)
						}
					}
					if out := os.Getenv("VENTOY_TEST_OUTPUT"); out != "" {
						dir := filepath.Join(out, "filesystems", fs, mode, p.ID, firmware)
						if err = os.MkdirAll(dir, 0700); err != nil {
							t.Fatal(err)
						}
						for _, name := range []string{"preinstall.sh", "user-data", "kickstart.cfg", "preseed.cfg", "filesystem-check.sh"} {
							if data, ok := files[name]; ok {
								if err = os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
									t.Fatal(err)
								}
							}
						}
						meta, _ := json.Marshal(map[string]string{"filesystem": fs, "profile": p.ID, "mode": mode})
						if err = os.WriteFile(filepath.Join(dir, "case.json"), meta, 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		}
	}
}

func TestFilesystemCheckRequiresExactMounts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses isolated shell/findmnt fixture")
	}
	for _, fs := range []string{"ext4", "xfs"} {
		data := renderData{PrivateJob: PrivateJob{Job: Job{Machine: Machine{Firmware: "uefi"}}}, DataFilesystem: fs}
		source, err := render("filesystem-check.sh", data)
		if err != nil {
			t.Fatal(err)
		}
		mounts := "/ " + fs + "\n/tmp " + fs + "\n/var " + fs + "\n/home " + fs + "\n/boot ext4\n/boot/efi vfat\n"
		for _, tc := range []struct {
			name, mounts string
			success      bool
		}{
			{"correct", mounts, true},
			{"missing-home", strings.ReplaceAll(mounts, "/home "+fs+"\n", ""), false},
			{"wrong-root", strings.Replace(mounts, "/ "+fs, "/ btrfs", 1), false},
			{"wrong-boot", strings.ReplaceAll(mounts, "/boot ext4", "/boot xfs"), false},
			{"missing-efi", strings.ReplaceAll(mounts, "/boot/efi vfat\n", ""), false},
		} {
			dir := t.TempDir()
			if err = os.WriteFile(filepath.Join(dir, "findmnt"), []byte("#!/bin/sh\ncat <<'MOUNTS'\n"+tc.mounts+"MOUNTS\n"), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("/bin/sh", "-eu")
			cmd.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin")
			cmd.Stdin = strings.NewReader(source)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.success {
				t.Fatalf("%s %s: %v %s", fs, tc.name, err, out)
			}
		}
	}
}
