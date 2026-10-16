package center

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func injectionFixtureJob(t *testing.T, s *Server, profile string) PrivateJob {
	t.Helper()
	w := request(s, "POST", "/api/machines", s.admin, Machine{Name: "C7-TEST", MAC: "02:00:00:00:07:09", Serial: "C7-ASSET", DiskSerial: "C7-DISK", Firmware: "uefi"})
	mustCode(t, w, 201)
	var m Machine
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	p, _ := findProfile(profile)
	w = request(s, "POST", "/api/jobs", s.admin, profileRequest(p, m.ID))
	mustCode(t, w, 201)
	var j Job
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	return savedJob(s, j.ID)
}

func TestKickstartInjectionBundle(t *testing.T) {
	for _, profile := range []string{"centos-7.9", "rocky-8", "rocky-9"} {
		t.Run(profile, func(t *testing.T) {
			injectionName := bootInjectionName(profile)
			s := fixture(t)
			j := arm(t, s, injectionFixtureJob(t, s, profile))
			files, err := s.renderFiles(j)
			if err != nil {
				t.Fatal(err)
			}
			packed, err := s.renderBundle(j)
			if err != nil {
				t.Fatal(err)
			}
			z, err := zip.NewReader(bytes.NewReader(packed), int64(len(packed)))
			if err != nil {
				t.Fatal(err)
			}
			f, err := z.Open(injectionName)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil || string(data) != files[injectionName] {
				t.Fatal("binary injection archive changed during ZIP export", err)
			}
			gz, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			defer gz.Close()
			tr := tar.NewReader(gz)
			h, err := tr.Next()
			if err != nil {
				t.Fatal(err)
			}
			if h.Name != "etc/cmdline.d/99-ventoy-center-ks.conf" || h.Typeflag != tar.TypeReg || h.Mode != 0644 {
				t.Fatalf("unexpected initramfs write: %+v", h)
			}
			content, err := io.ReadAll(tr)
			if err != nil || string(content) != "inst.ks=file:/ventoy/autoinstall\n" {
				t.Fatal("missing dracut Kickstart entry", err)
			}
			if _, err = tr.Next(); err != io.EOF {
				t.Fatal("archive must not contain disk operations or unrelated files", err)
			}
			again, err := s.renderFiles(j)
			if err != nil || files[injectionName] != again[injectionName] {
				t.Fatal("archive is not deterministic; staging retries would fail", err)
			}
			if !strings.Contains(files["README.txt"], injectionName) {
				t.Fatal("bundle omits required injection binding instructions")
			}
		})
	}
}

func TestKickstartInjectionPreparation(t *testing.T) {
	for _, profile := range []string{"centos-7.9", "rocky-8", "rocky-9", "ubuntu-22.04"} {
		t.Run(profile, func(t *testing.T) {
			injectionName := bootInjectionName(profile)
			s := stagingFixture(t)
			j := arm(t, s, injectionFixtureJob(t, s, profile))
			url := "/api/jobs/" + j.ID
			w := request(s, "POST", url+"/prepare", s.admin, nil)
			mustCode(t, w, 200)
			var plan PreparationPlan
			if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
				t.Fatal(err)
			}
			j = savedJob(s, j.ID)
			expected := filepath.Join(s.config.IVentoyRoot, filepath.Dir(filepath.FromSlash(j.Preparation.ScriptPath)), injectionName)
			if plan.InjectionSelection != expected || j.Preparation.Files[injectionName] == "" {
				t.Fatal("injection archive missing from plan or durable cleanup inventory")
			}
			if _, err := os.Stat(expected); err != nil {
				t.Fatal(err)
			}
			mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 200)
			// Old prepared jobs must not advertise a file that does not exist.
			delete(j.Preparation.Files, injectionName)
			oldPlan := s.plan(j)
			if oldPlan.CanPrepare || oldPlan.InjectionSelection != "" || oldPlan.Reason == "" {
				t.Fatal("old task incorrectly presented as having the compatibility file")
			}
			mustCode(t, request(s, "POST", url+"/cancel", s.admin, nil), 200)
			mustCode(t, request(s, "POST", url+"/cleanup", s.admin, nil), 200)
			if _, err := os.Stat(expected); !os.IsNotExist(err) {
				t.Fatal("injection archive left behind after cleanup")
			}
			if _, err := os.Stat(filepath.Join(s.config.ISODir, j.ISO)); err != nil {
				t.Fatal("cleanup removed the original ISO", err)
			}
		})
	}
}
