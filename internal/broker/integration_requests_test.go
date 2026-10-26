package broker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (h *lifecycleHerdr) Pane(context.Context, string) (herdr.Pane, error) {
	return herdr.Pane{ID: "pane-owned", Tab: "tab-owned", Workspace: "workspace-owned"}, nil
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

func TestIntegrationPaneExitRestoresHeadlessWithoutWaitingOnItself(t *testing.T) {
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
	l := r.opts.PaneLifecycle.(*integrationLifecycle)
	m, e := l.current(ctx, "writer")
	must(t, e)
	must(t, l.Open(ctx, *m, model.LeaseCauseOpenTerminal))
	m, e = l.current(ctx, "writer")
	must(t, e)
	r.mu.Lock()
	r.busy[m.ID] = true
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.busy, m.ID); r.mu.Unlock() }()
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	must(t, l.Exited(bounded, *m))
	m, e = l.current(ctx, "writer")
	must(t, e)
	if client.closed == 0 {
		t.Fatal("pane exit left an owned clean-shell tab")
	}
	if m.Lease.Driver != model.DriverHeadless || m.Lease.Generation != 2 {
		t.Fatal(m)
	}
}

func TestIntegrationUnusedClaudeSessionStartsThenResumesWithoutLosingBrief(t *testing.T) {
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
	l := r.opts.PaneLifecycle.(*integrationLifecycle)
	m, e := l.current(ctx, "writer")
	must(t, e)
	m, e = r.launchHeadless(ctx, *m, model.LeaseCauseStart)
	must(t, e)
	// Assigning an id did not create a harness session. The first pane must use
	// --session-id, even though the member already carries that assigned id.
	must(t, l.Open(ctx, *m, model.LeaseCauseOpenTerminal))
	if !slices.Contains(client.args, "--session-id") || slices.Contains(client.args, "--resume") {
		t.Fatal(client.args)
	}
	m, e = l.current(ctx, "writer")
	must(t, e)
	// Simulate the real SessionStart hook, with no delivery/brief yet.
	pointer := filepath.Join(i.opts.HomeDir, ".lugarn", "conversations")
	must(t, os.MkdirAll(pointer, 0700))
	must(t, os.WriteFile(filepath.Join(pointer, *m.Lease.Launch), []byte("{\"session_id\":\""+*m.Session+"\"}\n"), 0600))
	must(t, l.toHeadless(ctx, *m, model.LeaseCauseReturnHeadless, true))
	m, e = l.current(ctx, "writer")
	must(t, e)
	driver := &headlessStub{started: make(chan turnStart, 1), fn: func(s turnStart) {
		s.handle.complete(TurnOutcome{Acknowledged: true, Session: m.Session, ExitCode: ptr(int64(0))})
	}}
	r.opts.Headless = driver
	r.opts.Processes = &fakeProcesses{alive: true, birth: "fake"}
	sendToWriter(t, b, "first actual task")
	must(t, r.runTurn(ctx, *m))
	started := <-driver.started
	if started.launch.NewSession || !strings.Contains(started.prompt, "Real Lugarn headers end with") {
		t.Fatal("initialized but unbriefed session was not resumed with its brief", started.launch, started.prompt)
	}
}

