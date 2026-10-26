//go:build !windows

package herdr

import "syscall"

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func killProcess(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
