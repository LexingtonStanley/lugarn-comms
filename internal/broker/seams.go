package broker

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

var _ service.App = (*Broker)(nil)
var _ service.Tools = (*Broker)(nil)
var _ service.Authenticator = (*Broker)(nil)

// RunnerLauncher opens the runner and waits for its first heartbeat. Store-side
// start checks and the starting transition commit before Launch is called.
type RunnerLauncher interface {
	Launch(context.Context, store.Team) error
}

// TeamTeardown waits for runner shutdown, kills surviving turn processes and
// closes member/runner panes. The broker performs token and store cleanup.
type TeamTeardown interface {
	Close(context.Context, store.Team, []model.Member) error
}

// UnavailableRunner is C1's clean failure at the process-launch boundary.
type UnavailableRunner struct{}

func (UnavailableRunner) Launch(context.Context, store.Team) error {
	return model.NewError(model.ErrorRunnerUnavailable, "runner launch is not built until C2").WithDetail("reason", "not_built")
}

// NoopTeardown is C1's pane/process implementation; C1 launches no processes.
type NoopTeardown struct{}

func (NoopTeardown) Close(context.Context, store.Team, []model.Member) error { return nil }

func (b *Broker) StartHerdr(ctx context.Context, _ wire.StartHerdrRequest) (wire.StartHerdrResult, error) {
	if e := b.contact(ctx, nil); e != nil {
		return wire.StartHerdrResult{}, e
	}
	return wire.StartHerdrResult{}, model.NewError(model.ErrorHerdrUnavailable, "the broker cannot start Herdr until C2").WithDetail("reason", "not_running")
}
func (b *Broker) StartTeam(ctx context.Context, r wire.StartTeamRequest) (wire.StartTeamResult, error) {
	var out wire.StartTeamResult
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	var started store.Team
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		if t.State == model.TeamStateRunning {
			out.AlreadyRunning = true
			out.Team, e = b.status(tx, t)
			return e
		}
		if t.State != model.TeamStateStopped {
			return model.NewError(model.ErrorLeaseConflict, "team is already starting or stopping")
		}
		herdr, e := b.opts.Probe.Herdr(ctx, t.HerdrServer)
		if e != nil || !herdr.Running {
			reason := "not_running"
			if !herdr.Installed {
				reason = "not_installed"
			}
			return model.NewError(model.ErrorHerdrUnavailable, "the team's Herdr server is unavailable").WithDetail("reason", reason)
		}
		s, e := b.budget(tx, t, true)
		if e != nil {
			return e
		}
		reset := policy.ResetForStart(s)
		if e = b.writeBudget(tx, t.ID, reset); e != nil {
			return e
		}
		if e = b.policyEvents(tx, t.ID, reset.Events); e != nil {
			return e
		}
		if e = b.budgetDeliveryEvents(tx, t.ID, reset); e != nil {
			return e
		}
		now := b.now()
		if t.Run, e = tx.BumpTeamRun(t.ID, now); e != nil {
			return e
		}
		if e = tx.UpdateTeam(t.ID, store.TeamPatch{State: ptr(model.TeamStateStarting), StartedAt: ptr(&now), StoppedAt: ptr((*model.Time)(nil))}, now); e != nil {
			return e
		}
		if r.Resume == nil || !*r.Resume {
			ms, e := tx.ListMembers(t.ID, false)
			if e != nil {
				return e
			}
			for _, m := range ms {
				if m.Kind == model.MemberKindHuman {
					continue
				}
				if e = tx.SetMemberSession(m.ID, nil, now); e != nil {
					return e
				}
				if m.Origin == model.MemberOriginSpawn && m.State == model.MemberStateStopped {
					if e = tx.RemoveMember(m.ID, model.RemovedReasonNewRun, now); e != nil {
						return e
					}
					v, e := tx.GetMember(m.ID)
					if e != nil {
						return e
					}
					if e = b.event(tx, &t.ID, model.EventMemberRemoved, false, &m.ID, nil, wire.MemberRemovedEventData{Member: *v}); e != nil {
						return e
					}
				}
			}
			// Retired workers' edges leave the current grid too.
			edges, e := b.grid(tx, t.ID)
			if e != nil {
				return e
			}
			live, e := tx.ListMembers(t.ID, false)
			if e != nil {
				return e
			}
			names := map[string]bool{}
			for _, m := range live {
				names[m.Name] = true
			}
			kept := []model.GridEdge{}
			for _, edge := range edges {
				if names[edge.From] && names[edge.To] {
					kept = append(kept, edge)
				}
			}
			rows, e := b.storeEdges(tx, t.ID, kept)
			if e != nil {
				return e
			}
			if e = tx.ReplaceGrid(t.ID, rows); e != nil {
				return e
			}
			human, e := tx.GetLiveMember(t.ID, "human")
			if e != nil {
				return e
			}
			for _, m := range live {
				if m.Kind != model.MemberKindAgent || m.Origin != model.MemberOriginSpec {
					continue
				}
				stored, e := tx.GetStoredMember(m.ID)
				if e != nil {
					return e
				}
				if stored.Task != nil {
					_, _, pe, e := b.accept(tx, t, *human, &m, false, *stored.Task, nil, model.MessageKindTask)
					if e != nil {
						return e
					}
					if pe != nil {
						return pe
					}
				}
			}
		}
		t, e = tx.GetTeam(t.ID)
		if e != nil {
			return e
		}
		started = *t
		out.Team, e = b.status(tx, t)
		if e != nil {
			return e
		}
		return b.event(tx, &t.ID, model.EventTeamStateChanged, true, nil, nil, wire.TeamStateChangedEventData{Team: out.Team, From: model.TeamStateStopped, To: model.TeamStateStarting})
	})
	if e != nil || out.AlreadyRunning {
		return out, e
	}
	if e = b.opts.Launcher.Launch(ctx, started); e != nil {
		launchErr := model.AsError(e)
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, cleanup := b.StopTeam(cleanupCtx, wire.StopTeamRequest{Team: r.Team}); cleanup != nil {
			return out, model.NewError(model.ErrorInternal, "runner launch and cleanup failed").WithDetails(map[string]any{"launch": launchErr, "cleanup": model.AsError(cleanup)})
		}
		return out, launchErr
	}
	e = b.db.Read(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		out.Team, e = b.status(tx, t)
		return e
	})
	return out, e
}
func (b *Broker) StopTeam(ctx context.Context, r wire.StopTeamRequest) (wire.StopTeamResult, error) {
	var out wire.StopTeamResult
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	var team store.Team
	var members []model.Member
	already := false
	stopFrom := model.TeamStateStopping
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		team = *t
		if t.State == model.TeamStateStopped {
			already = true
			out.Team, e = b.status(tx, t)
			return e
		}
		members, e = tx.ListMembers(t.ID, true)
		if e != nil {
			return e
		}
		from := t.State
		if from == model.TeamStateStarting {
			// A failed start has no public stopping state.
			stopFrom = model.TeamStateStarting
		}
		if e = tx.UpdateTeam(t.ID, store.TeamPatch{State: ptr(model.TeamStateStopping)}, b.now()); e != nil {
			return e
		}
		t.State = model.TeamStateStopping
		s, e := b.status(tx, t)
		if e != nil {
			return e
		}
		if from == model.TeamStateStarting || from == model.TeamStateStopping {
			return nil
		}
		return b.event(tx, &t.ID, model.EventTeamStateChanged, true, nil, nil, wire.TeamStateChangedEventData{Team: s, From: from, To: model.TeamStateStopping})
	})
	if e != nil || already {
		return out, e
	}
	externalErr := b.opts.Teardown.Close(ctx, team, members)
	var ended []string
	e = b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		if t.State == model.TeamStateStopped {
			out.Team, e = b.status(tx, t)
			return e
		}
		now := b.now()
		launches, e := tx.ListLaunches(t.ID)
		if e != nil {
			return e
		}
		for _, l := range launches {
			if l.EndedAt == nil {
				if e = tx.EndLaunch(l.ID, model.LaunchEndReasonStopped, now); e != nil {
					return e
				}
			}
			ended = append(ended, l.ID)
		}
		ms, e := tx.ListMembers(t.ID, true)
		if e != nil {
			return e
		}
		for _, m := range ms {
			if m.Kind == model.MemberKindHuman {
				continue
			}
			if _, e = tx.RevokeMemberTokens(m.ID, now); e != nil {
				return e
			}
			if e = tx.ReleaseHold(m.ID, now); e != nil && !missing(e) {
				return e
			}
			if turn, e := tx.GetLiveTurn(m.ID); e == nil {
				if e = tx.UpdateTurn(turn.ID, nil, store.TurnPatch{Status: ptr(model.TurnStatusCancelled), EndedAt: ptr(&now)}); e != nil {
					return e
				}
				v, e := tx.GetTurn(turn.ID)
				if e != nil {
					return e
				}
				if e = b.event(tx, &t.ID, model.EventTurnFinished, true, &m.ID, nil, wire.TurnFinishedEventData{Turn: *v}); e != nil {
					return e
				}
			} else if !missing(e) {
				return e
			}
			if m.Lease != nil && m.Lease.Driver != model.DriverNone {
				next := model.Lease{Driver: model.DriverNone, Generation: m.Lease.Generation + 1}
				if e = tx.CompareAndSwapLease(m.ID, m.Lease.Driver, m.Lease.Generation, next, now); e != nil {
					return e
				}
				v, e := tx.GetMember(m.ID)
				if e != nil {
					return e
				}
				if e = b.event(tx, &t.ID, model.EventLeaseChanged, true, &m.ID, nil, wire.LeaseChangedEventData{Member: *v, From: m.Lease.Driver, To: model.DriverNone, Generation: next.Generation, Cause: model.LeaseCauseStop}); e != nil {
					return e
				}
			}
			if e = tx.SetMemberActivity(m.ID, nil, now); e != nil {
				return e
			}
			if m.State != model.MemberStateStopped {
				if e = tx.SetMemberState(m.ID, model.MemberStateStopped, ptr(model.StopReasonTeamStopped), nil, ptr(&now), now); e != nil {
					return e
				}
				v, e := tx.GetMember(m.ID)
				if e != nil {
					return e
				}
				if e = b.event(tx, &t.ID, model.EventMemberStateChanged, true, &m.ID, nil, wire.MemberStateChangedEventData{Member: *v, From: m.State, To: model.MemberStateStopped, Reason: ptr(model.StopReasonTeamStopped)}); e != nil {
					return e
				}
			}
		}
		ds, e := tx.ListTeamDeliveries(t.ID, model.DeliveryStateQueued, model.DeliveryStateHeld)
		if e != nil {
			return e
		}
		for _, d := range ds {
			if _, e = b.changeDelivery(tx, t.ID, d.MessageID, d.Delivery, model.DeliveryStateExpired, ptr(model.DeliveryReasonTeamStopped)); e != nil {
				return e
			}
			out.Expired++
		}
		rs, e := tx.ListOpenRequests(t.ID)
		if e != nil {
			return e
		}
		for _, request := range rs {
			failure := model.NewError(model.ErrorTeamNotRunning, "the team stopped")
			if e = tx.UpdateRequest(request.ID, request.Status, store.RequestPatch{Status: model.RequestStatusFailed, Error: &failure, FinishedAt: ptr(&now)}); e != nil {
				return e
			}
			v, e := tx.GetRequest(request.ID)
			if e != nil {
				return e
			}
			var member *string
			if v.Member != nil {
				member = &v.Member.ID
			}
			if e = b.event(tx, &t.ID, model.EventRequestChanged, false, member, nil, wire.RequestChangedEventData{Request: *v}); e != nil {
				return e
			}
		}
		oldRunner := t.Runner.State
		t.Runner.State = model.RunnerStateDown
		t.Runner.PID = nil
		t.Runner.Pane = nil
		if e = tx.Heartbeat(t.ID, t.Runner, now); e != nil {
			return e
		}
		if e = tx.UpdateTeam(t.ID, store.TeamPatch{State: ptr(model.TeamStateStopped), StoppedAt: ptr(&now)}, now); e != nil {
			return e
		}
		t, e = tx.GetTeam(t.ID)
		if e != nil {
			return e
		}
		out.Team, e = b.status(tx, t)
		if e != nil {
			return e
		}
		if oldRunner != model.RunnerStateDown {
			if e = b.event(tx, &t.ID, model.EventRunnerStateChanged, true, nil, nil, wire.RunnerStateChangedEventData{Team: out.Team, From: oldRunner, To: model.RunnerStateDown}); e != nil {
				return e
			}
		}
		return b.event(tx, &t.ID, model.EventTeamStateChanged, true, nil, nil, wire.TeamStateChangedEventData{Team: out.Team, From: stopFrom, To: model.TeamStateStopped})
	})
	if e != nil {
		return out, e
	}
	if b.opts.StateDir != "" {
		for _, id := range ended {
			if model.ValidateULID(id) != nil {
				return out, model.NewError(model.ErrorInternal, "invalid stored launch id")
			}
			for _, suffix := range []string{".token", ".json", ".opencode.json", ""} {
				if e = os.RemoveAll(filepath.Join(b.opts.StateDir, "launch", id+suffix)); e != nil {
					return out, model.AsError(e)
				}
			}
		}
	}
	if externalErr != nil {
		return out, model.AsError(externalErr)
	}
	return out, nil
}
func (b *Broker) appRequest(ctx context.Context, team, id string, kind model.RequestKind, generation *int64, args map[string]any) (wire.RequestResult, error) {
	var out wire.RequestResult
	if e := b.contact(ctx, &team); e != nil {
		return out, e
	}
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(team)
		if e != nil {
			return e
		}
		if e = running(t); e != nil {
			return e
		}
		m, e := b.member(tx, team, id)
		if e != nil {
			return e
		}
		if m.Kind == model.MemberKindHuman {
			return invalid("human has no driver")
		}
		if !b.fresh(t) {
			return model.NewError(model.ErrorRunnerUnavailable, "the team's runner isn't up")
		}
		if generation != nil && (m.Lease == nil || m.Lease.Generation != *generation) {
			return model.NewError(model.ErrorLeaseConflict, "lease generation changed")
		}
		if kind == model.RequestKindOpenTerminal && m.Lease != nil && m.Lease.Driver == model.DriverPane {
			return model.NewError(model.ErrorLeaseConflict, "member already has a pane")
		}
		if kind == model.RequestKindReturnHeadless && (m.Lease == nil || m.Lease.Driver != model.DriverPane) {
			return model.NewError(model.ErrorLeaseConflict, "member has no pane to return from")
		}
		human, e := tx.GetLiveMember(team, "human")
		if e != nil {
			return e
		}
		out.Request, e = b.request(tx, t, kind, *m, *human, args)
		return e
	})
	return out, e
}
func (b *Broker) OpenTerminal(ctx context.Context, r wire.OpenTerminalRequest) (wire.RequestResult, error) {
	return b.appRequest(ctx, r.Team, r.Member, model.RequestKindOpenTerminal, r.IfGeneration, map[string]any{"cancel_turn": r.CancelTurn != nil && *r.CancelTurn})
}
func (b *Broker) ReturnHeadless(ctx context.Context, r wire.ReturnHeadlessRequest) (wire.RequestResult, error) {
	return b.appRequest(ctx, r.Team, r.Member, model.RequestKindReturnHeadless, r.IfGeneration, map[string]any{})
}
func (b *Broker) StopMember(ctx context.Context, r wire.StopMemberRequest) (wire.RequestResult, error) {
	return b.appRequest(ctx, r.Team, r.Member, model.RequestKindStopMember, nil, map[string]any{"reason": model.StopReasonPerson})
}
