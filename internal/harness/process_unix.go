//go:build !windows

package harness

import (
	"errors"
	"os/exec"
	"syscall"
)

type processGroup struct{ pid int }

func newProcessGroup(cmd *exec.Cmd, _ string) (*processGroup, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &processGroup{}, nil
}
func (g *processGroup) assign(cmd *exec.Cmd) error { g.pid = cmd.Process.Pid; return nil }

// Codex puts stdio MCP children in separate process groups, even
// with --no-daemon. Snapshot ancestry while the harness is alive, then kill
// those descendants as well as its group. Only numeric descendants of our pid
// are eligible; this never selects a process by executable name.
func (g *processGroup) kill() error {
	if g.pid == 0 {
		return nil
	}
	parents, snapshotErr := processParents()
	var killErr error
	// Visit leaves first so an intermediate parent cannot reparent its children
	// before their pids have been selected. The ancestry snapshot is finite.
	var visit func(int)
	visited := map[int]bool{g.pid: true}
	visit = func(parent int) {
		for pid, ppid := range parents {
			if ppid != parent || visited[pid] {
				continue
			}
			visited[pid] = true
			visit(pid)
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
				killErr = errors.Join(killErr, err)
			}
		}
	}
	visit(g.pid)
	err := syscall.Kill(-g.pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		err = nil
	}
	return errors.Join(snapshotErr, killErr, err)
}
func (g *processGroup) close() {}

func (g *processGroup) jobName() string { return "" }
func (g *processGroup) started()        {}
