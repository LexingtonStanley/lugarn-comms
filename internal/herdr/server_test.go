package herdr

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func wantMethod() StartMethod {
	if runtime.GOOS == "windows" {
		return MethodWMI
	}
	return MethodDetached
}

// stopFakeServer ends a fake server started from f, by its stop file and pid.
func stopFakeServer(t *testing.T, f *fake, pid int) {
	t.Helper()
	_ = os.WriteFile(filepath.Join(f.dir, "server.stop"), nil, 0o600)
	deadline := time.Now().Add(5 * time.Second)
	for pid > 0 && processAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if pid > 0 && processAlive(pid) {
		killProcess(pid)
	}
}

func serverCalls(f *fake) int {
	n := 0
	for _, c := range f.calls() {
		if len(c.Args) == 3 && c.Args[2] == "server" {
			n++
		}
	}
	return n
}

func TestStartServer(t *testing.T) {
	f := newFake(t)
	ctx := testContext(t)
	res, err := StartServer(ctx, f.bin, "lugarn-fake")
	if res.PID > 0 {
		t.Cleanup(func() { stopFakeServer(t, f, res.PID) })
	}
	if err != nil {
		t.Fatal(err)
	}
	if !res.Started || res.Method != wantMethod() || res.PID <= 0 || !res.Status.Running {
		t.Fatalf("result = %+v", res)
	}
	b, err := os.ReadFile(filepath.Join(f.dir, "server.pid"))
	if err != nil || strings.TrimSpace(string(b)) != strconv.Itoa(res.PID) {
		t.Fatalf("server pid file %q, %v; started %d", b, err, res.PID)
	}
	var started *call
	for _, c := range f.calls() {
		if len(c.Args) == 3 && c.Args[2] == "server" {
			started = &c
		}
	}
	if started == nil || started.Args[0] != "--session" || started.Args[1] != "lugarn-fake" || len(started.HerdrEnv) != 0 {
		t.Fatalf("server argv = %+v", started)
	}

	// Already answering: nothing starts.
	again, err := StartServer(ctx, f.bin, "lugarn-fake")
	if err != nil || again.Started || again.Method != "" || again.PID != 0 || !again.Status.Running {
		t.Fatalf("second start = %+v, %v", again, err)
	}
	if n := serverCalls(f); n != 1 {
		t.Fatalf("server started %d times", n)
	}
}

func TestStartServerNotInstalled(t *testing.T) {
	_, err := StartServer(testContext(t), filepath.Join(t.TempDir(), "no-herdr"), "lugarn-fake")
	wantProtocol(t, err, model.ErrorHerdrUnavailable, "not_installed")
}

func TestStartServerAlreadyRunning(t *testing.T) {
	f := newFake(t)
	f.on("status", response{Stdout: statusJSON("lugarn-fake", true)})
	res, err := StartServer(testContext(t), f.bin, "lugarn-fake")
	if err != nil || res.Started || !res.Status.Running {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if n := serverCalls(f); n != 0 {
		t.Fatalf("server started %d times", n)
	}
}

func TestStartServerFailures(t *testing.T) {
	restore := startTimeout
	t.Cleanup(func() { startTimeout = restore })
	startTimeout = 1500 * time.Millisecond
	for _, mode := range []string{"exit", "silent"} {
		t.Run(mode, func(t *testing.T) {
			f := newFake(t)
			if err := os.WriteFile(filepath.Join(f.dir, "server.mode"), []byte(mode), 0o600); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			res, err := StartServer(testContext(t), f.bin, "lugarn-fake")
			if res.PID > 0 {
				t.Cleanup(func() { stopFakeServer(t, f, res.PID) })
			}
			p := wantProtocol(t, err, model.ErrorHerdrUnavailable, "start_timeout")
			if !IsUnavailable(err) || p.Details["pid"] != res.PID {
				t.Fatalf("err = %v, details %v, result %+v", err, p.Details, res)
			}
			// A server that exits is reported at once on POSIX, where it's our child.
			if mode == "exit" && runtime.GOOS != "windows" && time.Since(start) > time.Second {
				t.Fatalf("exit noticed after %s", time.Since(start))
			}
		})
	}
}
