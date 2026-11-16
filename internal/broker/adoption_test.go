package broker

import (
	"context"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type adoptionHerdr struct {
	HerdrAPI    // Any unimplemented mutation panics: adoption must be read-only.
	agent       herdr.Agent
	pane        herdr.Pane
	gets        int
	lists       [3]int
	extraAgents []herdr.Agent
	extraPanes  []herdr.Pane
}

func (h *adoptionHerdr) Workspace(context.Context, string) (herdr.Workspace, error) {
	h.gets++
	return herdr.Workspace{ID: h.pane.Workspace}, nil
}
func (h *adoptionHerdr) Pane(context.Context, string) (herdr.Pane, error) {
	h.gets++
	return h.pane, nil
}
func (h *adoptionHerdr) Agent(context.Context, string) (herdr.Agent, error) {
	h.gets++
	return h.agent, nil
}
func (h *adoptionHerdr) Agents(context.Context) ([]herdr.Agent, error) {
	h.lists[2]++
	return append([]herdr.Agent{h.agent}, h.extraAgents...), nil
}

func adoptionSetup(t *testing.T) (*Broker, *adoptionHerdr, wire.AdoptTeamRequest) {
	t.Helper()
	b, _ := setup(t)
	folder := t.TempDir()
	h := &adoptionHerdr{agent: herdr.Agent{Name: "external_name", Kind: "pi", Pane: "w1:p1", Workspace: "w1", Tab: "w1:t1", Cwd: folder, Activity: model.ActivityIdle}, pane: herdr.Pane{ID: "w1:p1", Workspace: "w1", Tab: "w1:t1", Terminal: "term_123", Agent: "pi", Cwd: folder}}
	bin, e := os.Executable()
	must(t, e)
	i, e := NewIntegration(b, IntegrationOptions{Binary: bin, Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	i.Attach()
	r := wire.AdoptTeamRequest{Workspace: "w1", Members: []wire.AdoptMember{{Member: "external", Pane: "w1:p1", Agent: "external_name"}}, Spec: model.TeamSpec{Version: 1, ID: "adopted", Name: "adopted", Pattern: model.PatternCustom, Members: []model.MemberSpec{{ID: "external", Name: "external", Role: model.RoleCustom, Harness: model.HarnessPi, Folder: folder, Profile: model.ProfileWorkspace, Mode: model.ModePane, Brief: "b"}}, Grid: []model.GridEdge{}}}
	return b, h, r
}
func TestAdoptionReadOnlyAndExplicitCommsBoundary(t *testing.T) {
	b, h, r := adoptionSetup(t)
	ctx := context.Background()
	_, e := b.AdoptTeam(ctx, r)
	must(t, e)
	ms, e := b.Members(ctx, wire.MembersRequest{Team: r.Spec.ID})
	must(t, e)
	var m model.Member
	for _, v := range ms.Members {
		if v.ID == "external" {
			m = v
		}
	}
	if m.External == nil || m.External.Terminal != "term_123" || m.Lease.Driver != model.DriverNone || m.Lease.Launch != nil || *m.HerdrName != "external_name" {
		t.Fatalf("bad tracking record: %+v", m)
	}
	preview, e := b.BringOntoComms(ctx, wire.BringOntoCommsRequest{Team: r.Spec.ID, Member: m.ID})
	must(t, e)
	if preview.Conversation != "fresh" || preview.Request != nil {
		t.Fatal(preview)
	}
	_, e = b.OpenTerminal(ctx, wire.OpenTerminalRequest{Team: r.Spec.ID, Member: m.ID})
	if e == nil {
		t.Fatal("external member launched implicitly")
	}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		now := b.now()
		if e := tx.UpdateTeam(r.Spec.ID, store.TeamPatch{State: ptr(model.TeamStateRunning)}, now); e != nil {
			return e
		}
		return tx.Heartbeat(r.Spec.ID, model.Runner{State: model.RunnerStateUp, HeartbeatAt: &now}, now)
	}))
	_, e = b.Send(ctx, wire.SendRequest{Team: r.Spec.ID, To: &m.ID, Text: "must not deliver"})
	if e == nil {
		t.Fatal("external member accepted comms")
	}
	_, e = b.StopMember(ctx, wire.StopMemberRequest{Team: r.Spec.ID, Member: m.ID})
	if e == nil {
		t.Fatal("external stop accepted")
	}
	h.agent.Activity = model.ActivityBlocked
	ms, e = b.Members(ctx, wire.MembersRequest{Team: r.Spec.ID})
	must(t, e)
	for _, v := range ms.Members {
		if v.ID == m.ID && v.External.Activity != model.ActivityBlocked {
			t.Fatal(v)
		}
	}
	// Stale generation confirmation is read-only as well.
	_, e = b.BringOntoComms(ctx, wire.BringOntoCommsRequest{Team: r.Spec.ID, Member: m.ID, Confirm: true, IfGeneration: ptr(int64(8))})
	if model.AsError(e).Code != model.ErrorLeaseConflict {
		t.Fatal(e)
	}
}
func TestAdoptionInvalidAndDuplicateBindingsRollback(t *testing.T) {
	for _, bad := range []string{"folder", "name", "terminal", "workspace", "harness"} {
		t.Run(bad, func(t *testing.T) {
			b, h, r := adoptionSetup(t)
			switch bad {
			case "folder":
				h.agent.Cwd = "relative"
			case "name":
				h.agent.Name = "other"
			case "terminal":
				h.pane.Terminal = "--bad"
			case "workspace":
				h.pane.Workspace = "w2"
			case "harness":
				h.agent.Kind = "shell"
			}
			if _, e := b.AdoptTeam(context.Background(), r); e == nil {
				t.Fatal("invalid server data adopted")
			}
			must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
				if _, e := tx.GetTeam(r.Spec.ID); !missing(e) {
					t.Fatalf("partial team: %v", e)
				}
				return nil
			}))
		})
	}
	b, _, r := adoptionSetup(t)
	_, e := b.AdoptTeam(context.Background(), r)
	must(t, e)
	r.Spec.ID = "second"
	r.Spec.Name = "second"
	r.Spec.Members[0].ID = "external2"
	r.Members[0].Member = "external2"
	if _, e = b.AdoptTeam(context.Background(), r); model.AsError(e).Code != model.ErrorLeaseConflict {
		t.Fatal(e)
	}
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
		if _, e := tx.GetTeam("second"); !missing(e) {
			t.Fatal("partial duplicate team")
		}
		return nil
	}))
}
func TestConcurrentNameReservationsAndReuse(t *testing.T) {
	b, h, _ := adoptionSetup(t)
	h.agent.Name = "writer"
	ctx := context.Background()
	// Two new teams use the same member name and encounter an external writer.
	for _, id := range []string{"one", "two"} {
		must(t, b.db.Write(ctx, func(tx *store.Tx) error {
			if e := tx.CreateTeam(store.Team{ID: id, Name: id, Pattern: model.PatternCustom}); e != nil {
				return e
			}
			return tx.InsertMember(store.MemberParams{ID: id, TeamID: id, Name: "writer", Kind: model.MemberKindAgent, Role: ptr(model.RoleCustom), Origin: model.MemberOriginSpec, Harness: ptr(model.HarnessPi), Profile: ptr(model.ProfileWorkspace), Mode: ptr(model.ModePane), Folder: ptr("/tmp"), Brief: ptr("b")})
		}))
	}
	var wg sync.WaitGroup
	names := make(chan string, 2)
	errs := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- b.db.Write(ctx, func(tx *store.Tx) error {
				team, e := tx.GetTeam(id)
				if e != nil {
					return e
				}
				m, e := tx.GetMember(id)
				if e != nil {
					return e
				}
				name, e := b.integration.reserveAgentName(tx, team, *m, []herdr.Agent{h.agent})
				if e != nil {
					return e
				}
				names <- name
				return tx.UpdateMember(id, store.MemberPatch{HerdrAgent: ptr(&name)}, b.now())
			})
		}()
	}
	wg.Wait()
	close(errs)
	close(names)
	for e := range errs {
		must(t, e)
	}
	got := map[string]bool{}
	for n := range names {
		got[n] = true
	}
	if !reflect.DeepEqual(got, map[string]bool{"writer-2": true, "writer-3": true}) {
		t.Fatal(got)
	}
	h.agent.Name = "free"
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		team, e := tx.GetTeam("one")
		if e != nil {
			return e
		}
		m, e := tx.GetMember("one")
		if e != nil {
			return e
		}
		name, e := b.integration.reserveAgentName(tx, team, *m, []herdr.Agent{h.agent})
		if name != *m.HerdrName {
			t.Fatal("name changed")
		}
		return e
	}))
}

