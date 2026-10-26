//go:build !windows

package harness

import (
	"os/exec"
	"syscall"
)

type processGroup struct{ pid int }

func newProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &processGroup{}, nil
}
func (g *processGroup) assign(cmd *exec.Cmd) error { g.pid = cmd.Process.Pid; return nil }
func (g *processGroup) kill() error {
	if g.pid == 0 {
		return nil
	}
	err := syscall.Kill(-g.pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}
func (g *processGroup) close() {}
