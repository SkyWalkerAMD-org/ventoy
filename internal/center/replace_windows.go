package center

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func windowsPath(path string) (*uint16, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(p, `\\?\`) {
		if strings.HasPrefix(p, `\\`) {
			p = `\\?\UNC\` + p[2:]
		} else {
			p = `\\?\` + p
		}
	}
	return syscall.UTF16PtrFromString(p)
}

func replaceSnapshot(source, target string) (bool, error) {
	src, err := windowsPath(source)
	if err != nil {
		return false, err
	}
	dst, err := windowsPath(target)
	if err != nil {
		return false, err
	}
	// REPLACE_EXISTING | WRITE_THROUGH. The temporary file was already flushed.
	ok, _, callErr := moveFileEx.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(dst)), uintptr(0x1|0x8))
	if ok == 0 {
		return false, callErr
	}
	return true, nil
}
