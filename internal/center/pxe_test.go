package center

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func pxeFixture(t *testing.T, profile string, legacyPolicy ...string) (*Server, PrivatePXEEntry) {
	t.Helper()
	s := stagingFixture(t)
	s.config.PublicURL = "https://deploy.example:26080"
	body := map[string]string{"label": "Test release auto install", "profile": profile, "iso": "test.iso", "username": "deployadmin", "password_hash": "$6$example$" + strings.Repeat("a", 86)}
	mustCode(t, request(s, "POST", "/api/pxe/entries", "", body), 401)
	w := request(s, "POST", "/api/pxe/entries", s.admin, body)
	mustCode(t, w, 201)
	var e PXEEntry
	json.Unmarshal(w.Body.Bytes(), &e)
	if len(legacyPolicy) != 0 {
		if err := s.store.update(func(st *State) error { findPXEEntry(st, e.ID).DiskPolicy = legacyPolicy[0]; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	base := "/api/pxe/entries/" + e.ID
	mustCode(t, request(s, "POST", base+"/enabled", s.admin, map[string]bool{"enabled": true}), 409)
	mustCode(t, request(s, "POST", base+"/prepare", s.admin, nil), 200)
	mustCode(t, request(s, "POST", base+"/enabled", s.admin, map[string]bool{"enabled": true}), 200)
	st := s.store.snapshot()
	return s, *findPXEEntry(&st, e.ID)
}
func pxeInventory() PXEInventory {
	return PXEInventory{Version: pxeInventoryVersion, MAC: "02:00:00:00:01:01", Firmware: "uefi", Disks: []PXEDisk{{Name: "sda", Serial: "DISK-DEMO-001", Model: "Test disk", Size: 128 << 30, Location: "internal"}}}
}
func pxeTicket(t *testing.T, s *Server, e PrivatePXEEntry, inv PXEInventory) map[string]string {
	t.Helper()
	w := request(s, "POST", "/pxe/"+e.ID+"/discover", e.Token, inv)
	mustCode(t, w, 200)
	var v map[string]string
	json.Unmarshal(w.Body.Bytes(), &v)
	return v
}
func TestPXEConsoleClaimAndRevocation(t *testing.T) {
	s, e := pxeFixture(t, "ubuntu-22.04", "")
	base := "/pxe/" + e.ID
	mustCode(t, request(s, "POST", base+"/discover", "wrong", pxeInventory()), 409)
	ticket := pxeTicket(t, s, e, pxeInventory())
	body := map[string]string{"ticket": ticket["ticket"], "disk_serial": "DISK-DEMO-001", "confirmation": "wrong"}
	mustCode(t, request(s, "POST", base+"/install", e.Token, body), 409)
	if len(s.store.snapshot().Jobs) != 0 {
		t.Fatal("invalid confirmation created a job")
	}
	body["confirmation"] = "ERASE DISK-DEMO-001"
	// A process restart must not lose pending confirmations or consumed claims.
	var err error
	s, err = NewServer(s.config)
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "POST", base+"/install", e.Token, body)
	mustCode(t, w, 200)
	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)
	if strings.Contains(result["config"], "early-commands:") || !strings.Contains(result["config"], "ventoy-center-update-policy") || !strings.Contains(result["preinstall"], "DISK-DEMO-001") {
		t.Fatal("dynamic config did not retain bound guards and policy")
	}
	mustCode(t, request(s, "POST", base+"/install", e.Token, body), 409)
	mustCode(t, request(s, "POST", base+"/discover", e.Token, pxeInventory()), 409)
	st := s.store.snapshot()
	j := st.Jobs[0]
	view := request(s, "GET", "/api/overview", s.admin, nil).Body.String()
	for _, secret := range []string{e.Token, e.Request.PasswordHash, j.Token, ticket["ticket"]} {
		if strings.Contains(view, secret) {
			t.Fatal("overview leaked a credential")
		}
	}
	mustCode(t, request(s, "POST", "/api/pxe/entries/"+e.ID+"/enabled", s.admin, map[string]bool{"enabled": false}), 200)
	mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installing"}), 401)
	mustCode(t, request(s, "POST", "/api/pxe/entries/"+e.ID+"/enabled", s.admin, map[string]bool{"enabled": true}), 200)
	if savedJob(s, j.ID).State != "canceled" {
		t.Fatal("re-enabling revived old job")
	}
}
func TestPXEPresetAndConcurrentIssue(t *testing.T) {
	s, e := pxeFixture(t, "rocky-9", pxeConsole)
	j := arm(t, s, createFixtureJob(t, s, "rocky", false))
	info := pxeTicket(t, s, e, pxeInventory())
	if info["preset_disk"] != j.Machine.DiskSerial {
		t.Fatal("preset not recognized")
	}
	body := map[string]string{"ticket": info["ticket"], "disk_serial": j.Machine.DiskSerial}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body).Code }()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatal(code)
		}
	}
	if success != 1 || len(s.store.snapshot().Jobs) != 1 {
		t.Fatal("concurrent ticket use was not exclusive")
	}
	mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installing"}), 200)
	mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installed"}), 200)
	info = pxeTicket(t, s, e, pxeInventory())
	if info["preset_disk"] != "" {
		t.Fatal("completed job rearmed automatically")
	}
	mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, map[string]string{"ticket": info["ticket"], "disk_serial": j.Machine.DiskSerial}), 409)
}

