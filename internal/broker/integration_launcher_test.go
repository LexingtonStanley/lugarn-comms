package broker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type launcherHerdr struct {
	HerdrAPI
	b       *Broker
	created int
	argv    []string
	env     map[string]string
}

func (h *launcherHerdr) CreateWorkspace(_ context.Context, _ string, _ string, env map[string]string) (herdr.Workspace, error) {
	h.created++
	h.env = env
	return herdr.Workspace{ID: "w-owned", RootPane: "p-owned"}, nil
}
func (h *launcherHerdr) OpenTab(_ context.Context, _ string, _ string, _ string, env map[string]string) (herdr.Tab, error) {
	h.env = env
	return herdr.Tab{RootPane: "p-owned"}, nil
}
func (h *launcherHerdr) Workspace(context.Context, string) (herdr.Workspace, error) {
	return herdr.Workspace{ID: "w-owned"}, nil
}
func (h *launcherHerdr) Pane(context.Context, string) (herdr.Pane, error) {
	return herdr.Pane{ID: "p-owned", Workspace: "w-owned"}, nil
}
func (h *launcherHerdr) ClosePane(context.Context, string) error { return nil }
func (h *launcherHerdr) RunCommand(_ context.Context, _ string, argv ...string) error {
	h.argv = argv
	now := h.b.now()
	return h.b.db.Write(context.Background(), func(tx *store.Tx) error {
		return tx.Heartbeat("t", model.Runner{State: model.RunnerStateUp, PID: ptr(int64(123)), Pane: ptr("p-owned"), StartedAt: &now, HeartbeatAt: &now}, now)
	})
}
func TestIntegrationLauncherRecordsWorkspaceIDAndNativeBridge(t *testing.T) {
	b, _ := setup(t)
	runnerReady(t, b)
	binary, e := os.Executable()
	must(t, e)
	h := &launcherHerdr{b: b}
	i, e := NewIntegration(b, IntegrationOptions{Binary: binary, HomeDir: t.TempDir(), Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	var team *store.Team
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error { var e error; team, e = tx.GetTeam("t"); return e }))
	must(t, i.Launch(context.Background(), *team))
	placement, e := i.placement("t")
	must(t, e)
	t.Cleanup(func() { os.RemoveAll(placement.BridgeDir) })
	if h.created != 1 || len(h.argv) != 4 || !plainLaunchPath.MatchString(h.argv[0]) || h.env["LUGARN_COMMS_HOME"] != b.opts.StateDir {
		t.Fatal(h)
	}
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
		team, e := tx.GetTeam("t")
		if e == nil && (team.HerdrWorkspace == nil || *team.HerdrWorkspace != "w-owned") {
			t.Fatal(team)
		}
		return e
	}))
	// Teardown removed placement while a member tab kept the workspace alive.
	must(t, os.Remove(i.placementPath("t")))
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error { var e error; team, e = tx.GetTeam("t"); return e }))
	must(t, i.Launch(context.Background(), *team))
	if h.created != 1 {
		t.Fatal("created another workspace instead of a runner tab")
	}
}

func TestIntegrationRunnerBridgeHandlesSourcePathWithSpaces(t *testing.T) {
	b, _ := setup(t)
	binary, e := os.Executable()
	must(t, e)
	raw, e := os.ReadFile(binary)
	must(t, e)
	source := filepath.Join(t.TempDir(), "broker with spaces"+filepath.Ext(binary))
	must(t, os.WriteFile(source, raw, 0700))
	i, e := NewIntegration(b, IntegrationOptions{Binary: source})
	must(t, e)
	bridge, dir, e := i.runnerBridge()
	must(t, e)
	defer os.RemoveAll(dir)
	if plainLaunchPath.MatchString(b.opts.StateDir) && filepath.Dir(dir) != b.opts.StateDir {
		t.Fatal("bridge should use executable state filesystem", dir)
	}
	cmd := exec.Command(bridge)
	cmd.Env = append(os.Environ(), "LUGARN_INTEGRATION_HELPER=exit-seven")
	e = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("native bridge: %v", e)
	}
}

type blockedStartProbe struct {
	fakeProbe
	entered chan struct{}
	release chan struct{}
}

func (p *blockedStartProbe) Herdr(ctx context.Context, server string) (model.HerdrInfo, error) {
	close(p.entered)
	select {
	case <-p.release:
		return model.HerdrInfo{Server: server, Installed: true, Running: true}, nil
	case <-ctx.Done():
		return model.HerdrInfo{}, ctx.Err()
	}
}
func TestIntegrationStartPreflightDoesNotHoldWriter(t *testing.T) {
	b, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopped)}, b.now())
	}))
	probe := &blockedStartProbe{entered: make(chan struct{}), release: make(chan struct{})}
	b.opts.Probe = probe
	done := make(chan error, 1)
	go func() { _, e := b.StartTeam(ctx, wire.StartTeamRequest{Team: "t"}); done <- e }()
	defer func() { close(probe.release); <-done }()
	select {
	case <-probe.entered:
	case <-ctx.Done():
		t.Fatal("preflight never started")
	}
	writing, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	must(t, b.db.Write(writing, func(tx *store.Tx) error { _, e := tx.TouchAppContact(ptr("t"), b.now()); return e }))
}

func TestIntegrationBridgeUsesPlainExecutable(t *testing.T) {
	b, _ := setup(t)
	binary, e := os.Executable()
	must(t, e)
	if !plainLaunchPath.MatchString(binary) {
		t.Skip("test executable path is not plain")
	}
	i, e := NewIntegration(b, IntegrationOptions{Binary: binary})
	must(t, e)
	actual, dir, e := i.runnerBridge()
	must(t, e)
	if actual != binary || dir != "" {
		t.Fatal("unnecessary temporary executable", actual, dir)
	}
}
