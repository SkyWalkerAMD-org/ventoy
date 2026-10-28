package center

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// A published menu grants access only to one installation profile. It never
// carries the administrator token. Disk selection follows the published policy
// or a matching, already armed task before issuing a single-use job.
const (
	pxeConsole          = "console"
	pxeSingleInternal   = "single-internal"
	pxeLocalSelection   = "single-or-console"
	pxeInventoryVersion = 2
)

type PXEEntry struct {
	ID          string       `json:"id"`
	Label       string       `json:"label"`
	Profile     string       `json:"profile"`
	ISO         string       `json:"iso"`
	DiskPolicy  string       `json:"disk_policy,omitempty"`
	Enabled     bool         `json:"enabled"`
	Created     time.Time    `json:"created"`
	Preparation *Preparation `json:"preparation,omitempty"`
}
type PrivatePXEEntry struct {
	PXEEntry
	Request JobRequest `json:"request"`
	Token   string     `json:"token"`
}
type PXEDisk struct {
	Name     string `json:"name"`
	Serial   string `json:"serial"`
	Model    string `json:"model"`
	Size     int64  `json:"size"`
	Blocked  string `json:"blocked"`
	Location string `json:"location,omitempty"`
}
type PXEInventory struct {
	Version  int       `json:"version,omitempty"`
	MAC      string    `json:"mac"`
	Firmware string    `json:"firmware"`
	Disks    []PXEDisk `json:"disks"`
}
type PXETicket struct {
	ID         string       `json:"id"`
	EntryID    string       `json:"entry_id"`
	Inventory  PXEInventory `json:"inventory"`
	DiskPolicy string       `json:"disk_policy,omitempty"`
	Expires    time.Time    `json:"expires"`
}

func publicPXEEntries(st State) []PXEEntry {
	out := make([]PXEEntry, 0, len(st.PXEEntries))
	for _, e := range st.PXEEntries {
		out = append(out, e.PXEEntry)
	}
	return out
}
func findPXEEntry(st *State, id string) *PrivatePXEEntry {
	for i := range st.PXEEntries {
		if st.PXEEntries[i].ID == id {
			return &st.PXEEntries[i]
		}
	}
	return nil
}
func pxeProfile(id string) (Profile, error) {
	p, err := resolveProfile(id, "")
	if err != nil {
		return p, err
	}
	if p.Installer != "kickstart" && p.Installer != "preseed" && p.Installer != "autoinstall" && p.Installer != "ubiquity" {
		return p, errors.New("通用 PXE 菜单要求受支持的 Linux 安装模板")
	}
	return p, nil
}
func (s *Server) createPXEEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label        string `json:"label"`
		Profile      string `json:"profile"`
		ISO          string `json:"iso"`
		Username     string `json:"username"`
		PasswordHash string `json:"password_hash"`
		SSHKey       string `json:"ssh_key"`
	}
	if !decode(w, r, &req) {
		return
	}
	p, err := pxeProfile(req.Profile)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	if len(req.Label) < 3 || len(req.Label) > 120 || strings.ContainsAny(req.Label, "\r\n\x00") {
		fail(w, 400, "请填写包含系统版本的菜单名称（3–120 字节）")
		return
	}
	jr := JobRequest{Profile: p.ID, OS: p.OS, ISO: req.ISO, Username: req.Username, PasswordHash: req.PasswordHash, SSHKey: req.SSHKey}
	s.config.applyLinuxAccounts(&jr)
	if err = validateJob(jr); err == nil {
		err = validateMediaProfile(p, req.ISO)
	}
	if err == nil && (strings.HasPrefix(req.ISO, "ventoy-center/") || strings.ContainsAny(req.ISO, " \r\n\t")) {
		err = errors.New("请选择无空格的原始 ISO 路径")
	}
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	f, err := safeOpen(s.config.ISODir, req.ISO, ".iso")
	if err != nil {
		fail(w, 400, "原始 ISO 不可读取")
		return
	}
	f.Close()
	e := PrivatePXEEntry{PXEEntry: PXEEntry{ID: randomID(12), Label: req.Label, Profile: p.ID, ISO: req.ISO, Created: time.Now().UTC()}, Request: jr, Token: randomID(32)}
	e.DiskPolicy = pxeLocalSelection
	err = s.store.update(func(st *State) error {
		for _, old := range st.PXEEntries {
			if old.ISO == e.ISO {
				return errors.New("此镜像已有菜单入口，请使用现有入口")
			}
		}
		st.PXEEntries = append(st.PXEEntries, e)
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 201, e.PXEEntry)
}
func (s *Server) enablePXEEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decode(w, r, &req) {
		return
	}
	err := s.store.update(func(st *State) error {
		e := findPXEEntry(st, r.PathValue("id"))
		if e == nil {
			return errors.New("入口不存在")
		}
		if req.Enabled {
			if err := s.verifyPXESource(*e); err != nil {
				return err
			}
		}
		e.Enabled = req.Enabled
		if !req.Enabled {
			// Invalidate pending confirmations as well as ready menu-issued tasks.
			for i := range st.PXETickets {
				if st.PXETickets[i].EntryID == e.ID {
					st.PXETickets[i].Expires = time.Time{}
				}
			}
			for i := range st.Jobs {
				j := &st.Jobs[i]
				if j.PXEEntryID == e.ID && j.State == "ready" {
					addEvent(j, "canceled", "PXE 菜单入口停用，未开始的授权已撤销")
				}
			}
		}
		return nil
	})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
