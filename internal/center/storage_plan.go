package center

import (
	"errors"
	"fmt"
)

// StoragePlan is an explicit destructive layout, bound to the inventoried
// serials rather than kernel names or a historical disk-number selection.
// Absence retains the existing single-disk behavior.
type StoragePlan struct {
	Mode        string   `json:"mode"`
	DiskSerials []string `json:"disk_serials"`
}

const storageRAID1LVM = "raid1-lvm"

func validateStorageShape(plan *StoragePlan) error {
	if plan == nil {
		return nil
	}
	if plan.Mode != storageRAID1LVM || len(plan.DiskSerials) != 2 || plan.DiskSerials[0] == plan.DiskSerials[1] {
		return errors.New("RAID1 + LVM requires exactly two different disk serials")
	}
	for _, serial := range plan.DiskSerials {
		if !serialRE.MatchString(serial) {
			return errors.New("Invalid RAID member serial")
		}
	}
	return nil
}

func raidInstaller(p Profile) bool {
	return p.Installer == "autoinstall" || p.Installer == "preseed" || p.Installer == "kickstart"
}

func validateStoragePlan(plan *StoragePlan, disks []PXEDisk) error {
	if plan == nil {
		return nil
	}
	if err := validateStorageShape(plan); err != nil {
		return err
	}
	for _, serial := range plan.DiskSerials {
		if !serialRE.MatchString(serial) {
			return errors.New("Invalid RAID member serial")
		}
		matches := 0
		for _, disk := range disks {
			if disk.Serial != serial {
				continue
			}
			matches++
			if disk.Location != "internal" || disk.Size < 24<<30 {
				return errors.New("RAID members must be internal disks of at least 24 GiB")
			}
			// The eventual installer guard independently verifies full LVM/MD
			// membership before any destructive claim. Mounts, swap, external
			// disks, ambiguous identifiers and unknown blocks cannot be bypassed.
			if disk.Blocked != "" && disk.Blocked != "Existing LVM/RAID: prepare storage separately" && disk.Blocked != "Existing LVM or RAID" && disk.Blocked != "Active RAID or LVM" {
				return fmt.Errorf("RAID member cannot be selected: %s", disk.Blocked)
			}
		}
		if matches != 1 {
			return errors.New("Each RAID member serial must match exactly one disk")
		}
	}
	return nil
}

func storageConfirmation(plan StoragePlan) string {
	// Only validated fixed-layout plans reach this function; member order also
	// identifies the primary boot disk and cannot silently change after yes.
	return "ERASE RAID1 " + plan.DiskSerials[0] + " " + plan.DiskSerials[1]
}