type detachTeardown struct {
	t     *testing.T
	calls int
}

func (d *detachTeardown) Close(_ context.Context, team store.Team, ms []model.Member) error {
	d.calls++
	if !team.Forgotten || len(ms) != 0 {
		d.t.Fatal("forget attempted to close agents", team, ms)
	}
	return nil
}
func TestForgetAdoptedTeamDetachesAndCannotRestart(t *testing.T) {
	b, _, r := adoptionSetup(t)
	ctx := context.Background()
	_, e := b.AdoptTeam(ctx, r)
	must(t, e)
	teardown := &detachTeardown{t: t}
	b.opts.Teardown = teardown
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam(r.Spec.ID, store.TeamPatch{State: ptr(model.TeamStateRunning)}, b.now())
	}))
	_, e = b.ForgetTeam(ctx, wire.ForgetTeamRequest{Team: r.Spec.ID, IfRevision: 99})
	if model.AsError(e).Code != model.ErrorRevisionConflict {
		t.Fatal(e)
	}
	out, e := b.ForgetTeam(ctx, wire.ForgetTeamRequest{Team: r.Spec.ID, IfRevision: 1})
	must(t, e)
	if out.Team.Forgotten == nil || !*out.Team.Forgotten || teardown.calls != 1 {
		t.Fatal(out, teardown)
	}
	_, e = b.ForgetTeam(ctx, wire.ForgetTeamRequest{Team: r.Spec.ID, IfRevision: 1})
	must(t, e)
	_, e = b.StartTeam(ctx, wire.StartTeamRequest{Team: r.Spec.ID})
	if model.AsError(e).Code != model.ErrorNotFound {
		t.Fatal(e)
	}
	status, e := b.Status(ctx, wire.StatusRequest{Team: &r.Spec.ID})
	must(t, e)
	if len(status.Teams) != 0 {
		t.Fatal("forgotten team still listed")
	}
}
func TestBringPreviewSessionValidationAndTerminalReplacement(t *testing.T) {
	b, h, r := adoptionSetup(t)
	ctx := context.Background()
	r.Spec.Members[0].Harness = model.HarnessClaude
	h.agent.Kind = "claude"
	h.pane.Agent = "claude"
	id := "00000000-0000-4000-8000-000000000001"
	h.agent.Session = &herdr.AgentSession{Agent: "claude", Kind: "id", Value: id}
	_, e := b.AdoptTeam(ctx, r)
	must(t, e)
	preview, e := b.BringOntoComms(ctx, wire.BringOntoCommsRequest{Team: r.Spec.ID, Member: "external"})
	must(t, e)
	if preview.Conversation != "resume" || preview.Session == nil || *preview.Session != id {
		t.Fatal(preview)
	}
	h.agent.Session.Value = "--resume malicious"
	preview, e = b.BringOntoComms(ctx, wire.BringOntoCommsRequest{Team: r.Spec.ID, Member: "external"})
	must(t, e)
	if preview.Conversation != "fresh" || preview.Session != nil {
		t.Fatal(preview)
	}
	h.agent.Session.Value = id
	h.pane.Terminal = "term_replacement"
	if _, e = b.BringOntoComms(ctx, wire.BringOntoCommsRequest{Team: r.Spec.ID, Member: "external"}); model.AsError(e).Code != model.ErrorLeaseConflict {
		t.Fatal(e)
	}
	ms, e := b.Members(ctx, wire.MembersRequest{Team: r.Spec.ID})
	must(t, e)
	for _, m := range ms.Members {
		if m.External != nil && (m.External.Available || m.External.Terminal == "term_replacement") {
			t.Fatal("rebound external terminal", m)
		}
	}
}

