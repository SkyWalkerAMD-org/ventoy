package main

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
)

type schedulingState struct {
	NodeActive bool   `json:"node_active"`
	CPUActive  bool   `json:"cpu_controller_active"`
	Group      string `json:"node_cpu_group"`
	Reason     string `json:"reason"`
}

// A v2 membership line alone does NOT prove CPU scheduling is enabled. Without
// cpu.weight at this level, setsid() workers can still compete as autogroups.
// On v1 the cpu controller's own membership must name the node's non-root group.
func inspectScheduling(raw []byte, group string, read func(string) ([]byte, error)) (schedulingState, error) {
	s := schedulingState{NodeActive: true, Group: cpuGroup(raw)}
	if group == "" || group == "/" || !strings.HasPrefix(group, "/") || path.Clean(group) != group {
		return s, errors.New("invalid node service group")
	}
	if strings.HasPrefix(s.Group, "v1:") {
		cpu := strings.TrimPrefix(s.Group, "v1:")
		if cpu != "/" && path.Clean(cpu) == cpu && inTree(cpu, group) {
			s.CPUActive, s.Reason = true, "node workers use a non-root CPU cgroup; autogroup is overridden"
			return s, nil
		}
	} else if strings.HasPrefix(s.Group, "v2:") {
		cpu := strings.TrimPrefix(s.Group, "v2:")
		if cpu != "/" && path.Clean(cpu) == cpu && inTree(cpu, group) {
			rawWeight, err := read("/sys/fs/cgroup" + cpu + "/cpu.weight")
			if err == nil {
				weight, err := strconv.Atoi(strings.TrimSpace(string(rawWeight)))
				if err == nil && weight >= 1 && weight <= 10000 {
					s.CPUActive, s.Reason = true, "node CPU controller is active; autogroup is overridden"
					return s, nil
				}
			}
		}
	}
	s.Reason = "node CPU controller is inactive; renicing alone does not fix separate memtester autogroups"
	return s, errors.New(s.Reason + "; reinstall the standalone helper or review custom service slices/controller restrictions")
}

func checkScheduling(product string) (schedulingState, error) {
	group, err := serviceGroup(product)
	if err != nil {
		return schedulingState{}, err
	}
	raw, err := command("systemctl", "show", product+"-node.service", "--property=MainPID")
	if err != nil {
		return schedulingState{}, err
	}
	pid, err := strconv.Atoi(strings.TrimPrefix(raw, "MainPID="))
	if err != nil || pid < 0 {
		return schedulingState{}, errors.New("cannot read node MainPID")
	}
	if pid == 0 {
		return schedulingState{Reason: "node service is inactive; CPU scheduling will be checked when it starts"}, nil
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return schedulingState{}, err
	}
	return inspectScheduling(data, group, os.ReadFile)
}
