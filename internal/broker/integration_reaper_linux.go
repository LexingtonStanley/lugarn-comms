//go:build linux

package broker

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

var runnerReaper = struct {
	sync.Mutex
	enabled   bool
	protected map[int]int
}{protected: map[int]int{}}

// EnableSubreaper makes this runner adopt detached descendants on Linux. Only
// Run's executable face calls it; merely importing broker never changes the OS.
func EnableSubreaper() error {
	if e := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); e != nil {
		return e
	}
	runnerReaper.Lock()
	runnerReaper.enabled = true
	runnerReaper.Unlock()
	return nil
}
func startProtection() func(int) {
	runnerReaper.Lock()
	return func(pid int) {
		if pid > 0 {
			runnerReaper.protected[pid]++
		}
		runnerReaper.Unlock()
	}
}
func releaseProtection(pid int) {
	runnerReaper.Lock()
	defer runnerReaper.Unlock()
	if runnerReaper.protected[pid] > 1 {
		runnerReaper.protected[pid]--
	} else {
		delete(runnerReaper.protected, pid)
	}
}
func reapRunnerOrphans() {
	runnerReaper.Lock()
	defer runnerReaper.Unlock()
	if !runnerReaper.enabled {
		return
	}
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return
	}
	ourPID, ourGroup := os.Getpid(), unix.Getpgrp()
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || runnerReaper.protected[pid] > 0 {
			continue
		}
		raw, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if e != nil {
			continue
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(raw[end+1:]))
		if len(fields) < 3 || fields[0] != "Z" {
			continue
		}
		parent, _ := strconv.Atoi(fields[1])
		group, _ := strconv.Atoi(fields[2])
		if parent != ourPID || group == ourGroup {
			continue
		}
		// Ordinary os/exec children (Herdr/git) inherit our group and are excluded.
		// Harness roots have their own groups, but stay protected from before Start
		// returns until os/exec.Wait reaps them. Only adopted zombies remain eligible;
		// Wait4 names that exact pid, never -1, so it cannot steal another child's exit.
		var status unix.WaitStatus
		_, _ = unix.Wait4(pid, &status, unix.WNOHANG, nil)
	}
}

func cleanupAdoptedDescendants(ctx context.Context) error {
	runnerReaper.Lock()
	defer runnerReaper.Unlock()
	if !runnerReaper.enabled {
		return nil
	}
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return e
	}
	ourPID, ourGroup := os.Getpid(), unix.Getpgrp()
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || runnerReaper.protected[pid] > 0 {
			continue
		}
		raw, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if e != nil {
			continue
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(raw[end+1:]))
		if len(fields) < 3 {
			continue
		}
		parent, _ := strconv.Atoi(fields[1])
		group, _ := strconv.Atoi(fields[2])
		if parent != ourPID || group == ourGroup {
			continue
		}
		state, e := (nativeProcesses{}).Inspect(ctx, int64(pid))
		if e != nil {
			return e
		}
		if state.Alive {
			if e = cancelOwnedTree(ctx, int64(pid), state.Birth); e != nil {
				return e
			}
		}
		var status unix.WaitStatus
		_, _ = unix.Wait4(pid, &status, unix.WNOHANG, nil)
	}
	return nil
}
