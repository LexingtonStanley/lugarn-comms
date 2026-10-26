//go:build !windows

package harness

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func processDead(pid int) bool {
	if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
		return true
	}
	if runtime.GOOS == "linux" {
		// A killed orphan can remain a zombie until the host's pid 1 reaps it.
		// Zombies execute no code and hold no live descriptors.
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) {
			return true
		}
		if err == nil {
			_, tail, _ := strings.Cut(string(b), ") ")
			return strings.HasPrefix(tail, "Z ")
		}
	}
	return false
}
func assertDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if processDead(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d remains live", pid)
}

func liveProcessIDs(h *Handle) ([]int, error) {
	b, err := exec.Command("ps", "-axo", "pid=,pgid=").Output()
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, line := range strings.Split(string(b), "\n") {
		var pid, group int
		if _, err := fmt.Sscan(line, &pid, &group); err == nil && group == h.PID {
			pids = append(pids, pid)
		}
	}
	if len(pids) == 0 {
		return nil, fmt.Errorf("no process group for %d", h.PID)
	}
	return pids, nil
}

func detachHelper(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