func TestPXEInstallerFailureAllowsRetryWithoutReusingJob(t *testing.T) {
	s, e := pxeFixture(t, "ubuntu-22.04", pxeConsole)
	base := "/pxe/" + e.ID
	ticket := pxeTicket(t, s, e, pxeInventory())
	w := request(s, "POST", base+"/install", e.Token, map[string]string{
		"ticket": ticket["ticket"], "disk_serial": "DISK-DEMO-001", "confirmation": "ERASE DISK-DEMO-001",
	})
	mustCode(t, w, 200)
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	j := savedJob(s, result["job_id"])
	if !strings.Contains(result["install_failure"], "/bootstrap/"+j.ID+"/event") || !strings.Contains(result["install_failure"], j.Token) {
		t.Fatal("failure hook is not bound to the issued job")
	}
	if strings.Contains(result["install_failure"], e.Token) || strings.Contains(result["install_failure"], s.admin) {
		t.Fatal("failure hook received a shared credential")
	}
	endpoint := "/bootstrap/" + j.ID + "/event"
	mustCode(t, request(s, "POST", endpoint, j.Token, map[string]string{"state": "installing"}), 200)
	mustCode(t, request(s, "POST", base+"/discover", e.Token, pxeInventory()), 409)
	// An unrelated job/menu credential cannot clear an installation in progress.
	mustCode(t, request(s, "POST", endpoint, e.Token, map[string]string{"state": "failed"}), 401)
	if savedJob(s, j.ID).State != "installing" {
		t.Fatal("invalid reporter closed the job")
	}
	mustCode(t, request(s, "POST", endpoint, j.Token, map[string]string{"state": "failed"}), 200)
	ticket = pxeTicket(t, s, e, pxeInventory())
	if ticket["preset_disk"] != "" {
		t.Fatal("failed job became a preset")
	}
	w = request(s, "POST", base+"/install", e.Token, map[string]string{
		"ticket": ticket["ticket"], "disk_serial": "DISK-DEMO-001", "confirmation": "ERASE DISK-DEMO-001",
	})
	mustCode(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &result)
	if result["job_id"] == j.ID || len(s.store.snapshot().Jobs) != 2 {
		t.Fatal("retry reused old install authorization")
	}
	// Terminal transitions revoke the previous token entirely.
	mustCode(t, request(s, "POST", endpoint, j.Token, map[string]string{"state": "failed"}), 401)
	if savedJob(s, result["job_id"]).State != "ready" {
		t.Fatal("repeated failure report changed the new job")
	}
}
func TestPXERejectsUnsafeInventoryAndExpiredTickets(t *testing.T) {
	for _, scenario := range []string{"duplicate", "raid", "mounted", "usb", "missing", "expired", "disabled", "other-job", "changed-iso"} {
		t.Run(scenario, func(t *testing.T) {
			s, e := pxeFixture(t, "rocky-9", pxeConsole)
			inv := pxeInventory()
			switch scenario {
			case "duplicate":
				d := inv.Disks[0]
				d.Name = "sdb"
				inv.Disks = append(inv.Disks, d)
			case "raid", "mounted", "usb":
				inv.Disks[0].Blocked = scenario
			case "missing":
				inv.Disks[0].Serial = ""
			}
			info := pxeTicket(t, s, e, inv)
			switch scenario {
			case "expired":
				s.store.update(func(st *State) error { st.PXETickets[0].Expires = time.Now().Add(-time.Minute); return nil })
			case "disabled":
				mustCode(t, request(s, "POST", "/api/pxe/entries/"+e.ID+"/enabled", s.admin, map[string]bool{"enabled": false}), 200)
			case "other-job":
				createFixtureJob(t, s, "rocky", false)
			case "changed-iso":
				os.WriteFile(filepath.Join(s.config.ISODir, "test.iso"), []byte("replacement"), 0600)
			}
			mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, map[string]string{"ticket": info["ticket"], "disk_serial": "DISK-DEMO-001", "confirmation": "ERASE DISK-DEMO-001"}), 409)
		})
	}
}
func TestPXEStagingAndInstallerPayloads(t *testing.T) {
	for _, profile := range []string{"centos-7.6", "centos-8.0", "centos-8.4", "centos-stream-8", "almalinux-8", "almalinux-9", "rhel-7", "rhel-8", "rhel-9", "centos-7.9", "centos-8.5", "centos-stream-9", "centos-stream-10", "rocky-8", "rocky-9", "rocky-10", "ubuntu-20.04", "ubuntu-22.04", "ubuntu-24.04", "ubuntu-24.10", "ubuntu-25.04", "ubuntu-26.04", "ubuntu-22.04-desktop", "ubuntu-26.04-desktop", "debian-10", "debian-11", "debian-12", "debian-13"} {
		t.Run(profile, func(t *testing.T) {
			s, e := pxeFixture(t, profile)
			if e.DiskPolicy != pxeLocalSelection {
				t.Fatal("All supported Linux entries must default to automatic single-disk installation")
			}
			files, err := s.renderPXEFiles(e)
			if err != nil {
				t.Fatal(err)
			}
			for _, content := range files {
				if strings.Contains(content, e.Request.PasswordHash) || strings.Contains(content, s.admin) {
					t.Fatal("shared bootstrap leaked account or admin credentials")
				}
			}
			info := pxeTicket(t, s, e, pxeInventory())
			w := request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, map[string]string{"ticket": info["ticket"], "disk_serial": "DISK-DEMO-001", "confirmation": "ERASE DISK-DEMO-001"})
			mustCode(t, w, 200)
			var payload map[string]string
			json.Unmarshal(w.Body.Bytes(), &payload)
			if payload["installer"] == "kickstart" && (strings.Contains(payload["config"], "%pre ") || !strings.Contains(payload["preinstall"], "ventoy-center-partition.ks")) {
				t.Fatal("Kickstart pre-pass must run once before dynamic include")
			}
			if out := os.Getenv("VENTOY_TEST_OUTPUT"); out != "" {
				dir := filepath.Join(out, "pxe", profile)
				if err = os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				for name, content := range files {
					if err = os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
				for name, content := range map[string]string{"selected.cfg": payload["config"], "preinstall.sh": payload["preinstall"]} {
					os.WriteFile(filepath.Join(dir, name), []byte(content), 0600)
				}
				policy, _ := pxeDiskPolicy(e)
				settings, _ := json.Marshal(map[string]string{"url": "https://deploy.example/pxe/test", "token": "fixture", "ca": "", "iso": "test.iso", "installer": payload["installer"], "disk_policy": policy})
				client, err := render("pxe-client.py", renderData{PXESettings: base64.StdEncoding.EncodeToString(settings)})
				if err != nil {
					t.Fatal(err)
				}
				os.WriteFile(filepath.Join(dir, "client.py"), []byte(client), 0600)
			}
			// Existing modified scripts are never overwritten.
			os.WriteFile(filepath.Join(e.Preparation.Root, filepath.FromSlash(e.Preparation.ScriptPath)), []byte("changed"), 0600)
			mustCode(t, request(s, "POST", "/api/pxe/entries/"+e.ID+"/prepare", s.admin, nil), 409)
		})
	}
}

