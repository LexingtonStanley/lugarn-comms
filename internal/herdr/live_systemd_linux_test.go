//go:build linux

package herdr

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestLiveSystemdServer starts a private server the systemd way (lexbox has
// KillUserProcesses=no, but systemd-run --user works there), first over a
// failed unit left behind under the server's name, then again after killing
// the server. It stops only its own lugarn-herdr-lugarn-test-* unit.
func TestLiveSystemdServer(t *testing.T) {
	bin := liveBinary(t)
	if _, err := exec.LookPath("systemd-run"); err != nil {
		t.Skip("no systemd-run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	session := newSession(t)
	c := privateClient(t, ctx, bin, session)
	unit := unitName(session)
	if !strings.HasPrefix(unit, "lugarn-herdr-lugarn-test-") {
		t.Fatalf("refusing unit %q", unit)
	}
	if state, loaded, err := unitState(ctx, unit); err != nil || loaded {
		t.Fatalf("unit %s already exists (%s, %v): not ours, not touching it", unit, state, err)
	}
	t.Logf("private session %s, unit %s", session, unit)
	t.Cleanup(func() { stopUnit(t, c, unit) })

	// A failed unit under the server's name, as one without --collect leaves.
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	out, code, err := runSystemd(ctx, "systemd-run", "--user", "--unit="+unit, "--", falseBin)
	t.Logf("$ systemd-run --user --unit=%s -- %s\n%s(exit %d, %v)", unit, falseBin, out, code, err)
	waitUnit(t, ctx, unit, "failed")
	logUnit(t, ctx, unit)

	res, err := StartServer(ctx, bin, session, KillUserProcesses(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("StartServer(KillUserProcesses(true)) over the failed unit: started=%v method=%s pid=%d running=%v version=%s",
		res.Started, res.Method, res.PID, res.Status.Running, res.Status.ServerVersion)
	if !res.Started || res.Method != MethodSystemd || res.PID <= 0 || !res.Status.Running {
		t.Fatalf("StartServer = %+v", res)
	}
	logUnit(t, ctx, unit)
	cgroup, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(res.PID), "cgroup"))
	parent := parentPID(res.PID)
	comm, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(parent), "comm"))
	t.Logf("server %d: cgroup %s; parent %d (%s)", res.PID, strings.TrimSpace(string(cgroup)), parent, strings.TrimSpace(string(comm)))
	if !strings.Contains(string(cgroup), "user@") || !strings.Contains(string(cgroup), unit) {
		t.Fatalf("server isn't in its unit under user@.service: %s", cgroup)
	}
	if st, err := c.Status(ctx); err != nil || !st.Running {
		t.Fatalf("status = %+v, %v", st, err)
	}
	again, err := StartServer(ctx, bin, session, KillUserProcesses(true))
	t.Logf("second StartServer while it answers: started=%v method=%q (%v)", again.Started, again.Method, err)
	if err != nil || again.Started {
		t.Fatalf("second start = %+v, %v", again, err)
	}

	// Kill the server (the unit's MainPID, ours): the unit dies under it.
	if err := syscall.Kill(res.PID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if !waitDead(res.PID) {
		t.Fatalf("server %d survived SIGKILL", res.PID)
	}
	time.Sleep(500 * time.Millisecond)
	state, loaded, _ := unitState(ctx, unit)
	t.Logf("after SIGKILL of %d: is-active %s, loaded %v", res.PID, state, loaded)
	logUnit(t, ctx, unit)
	res2, err := StartServer(ctx, bin, session, KillUserProcesses(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("StartServer after the dead server: started=%v method=%s pid=%d running=%v", res2.Started, res2.Method, res2.PID, res2.Status.Running)
	if !res2.Started || res2.Method != MethodSystemd || res2.PID == res.PID || !res2.Status.Running {
		t.Fatalf("restart = %+v", res2)
	}
	logUnit(t, ctx, unit)
}

func waitUnit(t *testing.T, ctx context.Context, unit, want string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		state, _, err := unitState(ctx, unit)
		if err == nil && state == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("unit %s is %q (%v), want %q", unit, state, err, want)
		}
	}
}

func logUnit(t *testing.T, ctx context.Context, unit string) {
	t.Helper()
	out, code, err := systemctl(ctx, "status", "--no-pager", "--lines=0", unit)
	t.Logf("$ systemctl --user status --no-pager --lines=0 %s\n%s(exit %d, %v)", unit, out, code, err)
}

// stopUnit stops this test's own unit and its session, and shows both gone.
func stopUnit(t *testing.T, c *Client, unit string) {
	t.Helper()
	if !strings.HasPrefix(unit, "lugarn-herdr-lugarn-test-") {
		t.Fatalf("refusing to stop %q", unit)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pid, _, _ := systemctl(ctx, "show", "-p", "MainPID", "--value", unit)
	out, code, err := systemctl(ctx, "stop", unit)
	t.Logf("$ systemctl --user stop %s\n%s(exit %d, %v)", unit, out, code, err)
	if state, loaded, _ := unitState(ctx, unit); loaded {
		// A failed run stays loaded until it's reset.
		out, code, err = systemctl(ctx, "reset-failed", unit)
		t.Logf("$ systemctl --user reset-failed %s (was %s)\n%s(exit %d, %v)", unit, state, out, code, err)
	}
	state, loaded, err := unitState(ctx, unit)
	st, _ := c.Status(ctx)
	mainPID, _ := strconv.Atoi(strings.TrimSpace(pid))
	t.Logf("after stop: unit %s is-active %s loaded %v (%v); main pid %d alive %v; session %s running %v",
		unit, state, loaded, err, mainPID, mainPID > 0 && processAlive(mainPID), c.session, st.Running)
	out2, err := c.call(ctx, 0, "session delete", "session", "delete", c.session)
	t.Logf("$ herdr --session %s session delete %s: %s %v", c.session, c.session, strings.TrimSpace(string(out2)), err)
}
