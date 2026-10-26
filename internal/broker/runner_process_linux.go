//go:build linux

package broker

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

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
	data, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if os.IsNotExist(e) {
		return ProcessState{}, nil
	}
	if e != nil {
		return ProcessState{Alive: true}, e
	}
	i := strings.LastIndexByte(string(data), ')')
	if i < 0 {
		return ProcessState{}, fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 20 {
		return ProcessState{}, fmt.Errorf("short process stat")
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return ProcessState{}, nil
	}
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return ProcessState{}, e
	}
	return ProcessState{Alive: true, Birth: strings.TrimSpace(string(boot)) + ":" + fields[19]}, nil
}
func (p nativeProcesses) Cancel(ctx context.Context, pid int64, birth string) error {
	s, e := p.Inspect(ctx, pid)
	if e != nil || !s.Alive {
		return e
	}
	if birth == "" || s.Birth != birth {
		return fmt.Errorf("refusing to kill unverified process %s", strconv.FormatInt(pid, 10))
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
