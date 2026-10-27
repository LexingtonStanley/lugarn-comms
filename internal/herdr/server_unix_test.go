//go:build !windows

package herdr

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// The detached server leads its own session, reads and writes the null
// device, and runs in the home directory.
func TestStartServerDetaches(t *testing.T) {
	home := shortHome(t)
	t.Setenv("HOME", home)
	f := newFake(t)
	res, err := StartServer(testContext(t), f.bin, "lugarn-fake", ServerEnvironment([]string{"PATH=/explicit/login/bin"}))
	if res.PID > 0 {
		t.Cleanup(func() { stopFakeServer(t, f, res.PID) })
	}
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.calls() {
		if len(c.Args) == 3 && c.Args[2] == "server" && c.Path == "/explicit/login/bin" {
			found = true
		}
	}
	if !found {
		t.Fatal("detached child did not inherit explicit PATH")
	}
	sid, err := unix.Getsid(res.PID)
	if err != nil || sid != res.PID {
		t.Fatalf("server %d has session %d (%v), want its own", res.PID, sid, err)
	}
	if mine, _ := unix.Getsid(0); mine == sid {
		t.Fatal("server shares the test's session")
	}
	if runtime.GOOS != "linux" {
		return
	}
	proc := filepath.Join("/proc", strconv.Itoa(res.PID))
	for _, fd := range []string{"0", "1", "2"} {
		if target, err := os.Readlink(filepath.Join(proc, "fd", fd)); err != nil || target != os.DevNull {
			t.Fatalf("fd %s -> %q, %v", fd, target, err)
		}
	}
	if cwd, err := os.Readlink(filepath.Join(proc, "cwd")); err != nil || cwd != home {
		t.Fatalf("cwd = %q, %v; want %q", cwd, err, home)
	}
}

// shortHome is a HOME short enough for Herdr's session sockets. macOS's
// t.TempDir is deep under /var/folders and overflows its 104-byte sun_path.
func shortHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "lh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	return home
}
