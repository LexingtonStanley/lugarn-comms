//go:build linux

package broker

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

func init() {
	integrationHelpers["adopted-leaf"] = func() int { time.Sleep(5 * time.Minute); return 0 }
	integrationHelpers["orphan-parent"] = func() int {
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), "LUGARN_INTEGRATION_HELPER=adopted-leaf")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if e := cmd.Start(); e != nil {
			return 1
		}
		fmt.Println(cmd.Process.Pid)
		return 0
	}
}
func TestIntegrationStopCleansAdoptedDescendant(t *testing.T) {
	must(t, EnableSubreaper())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0])
	cmd.Env = append(os.Environ(), "LUGARN_INTEGRATION_HELPER=orphan-parent", "LUGARN_COMMS_HOME="+t.TempDir())
	raw, e := cmd.Output()
	must(t, e)
	pid, e := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	must(t, e)
	state, e := (nativeProcesses{}).Inspect(ctx, pid)
	must(t, e)
	if !state.Alive {
		t.Fatal("orphan did not survive its parent")
	}
	t.Cleanup(func() {
		_ = cancelOwnedTree(context.Background(), pid, state.Birth)
		var status unix.WaitStatus
		_, _ = unix.Wait4(int(pid), &status, 0, nil)
	})
	must(t, cleanupAdoptedDescendants(ctx))
	eventually(t, func() bool { reapRunnerOrphans(); return unix.Kill(int(pid), 0) == unix.ESRCH })
}