func TestPXEDebianNativeProtocol(t *testing.T) {
	for _, profile := range []string{"debian-12", "debian-13", "ubuntu-22.04"} {
		t.Run(profile, func(t *testing.T) {
			s, e := pxeFixture(t, profile)
			call := func(endpoint string, body any) *httptest.ResponseRecorder {
				data, _ := json.Marshal(body)
				r := httptest.NewRequest("POST", "/pxe/"+e.ID+"/"+endpoint, bytes.NewReader(data))
				r.Header.Set("Authorization", "Bearer "+e.Token)
				r.Header.Set("Accept", "text/x-ventoy-preseed")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				return w
			}
			w := call("discover", pxeInventory())
			if profile == "ubuntu-22.04" {
				mustCode(t, w, 409)
				if len(s.store.snapshot().PXETickets) != 0 {
					t.Fatal("Wrong installer allocated a ticket")
				}
				return
			}
			mustCode(t, w, 200)
			fields := map[string]string{}
			for _, line := range strings.Split(strings.TrimSpace(w.Body.String()), "\n") {
				key, value, ok := strings.Cut(line, "=")
				if !ok {
					t.Fatal("Invalid native protocol")
				}
				fields[key] = value
			}
			if len(fields) != 6 || fields["filesystem"] != "ext4" || fields["preset_filesystem"] != "" || fields["disk_policy"] != pxeLocalSelection || fields["automatic_disk"] != "DISK-DEMO-001" {
				t.Fatal("Native disk selection did not match the shared policy", fields)
			}
			body := map[string]string{"ticket": fields["ticket"], "disk_serial": fields["automatic_disk"]}
			w = call("install", body)
			mustCode(t, w, 200)
			if !strings.Contains(w.Body.String(), "d-i partman/early_command string") || !strings.Contains(w.Body.String(), "d-i preseed/late_command string") {
				t.Fatal("Incomplete native configuration")
			}
			mustCode(t, call("install", body), 409)
			if len(s.store.snapshot().Jobs) != 1 {
				t.Fatal("Native request replay created another job")
			}
		})
	}
}

