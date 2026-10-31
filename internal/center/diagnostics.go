package center

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type InstallationReport struct {
	ID       string            `json:"id"`
	Time     time.Time         `json:"time"`
	Kind     string            `json:"kind"`
	Source   string            `json:"source"`
	Category string            `json:"category"`
	Advice   string            `json:"advice"`
	Text     string            `json:"text,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

var reportIDRE = regexp.MustCompile(`^[a-f0-9]{32}$`)
var reportSecretLine = regexp.MustCompile(`(?i)authorization|bearer\s|password|passwd|token|secret|--key|ssh-(rsa|ed25519)|private.key|\$[1256y]\$`)
var reportEncoded = regexp.MustCompile(`[A-Za-z0-9+/=_-]{160,}`)

func redactReport(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.Map(func(r rune) rune {
			if r == '\t' || !unicode.IsControl(r) {
				return r
			}
			return -1
		}, line)
		if reportSecretLine.MatchString(line) || reportEncoded.MatchString(line) {
			line = "[credential/configuration-bearing line redacted]"
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func classifyReport(text string) (string, string) {
	t := strings.ToLower(text)
	for _, row := range [][3]string{
		{"storage", "existing lvm|lvm.*(conflict|fail)|raid members require|unselected disk|mounted disk|storage spanning|vgchange.*fail|partman.*fail|block-meta.*fail", "核对目标盘及旧 LVM/RAID 成员；只有所选磁盘全部覆盖旧存储时才能重建。"},
		{"network", "could not resolve|failed to download|403 forbidden|404 not found|connection refused|certificate verify|network is unreachable", "检查部署网卡地址、中心依赖缓存和软件源；先修复下载问题，再重新进入安装器。"},
		{"bootloader", "grub-install|efibootmgr|failed.*bootloader|bootloader.*failed", "核对 UEFI/BIOS 模式、EFI 分区与引导程序安装日志。"},
		{"driver", "invalid module format|unknown symbol|no suitable driver|module verification failed", "核对安装内核、设备 ID、模块 ABI 及 Secure Boot 状态。"},
		{"selection", "confirmation timed out|installation canceled|no usable disk|no valid filesystem|disk policy changed", "重新进入 PXE 并完成选择；此类停止不应被当成已经完成安装。"},
	} {
		if regexp.MustCompile(row[1]).MatchString(t) {
			return row[0], row[2]
		}
	}
	return "installer", "查看日志中最早的原始错误；Pane is dead 和 updates_disk_hook 可能只是后续退出提示。"
}

func (s *Server) receiveInstallationReport(w http.ResponseWriter, r *http.Request) {
	if !sameToken(s.diagnosticToken(), bearer(r)) {
		fail(w, 401, "诊断上传凭据无效")
		return
	}
	kind := r.Header.Get("X-Ventoy-Report-Kind")
	if kind != "failure" && kind != "observed" && kind != "result" {
		fail(w, 400, "无效报告类型")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	b, err := io.ReadAll(r.Body)
	if err != nil || len(b) == 0 {
		fail(w, 400, "报告为空或超过 64 KiB")
		return
	}
	text := redactReport(string(b))
	for _, secret := range []string{s.admin, s.diagnosticToken()} {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	// Even innocuous-looking log lines must not expose an exact live credential.
	state := s.store.snapshot()
	for _, j := range state.Jobs {
		for _, secret := range []string{j.Token, j.Request.Password, j.Request.PasswordHash, j.Request.RootPasswordHash} {
			if len(secret) >= 6 {
				text = strings.ReplaceAll(text, secret, "[redacted]")
			}
		}
	}
	for _, e := range state.PXEEntries {
		if e.Token != "" {
			text = strings.ReplaceAll(text, e.Token, "[redacted]")
		}
	}
	category, advice := classifyReport(text)
	if kind == "result" {
		category, advice = "result", "区分已从硬盘启动、自动检查通过、setup 待执行与 setup 已完成。"
	}
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	// Bound storage and request rate independently of installation authorizations.
	if time.Since(s.lastReport) < 200*time.Millisecond {
		fail(w, 429, "报告上传过于频繁，请保留本地日志")
		return
	}
	dir := filepath.Join(s.config.DataDir, "installation-reports")
	if err = os.MkdirAll(dir, 0700); err != nil {
		fail(w, 500, "无法保存报告")
		return
	}
	rows, _ := os.ReadDir(dir)
	if len(rows) >= 500 {
		type saved struct {
			path     string
			modified time.Time
		}
		var files []saved
		for _, row := range rows {
			if row.Type().IsRegular() && reportIDRE.MatchString(strings.TrimSuffix(row.Name(), ".json")) && strings.HasSuffix(row.Name(), ".json") {
				st, e := row.Info()
				if e == nil {
					files = append(files, saved{filepath.Join(dir, row.Name()), st.ModTime()})
				}
			}
		}
		sort.Slice(files, func(i, j int) bool { return files[i].modified.Before(files[j].modified) })
		for len(files) >= 500 {
			if os.Remove(files[0].path) != nil {
				fail(w, 507, "诊断保留空间已满")
				return
			}
			files = files[1:]
		}
	}
	entry := InstallationReport{ID: randomID(16), Time: time.Now().UTC(), Kind: kind, Source: r.RemoteAddr, Category: category, Advice: advice, Text: text}
	entry.Metadata = reportMetadata(text)
	raw, _ := json.MarshalIndent(entry, "", "  ")
	if err = writeNew(filepath.Join(dir, entry.ID+".json"), raw); err != nil {
		fail(w, 500, "无法保存报告")
		return
	}
	s.lastReport = time.Now()
	respond(w, 201, map[string]string{"id": entry.ID, "category": category})
}

func (s *Server) installationReports(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(s.config.DataDir, "installation-reports")
	if id := r.PathValue("id"); id != "" {
		if !reportIDRE.MatchString(id) {
			fail(w, 400, "无效报告编号")
			return
		}
		f, err := safeOpen(dir, id+".json", ".json")
		if err != nil {
			fail(w, 404, "报告不存在")
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="installation-%s.json"`, id))
		io.Copy(w, io.LimitReader(f, 256<<10))
		return
	}
	rows, _ := os.ReadDir(dir)
	reports := []InstallationReport{}
	for _, row := range rows {
		if !row.Type().IsRegular() || !strings.HasSuffix(row.Name(), ".json") || !reportIDRE.MatchString(strings.TrimSuffix(row.Name(), ".json")) {
			continue
		}
		f, err := safeOpen(dir, row.Name(), ".json")
		if err != nil {
			continue
		}
		var entry InstallationReport
		err = json.NewDecoder(io.LimitReader(f, 256<<10)).Decode(&entry)
		f.Close()
		if err == nil {
			entry.Text = ""
			reports = append(reports, entry)
		}
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Time.After(reports[j].Time) })
	respond(w, 200, reports)
}