func TestServerChangeCannotReuseOtherServerWorkspaceID(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopped), HerdrWorkspace: ptr(ptr("w1"))}, b.now())
	}))
	current, e := b.GetTeam(ctx, wire.GetTeamRequest{Team: "t"})
	must(t, e)
	current.Spec.Herdr.Server = ptr("another-server")
	_, e = b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: current.Spec, IfRevision: current.Revision})
	must(t, e)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		team, e := tx.GetTeam("t")
		if e == nil && team.HerdrWorkspace != nil {
			t.Fatal("cross-server workspace ID retained")
		}
		return e
	}))
}

type failedExternalHerdr struct {
	*adoptionHerdr
	err error
}

func (h *failedExternalHerdr) Agents(context.Context) ([]herdr.Agent, error) {
	return nil, h.err
}
func TestMembersExternalReadFailuresAreObservations(t *testing.T) {
	for _, code := range []model.ErrorCode{model.ErrorTimeout, model.ErrorInternal, model.ErrorInvalidRequest} {
		t.Run(string(code), func(t *testing.T) {
			b, h, r := adoptionSetup(t)
			ctx := context.Background()
			_, e := b.AdoptTeam(ctx, r)
			must(t, e)
			b.integration.opts.Herdr = func(string) HerdrAPI { return &failedExternalHerdr{h, model.NewError(code, "failed observation")} }
			out, e := b.Members(ctx, wire.MembersRequest{Team: r.Spec.ID})
			must(t, e)
			found := false
			for _, m := range out.Members {
				if m.ID == "external" {
					found = true
					if m.External == nil || m.External.Available || m.External.Activity != model.ActivityUnknown {
						t.Fatal(m)
					}
				}
			}
			if !found {
				t.Fatal("external member missing")
			}
		})
	}
}
func TestUnusableExternalSessionsAreFresh(t *testing.T) {
	for _, session := range []herdr.AgentSession{
		{Agent: "claude", Kind: "path", Value: "/tmp/conversation"},
		{Agent: "claude", Kind: "id", Value: "--resume malicious"},
		{Agent: "codex", Kind: "id", Value: "00000000-0000-4000-8000-000000000001"},
	} {
		t.Run(session.Kind+session.Agent+session.Value, func(t *testing.T) {
			b, h, r := adoptionSetup(t)
			ctx := context.Background()
			r.Spec.Members[0].Harness = model.HarnessClaude
			h.agent.Kind = "claude"
			h.pane.Agent = "claude"
			_, e := b.AdoptTeam(ctx, r)
			must(t, e)
			h.agent.Session = &session
			out, e := b.Members(ctx, wire.MembersRequest{Team: r.Spec.ID})
			must(t, e)
			for _, m := range out.Members {
				if m.External != nil && (!m.External.Available || m.External.Session != nil) {
					t.Fatal(m)
				}
			}
			preview, e := b.BringOntoComms(ctx, wire.BringOntoCommsRequest{Team: r.Spec.ID, Member: "external"})
			must(t, e)
			if preview.Conversation != "fresh" || preview.Session != nil {
				t.Fatal(preview)
			}
		})
	}
}

