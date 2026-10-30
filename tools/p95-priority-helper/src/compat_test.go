package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestEL7PackagedToolsAndV1Threads(t *testing.T) {
	f := fake(t)
	f.add(100, "mprime", 1000, 19)
	f.add(200, "memtester", 2000, 19)
	f.links[filepath.Join(f.p.Root, "100", "exe")] = "/opt/fyhocsys/workloads/0.1.0-el7/mprime/mprime"
	f.links[filepath.Join(f.p.Root, "200", "exe")] = "/opt/fyhocsys/workloads/extra-0.2.0/memtester/memtester"
	f.addThread(100, 101, 1001, 19, 0)
	for _, tid := range []int{100, 101} {
		f.put(filepath.Join(f.p.Root, "100", "task", fmt.Sprint(tid), "cgroup"), []byte("1:name=systemd:"+testGroup+"\n"))
	}
	processes, err := f.p.discover(testGroup)
	if err != nil || len(processes) != 2 {
		t.Fatalf("EL7 workloads not discovered: %v %v", processes, err)
	}
	c := f.ctl()
	if n, err := c.adjust(testGroup); err != nil || n != 2 || fmt.Sprint(f.writes) != "[100 101]" {
		t.Fatalf("wrong EL7 priority changes: n=%d err=%v writes=%v", n, err, f.writes)
	}
	if n, err := c.restore(testGroup); err != nil || n != 2 {
		t.Fatalf("EL7 undo failed: n=%d err=%v", n, err)
	}
}

func TestPackagedPathsDoNotWidenOwnership(t *testing.T) {
	p := newProc("sckocstp")
	for _, exe := range []string{
		"/opt/fyhocsys/workloads/0.1.0-el7/mprime/mprime",
		"/opt/sckocstp/workloads/0.1.0-el7-other/mprime/mprime",
		"/opt/sckocstp/workloads/extra-0.2.0/mprime/mprime",
		"/opt/sckocstp/workloads/0.1.0-el7/mprime/mprime (deleted)",
		"/tmp/0.1.0-el7/mprime/mprime",
	} {
		if p.toolFor(exe) != "" {
			t.Fatalf("accepted unmanaged executable %q", exe)
		}
	}
	for _, tool := range []string{"mprime", "memtester"} {
		for _, exe := range toolPaths("sckocstp", tool) {
			if p.toolFor(exe) != tool {
				t.Fatalf("rejected packaged executable %q", exe)
			}
		}
	}
}
