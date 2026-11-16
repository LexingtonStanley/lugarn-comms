package broker

import (
	"context"
	"errors"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

type reviewHerdr struct {
	lifecycleHerdr
	list        func(context.Context) ([]herdr.Agent, error)
	startErr    error
	starts      []herdr.StartOptions
	closedPanes []string
}

func (h *reviewHerdr) Agents(ctx context.Context) ([]herdr.Agent, error) {
	if h.list != nil {
		return h.list(ctx)
	}
	return nil, nil
}
func (h *reviewHerdr) StartAgent(ctx context.Context, o herdr.StartOptions) (herdr.Agent, error) {
	h.starts = append(h.starts, o)
	if h.startErr != nil {
		return herdr.Agent{}, h.startErr
	}
	return h.lifecycleHerdr.StartAgent(ctx, o)
}
func (h *reviewHerdr) Pane(_ context.Context, id string) (herdr.Pane, error) {
	return herdr.Pane{ID: id, Workspace: "workspace-owned", Tab: "tab-owned"}, nil
}
func (h *reviewHerdr) ClosePane(_ context.Context, id string) error {
	h.closedPanes = append(h.closedPanes, id)
	return nil
}
func reviewLifecycle(t *testing.T, b *Broker, h HerdrAPI, team string) *integrationLifecycle {
	t.Helper()
	binary, e := os.Executable()
	must(t, e)
	i, e := NewIntegration(b, IntegrationOptions{Binary: binary, HomeDir: t.TempDir(), Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	i.Attach()
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		if e := tx.UpdateTeam(team, store.TeamPatch{State: ptr(model.TeamStateStarting)}, b.now()); e != nil {
			return e
		}
		return tx.Heartbeat(team, model.Runner{State: model.RunnerStateDown}, b.now())
	}))
	r := i.Runner(team, RunnerOptions{})
	r.opts.Files = fakeLaunchFiles{}
	r.opts.Binary = func(model.Harness) (string, error) { return binary, nil }
	r.opts.Processes = &fakeProcesses{alive: true, birth: "fake"}
	must(t, r.acquire(context.Background()))
	return r.opts.PaneLifecycle.(*integrationLifecycle)
}
func reviewPaneSetup(t *testing.T, h HerdrAPI) (*Broker, *integrationLifecycle, model.Member) {
	t.Helper()
	b, _ := setup(t)
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		if e := tx.UpdateTeam("t", store.TeamPatch{HerdrWorkspace: ptr(ptr("workspace-owned"))}, b.now()); e != nil {
			return e
		}
		return tx.UpdateMember("writer", store.MemberPatch{Harness: ptr(ptr(model.HarnessPi)), Mode: ptr(ptr(model.ModePane))}, b.now())
	}))
	l := reviewLifecycle(t, b, h, "t")
	m, e := l.current(context.Background(), "writer")
	must(t, e)
	return b, l, *m
}
func TestNameSnapshotDoesNotHoldStoreWrite(t *testing.T) {
	h := &reviewHerdr{}
	b, l, m := reviewPaneSetup(t, h)
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		return tx.CreateTeam(store.Team{ID: "other", Name: "other", Pattern: model.PatternCustom})
	}))
	h.list = func(ctx context.Context) ([]herdr.Agent, error) {
		heartbeat, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		// This independent team's heartbeat must finish while Herdr is still listing.
		e := b.db.Write(heartbeat, func(tx *store.Tx) error {
			now := b.now()
			return tx.Heartbeat("other", model.Runner{State: model.RunnerStateUp, HeartbeatAt: &now}, now)
		})
		return []herdr.Agent{{Name: "writer"}}, e
	}
	must(t, l.Open(context.Background(), m, model.LeaseCauseOpenTerminal))
	if len(h.starts) != 1 || h.starts[0].Name != "writer-2" {
		t.Fatal(h.starts)
	}
}
func TestNameSnapshotFailureDoesNotRotateLease(t *testing.T) {
	h := &reviewHerdr{list: func(context.Context) ([]herdr.Agent, error) {
		return nil, model.NewError(model.ErrorTimeout, "slow list")
	}}
	_, l, m := reviewPaneSetup(t, h)
	code(t, l.Open(context.Background(), m, model.LeaseCauseOpenTerminal), model.ErrorTimeout)
	after, e := l.current(context.Background(), m.ID)
	must(t, e)
	if after.Lease.Generation != m.Lease.Generation || after.Lease.Driver != m.Lease.Driver || after.HerdrName != nil || len(h.starts) != 0 {
		t.Fatal(after)
	}
}
func TestNameClashAtStartFailsWithoutRenamingOrTakeover(t *testing.T) {
	h := &reviewHerdr{startErr: model.NewError(model.ErrorNameTaken, "external race")}
	_, l, m := reviewPaneSetup(t, h)
	code(t, l.Open(context.Background(), m, model.LeaseCauseOpenTerminal), model.ErrorNameTaken)
	after, e := l.current(context.Background(), m.ID)
	must(t, e)
	if len(h.starts) != 1 || h.starts[0].Name != "writer" || after.HerdrName == nil || *after.HerdrName != "writer" || after.StopReason == nil || *after.StopReason != model.StopReasonLaunchFailed {
		t.Fatal(after, h.starts)
	}
	if h.closed != 1 || len(h.closedPanes) != 0 {
		t.Fatal(h.closedPanes)
	}
}

