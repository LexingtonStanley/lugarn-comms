package broker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

type windowsPaneHerdr struct {
	lifecycleHerdr
	calls   []string
	command []string
	listed  int
	detect  bool
}

func (h *windowsPaneHerdr) OpenTab(ctx context.Context, w, n, cwd string, env map[string]string) (herdr.Tab, error) {
	h.calls = append(h.calls, "tab")
	return h.lifecycleHerdr.OpenTab(ctx, w, n, cwd, env)
}
func (h *windowsPaneHerdr) RunCommand(_ context.Context, pane string, args ...string) error {
	h.calls = append(h.calls, "run")
	h.command = append([]string(nil), args...)
	return nil
}
func (h *windowsPaneHerdr) StartAgent(context.Context, herdr.StartOptions) (herdr.Agent, error) {
	panic("Windows must never call agent start")
}
func (h *windowsPaneHerdr) Agents(context.Context) ([]herdr.Agent, error) {
	h.calls = append(h.calls, "list")
	h.listed++
	// Neither another pane nor a different harness on this pane may be named.
	agents := []herdr.Agent{{Pane: "other", Workspace: "workspace-owned", Kind: "claude"}, {Pane: "pane-owned", Workspace: "workspace-owned", Kind: "codex"}}
	if h.detect && h.listed >= 2 {
		agents = append(agents, herdr.Agent{Pane: "pane-owned", Workspace: "workspace-owned", Kind: "claude", Activity: model.ActivityBlocked})
	}
	return agents, nil
}
func (h *windowsPaneHerdr) RenameAgent(_ context.Context, pane, name string) error {
	h.calls = append(h.calls, "rename")
	if pane != "pane-owned" || name != herdr.AgentName("t", "writer") {
		panic("named another pane or member")
	}
	return nil
}
func TestWindowsPaneFlowListsThenRenames(t *testing.T) {
	b, r, m := headlessFixture(t, &headlessStub{})
	binary, e := os.Executable()
	must(t, e)
	client := &windowsPaneHerdr{detect: true}
	i, e := NewIntegration(b, IntegrationOptions{Binary: fakePlainBrokerPath(binary), HomeDir: t.TempDir(), Herdr: func(string) HerdrAPI { return client }})
	must(t, e)
	i.windowsPanes = true
	r.opts.Binary = func(model.Harness) (string, error) { return binary, nil }
	// Reuse the existing lease/session while selecting Claude for this conversion.
	configureWindowsPaneMember(t, b, &m)
	l := &integrationLifecycle{i: i, r: r}
	b.opts.Wait = func(context.Context, time.Duration) error { return nil }
	must(t, l.Open(context.Background(), m, model.LeaseCauseOpenTerminal))
	if !reflect.DeepEqual(client.calls, []string{"tab", "run", "list", "list", "rename"}) {
		t.Fatal(client.calls)
	}
	if len(client.command) != 3 || client.command[1] != "pane-launch" || model.ValidateULID(client.command[2]) != nil {
		t.Fatal(client.command)
	}
	for _, arg := range client.command {
		if !plainLaunchPath.MatchString(arg) {
			t.Fatal("typed shell-special word", arg)
		}
	}
	if strings.Contains(strings.Join(client.command, " "), "--model") {
		t.Fatal("harness flags reached PowerShell")
	}
	raw, e := os.ReadFile(client.env["LUGARN_COMMS_PANE_CONFIG"])
	must(t, e)
	var config paneLaunchCapsule
	must(t, model.DecodeStrict(raw, &config))
	if config.Binary != binary || config.Env["PATH"] == "" || len(config.Args) == 0 {
		t.Fatal(config)
	}
}
func TestWindowsPaneDetectionTimeout(t *testing.T) {
	b, _ := setup(t)
	client := &windowsPaneHerdr{}
	i, e := NewIntegration(b, IntegrationOptions{})
	must(t, e)
	waited := time.Duration(0)
	b.opts.Wait = func(_ context.Context, d time.Duration) error { waited += d; return nil }
	_, e = i.detectWindowsPane(context.Background(), client, "pane-owned", "workspace-owned", "writer-k2m9xa", model.HarnessClaude)
	if model.AsError(e).Code != model.ErrorLaunchFailed || model.AsError(e).Details["cause"] != "agent_not_detected" || waited != 30*time.Second {
		t.Fatal(e, waited)
	}
	for _, call := range client.calls {
		if call == "rename" {
			t.Fatal("renamed an undetected agent")
		}
	}
}

func configureWindowsPaneMember(t *testing.T, b *Broker, m *model.Member) {
	t.Helper()
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		if e := tx.UpdateTeam("t", store.TeamPatch{HerdrWorkspace: ptr(ptr("workspace-owned"))}, b.now()); e != nil {
			return e
		}
		if e := tx.UpdateMember(m.ID, store.MemberPatch{Harness: ptr(ptr(model.HarnessClaude))}, b.now()); e != nil {
			return e
		}
		current, e := tx.GetMember(m.ID)
		if e == nil {
			*m = *current
		}
		return e
	}))
}

// The fake RunCommand never executes the broker. Model the production runner,
// which already runs from a plain path/bridge, independently of where a copied
// test executable was placed (including folders with spaces and no 8.3 names).
func fakePlainBrokerPath(binary string) string {
	return filepath.Join(filepath.VolumeName(binary)+string(os.PathSeparator), "lugarn-test-broker.exe")
}
