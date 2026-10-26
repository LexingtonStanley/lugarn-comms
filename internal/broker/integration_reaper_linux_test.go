//go:build linux

package broker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestIntegrationReaperDoesNotStealExecStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), "LUGARN_INTEGRATION_HELPER=exit-seven")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	protect := startProtection()
	if e := cmd.Start(); e != nil {
		protect(0)
		t.Fatal(e)
	}
	protect(cmd.Process.Pid)
	defer releaseProtection(cmd.Process.Pid)
	eventually(t, func() bool {
		state, e := (nativeProcesses{}).Inspect(ctx, int64(cmd.Process.Pid))
		must(t, e)
		return !state.Alive
	})
	runnerReaper.Lock()
	previous := runnerReaper.enabled
	runnerReaper.enabled = true
	runnerReaper.Unlock()
	reapRunnerOrphans()
	runnerReaper.Lock()
	runnerReaper.enabled = previous
	runnerReaper.Unlock()
	e := cmd.Wait()
	var exited *exec.ExitError
	if !errors.As(e, &exited) || exited.ExitCode() != 7 {
		t.Fatalf("os/exec lost its exit status: %v", e)
	}
}