func TestPXESingleInternalPolicy(t *testing.T) {
	for _, scenario := range []string{"single", "with-usb", "two-internal", "second-blocked", "second-unknown", "only-usb", "only-unknown", "blocked", "missing-serial", "duplicate-serial", "old-client"} {
		t.Run(scenario, func(t *testing.T) {
			s, e := pxeFixture(t, "ubuntu-22.04", pxeSingleInternal)
			if e.DiskPolicy != pxeSingleInternal {
				t.Fatal("new Ubuntu entry requires automatic policy")
			}
			inv := pxeInventory()
			other := PXEDisk{Name: "sdb", Serial: "SECOND-DISK", Model: "Other disk", Size: 256 << 30, Location: "internal"}
			switch scenario {
			case "with-usb":
				other.Location = "external"
				other.Blocked = "USB"
				inv.Disks = append(inv.Disks, other)
			case "two-internal":
				inv.Disks = append(inv.Disks, other)
			case "second-blocked":
				other.Blocked = "LVM"
				inv.Disks = append(inv.Disks, other)
			case "second-unknown":
				other.Location = "unknown"
				inv.Disks = append(inv.Disks, other)
			case "only-usb":
				inv.Disks[0].Location = "external"
			case "only-unknown":
				inv.Disks[0].Location = "unknown"
			case "blocked":
				inv.Disks[0].Blocked = "Mounted disk"
			case "missing-serial":
				inv.Disks[0].Serial = ""
			case "duplicate-serial":
				other.Serial = inv.Disks[0].Serial
				other.Location = "external"
				inv.Disks = append(inv.Disks, other)
			case "old-client":
				inv.Version = 0
				inv.Disks[0].Location = ""
			}
			w := request(s, "POST", "/pxe/"+e.ID+"/discover", e.Token, inv)
			if scenario != "single" && scenario != "with-usb" {
				mustCode(t, w, 409)
				if st := s.store.snapshot(); len(st.Jobs) != 0 || len(st.PXETickets) != 0 {
					t.Fatal("rejected inventory created authorization")
				}
				return
			}
			mustCode(t, w, 200)
			var info map[string]string
			json.Unmarshal(w.Body.Bytes(), &info)
			if info["automatic_disk"] != inv.Disks[0].Serial || info["preset_disk"] != "" || info["disk_policy"] != pxeSingleInternal {
				t.Fatal(info)
			}
			body := map[string]string{"ticket": info["ticket"], "disk_serial": info["automatic_disk"]}
			mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 200)
			mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
			j := s.store.snapshot().Jobs[0]
			if j.Machine.DiskSerial != inv.Disks[0].Serial || j.State != "ready" || !strings.Contains(j.Events[0].Message, "单内置盘") {
				t.Fatal("automatic job is not bound to the selected disk")
			}
		})
	}
}

