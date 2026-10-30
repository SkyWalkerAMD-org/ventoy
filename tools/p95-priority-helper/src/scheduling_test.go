package main

import (
	"os"
	"testing"
)

func TestSchedulingRequiresRealCPUController(t *testing.T) {
	group := "/system.slice/fyhocsys-node.service"
	for _, tc := range []struct {
		name, membership, weight string
		ok                       bool
	}{
		{"v2_without_cpu", "0::" + group + "\n", "", false},
		{"v2_active", "0::" + group + "\n", "100\n", true},
		{"v2_invalid_weight", "0::" + group + "\n", "idle", false},
		{"v2_other_service", "0::/system.slice/unrelated.service\n", "100", false},
		{"v1_cpu_root", "1:name=systemd:" + group + "\n2:cpu,cpuacct:/\n", "", false},
		{"v1_cpu_active", "1:name=systemd:" + group + "\n2:cpu,cpuacct:" + group + "\n", "", true},
		{"hybrid_cpu_root", "0::" + group + "\n2:cpu,cpuacct:/\n", "100", false},
		{"systemd_only", "1:name=systemd:" + group + "\n", "", false},
		{"path_escape", "0::" + group + "/../other\n", "100", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := func(name string) ([]byte, error) {
				if name != "/sys/fs/cgroup"+group+"/cpu.weight" || tc.weight == "" {
					return nil, os.ErrNotExist
				}
				return []byte(tc.weight), nil
			}
			got, err := inspectScheduling([]byte(tc.membership), group, read)
			if (err == nil) != tc.ok || got.CPUActive != tc.ok {
				t.Fatalf("got %+v err=%v; want active=%v", got, err, tc.ok)
			}
		})
	}
}
