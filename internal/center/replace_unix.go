//go:build !windows

package center

import (
	"os"
	"path/filepath"
)

func replaceSnapshot(source, target string) (bool, error) {
	if err := os.Rename(source, target); err != nil {
		return false, err
	}
	dir, err := os.Open(filepath.Dir(target))
	if err != nil {
		return true, err
	}
	defer dir.Close()
	return true, dir.Sync()
}
