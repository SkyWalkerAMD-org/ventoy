package center

import (
	"io/fs"
	"strings"
	"testing"
)

// Both manual and automatic installers use this embedded asset set. The
// independently invoked P95 installer must never be part of a full setup.
func TestP95PriorityNotEmbeddedInSetup(t *testing.T) {
	err := fs.WalkDir(manualAssets, "assets/manual", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(name), "p95") {
			t.Errorf("standalone P95 asset embedded in full setup: %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
