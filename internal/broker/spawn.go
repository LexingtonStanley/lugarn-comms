package broker

import (
	"context"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (b *Broker) request(tx *store.Tx, t *store.Team, kind model.RequestKind, target, by model.Member, args map[string]any) (model.Request, error) {
	id, e := b.opts.ID()
	if e != nil {
		return model.Request{}, model.AsError(e)
	}
	r := model.Request{ID: id, Team: t.ID, Kind: kind, Member: ptr(ref(target)), RequestedBy: ref(by), Args: args, Status: model.RequestStatusPending, CreatedAt: b.now()}
	if e = tx.InsertRequest(store.RequestParams{Request: r, MemberID: &target.ID, RequestedByID: by.ID}); e != nil {
		return r, e
	}
	e = b.event(tx, &t.ID, model.EventRequestChanged, false, &target.ID, nil, wire.RequestChangedEventData{Request: r})
	return r, e
}
func (b *Broker) waitRequest(ctx context.Context, id string, timeout time.Duration) (*model.Request, error) {
	// The deadline uses wall time even with a deterministic domain clock. Counting
	// requested waits also lets tests advance the bounded wait without sleeping.
	deadline := time.Now().Add(timeout)
	elapsed := time.Duration(0)
	for {
		var r *model.Request
		e := b.db.Read(ctx, func(tx *store.Tx) error { var e error; r, e = tx.GetRequest(id); return e })
		if e != nil {
			return nil, e
		}
		switch r.Status {
		case model.RequestStatusSucceeded:
			return r, nil
		case model.RequestStatusFailed:
			if r.Error != nil {
				return r, r.Error
			}
			return r, model.NewError(model.ErrorInternal, "failed request has no error")
		}
		remaining := min(timeout-elapsed, time.Until(deadline))
		if remaining <= 0 {
			return r, model.NewError(model.ErrorTimeout, "request is still pending; it may complete later")
		}
		d := min(250*time.Millisecond, remaining)
		if e = b.opts.Wait(ctx, d); e != nil {
			return r, model.AsError(e)
		}
		elapsed += d
	}
}
func (b *Broker) ToolSpawn(ctx context.Context, c service.Caller, r wire.SpawnInput) (wire.SpawnOutput, error) {
	var out wire.SpawnOutput
	var refusal *model.Error
	var req model.Request
	var memberID string
	h := b.host(ctx)
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(c.TeamID)
		if e != nil {
			return e
		}
		m, e := b.member(tx, c.TeamID, c.MemberID)
		if e != nil {
			return e
		}
		live, e := tx.ListMembers(t.ID, false)
		if e != nil {
			return e
		}
		settings, e := tx.GetSpawnSettings(m.ID)
		if e != nil && !missing(e) {
			return e
		}
		cfg := policy.ResolveSpawn(spawnSpec(settings))
		count := store.SpawnCount{}
		if settings != nil {
			v, e := tx.GetSpawnCount(m.ID)
			if e != nil {
				return e
			}
			count = *v
		}
		d := policy.Spawn(policy.SpawnInput{Name: r.Name, Harness: r.Harness, Task: r.Task, Profile: r.Profile, Mode: r.Mode, TeamState: t.State, RunnerUp: b.fresh(t), Caller: *m, Settings: cfg, Live: live, Running: int(count.Running), SpawnedThisRun: int(count.ThisRun)}, h)
		if err := hostError(h); err != nil {
			return err
		}
		if d.Err != nil {
			refusal = d.Err
			return b.policyEvents(tx, t.ID, d.Events)
		}
		p := d.Plan
		id, e := b.opts.ID()
		if e != nil {
			return model.AsError(e)
		}
		memberID = id
		now := b.now()
		if e = tx.InsertMember(store.MemberParams{ID: id, TeamID: t.ID, Name: p.Name, Kind: model.MemberKindAgent, Origin: model.MemberOriginSpawn, Role: &p.Role, SpawnedBy: &m.ID, SpawnedRun: &t.Run, Harness: &p.Harness, Profile: &p.Profile, Mode: &p.Mode, Folder: &p.Folder, Worktree: p.Worktree, Brief: &p.Brief, State: model.MemberStateStarting, CreatedAt: &now, UpdatedAt: &now}); e != nil {
			return e
		}
		if e = tx.InsertLease(id, model.Lease{Driver: model.DriverNone}, now); e != nil {
			return e
		}
		edges, e := b.storeEdges(tx, t.ID, p.Edges)
		if e != nil {
			return e
		}
		if e = tx.AddGridEdges(t.ID, edges); e != nil {
			return e
		}
		worker, e := tx.GetMember(id)
		if e != nil {
			return e
		}
		if e = b.event(tx, &t.ID, model.EventMemberAdded, true, &id, nil, wire.MemberAddedEventData{Member: *worker}); e != nil {
			return e
		}
		task, _, pe, e := b.accept(tx, t, *m, worker, false, p.Task, nil, model.MessageKindTask)
		if e != nil {
			return e
		}
		if pe != nil {
			return pe
		}
		out.TaskMessage = task.ID
		if _, e = tx.BumpTeamRevision(t.ID, t.Revision, now); e != nil {
			return e
		}
		req, e = b.request(tx, t, model.RequestKindSpawn, *worker, *m, map[string]any{"worktree": p.Worktree})
		return e
	})
	if e != nil {
		return out, e
	}
	if refusal != nil {
		return out, refusal
	}
	_, e = b.waitRequest(ctx, req.ID, b.opts.RequestTimeout)
	if e != nil {
		pe := model.AsError(e)
		if pe.Code == model.ErrorTimeout {
			pe.WithDetail("member", memberID)
		}
		return out, pe
	}
	e = b.db.Read(ctx, func(tx *store.Tx) error {
		m, e := tx.GetMember(memberID)
		if e != nil {
			return e
		}
		out.Member = wire.SpawnOutputMember{ID: m.ID, Name: m.Name, Harness: *m.Harness, Profile: *m.Profile, Mode: *m.Mode, State: m.State, Workdir: *m.Workdir, Worktree: m.Worktree}
		return nil
	})
	return out, e
}
func (b *Broker) ToolStop(ctx context.Context, c service.Caller, r wire.StopInput) (wire.StopOutput, error) {
	var out wire.StopOutput
	var refusal *model.Error
	var req *model.Request
	var target string
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, m, e := b.caller(tx, c)
		if e != nil {
			return e
		}
		to, e := tx.GetLiveMember(t.ID, r.Member)
		if e != nil {
			return e
		}
		target = to.ID
		d := policy.Stop(*m, *to, b.fresh(t))
		if d.Err != nil {
			refusal = d.Err
			return b.policyEvents(tx, t.ID, d.Events)
		}
		if d.AlreadyStopped {
			out.Member = wire.StopOutputMember{ID: to.ID, Name: to.Name, State: to.State, StopReason: to.StopReason}
			return nil
		}
		v, e := b.request(tx, t, model.RequestKindStopMember, *to, *m, map[string]any{"reason": model.StopReasonSpawner})
		req = &v
		return e
	})
	if e != nil {
		return out, e
	}
	if refusal != nil {
		return out, refusal
	}
	if req == nil {
		return out, nil
	}
	if _, e = b.waitRequest(ctx, req.ID, min(30*time.Second, b.opts.RequestTimeout)); e != nil {
		return out, e
	}
	e = b.db.Read(ctx, func(tx *store.Tx) error {
		m, e := tx.GetMember(target)
		if e != nil {
			return e
		}
		out.Member = wire.StopOutputMember{ID: m.ID, Name: m.Name, State: m.State, StopReason: m.StopReason}
		return nil
	})
	return out, e
}
