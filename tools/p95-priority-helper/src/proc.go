package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type taskStat struct {
	ID     int
	Start  uint64
	Ticks  uint64
	Nice   int
	Policy int
	State  string
}

// comm may contain spaces and closing parentheses. Fields after its final ')'
// start at field 3; starttime is field 22, nice is field 19, policy is field 41.
func parseStat(raw []byte) (taskStat, error) {
	var s taskStat
	left, right := bytes.IndexByte(raw, '('), bytes.LastIndexByte(raw, ')')
	if left < 1 || right <= left {
		return s, errors.New("invalid proc stat")
	}
	fields := strings.Fields(string(raw[right+1:]))
	if len(fields) < 39 {
		return s, errors.New("short proc stat")
	}
	var err error
	if s.ID, err = strconv.Atoi(strings.TrimSpace(string(raw[:left]))); err != nil {
		return s, err
	}
	if s.Start, err = strconv.ParseUint(fields[19], 10, 64); err != nil {
		return s, err
	}
	u, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return s, err
	}
	v, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return s, err
	}
	if s.Nice, err = strconv.Atoi(fields[16]); err != nil {
		return s, err
	}
	if s.Policy, err = strconv.Atoi(fields[38]); err != nil {
		return s, err
	}
	s.Ticks, s.State = u+v, fields[0]
	return s, nil
}

func inTree(path, parent string) bool {
	return path == parent || strings.HasPrefix(path, parent+"/")
}

// The systemd hierarchy on cgroup v1, or unified hierarchy on v2, identifies
// service ownership. A CPU controller's coincidentally matching path is not enough.
func inService(raw []byte, group string) bool {
	if group == "" || group == "/" || !strings.HasPrefix(group, "/") {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		v := strings.SplitN(line, ":", 3)
		if len(v) == 3 && ((v[0] == "0" && v[1] == "") || v[1] == "name=systemd") && inTree(v[2], group) {
			return true
		}
	}
	return false
}

type process struct {
	Stat      taskStat
	Tool      string
	Exe       string
	CWD       string
	Batch     string
	Threads   []taskStat
	CPUGroup  string
	AutoGroup string
}

func cpuGroup(raw []byte) string {
	unified := "not exposed"
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		v := strings.SplitN(line, ":", 3)
		if len(v) != 3 {
			continue
		}
		if v[0] == "0" && v[1] == "" {
			unified = "v2:" + v[2]
		}
		for _, controller := range strings.Split(v[1], ",") {
			if controller == "cpu" {
				return "v1:" + v[2]
			}
		}
	}
	return unified
}

type procFS struct {
	Root    string
	Product string
	// Functions make the real discovery and identity checks testable on Windows.
	ReadFile func(string) ([]byte, error)
	Readlink func(string) (string, error)
	ReadDir  func(string) ([]os.DirEntry, error)
}

func newProc(product string) procFS {
	return procFS{"/proc", product, os.ReadFile, os.Readlink, os.ReadDir}
}

func (p procFS) readStat(path string) (taskStat, error) {
	b, err := p.ReadFile(path)
	if err != nil {
		return taskStat{}, err
	}
	return parseStat(b)
}

// Fixed paths from the 0.8.6 node packages, including their EL7 build and
// separately versioned memtester bundle. Never accept a wildcard executable.
func toolPaths(product, tool string) []string {
	versions := []string{"0.1.0"}
	switch tool {
	case "mprime":
		versions = append(versions, "0.1.0-el7")
	case "memtester":
		versions = append(versions, "extra-0.2.0")
	default:
		return nil
	}
	var paths []string
	for _, version := range versions {
		paths = append(paths, "/opt/"+product+"/workloads/"+version+"/"+tool+"/"+tool)
	}
	return paths
}

func (p procFS) toolFor(exe string) string {
	for _, tool := range []string{"mprime", "memtester"} {
		for _, allowed := range toolPaths(p.Product, tool) {
			if exe == allowed {
				return tool
			}
		}
	}
	return ""
}

func (p procFS) readProcess(pid int, group string) (process, error) {
	var out process
	dir := filepath.Join(p.Root, strconv.Itoa(pid))
	exe, err := p.Readlink(filepath.Join(dir, "exe"))
	if err != nil {
		return out, err
	}
	tool := p.toolFor(exe)
	if tool == "" {
		return out, errors.New("unmanaged executable")
	}
	cg, err := p.ReadFile(filepath.Join(dir, "cgroup"))
	if err != nil {
		return out, err
	}
	if !inService(cg, group) {
		return out, errors.New("outside node service")
	}
	st, err := p.readStat(filepath.Join(dir, "stat"))
	if err != nil {
		return out, err
	}
	if st.ID != pid || st.State == "Z" || st.State == "X" {
		return out, errors.New("exited process")
	}
	cwd, err := p.Readlink(filepath.Join(dir, "cwd"))
	if err != nil {
		return out, err
	}
	if !inTree(cwd, "/var/lib/"+p.Product+"/node") {
		return out, errors.New("outside node data directory")
	}
	if tool == "mprime" {
		raw, err := p.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			return out, err
		}
		torture, work := false, false
		for _, arg := range strings.Split(string(raw), "\x00") {
			if arg == "-t" {
				torture = true
			}
			if arg == "-W"+cwd+"/" {
				work = true
			}
		}
		if !torture || !work {
			return out, errors.New("not a managed torture run")
		}
	}
	ag, _ := p.ReadFile(filepath.Join(dir, "autogroup"))
	return process{Stat: st, Tool: tool, Exe: exe, CWD: cwd, Batch: path.Dir(cwd), CPUGroup: cpuGroup(cg), AutoGroup: strings.TrimSpace(string(ag))}, nil
}

