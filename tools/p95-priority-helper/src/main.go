package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const version = "1.0.3"

func notifyReady() error {
	address := os.Getenv("NOTIFY_SOCKET")
	if address == "" {
		return nil
	} // Foreground mode has no service manager.
	c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return err
	}
	_, err = c.Write([]byte("READY=1\nSTATUS=Watching managed P95 threads; default nice 19 -> 0"))
	return err
}

func command(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w (%s)", name, err, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

func serviceGroup(product string) (string, error) {
	raw, err := command("systemctl", "show", product+"-node.service", "--property=LoadState", "--property=ControlGroup")
	if err != nil {
		return "", err
	}
	fields := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		v := strings.SplitN(line, "=", 2)
		if len(v) == 2 {
			fields[v[0]] = v[1]
		}
	}
	if fields["LoadState"] != "loaded" {
		return "", fmt.Errorf("%s-node.service is not installed; run this helper on the test NODE", product)
	}
	g := fields["ControlGroup"]
	if g != "" && (g == "/" || !strings.HasPrefix(g, "/") || filepath.Clean(g) != g || !strings.HasSuffix(g, "/"+product+"-node.service")) {
		return "", errors.New("unexpected node service cgroup")
	}
	return g, nil
}

func preflight(product string) error {
	if err := rootOnly(); err != nil {
		return err
	}
	for _, tool := range []string{"mprime", "memtester"} {
		found := false
		for _, path := range toolPaths(product, tool) {
			info, err := os.Stat(path)
			if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("required packaged executable missing: %s (checked %v)", tool, toolPaths(product, tool))
		}
	}
	_, err := serviceGroup(product)
	return err
}

