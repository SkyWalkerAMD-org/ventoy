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
const storageSingleRebuild = "single-rebuild"

func existingStorage(reason string) bool {
	switch reason {
	case "Existing LVM/RAID: prepare storage separately", "Existing LVM or RAID", "Active RAID or LVM":
		return true
	}
	return false
}

func raidPlan(plan *StoragePlan) bool { return plan != nil && plan.Mode == storageRAID1LVM }

func rebuildInstaller(p Profile) bool { return raidInstaller(p) || p.Installer == "ubiquity" }

// An omitted value keeps already-created jobs on their historical ext4 layout.
// This allowlist also prevents template/recipe injection through the API.
func linuxFilesystem(value string) (string, error) {
	switch value {
	case "", "ext4":
		return "ext4", nil
	case "xfs":
		return "xfs", nil
	default:
		return "", errors.New("Linux filesystem must be ext4 or xfs")
	}
}

// Resolve a new installation's default before storing its authorization. Stored
// jobs use linuxFilesystem instead so a release cannot change their layout.
func newLinuxFilesystem(value, osName string) (string, error) {
	if value == "" {
		switch osName {
		case "rhel", "centos", "rocky", "almalinux":
			value = "xfs"
		}
	}
	return linuxFilesystem(value)
}

func validateStorageShape(plan *StoragePlan) error {
	if plan == nil {
		return nil
	}
	switch plan.Mode {
	case storageRAID1LVM:
		if len(plan.DiskSerials) != 2 || plan.DiskSerials[0] == plan.DiskSerials[1] {
			return errors.New("RAID1 + LVM requires exactly two different disk serials")
		}
	case storageSingleRebuild:
		if len(plan.DiskSerials) != 1 {
			return errors.New("A single-disk rebuild requires exactly one disk serial")
		}
	default:
		return errors.New("Unknown storage rebuild mode")
	}
	for _, serial := range plan.DiskSerials {
		if !serialRE.MatchString(serial) {
			return errors.New("Invalid rebuild disk serial")
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
			return errors.New("Invalid rebuild disk serial")
		}
		matches := 0
		for _, disk := range disks {
			if disk.Serial != serial {
				continue
			}
			matches++
			if disk.Location != "internal" || disk.Size < 120000000000 {
				return errors.New("Rebuild targets must be internal disks of at least 120 GB")
			}
			// The eventual installer guard independently verifies full LVM/MD
			// membership before any destructive claim. Mounts, swap, external
			// disks, ambiguous identifiers and unknown blocks cannot be bypassed.
			if disk.Blocked != "" && !existingStorage(disk.Blocked) {
				return fmt.Errorf("Rebuild target cannot be selected: %s", disk.Blocked)
			}
		}
		if matches != 1 {
			return errors.New("Each rebuild target serial must match exactly one disk")
		}
	}
	return nil
}

func storageConfirmation(plan StoragePlan) string {
	if plan.Mode == storageSingleRebuild {
		return "ERASE REBUILD " + plan.DiskSerials[0]
	}
	// Only validated fixed-layout plans reach this function; member order also
	// identifies the primary boot disk and cannot silently change after yes.
	return "ERASE RAID1 " + plan.DiskSerials[0] + " " + plan.DiskSerials[1]
}
