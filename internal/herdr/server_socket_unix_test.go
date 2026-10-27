//go:build !windows

package herdr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestSessionSocketPath(t *testing.T) {
	t.Setenv("HOME", "/scratch/home")
	t.Setenv("XDG_CONFIG_HOME", "/scratch/xdg")
	t.Setenv("HERDR_CONFIG_PATH", "/ignored/config.toml")
	t.Setenv("HERDR_SOCKET_PATH", "/ignored/herdr.sock")
	for _, session := range []string{"default", "lugarn-fake"} {
		want := "/scratch/xdg/herdr/"
		if session != "default" {
			want += "sessions/" + session + "/"
		}
		if got := sessionSocketPath(session); got != want+"herdr.sock" {
			t.Fatal(got, want)
		}
	}
	if e := os.Unsetenv("XDG_CONFIG_HOME"); e != nil {
		t.Fatal(e)
	}
	if got := sessionSocketPath("lugarn-fake"); got != "/scratch/home/.config/herdr/sessions/lugarn-fake/herdr.sock" {
		t.Fatal(got)
	}
	if e := os.Unsetenv("HOME"); e != nil {
		t.Fatal(e)
	}
	if got := sessionSocketPath("default"); got != filepath.Join(os.TempDir(), "herdr", "herdr.sock") {
		t.Fatal(got)
	}
	t.Setenv("XDG_CONFIG_HOME", "/tmp/"+strings.Repeat("./", 60))
	if err := checkSessionSocketPaths("default"); err == nil {
		t.Fatal("cleaning dot components hid an overlong socket name")
	}
}

func TestLongHomeAndXDGFailBeforeHerdrInvocation(t *testing.T) {
	for _, useXDG := range []bool{false, true} {
		t.Run(fmt.Sprint(useXDG), func(t *testing.T) {
			f := newFake(t)
			t.Setenv("HOME", "/"+strings.Repeat("h", 108))
			t.Setenv("XDG_CONFIG_HOME", "/"+strings.Repeat("x", 108))
			if !useXDG {
				if e := os.Unsetenv("XDG_CONFIG_HOME"); e != nil {
					t.Fatal(e)
				}
			}
			for _, systemd := range []bool{false, true} {
				res, err := StartServer(testContext(t), f.bin, "lugarn-fake", KillUserProcesses(systemd))
				p := wantProtocol(t, err, model.ErrorHerdrUnavailable, "socket_path_too_long")
				if res.Started || p.Details["path"] != sessionSocketPath("lugarn-fake") || len(f.calls()) != 0 {
					t.Fatal(res, p.Details, f.calls())
				}
			}
		})
	}
	// A short XDG path takes precedence over even a very long HOME.
	t.Setenv("HOME", "/"+strings.Repeat("h", 108))
	t.Setenv("XDG_CONFIG_HOME", "/tmp/lx")
	if err := checkSessionSocketPaths("lugarn-fake"); err != nil {
		t.Fatal(err)
	}
}

func TestServerSocketPathLimit(t *testing.T) {
	limit := len(syscall.RawSockaddrUnix{}.Path) - 1
	for _, extra := range []int{-1, 0, 1, 7, 8} {
		t.Run(fmt.Sprintf("client_limit%+d", extra), func(t *testing.T) {
			client := "/" + strings.Repeat("a", limit-len("/herdr-client.sock")-1+extra) + "/herdr-client.sock"
			api := filepath.Join(filepath.Dir(client), "herdr.sock")
			err := checkServerSocketPaths(api)
			if extra <= 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			p := wantProtocol(t, err, model.ErrorHerdrUnavailable, "socket_path_too_long")
			want := client
			if len(api) > limit {
				want = api
			}
			if p.Details["path"] != want || p.Details["length"] != len(want) || p.Details["limit"] != limit {
				t.Fatal(p.Details)
			}
		})
	}
	// Non-ASCII homes consume more than one byte per character.
	path := "/" + strings.Repeat("é", limit/2) + "/herdr.sock"
	wantProtocol(t, checkServerSocketPaths(path), model.ErrorHerdrUnavailable, "socket_path_too_long")
}

func TestStartServerRejectsLongSocketBeforeSpawn(t *testing.T) {
	f := newFake(t)
	path := "/" + strings.Repeat("a", 108) + "/.config/herdr/sessions/lugarn-fake/herdr.sock"
	var raw map[string]any
	if e := json.Unmarshal([]byte(statusJSON("lugarn-fake", false)), &raw); e != nil {
		t.Fatal(e)
	}
	raw["server"].(map[string]any)["socket"] = path
	b, e := json.Marshal(raw)
	if e != nil {
		t.Fatal(e)
	}
	f.on("status", response{Stdout: string(b)})
	for _, systemd := range []bool{false, true} {
		res, err := StartServer(testContext(t), f.bin, "lugarn-fake", KillUserProcesses(systemd))
		p := wantProtocol(t, err, model.ErrorHerdrUnavailable, "socket_path_too_long")
		if res.Started || res.PID != 0 || p.Details["path"] != path || serverCalls(f) != 0 {
			t.Fatal(res, p.Details, f.calls())
		}
	}
}
