package broker

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"github.com/google/uuid"
)

type integrationLifecycle struct {
	i *Integration
	r *Runner
}

func (l *integrationLifecycle) Open(ctx context.Context, m model.Member, cause model.LeaseCause) (err error) {
	defer func() {
		if err != nil {
			err = model.AsError(err)
		}
	}()
	// Refusals are read-only. Check them before worktree preparation, lease
	// rotation, or the optional session-before-pane turn.
	if m.Lease == nil || m.Harness == nil {
		return invalid("member has no agent lease")
	}
	if m.Lease.Driver == model.DriverPane {
		return model.NewError(model.ErrorLeaseConflict, "member already has a pane")
	}
	if e := l.i.b.db.Read(ctx, func(tx *store.Tx) error {
		if _, e := tx.GetLiveTurn(m.ID); e == nil {
			return model.NewError(model.ErrorLeaseConflict, "member has a live turn")
		} else if !missing(e) {
			return e
		}
		return nil
	}); e != nil {
		return e
	}
	host := l.i.b.host(ctx)
	if issue := host.HarnessIssue(*m.Harness); issue != "" {
		return model.NewError(model.ErrorHarnessUnavailable, "harness unavailable").WithDetail("reason", issue)
	}
	binary, e := l.r.opts.Binary(*m.Harness)
	if e != nil {
		return unresolvable(e)
	}
	if m.Session != nil {
		if e := l.i.b.assertCodexWorkdir(m); e != nil {
			return e
		}
	}
	original := m
	launchStarted := false
	prepared, e := l.i.prepareWorktree(ctx, l.r, m)
	if e != nil {
		return e
	}
	m = *prepared
	defer func() {
		if err != nil && launchStarted {
			var current *model.Member
			_ = l.i.b.db.Read(context.WithoutCancel(ctx), func(tx *store.Tx) error { var e error; current, e = tx.GetMember(original.ID); return e })
			if current != nil && (current.StopReason == nil || *current.StopReason != model.StopReasonLaunchFailed) {
				_ = l.r.launchFailed(context.WithoutCancel(ctx), *current, err)
			}
		}
	}()
	if m.Session == nil && (m.Harness != nil && (*m.Harness == model.HarnessCodex || *m.Harness == model.HarnessOpenCode)) {
		headless := &m
		if m.Lease == nil || m.Lease.Driver != model.DriverHeadless {
			var e error
			headless, e = l.r.launchHeadless(ctx, m, model.LeaseCauseStart)
			if e != nil {
				return e
			}
		}
		brief, e := l.r.prepareTurnKind(ctx, *headless, true)
		if e != nil {
			return e
		}
		if brief == nil {
			return model.NewError(model.ErrorLeaseConflict, "no headless slot for session-before-a-pane")
		}
		launchStarted = true
		if e = l.r.runPreparedTurn(ctx, brief); e != nil {
			return e
		}
		e = l.i.b.db.Read(ctx, func(tx *store.Tx) error {
			turn, e := tx.GetTurn(brief.turn.ID)
			if e != nil {
				return e
			}
			if turn.Status != model.TurnStatusSucceeded {
				return model.NewError(model.ErrorLaunchFailed, "session-before-a-pane turn failed").WithDetail("turn", turn.ID)
			}
			current, e := tx.GetMember(m.ID)
			if e == nil {
				m = *current
			}
			return e
		})
		if e != nil {
			return e
		}
		cause = model.LeaseCauseStart
	}
	if e := l.i.b.assertCodexWorkdir(m); e != nil {
		return e
	}
	id, e := l.i.b.opts.ID()
	if e != nil {
		return e
	}
	token, e := model.NewToken()
	if e != nil {
		return e
	}
	var starting *model.Member
	var team *store.Team
	newSession := false
	if m.Session != nil && *m.Harness == model.HarnessClaude {
		var assigned bool
		if e = l.i.b.db.Read(ctx, func(tx *store.Tx) error {
			session, e := tx.GetMemberSession(m.ID, *m.Session)
			if e == nil {
				assigned = session.Source == model.SessionSourceAssigned && session.BriefedAt == nil
			}
			return e
		}); e != nil {
			return e
		}
		newSession = assigned && !l.i.b.sessionStarted(m.ID, *m.Session)
	}
	e = l.r.write(ctx, &m, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning || t.HerdrWorkspace == nil {
			return model.NewError(model.ErrorTeamNotRunning, "team has no running workspace")
		}
		if _, e := tx.GetLiveTurn(m.ID); e == nil {
			return model.NewError(model.ErrorLeaseConflict, "member has a live turn")
		} else if !missing(e) {
			return e
		}
		if m.Lease.Driver == model.DriverPane {
			return model.NewError(model.ErrorLeaseConflict, "member already has a pane")
		}
		now := l.i.b.now()
		if m.Lease.Launch != nil {
			if e := tx.EndLaunch(*m.Lease.Launch, model.LaunchEndReasonConverted, now); e != nil {
				return e
			}
			if _, e := tx.RevokeMemberTokens(m.ID, now); e != nil {
				return e
			}
		}
		next := model.Lease{Driver: model.DriverPane, Generation: m.Lease.Generation + 1, Launch: &id}
		if e := tx.InsertLaunch(store.Launch{ID: id, MemberID: m.ID, Generation: next.Generation, Driver: model.ModePane, SessionIDStart: m.Session, StartedAt: now}); e != nil {
			return e
		}
		if e := tx.InsertToken(store.Token{LaunchID: id, MemberID: m.ID, Hash: model.HashToken(token), CreatedAt: now}); e != nil {
			return e
		}
		if e := tx.CompareAndSwapLease(m.ID, m.Lease.Driver, m.Lease.Generation, next, now); e != nil {
			return e
		}
		if m.Session == nil && *m.Harness == model.HarnessClaude {
			session, e := uuid.NewRandom()
			if e != nil {
				return e
			}
			seal, e := model.NewSeal()
			if e != nil {
				return e
			}
			if e = l.r.session(tx, m, session.String(), seal, model.SessionSourceAssigned, id, false); e != nil {
				return e
			}
			newSession = true
		}
		agent := herdr.AgentName(m.Team, m.Name)
		if e := tx.UpdateMember(m.ID, store.MemberPatch{HerdrAgent: ptr(&agent), LastDriver: ptr(ptr(model.ModePane))}, now); e != nil {
			return e
		}
		if m.State != model.MemberStateStarting {
			if e := l.r.memberState(tx, m, model.MemberStateStarting, nil); e != nil {
				return e
			}
		}
		var e error
		starting, e = tx.GetMember(m.ID)
		copy := *t
		team = &copy
		return e
	})
	if e != nil {
		return e
	}
	launchStarted = true
	// The previous lease is durably ended now. Remove its secrets before any
	// fallible pane setup, so conversion failure cannot strand the old launch.
	if m.Lease.Launch != nil {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		e = l.r.removeLaunchFiles(cleanup, *m.Lease.Launch)
		done()
		if e != nil {
			return e
		}
	}
	desc := TurnLaunch{Mode: model.ModePane, Harness: *m.Harness, Profile: *m.Profile, Workdir: *m.Workdir, StateDir: l.i.b.opts.StateDir, LaunchID: id, MemberID: m.ID, Binary: binary, Session: starting.Session, NewSession: newSession}
	if e = statedir.Ensure(l.i.b.opts.StateDir); e != nil {
		return e
	}
	if e = statedir.WriteSecret(filepath.Join(l.i.b.opts.StateDir, "launch", id+".token"), []byte(token+"\n")); e != nil {
		return e
	}
	if e = l.i.adapter.Write(ctx, desc); e != nil {
		return e
	}
	if e = l.r.saveLaunch(desc); e != nil {
		return e
	}
	launch, e := l.i.adapter.launch(ctx, desc)
	if e != nil {
		return e
	}
	args, e := harness.Args(launch, model.ModePane)
	if launch.Harness == model.HarnessCodex {
		args = append(args, "--cd", launch.WorkDir)
	}
	if e != nil {
		return e
	}
	var env map[string]string
	var bootstrap []string
	if l.i.windowsPanes {
		env, bootstrap, e = l.i.prepareWindowsPane(launch, binary, args)
	} else {
		env, bootstrap, e = l.i.preparePaneShell(ctx, launch, binary)
	}
	if e != nil {
		return e
	}
	client := l.i.opts.Herdr(team.HerdrServer)
	tab, e := client.OpenTab(ctx, *team.HerdrWorkspace, m.Name, *m.Workdir, env)
	if e != nil {
		return e
	}
	success := false
	defer func() {
		if !success {
			_ = l.i.closeOwnedPane(context.WithoutCancel(ctx), team.HerdrServer, *team.HerdrWorkspace, tab.RootPane)
		}
	}()
	e = l.r.write(ctx, starting, func(tx *store.Tx, _ *store.Team) error {
		next := *starting.Lease
		next.Pane = &tab.RootPane
		if e := tx.CompareAndSwapLease(m.ID, next.Driver, next.Generation, next, l.i.b.now()); e != nil {
			return e
		}
		updated, e := tx.GetMember(m.ID)
		if e == nil {
			starting = updated
		}
		return e
	})
	if e != nil {
		return e
	}
	if e = client.RunCommand(ctx, tab.RootPane, bootstrap...); e != nil {
		return e
	}
	var agent herdr.Agent
	if l.i.windowsPanes {
		agent, e = l.i.detectWindowsPane(ctx, client, tab.RootPane, *team.HerdrWorkspace, herdr.AgentName(m.Team, m.Name), *m.Harness)
	} else {
		if e = l.i.waitShell(ctx, id); e != nil {
			return e
		}
		agent, e = client.StartAgent(ctx, herdr.StartOptions{Name: herdr.AgentName(m.Team, m.Name), Harness: *m.Harness, Pane: tab.RootPane, Args: args})
	}
	if e != nil {
		return e
	}
	if *m.Harness == model.HarnessClaude && agent.Activity != model.ActivityBlocked && starting.Session != nil {
		if e = l.i.b.rememberOrigin(*starting, *starting.Session); e != nil {
			return e
		}
	}
	e = l.r.write(ctx, starting, func(tx *store.Tx, _ *store.Team) error {
		if e := l.r.memberState(tx, *starting, model.MemberStateRunning, nil); e != nil {
			return e
		}
		if e := l.r.setActivity(tx, *starting, agent.Activity); e != nil {
			return e
		}
		updated, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		return l.i.b.event(tx, &m.Team, model.EventLeaseChanged, true, &m.ID, nil, wire.LeaseChangedEventData{Member: *updated, From: m.Lease.Driver, To: model.DriverPane, Generation: updated.Lease.Generation, Cause: cause})
	})
	if e != nil {
		return e
	}
	success = true
	return nil
}
func (l *integrationLifecycle) Exited(ctx context.Context, m model.Member) error {
	if m.Lease == nil || m.Lease.Driver != model.DriverPane {
		return nil
	}
	server, _ := paneIdentity(m)
	var e error
	if m.Lease.Pane != nil {
		_, e = l.i.opts.Herdr(server).Pane(ctx, *m.Lease.Pane)
	}
	reason := model.StopReasonExited
	cause := model.LeaseCausePaneExit
	if m.Lease.Pane == nil || herdr.IsGone(e) || herdr.IsUnavailable(e) {
		reason = model.StopReasonDied
		cause = model.LeaseCauseDied
	} else if e != nil {
		return l.r.paneRetry(ctx, m, e)
	}
	if m.Mode != nil && *m.Mode == model.ModeHeadless {
		if e := l.closePane(ctx, m); e != nil {
			return e
		}
		return l.toHeadless(ctx, m, model.LeaseCausePaneExit, false)
	}
	return l.stopMember(ctx, m, reason, cause)
}

// Herdr's idle state includes Codex's resume picker. Before the first delivery
// in each pane generation, inspect its fixed title, never its choices.
func (l *integrationLifecycle) checkPane(ctx context.Context, m model.Member) error {
	if m.Harness == nil || *m.Harness != model.HarnessCodex {
		return nil
	}
	p := l.i.panes
	key := paneKey(m)
	p.mu.Lock()
	generation, ok := p.checked[key]
	p.mu.Unlock()
	if ok && generation == m.Lease.Generation {
		return nil
	}
	server, name := paneIdentity(m)
	text, e := l.i.opts.Herdr(server).ReadAgent(ctx, name, 100)
	if e != nil {
		return e
	}
	if strings.Contains(strings.Join(strings.Fields(text), " "), "Working directory · resume") {
		if e = l.closePane(ctx, m); e != nil {
			return e
		}
		return l.r.launchFailed(ctx, m, model.NewError(model.ErrorLaunchFailed, "Codex resume picker").WithDetail("cause", "resume_picker"))
	}
	p.mu.Lock()
	p.checked[key] = m.Lease.Generation
	p.mu.Unlock()
	return nil
}
