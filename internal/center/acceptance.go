package center

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func reportMetadata(text string) map[string]string {
	allowed := map[string]bool{}
	for _, k := range strings.Fields("node_id attempt_id boot_id hostname system mode release cpu_vendor rootfs raid setup_sha256 setup_status update_policy") {
		allowed[k] = true
	}
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "VC_META ") {
			continue
		}
		pair := strings.SplitN(strings.TrimPrefix(line, "VC_META "), "=", 2)
		if len(pair) == 2 && allowed[pair[0]] && len(pair[1]) <= 180 && !strings.ContainsAny(pair[1], "\r\x00") {
			if _, exists := out[pair[0]]; !exists {
				out[pair[0]] = pair[1]
			}
		}
	}
	return out
}

type AcceptanceRecord struct {
	ID       string          `json:"id"`
	EntryID  string          `json:"entry_id"`
	ISO      string          `json:"iso"`
	Release  string          `json:"release"`
	Mode     string          `json:"mode"`
	Platform string          `json:"platform"`
	Storage  string          `json:"storage"`
	Node     string          `json:"node"`
	Result   string          `json:"result"`
	Checks   map[string]bool `json:"checks"`
	Reports  []string        `json:"reports"`
	Notes    string          `json:"notes"`
	Time     time.Time       `json:"time"`
	Basis    string          `json:"basis"`
}

var acceptanceChecks = []string{"disk_boot", "second_boot", "ssh", "console", "dhcp", "kernel_updates", "filesystem", "setup"}
var acceptanceReleaseRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$`)

func (s *Server) acceptanceRecords() []AcceptanceRecord {
	rows := []AcceptanceRecord{}
	dir := filepath.Join(s.config.DataDir, "acceptance")
	files, _ := os.ReadDir(dir)
	for _, e := range files {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".json") || !reportIDRE.MatchString(strings.TrimSuffix(e.Name(), ".json")) {
			continue
		}
		f, err := safeOpen(dir, e.Name(), ".json")
		if err != nil {
			continue
		}
		var a AcceptanceRecord
		err = json.NewDecoder(io.LimitReader(f, 32<<10)).Decode(&a)
		f.Close()
		if err == nil {
			rows = append(rows, a)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Time.After(rows[j].Time) })
	return rows
}

func (s *Server) acceptanceMatrix(w http.ResponseWriter, r *http.Request) {
	rows := s.acceptanceRecords()
	respond(w, 200, map[string]any{"release": Version, "entries": publicPXEEntries(s.store.snapshot()), "records": rows, "required_checks": acceptanceChecks, "basis": "operator-attested; reports are observations and never automatically imply hardware acceptance"})
}

func (s *Server) recordAcceptance(w http.ResponseWriter, r *http.Request) {
	var a AcceptanceRecord
	if !decode(w, r, &a) {
		return
	}
	if !acceptanceReleaseRE.MatchString(a.Release) || len(a.Release) > 64 {
		fail(w, 400, "请填写实际验收的 setup 版本")
		return
	}
	st := s.store.snapshot()
	e := findPXEEntry(&st, a.EntryID)
	if e == nil || (a.Mode != "manual" && a.Mode != "automatic") || (a.Platform != "Intel" && a.Platform != "AMD" && a.Platform != "VM") || (a.Storage != "single" && a.Storage != "hardware-raid" && a.Storage != "raid1-lvm") || (a.Result != "passed" && a.Result != "failed") || len(strings.TrimSpace(a.Node)) == 0 || len(a.Node) > 120 || len(a.Notes) > 2000 || len(a.Reports) > 10 {
		fail(w, 400, "请填写有效的系统、模式、平台、布局、节点和结果")
		return
	}
	for k := range a.Checks {
		valid := false
		for _, key := range acceptanceChecks {
			if k == key {
				valid = true
			}
		}
		if !valid {
			fail(w, 400, "无效检查项")
			return
		}
	}
	if a.Result == "passed" {
		for _, key := range acceptanceChecks {
			if !a.Checks[key] {
				fail(w, 400, "通过验收需要逐项确认检查结果")
				return
			}
		}
	}
	for _, id := range a.Reports {
		if !reportIDRE.MatchString(id) {
			fail(w, 400, "无效报告编号")
			return
		}
		f, err := safeOpen(filepath.Join(s.config.DataDir, "installation-reports"), id+".json", ".json")
		if err != nil {
			fail(w, 400, "关联报告不存在")
			return
		}
		var report InstallationReport
		err = json.NewDecoder(io.LimitReader(f, 128<<10)).Decode(&report)
		f.Close()
		if err != nil || (report.Metadata["release"] != "" && report.Metadata["release"] != a.Release) {
			fail(w, 400, "关联报告的版本与验收版本不一致")
			return
		}
	}
	// The operator explicitly attests the physical checks, not the uploaded token.
	a.ID = randomID(16)
	a.ISO = e.ISO
	a.Time = time.Now().UTC()
	a.Basis = "operator-attested"
	a.Node = redactReport(a.Node)
	a.Notes = redactReport(a.Notes)
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	dir := filepath.Join(s.config.DataDir, "acceptance")
	if err := os.MkdirAll(dir, 0700); err != nil {
		fail(w, 500, "无法保存验收记录")
		return
	}
	existing, _ := os.ReadDir(dir)
	if len(existing) >= 5000 {
		fail(w, 507, "验收记录已满，请先归档")
		return
	}
	b, _ := json.MarshalIndent(a, "", "  ")
	if err := writeNew(filepath.Join(dir, a.ID+".json"), b); err != nil {
		fail(w, 500, "无法保存验收记录")
		return
	}
	respond(w, 201, a)
}
