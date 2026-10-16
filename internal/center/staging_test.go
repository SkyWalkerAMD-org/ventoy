package center

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stagingFixture(t *testing.T) *Server {
	t.Helper()
	s := fixture(t)
	root := t.TempDir()
	for _, d := range []string{"iso", "user/scripts", "data"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"data/iventoy.dat": "test marker, not iVentoy", "iso/test.iso": "test image, never boot"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s.config.IVentoyRoot, s.config.ISODir = root, filepath.Join(root, "iso")
	return s
}

func savedJob(s *Server, id string) PrivateJob { st := s.store.snapshot(); return *findJob(&st, id) }

func TestPreparationLifecycle(t *testing.T) {
	s := stagingFixture(t)
	j := createFixtureJob(t, s, "rocky", false)
	url := "/api/jobs/" + j.ID
	mustCode(t, request(s, "GET", "/api/iventoy", "", nil), 401)
	mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 409)
	j = arm(t, s, j)
	mustCode(t, request(s, "POST", url+"/prepare", "", nil), 401)
	mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 200)
	j = savedJob(s, j.ID)
	if j.Preparation == nil || j.Preparation.PreparedAt.IsZero() {
		t.Fatal("missing completed receipt")
	}
	source, _ := os.Stat(filepath.Join(s.config.ISODir, j.ISO))
	alias, _ := os.Stat(filepath.Join(s.config.IVentoyRoot, j.Preparation.ISOPath))
	if !os.SameFile(source, alias) {
		t.Fatal("expected a hard link")
	}
	if len(j.Preparation.Files) != 6 || j.Preparation.Files[rockyInjectionName] == "" {
		t.Fatalf("incomplete inventory: %v", j.Preparation.Files)
	}
	mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 200)
	if !savedJob(s, j.ID).Preparation.PreparedAt.Equal(j.Preparation.PreparedAt) {
		t.Fatal("retry changed creation time")
	}
	mustCode(t, request(s, "POST", url+"/cleanup", s.admin, nil), 409)
	listing := request(s, "GET", "/api/overview", s.admin, nil)
	var overview struct{ Images []Media }
	json.Unmarshal(listing.Body.Bytes(), &overview)
	if len(overview.Images) != 1 || overview.Images[0].Path != "test.iso" {
		t.Fatalf("managed aliases in source catalog: %+v", overview.Images)
	}
	for _, secret := range []string{j.Token, j.Request.PasswordHash} {
		if strings.Contains(listing.Body.String(), secret) {
			t.Fatal("secret in overview")
		}
	}
	mustCode(t, request(s, "POST", url+"/cancel", s.admin, nil), 200)
	// Reload to ensure cleanup uses the durable inventory after secrets are erased.
	restarted, err := NewServer(s.config)
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, request(restarted, "POST", url+"/cleanup", s.admin, nil), 200)
	mustCode(t, request(restarted, "POST", url+"/cleanup", s.admin, nil), 200)
	if _, err = os.Stat(filepath.Join(s.config.IVentoyRoot, j.Preparation.ISOPath)); !os.IsNotExist(err) {
		t.Fatal("alias retained")
	}
	if current, err := os.Stat(filepath.Join(s.config.ISODir, j.ISO)); err != nil || !os.SameFile(source, current) {
		t.Fatal("original image changed")
	}
}

func TestPreparationFailureLeavesCleanupInventory(t *testing.T) {
	s := stagingFixture(t)
	j := arm(t, s, createFixtureJob(t, s, "rocky", false))
	url := "/api/jobs/" + j.ID
	// Force an external filesystem failure after the preparation intent is saved.
	block := filepath.Join(s.config.IVentoyRoot, managedISO)
	if err := os.WriteFile(block, []byte("operator file"), 0600); err != nil {
		t.Fatal(err)
	}
	mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 409)
	j = savedJob(s, j.ID)
	if j.Preparation == nil || !j.Preparation.PreparedAt.IsZero() {
		t.Fatal("failure did not retain pending inventory")
	}
	if err := os.Remove(block); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewServer(s.config)
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, request(restarted, "POST", url+"/prepare", s.admin, nil), 200)
	mustCode(t, request(restarted, "POST", url+"/cancel", s.admin, nil), 200)
	mustCode(t, request(restarted, "POST", url+"/cleanup", s.admin, nil), 200)
}

func TestPreparationRequiresDurableIntent(t *testing.T) {
	s := stagingFixture(t)
	j := arm(t, s, createFixtureJob(t, s, "rocky", false))
	s.store.path = filepath.Join(t.TempDir(), "missing", "state.json")
	mustCode(t, request(s, "POST", "/api/jobs/"+j.ID+"/prepare", s.admin, nil), 409)
	if _, err := os.Stat(filepath.Join(s.config.IVentoyRoot, managedScripts)); !os.IsNotExist(err) {
		t.Fatal("files written without a durable inventory")
	}
}

func TestCleanupPreservesConflicts(t *testing.T) {
	for _, change := range []string{"unknown file", "changed script", "replaced ISO", "symlink namespace"} {
		t.Run(change, func(t *testing.T) {
			s := stagingFixture(t)
			j := arm(t, s, createFixtureJob(t, s, "rocky", false))
			url := "/api/jobs/" + j.ID
			mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 200)
			j = savedJob(s, j.ID)
			script := filepath.Join(s.config.IVentoyRoot, j.Preparation.ScriptPath)
			var err error
			switch change {
			case "unknown file":
				err = os.WriteFile(filepath.Join(filepath.Dir(script), "operator.txt"), []byte("keep me"), 0600)
			case "changed script":
				err = os.WriteFile(script, []byte("operator changes"), 0600)
			case "replaced ISO":
				name := filepath.Join(s.config.IVentoyRoot, j.Preparation.ISOPath)
				err = os.Remove(name)
				if err == nil {
					err = os.WriteFile(name, []byte("different image"), 0600)
				}
			case "symlink namespace":
				name := filepath.Join(s.config.IVentoyRoot, managedScripts)
				err = os.Rename(name, name+"-saved")
				if err == nil {
					err = os.Symlink(name+"-saved", name)
				}
				if err != nil {
					t.Skip("symlink unavailable:", err)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			mustCode(t, request(s, "POST", url+"/cancel", s.admin, nil), 200)
			mustCode(t, request(s, "POST", url+"/cleanup", s.admin, nil), 409)
			if _, err = os.Stat(script); err != nil {
				t.Fatal("cleanup removed files before checking the whole inventory")
			}
		})
	}
}

func TestPreparationConfigAndMediaChanges(t *testing.T) {
	s := stagingFixture(t)
	j := arm(t, s, createFixtureJob(t, s, "rocky", false))
	url := "/api/jobs/" + j.ID
	mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 200)
	s.config.PublicURL = "https://changed.example:26080"
	mustCode(t, request(s, "POST", url+"/prepare", s.admin, nil), 409)
	mustCode(t, request(s, "POST", url+"/cancel", s.admin, nil), 200)
	mustCode(t, request(s, "POST", url+"/cleanup", s.admin, nil), 200)
	old := s.config.IVentoyRoot
	s.config.IVentoyRoot = t.TempDir()
	if _, err := s.openIVentoyRoot(); err == nil {
		t.Fatal("accepted unrelated root")
	}
	s.config.IVentoyRoot = old
	s.config.ISODir = t.TempDir()
	if _, err := s.openIVentoyRoot(); err == nil {
		t.Fatal("accepted unrelated ISO directory")
	}
}