func (s *Server) verifyPXESource(e PrivatePXEEntry) error {
	p := e.Preparation
	if p == nil || p.PreparedAt.IsZero() || p.Root != s.config.IVentoyRoot {
		return errors.New("请先准备 PXE 菜单文件")
	}
	f, err := safeOpen(s.config.ISODir, e.ISO, ".iso")
	if err != nil {
		return errors.New("菜单原始镜像不可读取")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() != p.SourceSize || !info.ModTime().Equal(p.SourceModified) {
		return errors.New("菜单原始镜像已改变，请停用并核对")
	}
	return nil
}
func (s *Server) authorizePXE(st *State, id, token string) (*PrivatePXEEntry, error) {
	e := findPXEEntry(st, id)
	if e == nil || token == "" || !sameToken(e.Token, token) {
		return nil, errors.New("PXE 入口凭据无效")
	}
	if !e.Enabled {
		return nil, errors.New("PXE 入口已停用")
	}
	if err := s.verifyPXESource(*e); err != nil {
		return nil, err
	}
	return e, nil
}
func validateInventory(inv *PXEInventory) error {
	if inv.Version != 0 && inv.Version != pxeInventoryVersion {
		return errors.New("PXE 客户端版本不兼容，请刷新入口后重新网启")
	}
	m := Machine{Name: "PXE", MAC: inv.MAC, Serial: "inventory", DiskSerial: "inventory", Firmware: inv.Firmware}
	if inv.Firmware != "uefi" && inv.Firmware != "bios" {
		return errors.New("启动模式无效")
	}
	if err := validateMachine(&m); err != nil {
		return err
	}
	inv.MAC = m.MAC
	if len(inv.Disks) == 0 || len(inv.Disks) > 64 {
		return errors.New("磁盘列表为空或过大")
	}
	names := map[string]bool{}
	for _, d := range inv.Disks {
		if inv.Version == pxeInventoryVersion && d.Location != "internal" && d.Location != "external" && d.Location != "unknown" {
			return errors.New("磁盘连接位置无效，请重新网启")
		}
		if d.Name == "" || len(d.Name) > 64 || strings.Trim(d.Name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" || names[d.Name] {
			return errors.New("磁盘设备名无效或重复")
		}
		names[d.Name] = true
		if d.Size <= 0 || len(d.Serial) > 120 || len(d.Model) > 200 || len(d.Blocked) > 200 {
			return errors.New("磁盘信息无效")
		}
	}
	return nil
}
func eligibleDisk(inv PXEInventory, serial string) error {
	if !serialRE.MatchString(serial) {
		return errors.New("磁盘缺少可用序列号")
	}
	count := 0
	for _, d := range inv.Disks {
		if d.Serial == serial {
			count++
			if inv.Version == pxeInventoryVersion && d.Location != "internal" {
				return errors.New("目标必须是已识别的内置磁盘")
			}
			if d.Blocked != "" {
				return errors.New("磁盘不可直接安装：" + d.Blocked)
			}
		}
	}
	if count != 1 {
		return errors.New("目标磁盘序列号必须唯一匹配")
	}
	return nil
}

func pxeDiskPolicy(e PrivatePXEEntry) (string, error) {
	// Entries created before this policy existed retain console confirmation.
	if e.DiskPolicy == "" || e.DiskPolicy == pxeConsole {
		return pxeConsole, nil
	}
	if e.DiskPolicy == pxeSingleInternal || e.DiskPolicy == pxeLocalSelection {
		if _, err := pxeProfile(e.Profile); err == nil {
			return e.DiskPolicy, nil
		}
	}
	return "", errors.New("PXE 选盘策略无效，请核对入口配置")
}

// A multi-disk inventory never implies consent to erase any particular disk.
// The local policy returns no automatic target so the client can ask on the
// installation console. Blocked/unknown disks still count: do not silently
// choose the remaining usable disk when another device is present.
func automaticPXEDisk(inv PXEInventory, policy string) (string, error) {
	if policy == pxeConsole {
		return "", nil
	}
	if inv.Version != pxeInventoryVersion {
		return "", errors.New("PXE 客户端过旧，请刷新 iVentoy 后重新网启")
	}
	if policy == pxeLocalSelection {
		count := 0
		for _, d := range inv.Disks {
			if d.Location != "external" {
				count++
			}
		}
		if count > 1 {
			return "", nil
		}
		// Old storage needs local confirmation and a full membership guard,
		// even when this is the only internal disk. Do not implicitly erase it.
		for _, d := range inv.Disks {
			if d.Location == "internal" && existingStorage(d.Blocked) {
				return "", nil
			}
		}
	}
	return singleInternalDisk(inv)
}

func singleInternalDisk(inv PXEInventory) (string, error) {
	if inv.Version != pxeInventoryVersion {
		return "", errors.New("PXE 客户端过旧，请刷新 iVentoy 后重新网启")
	}
	var candidates []PXEDisk
	for _, d := range inv.Disks {
		// Count blocked and unknown-location disks too. Never fall back to the
		// remaining usable disk when another internal disk cannot be installed.
		if d.Location != "external" {
			candidates = append(candidates, d)
		}
	}
	if len(candidates) > 1 {
		return "", errors.New("检测到多块内置或连接位置不明的磁盘；请先在管理端指定目标盘并授权任务，然后重新网启")
	}
	if len(candidates) != 1 {
		return "", errors.New("未找到可自动安装的内置磁盘")
	}
	serial := candidates[0].Serial
	if err := eligibleDisk(inv, serial); err != nil {
		return "", err
	}
	return serial, nil
}
func matchingPXEJob(st *State, e PrivatePXEEntry, inv PXEInventory) (*PrivateJob, error) {
	var match *PrivateJob
	for i := range st.Jobs {
		j := &st.Jobs[i]
		if j.Machine.MAC != inv.MAC || terminal(j.State) {
			continue
		}
		if match != nil || j.State != "ready" || !time.Now().Before(j.Expires) || j.PXEEntryID != "" || j.ISO != e.ISO || j.Profile != e.Profile || j.Machine.Firmware != inv.Firmware {
			return nil, errors.New("此机器存在未完成任务；请在管理端完成或取消后再安装")
		}
		if err := eligibleDisk(inv, j.Machine.DiskSerial); err != nil {
			return nil, err
		}
		match = j
	}
	return match, nil
}

// Installer consoles often have no CJK font. Keep their response readable,
// including for clients already booted from an older published entry.
func failPXE(w http.ResponseWriter, status int, err error) {
	message := err.Error()
	code := "PXE_REQUEST_REJECTED"
	switch message {
	case "检测到多块内置或连接位置不明的磁盘；请先在管理端指定目标盘并授权任务，然后重新网启":
		code = "MULTIPLE_DISKS"
		message = "Multiple internal or unknown disks detected. Select the target disk and authorize a matching installation task in Ventoy Center, then boot PXE again. No disk has been erased."
	case "此机器存在未完成任务；请在管理端完成或取消后再安装":
		code = "ACTIVE_TASK"
		message = "This machine has an unfinished or incompatible task. Review or cancel it in Ventoy Center before starting a new installation."
	case "PXE 客户端过旧，请刷新 iVentoy 后重新网启", "PXE 客户端版本不兼容，请刷新入口后重新网启":
		code = "OLD_CLIENT"
		message = "This PXE client is outdated. Refresh the published entry and boot PXE again."
	case "未找到可自动安装的内置磁盘":
		code = "NO_INTERNAL_DISK"
		message = "No internal installation disk was found. Check the disk controller and driver."
	case "目标磁盘序列号必须唯一匹配":
		code = "DISK_SERIAL_MISMATCH"
		message = "The authorized disk serial must match exactly one detected disk. Check the target disk in Ventoy Center."
	default:
		if strings.HasPrefix(message, "磁盘不可直接安装：") {
			code = "DISK_BLOCKED"
			message = "Target disk requires storage preparation: " + strings.TrimPrefix(message, "磁盘不可直接安装：")
		}
	}
	for _, r := range message {
		if r > 127 {
			message = "The controller rejected this installation request. Check the entry, task authorization and controller log. No installation will start."
			break
		}
	}
	log.Printf("PXE rejected [%s]: %s", code, err)
	respond(w, status, map[string]string{"error": message, "code": code})
}

func (s *Server) pxeDiscover(w http.ResponseWriter, r *http.Request) {
	var inv PXEInventory
	if !decode(w, r, &inv) {
		return
	}
	if err := validateInventory(&inv); err != nil {
		failPXE(w, 400, err)
		return
	}
	var response map[string]any
	err := s.store.update(func(st *State) error {
		e, err := s.authorizePXE(st, r.PathValue("id"), bearer(r))
		if err != nil {
			return err
		}
		if err := pxeResponseFormat(*e, r); err != nil {
			return err
		}
		j, err := matchingPXEJob(st, *e, inv)
		if err != nil {
			return err
		}
		policy, err := pxeDiskPolicy(*e)
		if err != nil {
			return err
		}
		if policy != pxeConsole && inv.Version != pxeInventoryVersion {
			return errors.New("PXE 客户端过旧，请刷新 iVentoy 后重新网启")
		}
		preset, automatic := "", ""
		filesystem, err := newLinuxFilesystem(e.Request.Filesystem, e.Request.OS)
		if err != nil {
			return err
		}
		presetFilesystem := ""
		if j != nil {
			preset = j.Machine.DiskSerial
			presetFilesystem, err = linuxFilesystem(j.Request.Filesystem)
			if err != nil {
				return err
			}
		} else {
			automatic, err = automaticPXEDisk(inv, policy)
			if err != nil {
				return err
			}
		}
		kept := make([]PXETicket, 0, len(st.PXETickets))
		for _, t := range st.PXETickets {
			if time.Now().Before(t.Expires) && !(t.EntryID == e.ID && t.Inventory.MAC == inv.MAC) {
				kept = append(kept, t)
			}
		}
		if len(kept) >= 256 {
			return errors.New("等待确认的客户端过多，请稍后再试")
		}
		t := PXETicket{ID: randomID(24), EntryID: e.ID, Inventory: inv, DiskPolicy: policy, Expires: time.Now().UTC().Add(15 * time.Minute)}
		st.PXETickets = append(kept, t)
		response = map[string]any{"ticket": t.ID, "label": e.Label, "preset_disk": preset, "automatic_disk": automatic, "disk_policy": policy, "filesystem": filesystem, "preset_filesystem": presetFilesystem, "expires": t.Expires}
		return nil
	})
	if err != nil {
		failPXE(w, 409, err)
		return
	}
	if r.Header.Get("Accept") == "text/x-ventoy-preseed" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, key := range []string{"ticket", "preset_disk", "automatic_disk", "disk_policy", "filesystem", "preset_filesystem"} {
			fmt.Fprintf(w, "%s=%s\n", key, response[key])
		}
		return
	}
	respond(w, 200, response)
}

