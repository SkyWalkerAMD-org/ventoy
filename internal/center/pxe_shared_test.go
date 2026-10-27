package center

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPXESharedISOAndLegacyMigration(t *testing.T) {
	for _, profile := range []string{"centos-7.9", "rocky-10", "ubuntu-22.04", "ubuntu-24.04", "debian-12"} {
		t.Run(profile, func(t *testing.T) {
			s, e := pxeFixture(t, profile)
			if e.Preparation.ISOPath != "iso/test.iso" {
				t.Fatal("menu did not select the original ISO")
			}
			source := filepath.Join(s.config.ISODir, e.ISO)
			before, err := os.Stat(source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(s.config.IVentoyRoot, "iso", "ventoy-center")); !os.IsNotExist(err) {
				t.Fatal("shared menu created an extra ISO tree", err)
			}
			url := "/api/pxe/entries/" + e.ID
			mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 200)
			mustCode(t, request(s, "POST", url+"/enabled", s.admin, map[string]bool{"enabled": false}), 200)
			after, err := os.Stat(source)
			if err != nil || !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
				t.Fatal("preparing/disabling modified the source ISO", err)
			}
			// Do not silently switch an already bound legacy alias; deployment
			// must migrate both the native binding and durable preparation.
			legacy := "iso/ventoy-center/PXE-Auto/test/image.iso"
			if err := s.store.update(func(st *State) error { findPXEEntry(st, e.ID).Preparation.ISOPath = legacy; return nil }); err != nil {
				t.Fatal(err)
			}
			mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 409)
			st := s.store.snapshot()
			if findPXEEntry(&st, e.ID).Preparation.ISOPath != legacy {
				t.Fatal("legacy preparation changed")
			}
		})
	}
}