func (p procFS) thread(pid, tid int, group string) (taskStat, error) {
	dir := filepath.Join(p.Root, strconv.Itoa(pid), "task", strconv.Itoa(tid))
	cg, err := p.ReadFile(filepath.Join(dir, "cgroup"))
	if err != nil {
		return taskStat{}, err
	}
	if !inService(cg, group) {
		return taskStat{}, errors.New("thread outside node service")
	}
	s, err := p.readStat(filepath.Join(dir, "stat"))
	if err == nil && (s.ID != tid || s.State == "Z" || s.State == "X") {
		err = errors.New("exited thread")
	}
	return s, err
}

func (p procFS) discover(group string) ([]process, error) {
	entries, err := p.ReadDir(p.Root)
	if err != nil {
		return nil, err
	}
	var out []process
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		item, err := p.readProcess(pid, group)
		if err != nil {
			continue
		}
		tasks, err := p.ReadDir(filepath.Join(p.Root, entry.Name(), "task"))
		if err != nil {
			continue
		}
		for _, t := range tasks {
			tid, err := strconv.Atoi(t.Name())
			if err != nil {
				continue
			}
			st, err := p.thread(pid, tid, group)
			if err == nil {
				item.Threads = append(item.Threads, st)
			}
		}
		out = append(out, item)
	}
	return out, nil
}

type savedThread struct {
	PID          int    `json:"pid"`
	TID          int    `json:"tid"`
	ProcessStart uint64 `json:"process_start"`
	ThreadStart  uint64 `json:"thread_start"`
	Original     int    `json:"original_nice"`
}

func (s savedThread) key() string {
	return fmt.Sprintf("%d:%d:%d:%d", s.PID, s.ProcessStart, s.TID, s.ThreadStart)
}

func identity(pr process, th taskStat) savedThread {
	return savedThread{pr.Stat.ID, th.ID, pr.Stat.Start, th.Start, th.Nice}
}

// Revalidate both process and thread immediately before setpriority. Linux 4.18
// exposes this operation by numeric TID; it has no atomic pidfd equivalent.
func (p procFS) recheck(s savedThread, group string) (taskStat, error) {
	pr, err := p.readProcess(s.PID, group)
	if err != nil {
		return taskStat{}, err
	}
	if pr.Tool != "mprime" || pr.Stat.Start != s.ProcessStart {
		return taskStat{}, errors.New("process identity changed")
	}
	th, err := p.thread(s.PID, s.TID, group)
	if err != nil {
		return th, err
	}
	if th.Start != s.ThreadStart {
		return th, errors.New("thread identity changed")
	}
	return th, nil
}

type journal struct {
	Version int                    `json:"version"`
	Product string                 `json:"product"`
	BootID  string                 `json:"boot_id"`
	Threads map[string]savedThread `json:"threads"`
}

type controller struct {
	Proc  procFS
	State *journal
	Save  func() error
	Set   func(int, int) error
}

func ordinary(policy int) bool { return policy == 0 || policy == 3 }

func (c controller) adjust(group string) (int, error) {
	runs, err := c.Proc.discover(group)
	if err != nil {
		return 0, err
	}
	changed := false
	for k, s := range c.State.Threads {
		if _, err := c.Proc.recheck(s, group); err != nil {
			var ioerr *os.PathError
			if !os.IsNotExist(err) && errors.As(err, &ioerr) {
				return 0, err
			}
			delete(c.State.Threads, k)
			changed = true
		}
	}
	var candidates []savedThread
	for _, pr := range runs {
		if pr.Tool != "mprime" {
			continue
		}
		for _, th := range pr.Threads {
			if th.Nice != 19 || !ordinary(th.Policy) {
				continue
			}
			s := identity(pr, th)
			candidates = append(candidates, s)
			if _, ok := c.State.Threads[s.key()]; !ok {
				c.State.Threads[s.key()] = s
				changed = true
			}
		}
	}
	// Write the undo record before any scheduling change; fail closed on I/O errors.
	if changed {
		if err := c.Save(); err != nil {
			return 0, err
		}
	}
	n := 0
	for _, s := range candidates {
		th, err := c.Proc.recheck(s, group)
		if err != nil || th.Nice != 19 || !ordinary(th.Policy) {
			continue
		}
		if err := c.Set(s.TID, 0); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				continue
			}
			return n, fmt.Errorf("TID %d: %w", s.TID, err)
		}
		n++
	}
	return n, nil
}

func (c controller) restore(group string) (int, error) {
	n := 0
	var first error
	for k, s := range c.State.Threads {
		th, err := c.Proc.recheck(s, group)
		var ioerr *os.PathError
		if err != nil && !os.IsNotExist(err) && errors.As(err, &ioerr) {
			if first == nil {
				first = err
			}
			continue
		}
		if err != nil || th.Nice != 0 || !ordinary(th.Policy) {
			delete(c.State.Threads, k)
			continue
		}
		if err = c.Set(s.TID, s.Original); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				delete(c.State.Threads, k)
				continue
			}
			if first == nil {
				first = err
			}
			continue
		}
		n++
		delete(c.State.Threads, k)
	}
	if err := c.Save(); err != nil && first == nil {
		first = err
	}
	return n, first
}
