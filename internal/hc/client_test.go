package hc_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
	"github.com/LexingtonStanley/lugarn-comms/internal/hc/hctest"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestMain(m *testing.M) {
	if os.Getenv("LUGARN_HC_FAKE_STATE") != "" {
		os.Exit(hctest.Serve())
	}
	os.Exit(m.Run())
}
func fake(t *testing.T) (string, string, hctest.State) {
	t.Helper()
	bin, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	var s hctest.State
	for name, out := range map[string]any{"hello": &s.Hello, "terminal": new(hc.Terminal)} {
		raw, e := os.ReadFile(filepath.Join("testdata", name+".json"))
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(raw, out); e != nil {
			t.Fatal(e)
		}
		if v, ok := out.(*hc.Terminal); ok {
			s.Terminals = []hc.Terminal{*v}
		}
	}
	path := filepath.Join(t.TempDir(), "peer.json")
	if e = hctest.Write(path, s); e != nil {
		t.Fatal(e)
	}
	t.Setenv("LUGARN_HC_FAKE_STATE", path)
	t.Setenv("HC_TERMINAL_ID", "live-parent-must-not-leak")
	t.Setenv("HC_SESSION", "default")
	return bin, path, s
}
func TestLinePeerFixturesAndErrors(t *testing.T) {
	bin, path, s := fake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, e := hc.Connect(ctx, bin, "lugarn-test-fake")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	terminals, e := c.List(ctx, nil)
	if e != nil || len(terminals) != 1 {
		t.Fatal(terminals, e)
	}
	if _, e = c.Get(ctx, terminals[0].Address); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Get(ctx, "tmissing"); !hc.IsGone(e) {
		t.Fatal(e)
	}
	s, e = hctest.Read(path)
	if e != nil {
		t.Fatal(e)
	}
	s.RunError = "name_taken"
	if e = hctest.Write(path, s); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Run(ctx, hc.RunOptions{Argv: []string{"unused"}}); !hc.IsKind(e, "name_taken") || model.AsError(e).Code != model.ErrorNameTaken {
		t.Fatal(e)
	}
	if _, e = hc.Connect(ctx, bin, "../default"); e == nil {
		t.Fatal("accepted traversal")
	}
}
func TestGenerationAndMissing(t *testing.T) {
	bin, path, s := fake(t)
	s.Hello.Generation = 2
	if e := hctest.Write(path, s); e != nil {
		t.Fatal(e)
	}
	if _, e := hc.Connect(context.Background(), bin, "lugarn-test-fake"); !errors.Is(e, hc.ErrGeneration) {
		t.Fatal(e)
	}
	if _, e := hc.Connect(context.Background(), filepath.Join(t.TempDir(), "absent"), "lugarn-test-fake"); e == nil {
		t.Fatal("missing binary accepted")
	}
}
func TestProbeDoesNotStartAbsentCore(t *testing.T) {
	bin, path, _ := fake(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	result := hc.Probe(context.Background(), bin, "lugarn-test-fake")
	if !result.Installed || result.Core != nil {
		t.Fatal(result)
	}
	s, e := hctest.Read(path)
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Calls) != 0 {
		t.Fatal("probe connected without socket", s.Calls)
	}
}
func TestSubscriptionRecordedEventsAndReconnect(t *testing.T) {
	bin, path, s := fake(t)
	raw, e := os.ReadFile("testdata/events.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &s.Events); e != nil {
		t.Fatal(e)
	}
	raw, e = os.ReadFile("testdata/recovery-events.json")
	if e != nil {
		t.Fatal(e)
	}
	var recovery []json.RawMessage
	if e = json.Unmarshal(raw, &recovery); e != nil {
		t.Fatal(e)
	}
	s.Events = append(s.Events, recovery...)

	if e = hctest.Write(path, s); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	v := hc.NewView()
	go v.Run(ctx, bin, "lugarn-test-fake", map[string]string{})
	deadline := time.Now().Add(5 * time.Second)
	for {
		view, ok := v.Current(s.Hello.Core.ID)
		if ok && len(view) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no snapshot")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	deadline = time.Now().Add(3 * time.Second)
	for {
		_, ok := v.Current(s.Hello.Core.ID)
		if !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("view did not invalidate")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestActivityAndEnvironment(t *testing.T) {
	for state, want := range map[string]model.Activity{"idle": model.ActivityIdle, "working": model.ActivityWorking, "blocked": model.ActivityBlocked, "done": model.ActivityDone, "error": model.ActivityBlocked, "future": model.ActivityUnknown} {
		got := (hc.Terminal{State: "live", Status: &hc.Status{State: state}}).Activity()
		if got != want {
			t.Fatal(state, got)
		}
	}
	for _, v := range hc.CleanEnv([]string{"HC_SESSION=default", "HC_TERMINAL_ID=t1", "hc_socket=bad", "HOME=real", "PATH=merged"}) {
		if strings.HasPrefix(strings.ToUpper(v), "HC_") {
			t.Fatal(v)
		}
	}
}

func TestSubscriptionReconnectAfterClose(t *testing.T) {
	bin, path, s := fake(t)
	s.Disconnect = true
	s.Events = nil
	if e := hctest.Write(path, s); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	view := hc.NewView()
	go view.Run(ctx, bin, "lugarn-test-fake", map[string]string{})
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, e := hctest.Read(path)
		if e != nil {
			t.Fatal(e)
		}
		count := 0
		for _, call := range current.Calls {
			if call.Method == "events.subscribe" {
				count++
			}
		}
		if count >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("did not reconnect")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// A closed subscription exposes no authoritative snapshot, forcing the
	// broker's next loop to use its label-filtered poll.
	// The view stops being authoritative when the peer's pipe closes, which
	// waits for the fake (a race-instrumented test binary) to exit. That took
	// over a second on a loaded macOS runner, so allow it a generous bound.
	deadline = time.Now().Add(10 * time.Second)
	for {
		_, ok := view.Current(s.Hello.Core.ID)
		if !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed view remained authoritative")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
