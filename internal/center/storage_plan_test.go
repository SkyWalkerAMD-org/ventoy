package center

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRAIDPlanRejectsAmbiguousOrOutOfScopeDisks(t *testing.T) {
	plan := StoragePlan{Mode: storageRAID1LVM, DiskSerials: []string{"Q0Q", "Q0R"}}
	disks := []PXEDisk{
		{Serial: "Q0Q", Location: "internal", Size: 447 << 30},
		{Serial: "Q0R", Location: "internal", Size: 447 << 30},
	}
	if err := validateStoragePlan(&plan, disks); err != nil {
		t.Fatal(err)
	}
	disks[0].Blocked = "Active swap"
	if err := validateStoragePlan(&plan, disks); err != nil {
		t.Fatal(err)
	}
	for _, reason := range []string{"Mounted disk", "Mounted disk or active swap", "Read-only disk", "Duplicate disk serial", "Unknown storage state"} {
		bad := append([]PXEDisk(nil), disks...)
		bad[1].Blocked = reason
		if validateStoragePlan(&plan, bad) == nil {
			t.Fatal("accepted blocked member", reason)
		}
	}
	if validateStoragePlan(&plan, append(disks, disks[1])) == nil || validateStoragePlan(&plan, disks[:1]) == nil {
		t.Fatal("ambiguous or missing member accepted")
	}
	for _, location := range []string{"external", "unknown", ""} {
		bad := append([]PXEDisk(nil), disks...)
		bad[1].Location = location
		if validateStoragePlan(&plan, bad) == nil {
			t.Fatal("unverified disk accepted", location)
		}
	}
	for _, serials := range [][]string{{"Q0Q"}, {"Q0Q", "Q0Q"}, {"Q0Q", "Q0R", "Q0S"}} {
		bad := plan
		bad.DiskSerials = serials
		if validateStoragePlan(&bad, disks) == nil {
			t.Fatal("invalid RAID cardinality accepted")
		}
	}
}

func TestPXERAIDAuthorization(t *testing.T) {
	s, e := pxeFixture(t, "ubuntu-22.04")
	inv := pxeInventory()
	inv.Disks = append(inv.Disks, PXEDisk{Name: "sdb", Serial: "SECOND", Size: 128 << 30, Location: "internal"}, PXEDisk{Name: "sdc", Serial: "KEEP", Size: 128 << 30, Location: "internal"})
	inv.Disks[0].Blocked = "Existing LVM/RAID: prepare storage separately"
	ticket := pxeTicket(t, s, e, inv)
	plan := StoragePlan{Mode: storageRAID1LVM, DiskSerials: []string{inv.Disks[0].Serial, "SECOND"}}
	body := map[string]any{"ticket": ticket["ticket"], "disk_serial": inv.Disks[0].Serial, "storage_plan": plan}
	for _, text := range []string{"", "yes", "ERASE " + inv.Disks[0].Serial, "ERASE RAID1 SECOND " + inv.Disks[0].Serial} {
		body["confirmation"] = text
		mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
	}
	body["confirmation"] = storageConfirmation(plan)
	result := request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body)
	mustCode(t, result, 200)
	var payload map[string]string
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	if len(st.Jobs) != 1 || st.Jobs[0].StoragePlan == nil || st.Jobs[0].Request.StoragePlan == nil || st.Jobs[0].State != "ready" {
		t.Fatal("RAID authorization not retained")
	}
	if strings.Contains(payload["preinstall"], "'KEEP'") {
		t.Fatal("unselected disk included")
	}
	mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
	if strings.Contains(payload["config"], "early-commands:") {
		t.Fatal("duplicate job claim")
	}
}

func TestRAIDInstallerFiles(t *testing.T) {
	for _, p := range profiles {
		if !raidInstaller(p) {
			continue
		}
		for _, firmware := range []string{"uefi", "bios"} {
			t.Run(p.ID+"-"+firmware, func(t *testing.T) {
				s := &Server{config: Config{PublicURL: "https://controller.invalid"}}
				plan := &StoragePlan{Mode: storageRAID1LVM, DiskSerials: []string{"QA-FIRST", "QA-SECOND"}}
				job := PrivateJob{Job: Job{ID: "raid-fixture", OS: p.OS, Profile: p.ID, PXEEntryID: "test", Machine: Machine{Name: "QA-RAID", MAC: "52:54:00:25:01:01", DiskSerial: "QA-FIRST", Firmware: firmware}}, Request: JobRequest{Username: "deployadmin", PasswordHash: "$6$test$" + strings.Repeat("a", 86), StoragePlan: plan}, Token: "fixture"}
				files, err := s.renderFiles(job)
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"preinstall.sh", "raid-installed.sh"} {
					if !strings.Contains(files[name], "QA-FIRST") || !strings.Contains(files[name], "QA-SECOND") {
						t.Fatal("missing member binding", name)
					}
				}
				if p.Installer == "kickstart" && strings.Contains(files["kickstart.cfg"], "autopart --") {
					t.Fatal("autopart overrides RAID")
				}
				if out := os.Getenv("VENTOY_TEST_OUTPUT"); out != "" {
					dir := filepath.Join(out, "raid", p.ID, firmware)
					if err = os.MkdirAll(dir, 0700); err != nil {
						t.Fatal(err)
					}
					for name, body := range files {
						if !strings.Contains(name, "/") && !strings.HasSuffix(name, ".gz") {
							if err = os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			})
		}
	}
}
