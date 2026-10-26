//go:build !windows

package herdr

import (
	"context"
	"os"
	"os/exec"
	"syscall"
)

// startServerProcess starts the server in its own session (setsid), so the
// caller's terminal hanging up doesn't reach it, with stdin, stdout and stderr
// on the null device. Nothing waits for it: a goroutine only reaps it should
// it exit while this process still runs, and reports that on exited.
func startServerProcess(_ context.Context, bin, session, dir string, overrides []string) (int, <-chan struct{}, StartMethod, error) {
	// Not CommandContext: the server must outlive the caller's context.
	cmd := exec.Command(bin, "--session", session, "server")
	cmd.Env = append(cleanEnv(os.Environ()), overrides...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Nil Stdin, Stdout and Stderr are the null device.
	if err := cmd.Start(); err != nil {
		return 0, nil, "", clientError("server", kindOther, err.Error())
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	return cmd.Process.Pid, exited, MethodDetached, nil
}
