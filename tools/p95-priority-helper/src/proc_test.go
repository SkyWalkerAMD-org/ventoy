package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const testGroup = "/system.slice/fyhocsys-node.service"

func statText(id int, start uint64, nice, policy int, ticks uint64) []byte {
	f := make([]string, 45)
	for i := range f {
		f[i] = "0"
	}
	f[0], f[11], f[16], f[19], f[38] = "R", strconv.FormatUint(ticks, 10), strconv.Itoa(nice), strconv.FormatUint(start, 10), strconv.Itoa(policy)
	return []byte(fmt.Sprintf("%d (mprime ) worker) %s", id, strings.Join(f, " ")))
}

type fakeProc struct {
	p      procFS
	links  map[string]string
	t      *testing.T
	writes []int
}

func fake(t *testing.T) *fakeProc {
	f := &fakeProc{t: t, links: map[string]string{}}
	f.p = procFS{Root: t.TempDir(), Product: "fyhocsys", ReadFile: os.ReadFile, ReadDir: os.ReadDir}
	f.p.Readlink = func(path string) (string, error) {
		v, ok := f.links[path]
		if !ok {
			return "", os.ErrNotExist
		}
		return v, nil
	}
	return f
}

func (f *fakeProc) put(path string, data []byte) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeProc) add(pid int, tool string, start uint64, nice int) {
	dir := filepath.Join(f.p.Root, strconv.Itoa(pid))
	cwd := "/var/lib/fyhocsys/node/runs/batch/steps/step-001/" + map[string]string{"mprime": "cpu", "memtester": "memory"}[tool]
	f.links[filepath.Join(dir, "exe")] = "/opt/fyhocsys/workloads/0.1.0/" + tool + "/" + tool
	f.links[filepath.Join(dir, "cwd")] = cwd
	f.put(filepath.Join(dir, "stat"), statText(pid, start, nice, 0, 100))
	f.put(filepath.Join(dir, "cgroup"), []byte("1:name=systemd:"+testGroup+"\n"))
	f.put(filepath.Join(dir, "cmdline"), []byte(tool+"\x00-t\x00-W"+cwd+"/\x00"))
	f.addThread(pid, pid, start, nice, 0)
}

func (f *fakeProc) addThread(pid, tid int, start uint64, nice, policy int) {
	dir := filepath.Join(f.p.Root, strconv.Itoa(pid), "task", strconv.Itoa(tid))
	f.put(filepath.Join(dir, "stat"), statText(tid, start, nice, policy, 20))
	f.put(filepath.Join(dir, "cgroup"), []byte("0::"+testGroup+"\n"))
}

func (f *fakeProc) ctl() controller {
	c := controller{Proc: f.p, State: &journal{Version: 1, Product: "fyhocsys", BootID: "boot", Threads: map[string]savedThread{}}, Save: func() error { return nil }}
	c.Set = func(tid, nice int) error {
		entries, _ := os.ReadDir(f.p.Root)
		for _, e := range entries {
			path := filepath.Join(f.p.Root, e.Name(), "task", strconv.Itoa(tid), "stat")
			s, err := f.p.readStat(path)
			if err != nil {
				continue
			}
			f.put(path, statText(tid, s.Start, nice, s.Policy, s.Ticks))
			f.writes = append(f.writes, tid)
			return nil
		}
		return syscall.ESRCH
	}
	return c
}

