package broker

import (
	"context"
	"errors"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (r *Runner) heartbeats(ctx context.Context) {
	tick := time.NewTicker(r.opts.HeartbeatInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if e := r.heartbeat(ctx); e != nil {
				if ctx.Err() == nil {
					r.fail(e)
				}
				return
			}
		}
	}
}
func (r *Runner) fail(e error) {
	select {
	case r.workerErrors <- e:
	default:
	}
}
func (r *Runner) worker(ctx context.Context, key string, fn func(context.Context) error) {
	r.mu.Lock()
	if r.busy[key] {
		r.mu.Unlock()
		return
	}
	r.busy[key] = true
	r.workers.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.workers.Done()
		defer func() {
			r.mu.Lock()
			delete(r.busy, key)
			r.mu.Unlock()
		}()
		if e := fn(ctx); e != nil && ctx.Err() == nil && !errors.Is(e, errRunnerGeneration) {
			r.fail(e)
		}
	}()
}
func (r *Runner) pass(ctx context.Context) (bool, error) {
	reapRunnerOrphans()
	var team *store.Team
	var members []model.Member
	var requests []model.Request
	e := r.b.db.Read(ctx, func(tx *store.Tx) error {
		var e error
		team, e = tx.GetTeam(r.team)
		if e != nil {
			return e
		}
		if !r.owns(team) && team.State != model.TeamStateStopping && team.State != model.TeamStateStopped {
			return errRunnerLost
		}
		members, e = tx.ListMembers(r.team, false)
		if e != nil {
			return e
		}
		requests, e = tx.ListOpenRequests(r.team)
		return e
	})
	if e != nil {
		return false, e
	}
	if team.State != model.TeamStateRunning {
		return true, nil
	}
	if e = r.checkOrphans(ctx, false); e != nil {
		return false, e
	}
	blocked := map[string]bool{}
	for _, req := range requests {
		if req.Member != nil {
			blocked[req.Member.ID] = true
		}
		if req.Status == model.RequestStatusPending {
			req := req
			r.worker(ctx, "request:"+req.ID, func(ctx context.Context) error { return r.executeRequest(ctx, req) })
		}
	}
	for _, m := range members {
		if m.Kind != model.MemberKindAgent || blocked[m.ID] {
			continue
		}
		if _, ok := r.orphans[m.ID]; ok {
			continue
		}
		m := m
		r.worker(ctx, m.ID, func(ctx context.Context) error { return r.workMember(ctx, m) })
	}
	return false, nil
}
func (r *Runner) executeRequest(ctx context.Context, req model.Request) error {
	e := r.write(ctx, nil, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning {
			return errRunnerGeneration
		}
		current, e := tx.GetRequest(req.ID)
		if e != nil {
			return e
		}
		if current.Status != model.RequestStatusPending {
			return errRunnerGeneration
		}
		if e = r.requestStatus(tx, *current, model.RequestStatusRunning, nil); e != nil {
			return e
		}
		updated, e := tx.GetRequest(req.ID)
		if e != nil {
			return e
		}
		req = *updated
		return nil
	})
	if e != nil {
		return e
	}
	failure := model.AsError(r.opts.Requests.Execute(ctx, req))
	if ctx.Err() != nil {
		return nil
	}
	return r.write(ctx, nil, func(tx *store.Tx, _ *store.Team) error {
		current, e := tx.GetRequest(req.ID)
		if e != nil {
			return e
		}
		if current.Status != model.RequestStatusRunning {
			return nil
		}
		status := model.RequestStatusSucceeded
		if failure != nil {
			status = model.RequestStatusFailed
		}
		return r.requestStatus(tx, *current, status, failure)
	})
}
func (r *Runner) workMember(ctx context.Context, m model.Member) error {
	held, e := r.syncHolds(ctx, m)
	if e != nil || held {
		return e
	}
	if m.Lease == nil {
		return nil
	}
	if m.Lease.Driver == model.DriverNone && m.Origin == model.MemberOriginSpec && (r.takeStart(m) || m.StopReason == nil) {
		if m.Mode != nil && *m.Mode == model.ModeHeadless {
			_, e = r.launchHeadless(ctx, m, model.LeaseCauseStart)
		} else {
			e = r.openPane(ctx, m, model.LeaseCauseStart)
		}
		if e != nil && model.AsError(e).Code == model.ErrorLaunchFailed {
			return nil
		}
		return e
	}
	if m.State == model.MemberStateStopped && m.Lease.Driver == model.DriverNone {
		return r.wakeMember(ctx, m)
	}
	if m.State == model.MemberStateRunning && m.Lease.Driver == model.DriverHeadless {
		return r.runTurn(ctx, m)
	}
	if m.State == model.MemberStateRunning && m.Lease.Driver == model.DriverPane {
		return r.runPane(ctx, m)
	}
	return nil
}
func (r *Runner) syncHolds(ctx context.Context, m model.Member) (bool, error) {
	active := false
	need := false
	e := r.b.db.Read(ctx, func(tx *store.Tx) error {
		h, e := tx.GetHold(m.ID)
		if e != nil && !missing(e) {
			return e
		}
		active = e == nil && h.ReleasedAt == nil && h.HeldUntil.After(r.b.now().Time)
		ds, e := tx.ListTeamDeliveries(r.team, model.DeliveryStateQueued, model.DeliveryStateHeld)
		if e != nil {
			return e
		}
		for _, d := range ds {
			if d.Delivery.To.ID != m.ID {
				continue
			}
			if active && d.Delivery.State == model.DeliveryStateQueued || !active && d.Delivery.State == model.DeliveryStateHeld && d.Delivery.Reason != nil && *d.Delivery.Reason == model.DeliveryReasonPersonControl {
				need = true
			}
		}
		return nil
	})
	if e != nil || !need {
		return active, e
	}
	e = r.write(ctx, &m, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning {
			return nil
		}
		h, e := tx.GetHold(m.ID)
		if e != nil && !missing(e) {
			return e
		}
		active = e == nil && h.ReleasedAt == nil && h.HeldUntil.After(r.b.now().Time)
		ds, e := tx.ListTeamDeliveries(r.team, model.DeliveryStateQueued, model.DeliveryStateHeld)
		if e != nil {
			return e
		}
		for _, d := range ds {
			if d.Delivery.To.ID != m.ID {
				continue
			}
			if active && d.Delivery.State == model.DeliveryStateQueued {
				if _, e = r.b.changeDelivery(tx, r.team, d.MessageID, d.Delivery, model.DeliveryStateHeld, ptr(model.DeliveryReasonPersonControl)); e != nil {
					return e
				}
			} else if !active && d.Delivery.State == model.DeliveryStateHeld && d.Delivery.Reason != nil && *d.Delivery.Reason == model.DeliveryReasonPersonControl {
				if _, e = r.b.changeDelivery(tx, r.team, d.MessageID, d.Delivery, model.DeliveryStateQueued, nil); e != nil {
					return e
				}
			}
		}
		if !active && h != nil && h.ReleasedAt == nil {
			return tx.ReleaseHold(m.ID, r.b.now())
		}
		return nil
	})
	return active, e
}
func (r *Runner) stop(ctx context.Context) error {
	r.mu.Lock()
	handles := make([]TurnHandle, 0, len(r.handles))
	for _, h := range r.handles {
		handles = append(handles, h)
	}
	r.mu.Unlock()
	for _, h := range handles {
		if e := h.Cancel(); e != nil {
			return e
		}
	}
	done := make(chan struct{})
	go func() { r.workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return model.NewError(model.ErrorTimeout, "runner workers did not stop")
	}
	if e := r.checkOrphans(ctx, true); e != nil {
		return e
	}
	var members []model.Member
	e := r.b.db.Read(ctx, func(tx *store.Tx) error { var e error; members, e = tx.ListMembers(r.team, true); return e })
	if e != nil {
		return e
	}
	for _, m := range members {
		if m.Lease == nil || m.Lease.Driver != model.DriverHeadless {
			continue
		}
		e = r.write(ctx, &m, func(tx *store.Tx, _ *store.Team) error {
			now := r.b.now()
			if m.Lease.Launch != nil {
				l, e := tx.GetLaunch(*m.Lease.Launch)
				if e != nil {
					return e
				}
				if l.EndedAt == nil {
					if e = tx.EndLaunch(l.ID, model.LaunchEndReasonStopped, now); e != nil {
						return e
					}
				}
				if _, e = tx.RevokeMemberTokens(m.ID, now); e != nil {
					return e
				}
			}
			next := model.Lease{Driver: model.DriverNone, Generation: m.Lease.Generation + 1}
			if e := tx.CompareAndSwapLease(m.ID, m.Lease.Driver, m.Lease.Generation, next, now); e != nil {
				return e
			}
			if e := tx.ReleaseHold(m.ID, now); e != nil && !missing(e) {
				return e
			}
			if e := tx.SetMemberActivity(m.ID, nil, now); e != nil {
				return e
			}
			if e := tx.SetMemberState(m.ID, model.MemberStateStopped, ptr(model.StopReasonTeamStopped), nil, ptr(&now), now); e != nil {
				return e
			}
			updated, e := tx.GetMember(m.ID)
			if e != nil {
				return e
			}
			if e = r.b.event(tx, &m.Team, model.EventLeaseChanged, true, &m.ID, nil, wire.LeaseChangedEventData{Member: *updated, From: m.Lease.Driver, To: next.Driver, Generation: next.Generation, Cause: model.LeaseCauseStop}); e != nil {
				return e
			}
			return r.b.event(tx, &m.Team, model.EventMemberStateChanged, true, &m.ID, nil, wire.MemberStateChangedEventData{Member: *updated, From: m.State, To: model.MemberStateStopped, Reason: updated.StopReason})
		})
		if e != nil && !errors.Is(e, errRunnerGeneration) {
			return e
		}
	}
	if e = r.cleanLaunchFiles(ctx); e != nil {
		return e
	}
	return r.write(ctx, nil, func(tx *store.Tx, t *store.Team) error {
		runner := t.Runner
		runner.State = model.RunnerStateDown
		if e := tx.Heartbeat(t.ID, runner, r.b.now()); e != nil {
			return e
		}
		s, e := r.b.status(tx, t)
		if e != nil {
			return e
		}
		s.Runner = runner
		if t.Runner.State == model.RunnerStateDown {
			return nil
		}
		return r.b.event(tx, &t.ID, model.EventRunnerStateChanged, true, nil, nil, wire.RunnerStateChangedEventData{Team: s, From: t.Runner.State, To: model.RunnerStateDown})
	})
}