func TestPXEAutomaticTicketRechecksPolicy(t *testing.T) {
	for _, scenario := range []string{"wrong-disk", "policy-change", "multiple-disks", "blocked", "expired", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			s, e := pxeFixture(t, "ubuntu-24.04")
			info := pxeTicket(t, s, e, pxeInventory())
			body := map[string]string{"ticket": info["ticket"], "disk_serial": info["automatic_disk"]}
			if err := s.store.update(func(st *State) error {
				switch scenario {
				case "wrong-disk":
					body["disk_serial"] = "ANOTHER-DISK"
				case "policy-change":
					findPXEEntry(st, e.ID).DiskPolicy = pxeConsole
					body["confirmation"] = "ERASE " + body["disk_serial"]
				case "multiple-disks":
					st.PXETickets[0].Inventory.Disks = append(st.PXETickets[0].Inventory.Disks, PXEDisk{Name: "sdb", Serial: "SECOND", Size: 128 << 30, Location: "internal"})
				case "blocked":
					st.PXETickets[0].Inventory.Disks[0].Blocked = "Mounted disk"
				case "expired":
					st.PXETickets[0].Expires = time.Now().Add(-time.Minute)
				case "disabled":
					findPXEEntry(st, e.ID).Enabled = false
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
			if len(s.store.snapshot().Jobs) != 0 {
				t.Fatal("rejected installation created job")
			}
		})
	}
}

func TestPXEAutomaticMultipleDisksNeedsArmedTask(t *testing.T) {
	s, e := pxeFixture(t, "ubuntu-24.04")
	j := createFixtureJob(t, s, "ubuntu", false)
	inv := pxeInventory()
	inv.Disks = append(inv.Disks, PXEDisk{Name: "sdb", Serial: "KEEP-SECOND", Size: 256 << 30, Location: "internal"})
	mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/discover", e.Token, inv), 409)
	j = arm(t, s, j)
	info := pxeTicket(t, s, e, inv)
	if info["preset_disk"] != j.Machine.DiskSerial || info["automatic_disk"] != "" {
		t.Fatal(info)
	}
	body := map[string]string{"ticket": info["ticket"], "disk_serial": "KEEP-SECOND"}
	mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
	body["disk_serial"] = j.Machine.DiskSerial
	mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 200)
	if len(s.store.snapshot().Jobs) != 1 {
		t.Fatal("preset duplicated instead of reused")
	}
}

func TestPXEConsoleErrorsAreReadableWithoutCJKFonts(t *testing.T) {
	s, e := pxeFixture(t, "ubuntu-22.04", pxeSingleInternal)
	inv := pxeInventory()
	inv.Disks = append(inv.Disks, PXEDisk{Name: "sdb", Serial: "KEEP-SECOND", Size: 256 << 30, Location: "internal"})
	w := request(s, "POST", "/pxe/"+e.ID+"/discover", e.Token, inv)
	mustCode(t, w, 409)
	var failure map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure["code"] != "MULTIPLE_DISKS" || !strings.Contains(failure["error"], "authorize a matching installation task") {
		t.Fatal(failure)
	}
	for _, ch := range failure["error"] {
		if ch > 127 {
			t.Fatal("Installer error requires a non-ASCII console font")
		}
	}
	if st := s.store.snapshot(); len(st.Jobs) != 0 || len(st.PXETickets) != 0 {
		t.Fatal("Error handling must not create disk authorization")
	}
	inv.Disks = inv.Disks[:1]
	inv.Disks[0].Blocked = "Existing LVM/RAID: prepare storage separately"
	w = request(s, "POST", "/pxe/"+e.ID+"/discover", e.Token, inv)
	mustCode(t, w, 409)
	json.Unmarshal(w.Body.Bytes(), &failure)
	if failure["code"] != "DISK_BLOCKED" || !strings.Contains(failure["error"], "Existing LVM/RAID") {
		t.Fatal(failure)
	}
}