func TestStatAndCgroups(t *testing.T) {
	s, err := parseStat(statText(512, 9876, 19, 3, 202))
	if err != nil || s.ID != 512 || s.Start != 9876 || s.Nice != 19 || s.Policy != 3 || s.Ticks != 202 {
		t.Fatalf("bad parse %+v %v", s, err)
	}
	if _, err := parseStat([]byte("12 (bad) R 1")); err == nil {
		t.Fatal("accepted truncated stat")
	}
	for _, tc := range []struct {
		raw, group string
		yes        bool
	}{
		{"0::" + testGroup, testGroup, true}, {"1:name=systemd:" + testGroup + "/worker", testGroup, true},
		{"2:cpu:" + testGroup, testGroup, false}, {"0::" + testGroup + "-other", testGroup, false},
		{"0::/system.slice/unrelated.service", testGroup, false}, {"0::/", "/", false}, {"0::/", "", false},
	} {
		if got := inService([]byte(tc.raw), tc.group); got != tc.yes {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
}

func TestCPUHierarchyIncludingHybrid(t *testing.T) {
	for raw, want := range map[string]string{
		"0::/system.slice/fyhocsys-node.service":            "v2:/system.slice/fyhocsys-node.service",
		"2:cpu,cpuacct:/system.slice/fyhocsys-node.service": "v1:/system.slice/fyhocsys-node.service",
		"0::/\n2:cpu,cpuacct:/node":                         "v1:/node",
		"1:name=systemd:/node":                              "not exposed",
	} {
		if got := cpuGroup([]byte(raw)); got != want {
			t.Fatalf("%q got %q want %q", raw, got, want)
		}
	}
}

func TestAllWorkerThreadsAndLateWorkers(t *testing.T) {
	f := fake(t)
	f.add(100, "mprime", 1000, 0)
	f.addThread(100, 101, 1001, 19, 0)
	f.addThread(100, 102, 1002, 19, 3)
	f.addThread(100, 103, 1003, 5, 0)
	f.addThread(100, 104, 1004, 19, 1)
	f.add(200, "memtester", 2000, 19)
	c := f.ctl()
	n, err := c.adjust(testGroup)
	if err != nil || n != 2 {
		t.Fatalf("adjust %d %v", n, err)
	}
	if fmt.Sprint(f.writes) != "[101 102]" {
		t.Fatalf("wrong targets: %v", f.writes)
	}
	if n, err = c.adjust(testGroup); err != nil || n != 0 {
		t.Fatal("not idempotent", n, err)
	}
	f.addThread(100, 105, 1005, 19, 0)
	if n, err = c.adjust(testGroup); err != nil || n != 1 {
		t.Fatal("late worker not adjusted", n, err)
	}
	if n, err = c.restore(testGroup); err != nil || n != 3 || len(c.State.Threads) != 0 {
		t.Fatal("restore", n, err, c.State)
	}
	for _, tid := range []int{101, 102, 105} {
		s, _ := f.p.thread(100, tid, testGroup)
		if s.Nice != 19 {
			t.Fatal("not restored", tid)
		}
	}
}

func TestRejectUnmanagedProcesses(t *testing.T) {
	for _, kind := range []string{"wrong_exe", "wrong_group", "group_prefix", "wrong_cwd", "not_torture", "wrong_work"} {
		t.Run(kind, func(t *testing.T) {
			f := fake(t)
			f.add(100, "mprime", 1000, 19)
			dir := filepath.Join(f.p.Root, "100")
			switch kind {
			case "wrong_exe":
				f.links[filepath.Join(dir, "exe")] = "/tmp/mprime"
			case "wrong_group":
				f.put(filepath.Join(dir, "cgroup"), []byte("0::/user.slice"))
			case "group_prefix":
				f.put(filepath.Join(dir, "cgroup"), []byte("0::"+testGroup+"-other"))
			case "wrong_cwd":
				f.links[filepath.Join(dir, "cwd")] = "/var/lib/fyhocsys/node-other/step"
			case "not_torture":
				f.put(filepath.Join(dir, "cmdline"), []byte("mprime\x00"))
			case "wrong_work":
				f.put(filepath.Join(dir, "cmdline"), []byte("mprime\x00-t\x00-W/tmp/\x00"))
			}
			c := f.ctl()
			n, err := c.adjust(testGroup)
			if n != 0 || err != nil || len(f.writes) != 0 {
				t.Fatal("touched unmanaged process", n, err)
			}
		})
	}
}

func TestJournalPrecedesMutationAndIdentityRecheck(t *testing.T) {
	for _, kind := range []string{"write_failure", "pid_reused", "tid_reused", "moved_group", "exited_at_syscall"} {
		t.Run(kind, func(t *testing.T) {
			f := fake(t)
			f.add(100, "mprime", 1000, 19)
			c := f.ctl()
			c.Save = func() error {
				if len(f.writes) != 0 {
					t.Fatal("write-ahead violated")
				}
				switch kind {
				case "write_failure":
					return errors.New("disk failure")
				case "pid_reused":
					f.put(filepath.Join(f.p.Root, "100", "stat"), statText(100, 2000, 19, 0, 100))
				case "tid_reused":
					f.addThread(100, 100, 2000, 19, 0)
				case "moved_group":
					f.put(filepath.Join(f.p.Root, "100", "cgroup"), []byte("0::/other"))
				}
				return nil
			}
			if kind == "exited_at_syscall" {
				c.Set = func(int, int) error { return syscall.ESRCH }
			}
			n, err := c.adjust(testGroup)
			if n != 0 || len(f.writes) != 0 {
				t.Fatal("unsafe mutation", n, err)
			}
			if (err != nil) != (kind == "write_failure") {
				t.Fatal("wrong error", err)
			}
		})
	}
}

func TestRestoreRespectsChangesAndReusedIDs(t *testing.T) {
	for _, kind := range []string{"manual_nice", "pid_reused", "tid_reused", "policy_changed"} {
		t.Run(kind, func(t *testing.T) {
			f := fake(t)
			f.add(100, "mprime", 1000, 19)
			c := f.ctl()
			if _, err := c.adjust(testGroup); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "manual_nice":
				f.addThread(100, 100, 1000, 7, 0)
			case "pid_reused":
				f.put(filepath.Join(f.p.Root, "100", "stat"), statText(100, 2000, 0, 0, 100))
			case "tid_reused":
				f.addThread(100, 100, 2000, 0, 0)
			case "policy_changed":
				f.addThread(100, 100, 1000, 0, 1)
			}
			n, err := c.restore(testGroup)
			if n != 0 || err != nil || len(f.writes) != 1 {
				t.Fatal("unsafe restore", n, err)
			}
		})
	}
}

func TestJournalBootAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := savedThread{100, 101, 1000, 1001, 19}
	j := journal{1, "fyhocsys", "old-boot", map[string]savedThread{s.key(): s}}
	b, _ := json.Marshal(j)
	os.WriteFile(path, b, 0600)
	got, err := loadState(path, "fyhocsys", "new-boot")
	if err != nil || len(got.Threads) != 0 {
		t.Fatal("old boot retained", err)
	}
	if _, err = loadState(path, "sckocstp", "old-boot"); err == nil {
		t.Fatal("wrong product accepted")
	}
	j.Threads[s.key()] = savedThread{100, 101, 1000, 1001, -10}
	b, _ = json.Marshal(j)
	os.WriteFile(path, b, 0600)
	if _, err = loadState(path, "fyhocsys", "old-boot"); err == nil {
		t.Fatal("invalid original nice accepted")
	}
}

