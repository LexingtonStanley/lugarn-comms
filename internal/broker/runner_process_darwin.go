//go:build darwin

package broker

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
)

func (nativeProcesses) Inspect(_ context.Context, pid int64) (ProcessState, error) {
	if pid <= 0 {
		return ProcessState{}, nil
	}
	if e := unix.Kill(int(pid), 0); e == unix.ESRCH {
		return ProcessState{}, nil
	} else if e != nil && e != unix.EPERM {
		return ProcessState{}, e
	}
	info, e := unix.SysctlKinfoProc("kern.proc.pid", int(pid))
	if e == unix.ESRCH || e == unix.ENOENT {
		return ProcessState{}, nil
	}
	if e != nil {
		return ProcessState{}, e
	}
	return ProcessState{Alive: info.Proc.P_stat != 5, Birth: fmt.Sprintf("%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec)}, nil
}
func (p nativeProcesses) Cancel(ctx context.Context, pid int64, birth string) error {
	s, e := p.Inspect(ctx, pid)
	if e != nil || !s.Alive {
		return e
	}
	if birth == "" || birth != s.Birth {
		return fmt.Errorf("refusing to kill unverified process %d", pid)
	}
	group, e := unix.Getpgid(int(pid))
	if e == unix.ESRCH {
		return nil
	}
	if e != nil {
		return e
	}
	target := int(pid)
	if group == int(pid) {
		target = -int(pid)
	}
	e = unix.Kill(target, unix.SIGKILL)
	if e == unix.ESRCH {
		return nil
	}
	return e
}
