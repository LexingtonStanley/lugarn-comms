//go:build linux

package herdr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Fake systemctl and systemd-run keep each unit's state in <dir>/unit-<name>:
// "active <pid>", "failed", "inactive" (loaded but dead), or "stuck" (failed,
// and reset-failed doesn't clear it). They answer as systemd 255 does on
// lexbox: is-active exits 4 for no such unit, systemd-run refuses a loaded
// name, and a --collect unit whose process died is gone.
func init() {
	personas["systemctl"] = fakeSystemctl
	personas["systemd-run"] = fakeSystemdRun
}

func unitFile(dir, unit string) string { return filepath.Join(dir, "unit-"+unit) }

func readUnit(dir, unit string) (state string, pid int) {
	b, err := os.ReadFile(unitFile(dir, unit))
	if err != nil {
		return "", 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 2 {
		pid, _ = strconv.Atoi(fields[1])
	}
	return fields[0], pid
}

func fakeSystemctl(dir string) int {
	args := os.Args[1:]
	appendJSON(filepath.Join(dir, "calls.jsonl"), call{Args: append([]string{"systemctl"}, args...)})
	if len(args) < 3 || args[0] != "--user" {
		return 90
	}
	unit := args[len(args)-1]
	state, pid := readUnit(dir, unit)
	if state == "active" && !processAlive(pid) {
		_ = os.Remove(unitFile(dir, unit)) // --collect
		state = ""
	}
	switch args[1] {
	case "is-active":
		switch state {
		case "":
			fmt.Println("inactive")
			return 4
		case "active":
			fmt.Println("active")
			return 0
		case "stuck":
			fmt.Println("failed")
			return 3
		default:
			fmt.Println(state)
			return 3
		}
	case "reset-failed":
		switch state {
		case "":
			fmt.Printf("Failed to reset failed state of unit %s: Unit %s not loaded.\n", unit, unit)
			return 1
		case "failed", "inactive":
			_ = os.Remove(unitFile(dir, unit))
		}
		return 0
	case "show":
		fmt.Println(pid)
		return 0
	}
	return 91
}

func fakeSystemdRun(dir string) int {
	args := os.Args[1:]
	appendJSON(filepath.Join(dir, "calls.jsonl"), call{Args: append([]string{"systemd-run"}, args...)})
	if len(args) < 5 || args[0] != "--user" || !strings.HasPrefix(args[1], "--unit=") || args[2] != "--collect" || args[3] != "--" {
		return 90
	}
	unit := strings.TrimPrefix(args[1], "--unit=")
	if state, pid := readUnit(dir, unit); state != "" && (state != "active" || processAlive(pid)) {
		fmt.Fprintf(os.Stderr, "Failed to start transient service unit: Unit %s was already loaded or has a fragment file.\n", unit)
		return 1
	}
	cmd := exec.Command(args[4], args[5:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 1
	}
	_ = os.WriteFile(unitFile(dir, unit), []byte("active "+strconv.Itoa(cmd.Process.Pid)), 0o600)
	fmt.Fprintf(os.Stderr, "Running as unit: %s; invocation ID: fake\n", unit)
	return 0
}

// systemdFake is a fake herdr with fake systemctl and systemd-run on PATH.
func systemdFake(t *testing.T) *fake {
	t.Helper()
	f := newFake(t)
	copyExecutable(t, filepath.Join(f.dir, "systemctl"))
	copyExecutable(t, filepath.Join(f.dir, "systemd-run"))
	t.Setenv("PATH", f.dir)
	return f
}

const fakeUnit = "lugarn-herdr-lugarn-fake.service"

// systemdCalls is the systemctl and systemd-run argv, in order.
func systemdCalls(f *fake) [][]string {
	var out [][]string
	for _, c := range f.calls() {
		if len(c.Args) > 0 && (c.Args[0] == "systemctl" || c.Args[0] == "systemd-run") {
			out = append(out, c.Args)
		}
	}
	return out
}

func TestStartServerSystemd(t *testing.T) {
	f := systemdFake(t)
	ctx := testContext(t)
	res, err := StartServer(ctx, f.bin, "lugarn-fake", KillUserProcesses(true))
	if res.PID > 0 {
		t.Cleanup(func() { stopFakeServer(t, f, res.PID) })
	}
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(f.dir, "server.pid"))
	if !res.Started || res.Method != MethodSystemd || !res.Status.Running || strconv.Itoa(res.PID) != string(b) {
		t.Fatalf("result = %+v (server pid %s)", res, b)
	}
	calls := systemdCalls(f)
	if !reflect.DeepEqual(calls[0], []string{"systemctl", "--user", "is-active", fakeUnit}) {
		t.Fatalf("first call %q", calls[0])
	}
	if want := []string{"systemd-run", "--user", "--unit=" + fakeUnit, "--collect", "--", f.bin, "--session", "lugarn-fake", "server"}; !reflect.DeepEqual(calls[1], want) {
		t.Fatalf("systemd-run argv\n got %q\nwant %q", calls[1], want)
	}
	if last := calls[len(calls)-1]; !reflect.DeepEqual(last, []string{"systemctl", "--user", "show", "-p", "MainPID", "--value", fakeUnit}) {
		t.Fatalf("last call %q", last)
	}
	for _, c := range calls {
		if c[0] == "systemctl" && c[2] == "reset-failed" {
			t.Fatal("reset-failed with no unit loaded")
		}
	}

	again, err := StartServer(ctx, f.bin, "lugarn-fake", KillUserProcesses(true))
	if err != nil || again.Started || len(systemdCalls(f)) != len(calls) {
		t.Fatalf("second start = %+v, %v; systemd calls %d -> %d", again, err, len(calls), len(systemdCalls(f)))
	}
}

// A failed or dead unit left behind is reset, then the start succeeds.
func TestStartServerSystemdLeftover(t *testing.T) {
	for _, state := range []string{"failed", "inactive"} {
		t.Run(state, func(t *testing.T) {
			f := systemdFake(t)
			if err := os.WriteFile(unitFile(f.dir, fakeUnit), []byte(state), 0o600); err != nil {
				t.Fatal(err)
			}
			res, err := StartServer(testContext(t), f.bin, "lugarn-fake", KillUserProcesses(true))
			if res.PID > 0 {
				t.Cleanup(func() { stopFakeServer(t, f, res.PID) })
			}
			if err != nil || !res.Started || res.Method != MethodSystemd {
				t.Fatalf("result = %+v, %v", res, err)
			}
			calls := systemdCalls(f)
			if !reflect.DeepEqual(calls[1], []string{"systemctl", "--user", "reset-failed", fakeUnit}) || calls[2][0] != "systemd-run" {
				t.Fatalf("calls = %q", calls)
			}
		})
	}
}

func TestStartServerSystemdFailures(t *testing.T) {
	restore := startTimeout
	t.Cleanup(func() { startTimeout = restore })
	startTimeout = 1500 * time.Millisecond

	t.Run("refused", func(t *testing.T) {
		f := systemdFake(t)
		_ = os.WriteFile(unitFile(f.dir, fakeUnit), []byte("stuck"), 0o600)
		_, err := StartServer(testContext(t), f.bin, "lugarn-fake", KillUserProcesses(true))
		p := wantProtocol(t, err, model.ErrorHerdrUnavailable, "start_timeout")
		if !strings.Contains(p.Details["herdr_message"].(string), "already loaded") {
			t.Fatalf("details = %v", p.Details)
		}
	})
	t.Run("server exits", func(t *testing.T) {
		f := systemdFake(t)
		_ = os.WriteFile(filepath.Join(f.dir, "server.mode"), []byte("exit"), 0o600)
		start := time.Now()
		_, err := StartServer(testContext(t), f.bin, "lugarn-fake", KillUserProcesses(true))
		wantProtocol(t, err, model.ErrorHerdrUnavailable, "start_timeout")
		if d := time.Since(start); d > time.Second {
			t.Fatalf("a dead unit was noticed after %s", d)
		}
	})
	t.Run("no systemd", func(t *testing.T) {
		f := newFake(t)
		t.Setenv("PATH", t.TempDir())
		_, err := StartServer(testContext(t), f.bin, "lugarn-fake", KillUserProcesses(true))
		wantProtocol(t, err, model.ErrorHerdrUnavailable, "start_timeout")
	})
}

// A unit that's already running is another start in flight: StartServer
// waits for its server rather than starting a second one.
func TestStartServerSystemdUnitAlreadyRunning(t *testing.T) {
	restore := startTimeout
	t.Cleanup(func() { startTimeout = restore })
	startTimeout = 1500 * time.Millisecond
	for _, answers := range []bool{true, false} {
		f := systemdFake(t)
		_ = os.WriteFile(unitFile(f.dir, fakeUnit), []byte("active "+strconv.Itoa(os.Getpid())), 0o600)
		if answers {
			f.on("status", response{Stdout: statusJSON("lugarn-fake", false)}, response{Stdout: statusJSON("lugarn-fake", true)})
		}
		res, err := StartServer(testContext(t), f.bin, "lugarn-fake", KillUserProcesses(true))
		if answers && (err != nil || res.Started || !res.Status.Running) {
			t.Fatalf("answering: %+v, %v", res, err)
		}
		if !answers {
			wantProtocol(t, err, model.ErrorHerdrUnavailable, "start_timeout")
		}
		for _, c := range systemdCalls(f) {
			if c[0] == "systemd-run" || c[2] == "reset-failed" {
				t.Fatalf("touched a running unit: %q", c)
			}
		}
	}
}

func TestStartServerWithoutKillUserProcessesIsDetached(t *testing.T) {
	f := systemdFake(t)
	res, err := StartServer(testContext(t), f.bin, "lugarn-fake", KillUserProcesses(false))
	if res.PID > 0 {
		t.Cleanup(func() { stopFakeServer(t, f, res.PID) })
	}
	if err != nil || res.Method != MethodDetached || len(systemdCalls(f)) != 0 {
		t.Fatalf("result = %+v, %v; systemd calls %q", res, err, systemdCalls(f))
	}
}
