package broker

import (
	"context"
	"os"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

var herdrIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_:-]{0,127}$`)

func externalSession(a herdr.Agent) (*string, error) {
	if a.Session == nil {
		return nil, nil
	}
	// pi paths are not resume ids in the broker's v1 launch table. Never pass
	// arbitrary server paths or strings through as harness arguments.
	if a.Kind == string(model.HarnessPi) {
		return nil, nil
	}
	if a.Session.Kind != "id" || a.Session.Agent != a.Kind || !harness.ValidSession(model.Harness(a.Kind), a.Session.Value) {
		return nil, nil
	}
	return ptr(a.Session.Value), nil
}

// inspectExternal makes only read calls and checks server data before it can
// become a launch path, name or terminal target.
func (b *Broker) inspectExternal(ctx context.Context, server, workspace, pane, name string, h model.Harness, folder string) (model.ExternalMember, error) {
	if b.integration == nil {
		return model.ExternalMember{}, model.NewError(model.ErrorHerdrUnavailable, "Herdr integration unavailable")
	}
	return b.integration.inspectExternal(ctx, server, workspace, pane, name, h, folder)
}

func (i *Integration) inspectExternal(ctx context.Context, server, workspace, pane, name string, h model.Harness, folder string) (model.ExternalMember, error) {
	var out model.ExternalMember
	if !herdrIDPattern.MatchString(workspace) || !herdrIDPattern.MatchString(pane) || herdr.ValidateAgentName(name) != nil {
		return out, invalid("invalid Herdr identity")
	}
	client := i.opts.Herdr(server)
	ws, e := client.Workspace(ctx, workspace)
	if e != nil {
		return out, e
	}
	p, e := client.Pane(ctx, pane)
	if e != nil {
		return out, e
	}
	a, e := client.Agent(ctx, name)
	if e != nil {
		return out, e
	}
	return i.validateExternal(ctx, workspace, pane, name, h, folder, ws, p, a)
}

// Targeted actions and list-based polling share all identity/path validation.
func (i *Integration) validateExternal(ctx context.Context, workspace, pane, name string, h model.Harness, folder string, ws herdr.Workspace, p herdr.Pane, a herdr.Agent) (model.ExternalMember, error) {
	var out model.ExternalMember
	if !herdrIDPattern.MatchString(workspace) || !herdrIDPattern.MatchString(pane) || herdr.ValidateAgentName(name) != nil {
		return out, invalid("invalid Herdr identity")
	}
	if ws.ID != workspace || p.ID != pane || p.Workspace != workspace || a.Pane != pane || a.Workspace != workspace || a.Name != name || a.Tab != p.Tab || !herdrIDPattern.MatchString(p.Tab) || !herdrIDPattern.MatchString(p.Terminal) {
		return out, model.NewError(model.ErrorLeaseConflict, "Herdr pane identity changed")
	}
	if a.Kind != string(h) || p.Agent != string(h) || a.Cwd != folder || p.Cwd != folder {
		return out, model.NewError(model.ErrorLeaseConflict, "Herdr harness or folder changed")
	}
	if !utf8.ValidString(folder) || strings.ContainsRune(folder, 0) || utf8.RuneCountInString(folder) > 4096 || !policy.IsAbsPath(i.b.host(ctx).OS(), folder) {
		return out, invalid("invalid Herdr folder")
	}
	session, e := externalSession(a)
	if e != nil {
		return out, e
	}
	return model.ExternalMember{Workspace: workspace, Pane: pane, Terminal: p.Terminal, Agent: name, Session: session, Activity: a.Activity, Available: true}, nil
}

func (b *Broker) AdoptTeam(ctx context.Context, r wire.AdoptTeamRequest) (wire.ApplyTeamResult, error) {
	var zero wire.ApplyTeamResult
	if len(r.Members) != len(r.Spec.Members) || len(r.Members) == 0 {
		return zero, invalid("adoption must bind every spec member exactly once")
	}
	if e := policy.ValidateSpec(r.Spec, policy.SpecFacts{}, b.host(ctx)); e != nil {
		return zero, e
	}
	spec := policy.ApplyDefaults(r.Spec)
	members := map[string]model.MemberSpec{}
	for _, m := range spec.Members {
		members[m.ID] = m
	}
	bindings := map[string]model.ExternalMember{}
	panes := map[string]bool{}
	for _, binding := range r.Members {
		m, ok := members[binding.Member]
		if !ok || panes[binding.Pane] || bindings[binding.Member].Pane != "" {
			return zero, invalid("duplicate or unknown adoption member or pane")
		}
		if *m.Worktree || m.Mode != model.ModePane {
			return zero, invalid("adopted members require pane mode and worktree false")
		}
		v, e := b.inspectExternal(ctx, *spec.Herdr.Server, r.Workspace, binding.Pane, binding.Agent, m.Harness, m.Folder)
		if e != nil {
			return zero, e
		}
		bindings[m.ID] = v
		panes[v.Pane] = true
	}
	return b.applyTeam(ctx, wire.ApplyTeamRequest{Spec: spec, IfRevision: 0}, func(tx *store.Tx) error {
		for id, v := range bindings {
			used, e := tx.PaneTracked(*spec.Herdr.Server, v.Pane)
			if e != nil {
				return e
			}
			if used {
				return model.NewError(model.ErrorLeaseConflict, "Herdr pane is already tracked")
			}
			names, e := tx.ReservedHerdrNames(*spec.Herdr.Server)
			if e != nil {
				return e
			}
			if names[v.Agent] {
				return model.NewError(model.ErrorNameTaken, "Herdr name is reserved by another member")
			}
			if e = tx.PutExternalMember(id, v); e != nil {
				return e
			}
			if e = tx.UpdateMember(id, store.MemberPatch{HerdrAgent: ptr(&v.Agent), HerdrWorkspace: ptr(&v.Workspace)}, b.now()); e != nil {
				return e
			}
		}
		return tx.UpdateTeam(spec.ID, store.TeamPatch{HerdrWorkspace: ptr(&r.Workspace), Adopted: ptr(true)}, b.now())
	})
}

func (b *Broker) refreshExternal(ctx context.Context, team string) error {
	if b.integration == nil {
		return nil
	}
	var t *store.Team
	var ms []model.Member
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		var e error
		t, e = tx.GetTeam(team)
		if e != nil {
			return e
		}
		ms, e = tx.ListMembers(team, false)
		return e
	})
	if e != nil {
		return e
	}
	if t.Forgotten {
		return nil
	}
	var snapshot externalSnapshot
	var observationErr error
	for _, m := range ms {
		if m.External != nil {
			snapshot, observationErr = b.integration.listExternal(ctx, t.HerdrServer)
			break
		}
	}
	for _, m := range ms {
		if m.External == nil {
			continue
		}
		old := *m.External
		var v model.ExternalMember
		err := observationErr
		if err == nil {
			v, err = b.integration.validateExternal(ctx, old.Workspace, old.Pane, old.Agent, *m.Harness, *m.Folder, snapshot.workspaces[old.Workspace], snapshot.panes[old.Pane], snapshot.agents[old.Agent])
		}
		if err != nil {
			v = old
			v.Available = false
			v.Activity = model.ActivityUnknown
		}
		if err == nil && v.Terminal != old.Terminal {
			v = old
			v.Available = false
			v.Activity = model.ActivityUnknown
		}
		if reflect.DeepEqual(old, v) {
			continue
		}
		e = b.db.Write(ctx, func(tx *store.Tx) error {
			current, e := tx.GetMember(m.ID)
			if e != nil {
				return e
			}
			if !reflect.DeepEqual(current.External, m.External) {
				return nil
			}
			return tx.PutExternalMember(m.ID, v)
		})
		if e != nil {
			return e
		}
	}
	return nil
}

// A failed or interrupted explicit restart can be retried after its old pane
// has gone. A missing agent alone is insufficient: the terminal might now hold
// somebody else's shell job. Never treat server unavailability as proof of exit.
func (i *Integration) externalForBring(ctx context.Context, t *store.Team, m model.Member) (model.ExternalMember, error) {
	old := *m.External
	v, err := i.inspectExternal(ctx, t.HerdrServer, old.Workspace, old.Pane, old.Agent, *m.Harness, *m.Folder)
	if err == nil {
		return v, nil
	}
	if !herdr.IsGone(err) {
		return v, err
	}
	var restarting bool
	if e := i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; restarting, e = tx.ExternalRestarting(m.ID); return e }); e != nil {
		return v, e
	}
	if !restarting {
		return v, err
	}
	if _, e := i.opts.Herdr(t.HerdrServer).Pane(ctx, old.Pane); !herdr.IsGone(e) {
		return v, err
	}
	old.Available = false
	return old, nil
}

func (b *Broker) BringOntoComms(ctx context.Context, r wire.BringOntoCommsRequest) (wire.BringOntoCommsResult, error) {
	out := wire.BringOntoCommsResult{RestartRequired: true, Conversation: "fresh"}
	var m *model.Member
	var t *store.Team
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		var e error
		t, e = tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		m, e = b.member(tx, r.Team, r.Member)
		return e
	})
	if e != nil {
		return out, e
	}
	if t.Forgotten || m.External == nil {
		return out, invalid("member is not an external tracked agent")
	}
	if b.integration == nil {
		return out, model.NewError(model.ErrorHerdrUnavailable, "Herdr integration unavailable")
	}
	v, e := b.integration.externalForBring(ctx, t, *m)
	if e != nil {
		return out, e
	}
	if v.Terminal != m.External.Terminal {
		return out, model.NewError(model.ErrorLeaseConflict, "tracked terminal was replaced")
	}
	out.Session = v.Session
	if v.Session != nil {
		out.Conversation = "resume"
	}
	if !r.Confirm {
		return out, nil
	}
	if r.IfGeneration == nil || !reflect.DeepEqual(r.ExpectedSession, v.Session) {
		return out, model.NewError(model.ErrorLeaseConflict, "confirmation is stale; preview bring-onto-comms again")
	}
	e = b.db.Write(ctx, func(tx *store.Tx) error {
		current, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		if e = running(current); e != nil {
			return e
		}
		if !b.fresh(current) {
			return model.NewError(model.ErrorRunnerUnavailable, "the team's runner isn't up")
		}
		member, e := b.member(tx, r.Team, r.Member)
		if e != nil {
			return e
		}
		if member.External == nil || member.Lease == nil || member.Lease.Generation != *r.IfGeneration || member.External.Pane != v.Pane {
			return model.NewError(model.ErrorLeaseConflict, "member changed")
		}
		human, e := tx.GetLiveMember(r.Team, "human")
		if e != nil {
			return e
		}
		var session any
		if v.Session != nil {
			session = *v.Session
		}
		req, e := b.request(tx, current, model.RequestKindOpenTerminal, *member, *human, map[string]any{"bring_onto_comms": true, "expected_session": session, "pane": v.Pane, "terminal": v.Terminal})
		if e == nil {
			out.Request = &req
		}
		return e
	})
	return out, e
}

func (l *integrationLifecycle) bringOntoComms(ctx context.Context, m model.Member, req model.Request) error {
	if m.External == nil {
		return model.NewError(model.ErrorLeaseConflict, "member is already on comms")
	}
	var team *store.Team
	if e := l.i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; team, e = tx.GetTeam(m.Team); return e }); e != nil {
		return e
	}
	v, e := l.i.externalForBring(ctx, team, m)
	if e != nil {
		return e
	}
	expected, _ := req.Args["expected_session"].(string)
	session := ""
	if v.Session != nil {
		session = *v.Session
	}
	if session != expected || req.Args["pane"] != v.Pane || req.Args["terminal"] != v.Terminal {
		return model.NewError(model.ErrorLeaseConflict, "external conversation or terminal changed; preview again")
	}
	if issue := l.i.b.host(ctx).HarnessIssue(*m.Harness); issue != "" {
		return model.NewError(model.ErrorHarnessUnavailable, "harness unavailable").WithDetail("reason", issue)
	}
	if _, e = l.r.opts.Binary(*m.Harness); e != nil {
		return unresolvable(e)
	}
	st, e := os.Stat(*m.Folder)
	if e != nil || !st.IsDir() {
		return model.NewError(model.ErrorLaunchFailed, "adopted folder is missing").WithDetail("cause", "workdir_missing")
	}
	m.Session = v.Session
	if m.Session != nil {
		if e = l.i.b.assertCodexWorkdir(m); e != nil {
			return e
		}
	}
	// Commit the authorized transition before touching the server. If interrupted,
	// reconciliation fails this request, and does not automatically restart it.
	e = l.r.write(ctx, &m, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning {
			return model.NewError(model.ErrorTeamNotRunning, "team stopped")
		}
		current, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		if current.External == nil {
			return model.NewError(model.ErrorLeaseConflict, "member changed")
		}
		if e := tx.MarkExternalRestarting(m.ID, v); e != nil {
			return e
		}
		return tx.SetMemberState(m.ID, model.MemberStateStopped, ptr(model.StopReasonPerson), nil, nil, l.i.b.now())
	})
	if e != nil {
		return e
	}
	// Close only the selected pane, never its whole tab (which may have siblings).
	if v.Available {
		if e = l.i.opts.Herdr(team.HerdrServer).ClosePane(ctx, v.Pane); e != nil {
			return e
		}
	}
	e = l.r.write(ctx, &m, func(tx *store.Tx, _ *store.Team) error {
		if v.Session != nil {
			seal, e := model.NewSeal()
			if e != nil {
				return e
			}
			if e = l.r.session(tx, m, *v.Session, seal, model.SessionSourceReported, "", false); e != nil {
				return e
			}
		}
		// Keep the restart marker until Open atomically transfers ownership to a
		// managed pane lease. Preflight failures can still retry this conversion.
		return tx.SetMemberSession(m.ID, v.Session, l.i.b.now())
	})
	if e != nil {
		return e
	}
	current, e := l.current(ctx, m.ID)
	if e != nil {
		return e
	}
	if e = l.Open(ctx, *current, model.LeaseCauseOpenTerminal); e != nil {
		_ = l.r.launchFailed(context.WithoutCancel(ctx), *current, e)
		return e
	}
	return nil
}

func (b *Broker) ForgetTeam(ctx context.Context, r wire.ForgetTeamRequest) (wire.ForgetTeamResult, error) {
	var out wire.ForgetTeamResult
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		if !t.Adopted {
			return invalid("forget-team is only for adopted teams")
		}
		if t.Forgotten {
			return nil
		}
		if t.Revision != r.IfRevision {
			return model.NewError(model.ErrorRevisionConflict, "team revision changed").WithDetails(map[string]any{"expected": r.IfRevision, "actual": t.Revision})
		}
		if t.State == model.TeamStateStarting || t.State == model.TeamStateStopping {
			return model.NewError(model.ErrorLeaseConflict, "team is starting or stopping")
		}
		requests, e := tx.ListOpenRequests(t.ID)
		if e != nil {
			return e
		}
		if len(requests) != 0 {
			return model.NewError(model.ErrorLeaseConflict, "wait for member requests before forgetting")
		}
		ms, e := tx.ListMembers(t.ID, false)
		if e != nil {
			return e
		}
		for _, m := range ms {
			if m.Lease != nil && (m.Lease.Driver == model.DriverHeadless || m.State == model.MemberStateStarting || m.State == model.MemberStateStopping) {
				return model.NewError(model.ErrorLeaseConflict, "open headless members in terminals and wait for launches before forgetting")
			}
		}
		patch := store.TeamPatch{Forgotten: ptr(true)}
		if t.State == model.TeamStateRunning {
			patch.State = ptr(model.TeamStateStopping)
		}
		return tx.UpdateTeam(t.ID, patch, b.now())
	})
	if e != nil {
		return out, e
	}
	stopped, e := b.StopTeam(ctx, wire.StopTeamRequest{Team: r.Team})
	if e != nil {
		return out, e
	}
	out.Team = stopped.Team
	// A stopped adopted team still needs a visible tombstone for watch clients.
	e = b.db.Write(ctx, func(tx *store.Tx) error {
		return b.event(tx, &r.Team, model.EventTeamApplied, true, nil, nil, wire.TeamAppliedEventData{Team: out.Team})
	})
	return out, e
}