func (h *adoptionHerdr) Workspaces(context.Context) ([]herdr.Workspace, error) {
	h.lists[0]++
	return []herdr.Workspace{{ID: h.pane.Workspace}}, nil
}
func (h *adoptionHerdr) Panes(context.Context) ([]herdr.Pane, error) {
	h.lists[1]++
	return append([]herdr.Pane{h.pane}, h.extraPanes...), nil
}
func TestMembersUsesOneExternalSnapshotPerPoll(t *testing.T) {
	b, h, r := adoptionSetup(t)
	ctx := context.Background()
	_, e := b.AdoptTeam(ctx, r)
	must(t, e)
	// Add a second valid binding without issuing targeted adoption reads.
	agent, pane := h.agent, h.pane
	agent.Name = "external_two"
	agent.Pane = "w1:p2"
	pane.ID = agent.Pane
	pane.Terminal = "term_456"
	h.extraAgents = []herdr.Agent{agent}
	h.extraPanes = []herdr.Pane{pane}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.InsertMember(store.MemberParams{ID: "external2", TeamID: r.Spec.ID, Name: "external-two", Role: ptr(model.RoleCustom), Kind: model.MemberKindAgent, Origin: model.MemberOriginSpec, Harness: ptr(model.HarnessPi), Profile: ptr(model.ProfileWorkspace), Mode: ptr(model.ModePane), Folder: &pane.Cwd, Brief: ptr("b")}); e != nil {
			return e
		}
		if e := tx.InsertLease("external2", model.Lease{Driver: model.DriverNone}, b.now()); e != nil {
			return e
		}
		return tx.PutExternalMember("external2", model.ExternalMember{Workspace: pane.Workspace, Pane: pane.ID, Terminal: pane.Terminal, Agent: agent.Name, Activity: model.ActivityIdle, Available: true})
	}))
	for poll := 1; poll <= 2; poll++ {
		h.agent.Activity = model.ActivityWorking
		out, e := b.Members(ctx, wire.MembersRequest{Team: r.Spec.ID})
		must(t, e)
		external := 0
		for _, m := range out.Members {
			if m.External != nil {
				external++
				if !m.External.Available {
					t.Fatal(m)
				}
				if m.ID == "external" && m.External.Activity != model.ActivityWorking {
					t.Fatal(m)
				}
			}
		}
		if external != 2 || h.gets != 3 || h.lists != [3]int{poll, poll, poll} {
			t.Fatal(external, h.gets, h.lists)
		}
	}
	// A mismatch in one snapshot entry must not hide its healthy sibling.
	h.extraAgents[0].Tab = "wrong-tab"
	out, e := b.Members(ctx, wire.MembersRequest{Team: r.Spec.ID})
	must(t, e)
	for _, m := range out.Members {
		if m.External != nil && m.External.Available != (m.ID == "external") {
			t.Fatal(m)
		}
	}
}