// d-i has no Python/JSON runtime. The same authorization and one-use tickets
// are exposed as fixed text fields and a native preseed response for this client.
func pxeResponseFormat(e PrivatePXEEntry, r *http.Request) error {
	if r.Header.Get("Accept") != "text/x-ventoy-preseed" {
		return nil
	}
	p, err := pxeProfile(e.Profile)
	if err != nil || p.Installer != "preseed" || (e.DiskPolicy != pxeSingleInternal && e.DiskPolicy != pxeLocalSelection) {
		return errors.New("Debian 自动安装要求对应模板和兼容的选盘策略")
	}
	return nil
}
func (s *Server) pxeInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ticket       string       `json:"ticket"`
		DiskSerial   string       `json:"disk_serial"`
		Confirmation string       `json:"confirmation"`
		StoragePlan  *StoragePlan `json:"storage_plan,omitempty"`
		Filesystem   string       `json:"filesystem,omitempty"`
	}
	if !decode(w, r, &req) {
		return
	}
	filesystem, err := linuxFilesystem(req.Filesystem)
	if err != nil {
		failPXE(w, 400, err)
		return
	}
	var payload map[string]string
	err = s.store.update(func(st *State) error {
		e, err := s.authorizePXE(st, r.PathValue("id"), bearer(r))
		if err != nil {
			return err
		}
		if err := pxeResponseFormat(*e, r); err != nil {
			return err
		}
		idx := -1
		for i, t := range st.PXETickets {
			if t.EntryID == e.ID && t.ID == req.Ticket && time.Now().Before(t.Expires) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return errors.New("本次确认已过期或已使用，请重新网启")
		}
		inv := st.PXETickets[idx].Inventory
		policy, err := pxeDiskPolicy(*e)
		if err != nil {
			return err
		}
		if st.PXETickets[idx].DiskPolicy != policy {
			return errors.New("选盘规则已改变，请重新网启")
		}
		if req.StoragePlan != nil {
			p, err := pxeProfile(e.Profile)
			if err != nil || !rebuildInstaller(p) || (raidPlan(req.StoragePlan) && !raidInstaller(p)) {
				return errors.New("This installer does not support the selected storage rebuild")
			}
			if policy != pxeLocalSelection || inv.Version != pxeInventoryVersion {
				return errors.New("Storage rebuild requires the current local-selection menu")
			}
			if err := validateStoragePlan(req.StoragePlan, inv.Disks); err != nil {
				return err
			}
			if req.DiskSerial != req.StoragePlan.DiskSerials[0] || req.Confirmation != storageConfirmation(*req.StoragePlan) {
				return errors.New("Confirm the selected rebuild disks on the local console")
			}
		} else if err := eligibleDisk(inv, req.DiskSerial); err != nil {
			return err
		}
		j, err := matchingPXEJob(st, *e, inv)
		if err != nil {
			return err
		}
		if j != nil {
			presetFilesystem, err := linuxFilesystem(j.Request.Filesystem)
			if req.Filesystem == "" {
				filesystem = presetFilesystem
			}
			if err != nil || presetFilesystem != filesystem {
				return errors.New("Filesystem does not match the existing authorized task")
			}
			if req.StoragePlan != nil {
				return errors.New("An existing single-disk task cannot authorize a two-disk rebuild")
			}
			if j.Machine.DiskSerial != req.DiskSerial {
				return errors.New("目标盘与已有授权不匹配")
			}
			j.PXEEntryID = e.ID
			j.Filesystem = filesystem
		} else {
			if req.Filesystem == "" {
				filesystem, err = newLinuxFilesystem(e.Request.Filesystem, e.Request.OS)
				if err != nil {
					return err
				}
			}
			message := "PXE 控制台已确认清空磁盘 " + req.DiskSerial
			serial := ""
			if req.StoragePlan == nil {
				serial, err = automaticPXEDisk(inv, policy)
			}
			if err != nil {
				return err
			}
			if req.StoragePlan != nil {
				message = "PXE 控制台确认存储重建 " + req.StoragePlan.Mode + "：" + strings.Join(req.StoragePlan.DiskSerials, ", ")
			} else if serial != "" {
				if req.DiskSerial != serial {
					return errors.New("目标盘与单内置盘规则不匹配")
				}
				message = "PXE 自动安装：按单内置盘规则选择 " + serial
			} else if req.Confirmation != "ERASE "+req.DiskSerial {
				return errors.New("必须在本机控制台确认清空所选磁盘")
			}
			m := Machine{ID: randomID(8), Name: "PXE" + strings.ReplaceAll(inv.MAC, ":", ""), MAC: inv.MAC, Serial: "PXE-" + strings.ReplaceAll(inv.MAC, ":", ""), DiskSerial: req.DiskSerial, Firmware: inv.Firmware}
			if err := validateMachine(&m); err != nil {
				return err
			}
			found := false
			for i := range st.Machines {
				if st.Machines[i].MAC == inv.MAC {
					m = st.Machines[i]
					m.DiskSerial = req.DiskSerial
					m.Firmware = inv.Firmware
					st.Machines[i] = m
					found = true
					break
				}
			}
			if !found {
				for _, old := range st.Machines {
					if old.Name == m.Name || old.Serial == m.Serial {
						return errors.New("自动生成的机器名称已存在，请在后台登记此 MAC")
					}
				}
				st.Machines = append(st.Machines, m)
			}
			jr := e.Request
			s.config.applyLinuxAccounts(&jr)
			jr.MachineID = m.ID
			jr.StoragePlan = req.StoragePlan
			jr.Filesystem = filesystem
			now := time.Now().UTC()
			job := PrivateJob{Job: Job{ID: randomID(12), Machine: m, OS: jr.OS, Profile: jr.Profile, ISO: jr.ISO, PXEEntryID: e.ID, Created: now, Expires: now.Add(24 * time.Hour), Events: []Event{}}, Request: jr, Token: randomID(32)}
			addEvent(&job, "ready", message+"; filesystem="+filesystem)
			job.StoragePlan = jr.StoragePlan
			job.Filesystem = filesystem
			st.Jobs = append(st.Jobs, job)
			j = &st.Jobs[len(st.Jobs)-1]
		}
		files, err := s.renderFiles(*j)
		if err != nil {
			return err
		}
		p, err := pxeProfile(j.Profile)
		if err != nil {
			return err
		}
		name := "kickstart.cfg"
		if p.Installer == "autoinstall" {
			name = "user-data"
		} else if p.Installer == "preseed" || p.Installer == "ubiquity" {
			name = "preseed.cfg"
		}
		payload = map[string]string{"job_id": j.ID, "installer": p.Installer, "config": files[name], "preinstall": files["preinstall.sh"]}
		if p.Installer == "autoinstall" {
			// The entry's early error hook runs before Subiquity rereads the
			// selected config. Bind that hook to this job in live RAM as well.
			payload["install_failure"] = files["ubuntu-install-failed.py"]
		}
		st.PXETickets = append(st.PXETickets[:idx], st.PXETickets[idx+1:]...)
		return nil
	})
	if err != nil {
		failPXE(w, 409, err)
		return
	}
	if r.Header.Get("Accept") == "text/x-ventoy-preseed" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, payload["config"])
		return
	}
	respond(w, 200, payload)
}
