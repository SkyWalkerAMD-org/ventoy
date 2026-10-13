package center

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Machine struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MAC         string `json:"mac"`
	Serial      string `json:"serial"`
	DiskSerial  string `json:"disk_serial"`
	WindowsDisk int    `json:"windows_disk"`
	Firmware    string `json:"firmware"`
}

type JobRequest struct {
	MachineID    string          `json:"machine_id"`
	OS           string          `json:"os"`
	ISO          string          `json:"iso"`
	Username     string          `json:"username"`
	Password     string          `json:"password"`
	PasswordHash string          `json:"password_hash"`
	SSHKey       string          `json:"ssh_key"`
	WindowsImage int             `json:"windows_image"`
	Locale       string          `json:"locale"`
	Enroll       bool            `json:"enroll"`
	Package      string          `json:"package"`
	Connection   json.RawMessage `json:"connection,omitempty"`
}

type Event struct {
	State   string    `json:"state"`
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}
type Job struct {
	ID            string    `json:"id"`
	Machine       Machine   `json:"machine"`
	OS            string    `json:"os"`
	ISO           string    `json:"iso"`
	State         string    `json:"state"`
	Created       time.Time `json:"created"`
	Expires       time.Time `json:"expires"`
	Enroll        bool      `json:"enroll"`
	Package       string    `json:"package,omitempty"`
	PackageSHA256 string    `json:"package_sha256,omitempty"`
	Events        []Event   `json:"events"`
}
type PrivateJob struct {
	Job
	Request JobRequest `json:"request"`
	Token   string     `json:"token"`
}
type State struct {
	Schema   int          `json:"schema"`
	Machines []Machine    `json:"machines"`
	Jobs     []PrivateJob `json:"jobs"`
}
type Store struct {
	mu    sync.Mutex
	path  string
	state State
}

var hostnameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{0,14}$`)
var serialRE = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,120}$`)
var userRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,19}$`)
var hashRE = regexp.MustCompile(`^\$6\$(rounds=[0-9]+\$)?[./A-Za-z0-9]{1,16}\$[./A-Za-z0-9]{86}$`)

func validateMachine(m *Machine) error {
	if !hostnameRE.MatchString(m.Name) {
		return errors.New("节点名须为字母开头的 1–15 位字母、数字或连字符")
	}
	m.Name = strings.ToUpper(m.Name)
	mac, err := net.ParseMAC(m.MAC)
	if err != nil || len(mac) != 6 || mac[0]&1 != 0 || mac.String() == "00:00:00:00:00:00" {
		return errors.New("请输入有效的单播 MAC 地址")
	}
	m.MAC = mac.String()
	if !serialRE.MatchString(m.Serial) || !serialRE.MatchString(m.DiskSerial) {
		return errors.New("资产编号和目标磁盘序列号须为 1–120 位字母、数字或 _ . : / -")
	}
	if m.WindowsDisk < 0 || m.WindowsDisk > 255 {
		return errors.New("Windows 磁盘编号须在 0–255 之间")
	}
	if m.Firmware == "" {
		m.Firmware = "uefi"
	}
	if m.Firmware != "uefi" && m.Firmware != "bios" {
		return errors.New("启动模式须为 uefi 或 bios")
	}
	return nil
}

func validateJob(r JobRequest) error {
	if r.OS != "windows" && r.OS != "rocky" && r.OS != "ubuntu" {
		return errors.New("系统类型须为 windows、rocky 或 ubuntu")
	}
	if !userRE.MatchString(r.Username) || r.Username == "root" {
		return errors.New("请设置有效的普通管理员用户名（不可为 root）")
	}
	if r.OS == "windows" {
		if r.Locale != "zh-CN" && r.Locale != "en-US" {
			return errors.New("Windows 语言须与镜像匹配：zh-CN 或 en-US")
		}
		if r.Username == "administrator" {
			return errors.New("请选择独立的本地管理员用户名，不使用内置 administrator")
		}
		if r.WindowsImage < 1 || r.WindowsImage > 100 {
			return errors.New("Windows 镜像索引须为 1–100")
		}
		if len(r.Password) < 14 || len(r.Password) > 128 || strings.ContainsAny(r.Password, "\x00\r\n") {
			return errors.New("Windows 管理员密码须为 14–128 字节且不含换行")
		}
		if r.Enroll {
			return errors.New("当前 fyhocsys 没有 Windows 节点包；Windows 任务暂不能启用自动接入")
		}
	} else {
		if !hashRE.MatchString(r.PasswordHash) {
			return errors.New("Linux 需要完整的 SHA-512 crypt 密码哈希（可用 openssl passwd -6 生成）")
		}
		if r.SSHKey != "" && (!strings.HasPrefix(r.SSHKey, "ssh-ed25519 ") && !strings.HasPrefix(r.SSHKey, "ssh-rsa ") || strings.ContainsAny(r.SSHKey, "\r\n\x00")) {
			return errors.New("SSH 公钥格式无效")
		}
	}
	if r.Enroll {
		var value map[string]any
		if json.Unmarshal(r.Connection, &value) != nil || len(value) == 0 {
			return errors.New("请导入中心生成的专属节点连接 JSON 文件")
		}
		if r.Package == "" {
			return errors.New("请选择 fyhocsys 节点包")
		}
	}
	return nil
}

func openStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "state.json"), state: State{Schema: 1, Machines: []Machine{}, Jobs: []PrivateJob{}}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &s.state); err != nil {
		return nil, err
	}
	if s.state.Schema != 1 {
		return nil, errors.New("unsupported state schema")
	}
	return s, nil
}

// Changes become visible only after the new snapshot is durably written.
func (s *Store) update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.state)
	var next State
	if err := json.Unmarshal(b, &next); err != nil {
		return err
	}
	if err := fn(&next); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".state-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	replaced, err := replaceSnapshot(tmp, s.path)
	if replaced {
		// Even if the directory sync fails, never roll a consumed claim back in memory.
		s.state = next
	}
	return err
}
func (s *Store) snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.state)
	var copy State
	json.Unmarshal(b, &copy)
	return copy
}
func findJob(s *State, id string) *PrivateJob {
	for i := range s.Jobs {
		if s.Jobs[i].ID == id {
			return &s.Jobs[i]
		}
	}
	return nil
}
func terminal(state string) bool {
	return state == "enrolled" || state == "installed" || state == "failed" || state == "canceled"
}
func addEvent(j *PrivateJob, state, message string) {
	j.State = state
	j.Events = append(j.Events, Event{State: state, Time: time.Now().UTC(), Message: message})
	if terminal(state) {
		j.Token = ""
		j.Request.Connection = nil
		j.Request.Password = ""
		j.Request.PasswordHash = ""
	}
}

func transition(j *PrivateJob, next string) error {
	allowed := map[string][]string{"ready": {"installing", "failed"}, "installing": {"installed", "enrolling", "failed"}, "enrolling": {"enrolled", "failed"}}
	for _, v := range allowed[j.State] {
		if next == v {
			if next == "enrolling" && !j.Enroll {
				return errors.New("任务未配置 fyhocsys 接入")
			}
			if next == "installed" && j.Enroll {
				return errors.New("任务需要完成 fyhocsys 接入")
			}
			addEvent(j, next, "")
			return nil
		}
	}
	return errors.New("当前任务状态不允许此操作；任务不会重复执行")
}
