package broker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (h *lifecycleHerdr) Pane(context.Context, string) (herdr.Pane, error) {
	return herdr.Pane{ID: "pane-owned", Tab: "tab-owned"}, nil
}
func TestIntegrationRequestConversionsAndStop(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.UpdateTeam("t", store.TeamPatch{HerdrWorkspace: ptr(ptr("workspace-owned"))}, b.now()); e != nil {
			return e
		}
		return tx.UpdateMember("writer", store.MemberPatch{Harness: ptr(ptr(model.HarnessClaude))}, b.now())
	}))
	runnerReady(t, b)
	client := &lifecycleHerdr{}
	bin, e := os.Executable()
	must(t, e)
	i, e := NewIntegration(b, IntegrationOptions{Binary: bin, HomeDir: t.TempDir(), Herdr: func(string) HerdrAPI { return client }})
	must(t, e)
	r := i.Runner("t", RunnerOptions{Binary: func(model.Harness) (string, error) { return bin, nil }})
	must(t, r.acquire(ctx))
	l := r.opts.Requests.(*integrationLifecycle)
	req := model.Request{Team: "t", Kind: model.RequestKindOpenTerminal, Member: ptr(model.MemberRef{ID: "writer", Name: "writer"}), Args: map[string]any{}}
	must(t, l.Execute(ctx, req))
	m, e := l.current(ctx, "writer")
	must(t, e)
	if m.Lease.Generation != 1 || m.Lease.Driver != model.DriverPane {
		t.Fatal(m)
	}
	oldLaunch := *m.Lease.Launch
	req.Kind = model.RequestKindReturnHeadless
	must(t, l.Execute(ctx, req))
	m, e = l.current(ctx, "writer")
	must(t, e)
	if m.Lease.Driver != model.DriverHeadless || m.Lease.Generation != 2 || *m.Lease.Launch == oldLaunch {
		t.Fatal(m)
	}
	if len(client.states) != 2 || client.states[0] != model.ActivityIdle || client.states[1] != model.ActivityDone {
		t.Fatal("return-headless wait included non-ready states", client.states)
	}
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		launch, e := tx.GetLaunch(oldLaunch)
		if e == nil && (launch.EndedAt == nil || *launch.EndReason != model.LaunchEndReasonConverted) {
			t.Fatal(launch)
		}
		return e
	}))
	req.Kind = model.RequestKindStopMember
	req.Args = map[string]any{"reason": "spawner"}
	must(t, l.Execute(ctx, req))
	m, e = l.current(ctx, "writer")
	must(t, e)
	if m.State != model.MemberStateStopped || m.Lease.Driver != model.DriverNone || m.StopReason == nil || *m.StopReason != model.StopReasonSpawner {
		t.Fatal(m)
	}
}

func TestIntegrationSpawnAndSpawnerStopRequests(t *testing.T) {
	b, caller := setup(t)
	spawning(t, b)
	runnerReady(t, b)
	ctx := context.Background()
	bin, e := os.Executable()
	must(t, e)
	integration, e := NewIntegration(b, IntegrationOptions{Binary: bin, HomeDir: t.TempDir()})
	must(t, e)
	r := integration.Runner("t", RunnerOptions{Binary: func(model.Harness) (string, error) { return bin, nil }})
	must(t, r.acquire(ctx))
	b.opts.Wait = func(ctx context.Context, _ time.Duration) error {
		var requests []model.Request
		if e := b.db.Read(ctx, func(tx *store.Tx) error { var e error; requests, e = tx.ListOpenRequests("t"); return e }); e != nil {
			return e
		}
		for _, req := range requests {
			if req.Status == model.RequestStatusPending {
				if e := r.executeRequest(ctx, req); e != nil {
					return e
				}
			}
		}
		return nil
	}
	b.opts.RequestTimeout = time.Second
	spawn, e := b.ToolSpawn(ctx, caller, wire.SpawnInput{Name: "worker", Harness: model.HarnessCodex, Task: "reply ok"})
	must(t, e)
	if spawn.Member.State != model.MemberStateRunning || spawn.TaskMessage == "" {
		t.Fatal(spawn)
	}
	stopped, e := b.ToolStop(ctx, caller, wire.StopInput{Member: "worker"})
	must(t, e)
	if stopped.Member.State != model.MemberStateStopped || stopped.Member.StopReason == nil || *stopped.Member.StopReason != model.StopReasonSpawner {
		t.Fatal(stopped)
	}
}
