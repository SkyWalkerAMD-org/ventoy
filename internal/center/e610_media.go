package center

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const liveInjectionHook = "live_injection_7ed136ec_7a61_4b54_adc3_ae494d5106ea/hook.sh"
const maxMediaInjection = 128 << 20

type e610MediaItem struct {
	Archive     string `json:"archive"`
	SHA256      string `json:"sha256"`
	ISOSize     int64  `json:"iso_size"`
	ISOModified int64  `json:"iso_mtime_ns"`
}
type e610MediaManifest struct {
	Version int                      `json:"version"`
	Images  map[string]e610MediaItem `json:"images"`
}
type injectionFile struct {
	Data []byte
	Mode int64
}

// Missing registry preserves existing behavior; corrupt registered media fails.
func (s *Server) e610Media(iso string) ([]byte, error) {
	dir := filepath.Join(s.config.PackageDir, "e610-media")
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	f, err := safeOpen(dir, "manifest.json", ".json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var manifest e610MediaManifest
	if err = json.NewDecoder(io.LimitReader(f, 2<<20)).Decode(&manifest); err != nil {
		return nil, err
	}
	if manifest.Version != 1 {
		return nil, errors.New("unsupported E610 media manifest")
	}
	item, ok := manifest.Images[iso]
	if !ok {
		return nil, nil
	}
	source, err := safeOpen(s.config.ISODir, iso, ".iso")
	if err != nil {
		return nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() != item.ISOSize || info.ModTime().UnixNano() != item.ISOModified {
		return nil, errors.New("ISO changed since E610 driver validation; rebuild its injection")
	}
	archive, err := safeOpen(dir, item.Archive, ".gz")
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	data, err := io.ReadAll(io.LimitReader(archive, maxMediaInjection+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxMediaInjection || fmt.Sprintf("%x", sha256.Sum256(data)) != item.SHA256 {
		return nil, errors.New("E610 injection checksum or size mismatch")
	}
	return data, nil
}

func readInjection(data []byte) (map[string]injectionFile, error) {
	files := map[string]injectionFile{}
	if len(data) == 0 {
		return files, nil
	}
	z, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	t := tar.NewReader(io.LimitReader(z, maxMediaInjection+1))
	var total int64
	for {
		h, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg || !fs.ValidPath(h.Name) || strings.ContainsAny(h.Name, "\\:\x00") || h.Size < 0 || h.Mode & ^int64(0777) != 0 {
			return nil, errors.New("unsafe E610 injection archive member")
		}
		total += h.Size
		if total > maxMediaInjection || len(files) >= 20000 {
			return nil, errors.New("injection archive too large")
		}
		if _, ok := files[h.Name]; ok {
			return nil, errors.New("duplicate injection archive member")
		}
		b, err := io.ReadAll(t)
		if err != nil {
			return nil, err
		}
		files[h.Name] = injectionFile{b, h.Mode}
	}
	return files, nil
}

func packInjection(files map[string]injectionFile) ([]byte, error) {
	var out bytes.Buffer
	z := gzip.NewWriter(&out)
	t := tar.NewWriter(z)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := files[n]
		if err := t.WriteHeader(&tar.Header{Name: n, Typeflag: tar.TypeReg, Mode: f.Mode, Size: int64(len(f.Data)), Format: tar.FormatUSTAR}); err != nil {
			return nil, err
		}
		if _, err := t.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := t.Close(); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Chain original iVentoy hook before E610; never discard PXE or preload logic.
func mergeMediaInjection(base, driver []byte) ([]byte, error) {
	return mergeInjectionHooks(base, driver, "ventoy-e610/previous-live-hook.sh", "ventoy-e610/driver-live-hook.sh")
}

// Separate hook namespaces allow the automatic installer, driver and inert
// setup preload to coexist. All other collisions must match byte-for-byte.
func mergeInjectionHooks(base, driver []byte, previous, current string) ([]byte, error) {
	files, err := readInjection(base)
	if err != nil {
		return nil, err
	}
	addition, err := readInjection(driver)
	if err != nil {
		return nil, err
	}
	if old, ok := files[liveInjectionHook]; ok {
		if next, exists := addition[liveInjectionHook]; exists {
			for _, n := range []string{previous, current} {
				if _, exists := files[n]; exists {
					return nil, errors.New("reserved injection hook path")
				}
				if _, exists := addition[n]; exists {
					return nil, errors.New("reserved injection hook path")
				}
			}
			files[previous] = old
			files[current] = next
			files[liveInjectionHook] = injectionFile{[]byte("#!/bin/sh\nset -eu\n/ventoy/busybox/sh /" + previous + "\n/ventoy/busybox/sh /" + current + "\n"), 0755}
			delete(addition, liveInjectionHook)
		}
	}
	for n, f := range addition {
		if old, ok := files[n]; ok && (old.Mode != f.Mode || !bytes.Equal(old.Data, f.Data)) {
			return nil, fmt.Errorf("injection member conflict: %s", n)
		}
		files[n] = f
	}
	return packInjection(files)
}

func (s *Server) addE610Media(iso, profile string, files map[string]string) error {
	driver, err := s.e610Media(iso)
	if err != nil || len(driver) == 0 {
		return err
	}
	name := bootInjectionName(profile)
	if name == "" {
		return errors.New("E610 injection is not supported for this installer")
	}
	merged, err := mergeMediaInjection([]byte(files[name]), driver)
	if err != nil {
		return err
	}
	files[name] = string(merged)
	return nil
}