// Targeted reads describe the external pane; launch calls describe its replacement.
type conversionHerdr struct {
	reviewHerdr
	external  *adoptionHerdr
	gone      error
	oldClosed bool
}

func (h *conversionHerdr) Workspace(ctx context.Context, id string) (herdr.Workspace, error) {
	return h.external.Workspace(ctx, id)
}
func (h *conversionHerdr) Pane(ctx context.Context, id string) (herdr.Pane, error) {
	if id == h.external.pane.ID {
		if h.oldClosed {
			return herdr.Pane{}, h.gone
		}
		return h.external.Pane(ctx, id)
	}
	return h.reviewHerdr.Pane(ctx, id)
}
func (h *conversionHerdr) Agent(ctx context.Context, id string) (herdr.Agent, error) {
	return h.external.Agent(ctx, id)
}
func TestBringFreshCodexUsesAdoptedFolder(t *testing.T) {
	b, h, req := adoptionSetup(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	req.Spec.Members[0].Harness = model.HarnessCodex
	h.agent.Kind = "codex"
	h.pane.Agent = "codex"
	ctx := context.Background()
	_, e := b.AdoptTeam(ctx, req)
	must(t, e)
	preview, e := b.BringOntoComms(ctx, wire.BringOntoCommsRequest{Team: req.Spec.ID, Member: "external"})
	must(t, e)
	if preview.Conversation != "fresh" || preview.Session != nil {
		t.Fatal(preview)
	}
	client := &conversionHerdr{external: h}
	l := reviewLifecycle(t, b, client, req.Spec.ID)
	fresh := false
	session := "00000000-0000-4000-8000-000000000002"
	l.r.opts.Headless = &headlessStub{fn: func(s turnStart) {
		fresh = s.launch.Session == nil && s.launch.Workdir == h.pane.Cwd
		writeCodexRollout(t, session, s.launch.Workdir)
		s.handle.complete(TurnOutcome{Acknowledged: true, Session: &session, ExitCode: ptr(int64(0)), Text: "ready"})
	}}
	m, e := l.current(ctx, "external")
	must(t, e)
	must(t, l.bringOntoComms(ctx, *m, model.Request{Args: map[string]any{"expected_session": nil, "pane": h.pane.ID, "terminal": h.pane.Terminal}}))
	after, e := l.current(ctx, m.ID)
	must(t, e)
	if !fresh || after.External != nil || after.State != model.MemberStateRunning || len(client.starts) != 1 || !slices.Contains(client.starts[0].Args, h.pane.Cwd) {
		t.Fatal(after, client.starts, fresh)
	}
	if len(client.closedPanes) != 1 || client.closedPanes[0] != h.pane.ID {
		t.Fatal(client.closedPanes)
	}
}

func (h *conversionHerdr) ClosePane(ctx context.Context, id string) error {
	if id == h.external.pane.ID {
		h.oldClosed = true
	}
	return h.reviewHerdr.ClosePane(ctx, id)
}

// Obtain the real client's gone classification without contacting any server.
func init() {
	if os.Getenv("LUGARN_TEST_GONE_HERDR") == "1" {
		_, _ = os.Stderr.WriteString(`{"error":{"code":"pane_not_found"}}`)
		os.Exit(1)
	}
}
func reviewGone(t *testing.T) error {
	t.Helper()
	binary, e := os.Executable()
	must(t, e)
	t.Setenv("LUGARN_TEST_GONE_HERDR", "1")
	_, e = herdr.New(binary, "lugarn-fake").Pane(context.Background(), "gone")
	t.Setenv("LUGARN_TEST_GONE_HERDR", "")
	if !herdr.IsGone(e) {
		t.Fatal(e)
	}
	return e
}
func TestBringRetryAcrossManagedLeaseHandoff(t *testing.T) {
	for _, phase := range []string{"before-lease", "after-lease"} {
		t.Run(phase, func(t *testing.T) {
			b, h, r := adoptionSetup(t)
			ctx := context.Background()
			r.Spec.Members[0].Harness = model.HarnessClaude
			h.agent.Kind = "claude"
			h.pane.Agent = "claude"
			session := "00000000-0000-4000-8000-000000000001"
			h.agent.Session = &herdr.AgentSession{Agent: "claude", Kind: "id", Value: session}
			_, e := b.AdoptTeam(ctx, r)
			must(t, e)
			client := &conversionHerdr{external: h, gone: reviewGone(t)}
			l := reviewLifecycle(t, b, client, r.Spec.ID)
			if phase == "before-lease" {
				b.opts.ID = func() (string, error) { return "", errors.New("interrupted before lease") }
			} else {
				client.startErr = model.NewError(model.ErrorLaunchFailed, "replacement failed")
			}
			m, e := l.current(ctx, "external")
			must(t, e)
			e = l.bringOntoComms(ctx, *m, model.Request{Args: map[string]any{"expected_session": session, "pane": h.pane.ID, "terminal": h.pane.Terminal}})
			if e == nil {
				t.Fatal("failure was not injected")
			}
			m, e = l.current(ctx, m.ID)
			must(t, e)
			if m.Session == nil || *m.Session != session || m.Lease.Driver != model.DriverNone {
				t.Fatal(m)
			}
			// Recreate the runner to exercise durable state, rather than local retry data.
			b.opts.ID = model.NewULID
			client.startErr = nil
			l = reviewLifecycle(t, b, client, r.Spec.ID)
			if phase == "before-lease" {
				if m.External == nil {
					t.Fatal("restart marker lost before replacement launch")
				}
				preview, e := b.BringOntoComms(ctx, wire.BringOntoCommsRequest{Team: r.Spec.ID, Member: m.ID})
				must(t, e)
				if preview.Conversation != "resume" || preview.Session == nil || *preview.Session != session {
					t.Fatal(preview)
				}
				confirmed, e := b.BringOntoComms(ctx, wire.BringOntoCommsRequest{Team: r.Spec.ID, Member: m.ID, Confirm: true, IfGeneration: &m.Lease.Generation, ExpectedSession: preview.Session})
				must(t, e)
				must(t, l.Execute(ctx, *confirmed.Request))
			} else {
				if m.External != nil {
					t.Fatal("managed lease retained external binding")
				}
				retry, e := b.OpenTerminal(ctx, wire.OpenTerminalRequest{Team: r.Spec.ID, Member: m.ID, IfGeneration: &m.Lease.Generation})
				must(t, e)
				must(t, l.Execute(ctx, retry.Request))
			}
			m, e = l.current(ctx, m.ID)
			must(t, e)
			if m.External != nil || m.State != model.MemberStateRunning || !slices.Contains(client.starts[len(client.starts)-1].Args, session) {
				t.Fatal(m, client.starts)
			}
			if len(client.closedPanes) != 1 || client.closedPanes[0] != h.pane.ID {
				t.Fatal("retry closed a pane", client.closedPanes)
			}
		})
	}
}