func TestStopMemberClosesPaneRecordedWhileWaiting(t *testing.T) {
	b, r, m := paneFixture(t, &panesStub{})
	h := &lifecycleHerdr{}
	i, e := NewIntegration(b, IntegrationOptions{Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	l := &integrationLifecycle{i: i, r: r}
	// The launch worker is still between its pane lease and pane-id writes.
	initial := m
	initial.Lease = &model.Lease{Driver: model.DriverPane, Generation: m.Lease.Generation, Launch: m.Lease.Launch}
	r.mu.Lock()
	r.busy[m.ID] = true
	r.mu.Unlock()
	b.opts.Wait = func(context.Context, time.Duration) error {
		r.mu.Lock()
		delete(r.busy, m.ID)
		r.mu.Unlock()
		return nil
	}
	must(t, l.stopMember(context.Background(), initial, model.StopReasonPerson, model.LeaseCauseStop))
	if h.closed != 1 {
		t.Fatal("post-wait recorded pane was orphaned", h.closed)
	}
}

type orderedReturnHerdr struct {
	lifecycleHerdr
	waited bool
	ready  *bool
	t      *testing.T
}

func (h *orderedReturnHerdr) Wait(ctx context.Context, name string, states []model.Activity, timeout time.Duration) (herdr.Agent, error) {
	if !*h.ready {
		h.t.Fatal("Herdr wait preceded in-flight delivery completion")
	}
	h.waited = true
	return h.lifecycleHerdr.Wait(ctx, name, states, timeout)
}
func TestReturnHeadlessWaitsForDeliveringBeforeHerdr(t *testing.T) {
	b, r, m := paneFixture(t, &panesStub{})
	ctx := context.Background()
	sendToWriter(t, b, "in-flight")
	var items []store.DeliveryItem
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		var e error
		items, e = tx.PickUpDeliveries(m.ID, 0, ptr(model.DeliveryViaPane), nil, &m.Lease.Generation, b.now())
		return e
	}))
	ready := false
	h := &orderedReturnHerdr{ready: &ready, t: t}
	i, e := NewIntegration(b, IntegrationOptions{Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	l := &integrationLifecycle{i: i, r: r}
	b.opts.Wait = func(ctx context.Context, _ time.Duration) error {
		if h.closed != 0 {
			t.Fatal("closed before delivery finished")
		}
		must(t, r.finishPane(ctx, m, items, model.DeliveryStateDelivered, nil))
		ready = true
		return nil
	}
	must(t, l.toHeadless(ctx, m, model.LeaseCauseReturnHeadless, true))
	if !h.waited || !ready {
		t.Fatal("missing waits")
	}
}

func TestReturnHeadlessReleasesPaneHoldsDuringPersonHold(t *testing.T) {
	b, r, m := paneFixture(t, &panesStub{})
	ctx := context.Background()
	message := sendToWriter(t, b, "blocked")
	must(t, r.paneHolds(ctx, m, ptr(model.DeliveryReasonRecipientBlocked)))
	_, e := b.Hold(ctx, wire.HoldRequest{Team: m.Team, Member: m.ID, TTLSeconds: ptr(int64(60))})
	must(t, e)
	h := &lifecycleHerdr{}
	i, e := NewIntegration(b, IntegrationOptions{Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	l := &integrationLifecycle{i: i, r: r}
	must(t, l.toHeadless(ctx, m, model.LeaseCauseReturnHeadless, true))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		d, e := tx.GetDelivery(message.ID, m.ID)
		if e == nil && (d.Reason != nil && (*d.Reason == model.DeliveryReasonRecipientBlocked || *d.Reason == model.DeliveryReasonRecipientUnknown)) {
			t.Fatal("pane reason stranded under headless lease", d)
		}
		hold, e := tx.GetHold(m.ID)
		if e == nil && hold.ReleasedAt != nil {
			t.Fatal("conversion released person hold")
		}
		return e
	}))
}
func TestPaneExitWithoutRecordedPaneID(t *testing.T) {
	b, r, m := paneFixture(t, &panesStub{})
	lease := *m.Lease
	lease.Pane = nil
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		return tx.CompareAndSwapLease(m.ID, m.Lease.Driver, m.Lease.Generation, lease, b.now())
	}))
	m.Lease = &lease
	i, e := NewIntegration(b, IntegrationOptions{Herdr: func(string) HerdrAPI { return &lifecycleHerdr{} }})
	must(t, e)
	l := &integrationLifecycle{i: i, r: r}
	must(t, l.Exited(context.Background(), m))
	current, e := l.current(context.Background(), m.ID)
	must(t, e)
	if current.Lease.Driver != model.DriverHeadless {
		t.Fatal(current)
	}
}

type foreignPaneHerdr struct{ lifecycleHerdr }

func (h *foreignPaneHerdr) Pane(context.Context, string) (herdr.Pane, error) {
	return herdr.Pane{Workspace: "persons-workspace", Tab: "persons-tab"}, nil
}
func TestClosePaneRefusesReusedIDInForeignWorkspace(t *testing.T) {
	b, r, m := paneFixture(t, &panesStub{})
	h := &foreignPaneHerdr{}
	i, e := NewIntegration(b, IntegrationOptions{Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	l := &integrationLifecycle{i: i, r: r}
	code(t, l.closePane(context.Background(), m), model.ErrorLeaseConflict)
	if h.closed != 0 {
		t.Fatal("closed the person's tab")
	}
}
