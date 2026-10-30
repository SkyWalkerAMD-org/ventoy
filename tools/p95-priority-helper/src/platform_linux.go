package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
)

func setNice(tid, nice int) error { return syscall.Setpriority(syscall.PRIO_PROCESS, tid, nice) }

func rootOnly() error {
	if os.Geteuid() != 0 {
		return errors.New("run as root on the TEST NODE")
	}
	return nil
}

func secureDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || !ok || st.Uid != 0 || info.Mode().Perm() != 0700 {
		return fmt.Errorf("require root-owned 0700 directory: %s", path)
	}
	return nil
}

func lockState(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("invalid lock file")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another helper is running; stop its service before manual restore")
	}
	return f, nil
}

// This probes the real kernel permission using only one thread in this helper.
// It starts no stress workload and never changes another program's priority.
func selfcheck() error {
	if err := rootOnly(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tid := syscall.Gettid()
	p := newProc("fyhocsys")
	path := filepath.Join("/proc/self/task", strconv.Itoa(tid), "stat")
	before, err := p.readStat(path)
	if err != nil {
		return err
	}
	defer setNice(tid, before.Nice)
	if err = setNice(tid, 19); err != nil {
		return err
	}
	if err = setNice(tid, 0); err != nil {
		return fmt.Errorf("CAP_SYS_NICE is required: %w", err)
	}
	after, err := p.readStat(path)
	if err != nil {
		return err
	}
	if after.Nice != 0 {
		return errors.New("kernel did not apply nice 0")
	}
	fmt.Println("SELF CHECK OK: own thread nice 19 -> 0 verified; no stress workload started.")
	return nil
}
