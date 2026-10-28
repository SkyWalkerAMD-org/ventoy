package center

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPXESingleRebuildRequiresLocalBoundConfirmation(t *testing.T) {
	s, e := pxeFixture(t, "centos-7.9")
	inv := pxeInventory()
	inv.Disks[0].Blocked = "Existing LVM/RAID: prepare storage separately"
	inv.Disks = append(inv.Disks, PXEDisk{Name: "sdb", Serial: "USB-KEEP", Size: 480000000000, Location: "external", Blocked: "External disk"})
	ticket := pxeTicket(t, s, e, inv)
	if ticket["automatic_disk"] != "" {
		t.Fatal("old storage was selected without confirmation")
	}
	plan := StoragePlan{Mode: storageSingleRebuild, DiskSerials: []string{inv.Disks[0].Serial}}
	body := map[string]any{"ticket": ticket["ticket"], "disk_serial": inv.Disks[0].Serial, "storage_plan": plan}
	for _, confirmation := range []string{"", "yes", "ERASE " + inv.Disks[0].Serial, "ERASE REBUILD USB-KEEP"} {
		body["confirmation"] = confirmation
		mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
	}
	body["confirmation"] = storageConfirmation(plan)
	result := request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body)
	mustCode(t, result, 200)
	var payload map[string]string
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload["preinstall"], "single-disk rebuild scope verified") || strings.Contains(payload["preinstall"], "USB-KEEP") || strings.Contains(payload["preinstall"], "VC_RAID_KS") {
		t.Fatal("rebuild guard or scope incorrect")
	}
	st := s.store.snapshot()
	if len(st.Jobs) != 1 || st.Jobs[0].Request.StoragePlan.Mode != storageSingleRebuild {
		t.Fatal("rebuild authorization not retained")
	}
	mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
}

func TestSingleRebuildScopeValidation(t *testing.T) {
	plan := StoragePlan{Mode: storageSingleRebuild, DiskSerials: []string{"TARGET"}}
	disk := PXEDisk{Serial: "TARGET", Location: "internal", Size: 120000000000, Blocked: "Existing LVM or RAID"}
	if err := validateStoragePlan(&plan, []PXEDisk{disk}); err != nil {
		t.Fatal(err)
	}
	for _, blocked := range []string{"Mounted disk", "Active swap", "Read-only disk", "Unrecognized metadata"} {
		bad := disk
		bad.Blocked = blocked
		if validateStoragePlan(&plan, []PXEDisk{bad}) == nil {
			t.Fatal("unsafe disk accepted", blocked)
		}
	}
	for _, location := range []string{"external", "unknown", ""} {
		bad := disk
		bad.Location = location
		if validateStoragePlan(&plan, []PXEDisk{bad}) == nil {
			t.Fatal("unsafe location", location)
		}
	}
	for _, serials := range [][]string{nil, {"TARGET", "OTHER"}, {"TARGET", "TARGET"}} {
		bad := plan
		bad.DiskSerials = serials
		if validateStoragePlan(&bad, []PXEDisk{disk}) == nil {
			t.Fatal("invalid scope", serials)
		}
	}
	if validateStoragePlan(&plan, []PXEDisk{disk, disk}) == nil {
		t.Fatal("ambiguous serial")
	}
}

func TestSingleRebuildInstallerFiles(t *testing.T) {
	for _, p := range profiles {
		if !rebuildInstaller(p) {
			continue
		}
		for _, firmware := range []string{"bios", "uefi"} {
			t.Run(p.ID+"-"+firmware, func(t *testing.T) {
				s := &Server{config: Config{PublicURL: "https://controller.invalid"}}
				plan := &StoragePlan{Mode: storageSingleRebuild, DiskSerials: []string{"QA-FIRST"}}
				job := PrivateJob{Job: Job{ID: "rebuild-fixture", OS: p.OS, Profile: p.ID, PXEEntryID: "test", Machine: Machine{Name: "QA-REBUILD", MAC: "52:54:00:25:01:01", DiskSerial: "QA-FIRST", Firmware: firmware}}, Request: JobRequest{Username: "user", PasswordHash: "$6$test$" + strings.Repeat("a", 86), StoragePlan: plan}, Token: "fixture"}
				files, err := s.renderFiles(job)
				if err != nil {
					t.Fatal(err)
				}
				guard := files["preinstall.sh"]
				if !strings.Contains(guard, "single-disk rebuild scope verified") || strings.Contains(guard, "$disk2") || files["raid-installed.sh"] != "" {
					t.Fatal("single rebuild inherited RAID layout")
				}
				if p.Installer == "preseed" && !strings.Contains(guard, "partman-lvm/device_remove_lvm boolean true") {
					t.Fatal("missing native removal answers")
				}
				if out := os.Getenv("VENTOY_TEST_OUTPUT"); out != "" {
					dir := filepath.Join(out, "single-rebuild", p.ID, firmware)
					if err := os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					for name, body := range files {
						if !strings.Contains(name, "/") && !strings.HasSuffix(name, ".gz") {
							if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			})
		}
	}
}