func TestPXELocalSelectionWithoutRegisteredMachine(t *testing.T) {
	for _, profile := range []string{"centos-7.9", "rocky-8", "ubuntu-22.04", "ubuntu-24.04", "debian-10", "debian-13"} {
		t.Run(profile, func(t *testing.T) {
			s, e := pxeFixture(t, profile)
			inv := pxeInventory()
			inv.Disks = append(inv.Disks, PXEDisk{Name: "sdb", Serial: "SELECTED-SECOND", Size: 256 << 30, Location: "internal"})
			info := pxeTicket(t, s, e, inv)
			if info["disk_policy"] != pxeLocalSelection || info["automatic_disk"] != "" || info["preset_disk"] != "" {
				t.Fatal("multiple disks must be selected locally", info)
			}
			body := map[string]string{"ticket": info["ticket"], "disk_serial": "SELECTED-SECOND"}
			for _, confirmation := range []string{"", "yes", "ERASE DISK-DEMO-001"} {
				body["confirmation"] = confirmation
				mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
				if len(s.store.snapshot().Jobs) != 0 {
					t.Fatal("an unconfirmed request created an installation")
				}
			}
			body["confirmation"] = "ERASE SELECTED-SECOND"
			mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 200)
			mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), 409)
			st := s.store.snapshot()
			if len(st.Jobs) != 1 || st.Jobs[0].Machine.DiskSerial != "SELECTED-SECOND" || st.Jobs[0].State != "ready" {
				t.Fatal("local choice did not bind a single-use job to the second disk")
			}
			j := st.Jobs[0]
			mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installing"}), 200)
			mustCode(t, request(s, "POST", "/bootstrap/"+j.ID+"/event", j.Token, map[string]string{"state": "installed"}), 200)
			// A future PXE boot must offer a new local choice, not reuse a disk
			// choice from machine registration as perpetual erase authorization.
			info = pxeTicket(t, s, e, inv)
			if info["preset_disk"] != "" || info["automatic_disk"] != "" {
				t.Fatal("a completed task silently authorized another erase")
			}
		})
	}
}

func TestPXELocalSelectionRetainsDiskProtection(t *testing.T) {
	for _, scenario := range []string{"blocked-other", "unknown-other", "external-selected", "blocked-selected", "unknown-selected", "duplicate", "single"} {
		t.Run(scenario, func(t *testing.T) {
			s, e := pxeFixture(t, "ubuntu-22.04")
			inv := pxeInventory()
			second := PXEDisk{Name: "sdb", Serial: "SECOND", Size: 128 << 30, Location: "internal"}
			switch scenario {
			case "blocked-other":
				second.Blocked = "LVM"
			case "unknown-other":
				second.Location = "unknown"
			case "external-selected":
				inv.Disks[0].Location = "external"
			case "blocked-selected":
				inv.Disks[0].Blocked = "Mounted disk"
			case "unknown-selected":
				inv.Disks[0].Location = "unknown"
			case "duplicate":
				second.Serial = inv.Disks[0].Serial
			}
			if scenario != "single" {
				inv.Disks = append(inv.Disks, second)
			}
			info := pxeTicket(t, s, e, inv)
			body := map[string]string{"ticket": info["ticket"], "disk_serial": inv.Disks[0].Serial, "confirmation": "ERASE " + inv.Disks[0].Serial}
			success := scenario == "blocked-other" || scenario == "unknown-other" || scenario == "single"
			if scenario == "single" {
				if info["automatic_disk"] != inv.Disks[0].Serial {
					t.Fatal(info)
				}
				body["confirmation"] = ""
			} else if scenario != "external-selected" && info["automatic_disk"] != "" {
				t.Fatal(info)
			}
			code := 409
			if success {
				code = 200
			}
			mustCode(t, request(s, "POST", "/pxe/"+e.ID+"/install", e.Token, body), code)
			if !success && len(s.store.snapshot().Jobs) != 0 {
				t.Fatal("unsafe local target created a job")
			}
		})
	}
}

func TestPXEDebianLocalSelectionNativeProtocol(t *testing.T) {
	s, e := pxeFixture(t, "debian-12")
	call := func(endpoint string, body any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/pxe/"+e.ID+"/"+endpoint, bytes.NewReader(data))
		r.Header.Set("Authorization", "Bearer "+e.Token)
		r.Header.Set("Accept", "text/x-ventoy-preseed")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	inv := pxeInventory()
	inv.Disks = append(inv.Disks, PXEDisk{Name: "sdb", Serial: "SECOND", Size: 128 << 30, Location: "internal"})
	w := call("discover", inv)
	mustCode(t, w, 200)
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(w.Body.String()), "\n") {
		key, value, _ := strings.Cut(line, "=")
		fields[key] = value
	}
	if fields["automatic_disk"] != "" || fields["preset_disk"] != "" || fields["disk_policy"] != pxeLocalSelection {
		t.Fatal(fields)
	}
	body := map[string]string{"ticket": fields["ticket"], "disk_serial": "SECOND"}
	mustCode(t, call("install", body), 409)
	body["confirmation"] = "ERASE SECOND"
	w = call("install", body)
	mustCode(t, w, 200)
	if !strings.Contains(w.Body.String(), "d-i partman-auto/disk string /dev/ventoy-center-not-authorized") {
		t.Fatal("native response bypassed the final installation guard")
	}
	if s.store.snapshot().Jobs[0].Machine.DiskSerial != "SECOND" {
		t.Fatal("native disk choice lost")
	}
}