func TestPermissionFailureRetainsUndo(t *testing.T) {
	f := fake(t)
	f.add(100, "mprime", 1000, 19)
	c := f.ctl()
	if _, err := c.adjust(testGroup); err != nil {
		t.Fatal(err)
	}
	c.Proc.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "read", Path: path, Err: syscall.EACCES}
	}
	if n, err := c.restore(testGroup); err == nil || n != 0 || len(c.State.Threads) != 1 {
		t.Fatal("lost undo on permission failure", n, err)
	}
}

func TestConcurrentProgressAndPIDReuse(t *testing.T) {
	p := process{Stat: taskStat{ID: 1, Start: 11, Ticks: 100}, Tool: "mprime", CWD: "step/cpu", Batch: "step", Threads: []taskStat{{ID: 2, Start: 12, Ticks: 100, Nice: 0}}}
	m := process{Stat: taskStat{ID: 3, Start: 13, Ticks: 100}, Tool: "memtester", CWD: "step/memory", Batch: "step", Threads: []taskStat{{ID: 3, Start: 13, Ticks: 100, Nice: 0}}}
	a, b := p, m
	a.Stat.Ticks = 600
	b.Stat.Ticks = 1100
	a.Threads = []taskStat{{ID: 2, Start: 12, Ticks: 600, Nice: 0}}
	got := summarize([]process{p, m}, []process{a, b}, 10, 100)
	if len(got) != 1 || !got[0].BothProgress || got[0].P95.AverageCores != 0.5 || got[0].Memory.AverageCores != 1 || got[0].P95.ThreadsProgress != 1 {
		t.Fatalf("bad progress %+v", got)
	}
	b.Stat.Start = 99
	got = summarize([]process{p, m}, []process{a, b}, 10, 100)
	if got[0].BothProgress {
		t.Fatal("PID reuse reported as progress")
	}
	b = m
	b.Stat.Ticks = 1100
	b.Batch = "another-step"
	got = summarize([]process{p, m}, []process{a, b}, 10, 100)
	for _, s := range got {
		if s.BothProgress {
			t.Fatal("different steps treated as concurrent")
		}
	}
}
