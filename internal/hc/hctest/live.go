package hctest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
)

// Private creates a brand-new live test core and owns its complete cleanup.
// The default id is supplied in the brief; it is never queried from that core.
func Private(t *testing.T) (*hc.Client, string, string) {
	t.Helper()
	if os.Getenv("LUGARN_HC_LIVE") != "1" {
		t.Skip("set LUGARN_HC_LIVE=1 for a private hc core")
	}
	if runtime.GOOS != "linux" {
		t.Skip("hc live verification is Linux only")
	}
	binary, e := hc.Resolve("")
	if e != nil {
		t.Fatal(e)
	}
	var random [12]byte
	if _, e = rand.Read(random[:]); e != nil {
		t.Fatal(e)
	}
	session := "lugarn-test-" + hex.EncodeToString(random[:])
	home, e := os.UserHomeDir()
	if e != nil {
		t.Fatal(e)
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(home, ".local", "state")
	}
	state := filepath.Join(base, "herdr-core", session)
	if st, e := os.Lstat(state); e == nil || !os.IsNotExist(e) {
		t.Fatalf("refusing existing state %s (%v, %v)", state, st, e)
	}
	exists := hc.SocketExists(session)
	if exists == nil || *exists {
		t.Fatal("private socket absence not verified")
	}
	t.Logf("created private hc session %s", session)
	var client *hc.Client
	t.Cleanup(func() {
		if client != nil {
			client.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "env", "-u", "HC_TERMINAL_ID", "HC_SESSION="+session, binary, "server", "stop")
		cmd.Env = hc.CleanEnv(os.Environ())
		if raw, e := cmd.CombinedOutput(); e != nil {
			t.Errorf("stop private core %s: %v: %s", session, e, raw)
			return
		}
		if e = os.RemoveAll(state); e != nil {
			t.Error(e)
		}
		t.Logf("removed private hc session %s", session)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, e = hc.Connect(ctx, binary, session)
	if e != nil {
		t.Fatal(e)
	}
	if client.Hello.Core.ID == "57d257e310b5b777c21d711f6e61f21d" {
		t.Fatal("REFUSING default core id")
	}
	return client, session, binary
}