func atomicState(path string, state *journal) error {
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(body); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func loadState(path, product, boot string) (*journal, error) {
	state := &journal{Version: 1, Product: product, BootID: boot, Threads: map[string]savedThread{}}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return nil, errors.New("invalid helper state file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var old journal
	if err = json.Unmarshal(b, &old); err != nil {
		return nil, err
	}
	if old.Version != 1 || old.Product != product || old.Threads == nil {
		return nil, errors.New("invalid helper journal")
	}
	if old.BootID != boot {
		return state, nil
	}
	for key, s := range old.Threads {
		if key != s.key() || s.PID <= 0 || s.TID <= 0 || s.ProcessStart == 0 || s.ThreadStart == 0 || s.Original != 19 {
			return nil, errors.New("invalid saved thread identity")
		}
	}
	return &old, nil
}

func jsonPrint(value any) { b, _ := json.MarshalIndent(value, "", "  "); fmt.Println(string(b)) }

func runController(product, action string) error {
	if err := preflight(product); err != nil {
		return err
	}
	dir := "/run/p95-priority-" + product
	if err := secureDirectory(dir); err != nil {
		return err
	}
	lock, err := lockState(filepath.Join(dir, "lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	statePath := filepath.Join(dir, "state.json")
	state, err := loadState(statePath, product, strings.TrimSpace(string(b)))
	if err != nil {
		return err
	}
	c := controller{Proc: newProc(product), State: state, Set: setNice}
	c.Save = func() error { return atomicState(statePath, state) }
	if action == "restore" {
		group, err := serviceGroup(product)
		if err != nil {
			return err
		}
		n, err := c.restore(group)
		jsonPrint(map[string]any{"restored_threads": n})
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("P95 PRIORITY HELPER %s: product=%s, default nice 19 -> 0, poll=1s\n", version, product)
	fmt.Println("Scope: node service cgroup + packaged mprime + torture arguments + node working directory.")
	var loopErr error
	ready := false
	checkedGroup := ""
	for ctx.Err() == nil {
		group, err := serviceGroup(product)
		if err == nil && group != "" {
			if group != checkedGroup {
				_, err = checkScheduling(product)
				if err == nil {
					checkedGroup = group
				}
			}
		} else if group == "" {
			checkedGroup = ""
		}
		if err == nil && group != "" {
			var n int
			n, err = c.adjust(group)
			if n > 0 {
				fmt.Printf("%s adjusted_threads=%d nice=0\n", time.Now().Format(time.RFC3339), n)
			}
		}
		if err != nil {
			loopErr = err
			break
		}
		if !ready {
			if err := notifyReady(); err != nil {
				loopErr = err
				break
			}
			ready = true
			fmt.Println("READY: first ownership/priority scan completed.")
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	group, err := serviceGroup(product)
	if err == nil {
		var n int
		n, err = c.restore(group)
		fmt.Printf("Stopped: restored_threads=%d retained_undo_records=%d\n", n, len(state.Threads))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Restore incomplete; undo records retained:", err)
		if loopErr == nil {
			loopErr = err
		}
	}
	return loopErr
}

type toolSummary struct {
	Processes        int            `json:"processes"`
	Threads          int            `json:"threads"`
	Nice             map[string]int `json:"thread_nice_counts"`
	States           map[string]int `json:"thread_state_counts"`
	CPUSeconds       float64        `json:"cpu_seconds_during_sample"`
	AverageCores     float64        `json:"average_cpu_cores"`
	ThreadsProgress  int            `json:"threads_with_cpu_progress"`
	MatchedProcesses int            `json:"processes_present_for_whole_sample"`
	CPUGroups        map[string]int `json:"cpu_cgroups"`
	AutoGroups       map[string]int `json:"autogroups"`
}

type stepSummary struct {
	Directory    string      `json:"step_directory"`
	P95          toolSummary `json:"p95"`
	Memory       toolSummary `json:"memtester"`
	BothProgress bool        `json:"both_made_cpu_progress"`
}

func summarize(before, after []process, elapsed, hz float64) []stepSummary {
	old := map[string]process{}
	for _, pr := range before {
		old[fmt.Sprintf("%d:%d", pr.Stat.ID, pr.Stat.Start)] = pr
	}
	steps := map[string]*stepSummary{}
	for _, pr := range after {
		step := steps[pr.Batch]
		if step == nil {
			step = &stepSummary{Directory: pr.Batch,
				P95:    toolSummary{Nice: map[string]int{}, States: map[string]int{}, CPUGroups: map[string]int{}, AutoGroups: map[string]int{}},
				Memory: toolSummary{Nice: map[string]int{}, States: map[string]int{}, CPUGroups: map[string]int{}, AutoGroups: map[string]int{}}}
			steps[pr.Batch] = step
		}
		s := &step.P95
		if pr.Tool == "memtester" {
			s = &step.Memory
		}
		s.Processes++
		s.CPUGroups[pr.CPUGroup]++
		if pr.AutoGroup != "" {
			s.AutoGroups[pr.AutoGroup]++
		}
		s.Threads += len(pr.Threads)
		for _, th := range pr.Threads {
			s.Nice[strconv.Itoa(th.Nice)]++
			s.States[th.State]++
		}
		prev, ok := old[fmt.Sprintf("%d:%d", pr.Stat.ID, pr.Stat.Start)]
		if !ok || prev.Tool != pr.Tool || prev.CWD != pr.CWD || pr.Stat.Ticks < prev.Stat.Ticks {
			continue
		}
		s.MatchedProcesses++
		s.CPUSeconds += float64(pr.Stat.Ticks-prev.Stat.Ticks) / hz
		threads := map[string]uint64{}
		for _, th := range prev.Threads {
			threads[fmt.Sprintf("%d:%d", th.ID, th.Start)] = th.Ticks
		}
		for _, th := range pr.Threads {
			if ticks, ok := threads[fmt.Sprintf("%d:%d", th.ID, th.Start)]; ok && th.Ticks > ticks {
				s.ThreadsProgress++
			}
		}
	}
	var out []stepSummary
	for _, step := range steps {
		step.P95.AverageCores = step.P95.CPUSeconds / elapsed
		step.Memory.AverageCores = step.Memory.CPUSeconds / elapsed
		step.BothProgress = step.P95.CPUSeconds > 0 && step.Memory.CPUSeconds > 0
		out = append(out, *step)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Directory < out[j].Directory })
	return out
}

func observe(product string, seconds int) error {
	if err := preflight(product); err != nil {
		return err
	}
	group, err := serviceGroup(product)
	if err != nil {
		return err
	}
	p := newProc(product)
	raw, err := command("getconf", "CLK_TCK")
	if err != nil {
		return err
	}
	hz, err := strconv.ParseFloat(raw, 64)
	if err != nil || hz <= 0 {
		return errors.New("cannot read CLK_TCK")
	}
	before, err := p.discover(group)
	if err != nil {
		return err
	}
	start := time.Now()
	fmt.Fprintf(os.Stderr, "Read-only sampling for %d seconds; no workload will be started.\n", seconds)
	time.Sleep(time.Duration(seconds) * time.Second)
	afterGroup, err := serviceGroup(product)
	if err != nil {
		return err
	}
	if afterGroup != group {
		return errors.New("node service changed during sampling; sample again")
	}
	after, err := p.discover(group)
	if err != nil {
		return err
	}
	elapsed := time.Since(start).Seconds()
	active, _ := command("systemctl", "is-active", "p95-priority-"+product+".service")
	steps := summarize(before, after, elapsed, hz)
	scheduling, schedulingErr := checkScheduling(product)
	ag, _ := os.ReadFile("/proc/sys/kernel/sched_autogroup_enabled")
	if steps == nil {
		steps = []stepSummary{}
	}
	jsonPrint(map[string]any{"version": version, "product": product, "helper_service_active": active == "active", "node_cgroup": group, "sched_autogroup_enabled": strings.TrimSpace(string(ag)), "cpu_scheduling": scheduling,
		"sample_seconds": elapsed, "steps": steps, "note": "CPU cores is summed CPU seconds / wall seconds. Progress is not a computation-correctness verdict. Empty steps means no recognized active workload; do not treat it as success."})
	return schedulingErr
}

func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: p95-priority preflight|selfcheck|check-scheduling|watch|status|restore [--product fyhocsys|sckocstp] [--seconds 10]")
	}
	action := os.Args[1]
	if action == "--version" || action == "version" {
		fmt.Println("p95-priority-helper " + version)
		return nil
	}
	f := flag.NewFlagSet(action, flag.ContinueOnError)
	product := f.String("product", "fyhocsys", "node service/product")
	seconds := f.Int("seconds", 10, "read-only status sample, 1 to 60 seconds")
	if err := f.Parse(os.Args[2:]); err != nil {
		return err
	}
	if f.NArg() != 0 || (*product != "fyhocsys" && *product != "sckocstp") || *seconds < 1 || *seconds > 60 {
		return errors.New("invalid arguments")
	}
	switch action {
	case "preflight":
		if err := preflight(*product); err != nil {
			return err
		}
		fmt.Println("PREFLIGHT OK: node service and packaged tools found.")
		return nil
	case "selfcheck":
		return selfcheck()
	case "check-scheduling":
		state, err := checkScheduling(*product)
		jsonPrint(state)
		return err
	case "watch", "restore":
		return runController(*product, action)
	case "status":
		return observe(*product, *seconds)
	default:
		return errors.New("unknown command: " + action)
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
