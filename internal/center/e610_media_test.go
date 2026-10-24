package center

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE610PreservesOriginalHooks(t *testing.T) {
	for _, mode := range []string{"manual", "pxe"} {
		t.Run(mode, func(t *testing.T) {
			var base []byte
			if mode == "manual" {
				var err error
				base, err = manualPreloadInjection([]byte("inert setup"))
				if err != nil {
					t.Fatal(err)
				}
			} else {
				b, err := ubuntuInjection(renderData{})
				if err != nil {
					t.Fatal(err)
				}
				base = []byte(b)
			}
			original, _ := readInjection(base)
			driver, _ := packInjection(map[string]injectionFile{liveInjectionHook: {[]byte("echo E610"), 0755}, "ventoy-e610/install-target.sh": {[]byte("install exact module"), 0755}})
			merged, err := mergeMediaInjection(base, driver)
			if err != nil {
				t.Fatal(err)
			}
			files, err := readInjection(merged)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(files["ventoy-e610/previous-live-hook.sh"].Data, original[liveInjectionHook].Data) {
				t.Fatal("original hook lost")
			}
			for n, f := range original {
				if n != liveInjectionHook && !bytes.Equal(files[n].Data, f.Data) {
					t.Fatal("existing member changed", n)
				}
			}
			wrapper := string(files[liveInjectionHook].Data)
			if !strings.Contains(wrapper, "set -eu") || strings.Index(wrapper, "previous-live-hook") > strings.Index(wrapper, "driver-live-hook") {
				t.Fatal("incorrect hook order")
			}
			if _, err = mergeMediaInjection(merged, driver); err == nil {
				t.Fatal("double composition must fail")
			}
		})
	}
}

func TestE610RegistryIdentityAndIntegrity(t *testing.T) {
	s := fixture(t)
	if data, err := s.e610Media("test.iso"); err != nil || len(data) != 0 {
		t.Fatal("absent registry", err)
	}
	dir := filepath.Join(s.config.PackageDir, "e610-media")
	os.Mkdir(dir, 0700)
	archive, _ := packInjection(map[string]injectionFile{"ventoy-e610/verify.sh": {[]byte("verified"), 0755}})
	os.WriteFile(filepath.Join(dir, "driver.tar.gz"), archive, 0600)
	info, _ := os.Stat(filepath.Join(s.config.ISODir, "test.iso"))
	item := e610MediaItem{Archive: "driver.tar.gz", SHA256: fmt.Sprintf("%x", sha256.Sum256(archive)), ISOSize: info.Size(), ISOModified: info.ModTime().UnixNano()}
	write := func(v e610MediaItem) {
		b, _ := json.Marshal(e610MediaManifest{Version: 1, Images: map[string]e610MediaItem{"test.iso": v}})
		os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600)
	}
	write(item)
	if data, err := s.e610Media("test.iso"); err != nil || !bytes.Equal(data, archive) {
		t.Fatal("valid registry", err)
	}
	bad := item
	bad.ISOSize++
	write(bad)
	if _, err := s.e610Media("test.iso"); err == nil {
		t.Fatal("changed ISO accepted")
	}
	bad = item
	bad.Archive = "../driver.tar.gz"
	write(bad)
	if _, err := s.e610Media("test.iso"); err == nil {
		t.Fatal("escaped archive accepted")
	}
	write(item)
	os.WriteFile(filepath.Join(dir, "driver.tar.gz"), []byte("corrupt"), 0600)
	if _, err := s.e610Media("test.iso"); err == nil {
		t.Fatal("corrupt driver accepted")
	}
}

func TestE610RejectsConflictingArchive(t *testing.T) {
	a, _ := packInjection(map[string]injectionFile{"same": {[]byte("a"), 0644}})
	b, _ := packInjection(map[string]injectionFile{"same": {[]byte("b"), 0644}})
	if _, err := mergeMediaInjection(a, b); err == nil {
		t.Fatal("conflict accepted")
	}
	bad, _ := packInjection(map[string]injectionFile{"../outside": {[]byte("a"), 0644}})
	if _, err := mergeMediaInjection(nil, bad); err == nil {
		t.Fatal("traversal accepted")
	}
}
