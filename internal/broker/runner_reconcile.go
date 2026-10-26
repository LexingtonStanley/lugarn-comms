package broker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type runnerOrphan struct {
	turn   model.Turn
	member model.Member
	birth  string
}

func (r *Runner) reconcile(ctx context.Context) error {
	var panes []model.Member
	var interrupted []model.Member
	e := r.write(ctx, nil, func(tx *store.Tx, t *store.Team) error {
		ds, e := tx.ListTeamDeliveries(t.ID, model.DeliveryStateDelivering)
		if e != nil {
			return e
		}
		for _, d := range ds {
			if _, e = r.b.changeDelivery(tx, t.ID, d.MessageID, d.Delivery, model.DeliveryStateUncertain, ptr(model.DeliveryReasonRunnerRestart)); e != nil {
				return e
			}
		}
		ms, e := tx.ListMembers(t.ID, true)
		if e != nil {
			return e
		}
		for _, m := range ms {
			if r.freshStart && m.Origin == model.MemberOriginSpec && m.Lease != nil && m.Lease.Driver == model.DriverNone {
				r.startMembers[m.ID] = true
			}
			turn, e := tx.GetLiveTurn(m.ID)
			if e != nil && !missing(e) {
				return e
			}
			if e == nil {
				r.orphans[m.ID] = runnerOrphan{turn: *turn, member: m}
			}
			if missing(e) && m.State == model.MemberStateStarting && m.Lease != nil && m.Lease.Driver == model.DriverHeadless {
				interrupted = append(interrupted, m)
			}
			if m.Lease != nil && m.Lease.Driver == model.DriverPane {
				panes = append(panes, m)
			}
		}
		rs, e := tx.ListOpenRequests(t.ID)
		if e != nil {
			return e
		}
		for _, req := range rs {
			if req.Status == model.RequestStatusRunning {
				if e = r.requestStatus(tx, req, model.RequestStatusFailed, model.NewError(model.ErrorInternal, "runner restarted during request")); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	for _, m := range interrupted {
		e := r.launchFailed(ctx, m, errors.New("runner restarted during headless launch"))
		if e != nil && model.AsError(e).Code != model.ErrorLaunchFailed {
			return e
		}
	}
	for id, o := range r.orphans {
		o.birth = r.processBirth(o.turn)
		r.orphans[id] = o
	}
	if e = r.checkOrphans(ctx, false); e != nil {
		return e
	}
	for _, m := range panes {
		obs, e := r.opts.Panes.State(ctx, m)
		if e != nil {
			return e
		}
		if obs.Gone {
			if e = r.opts.PaneLifecycle.Exited(ctx, m); e != nil {
				return e
			}
		}
	}
	return r.cleanLaunchFiles(ctx)
}
func (r *Runner) requestStatus(tx *store.Tx, req model.Request, status model.RequestStatus, failure *model.Error) error {
	p := store.RequestPatch{Status: status}
	now := r.b.now()
	if status == model.RequestStatusRunning {
		p.StartedAt = ptr(&now)
	} else {
		p.FinishedAt = ptr(&now)
	}
	if status == model.RequestStatusFailed {
		p.Error = &failure
	}
	if e := tx.UpdateRequest(req.ID, req.Status, p); e != nil {
		return e
	}
	updated, e := tx.GetRequest(req.ID)
	if e != nil {
		return e
	}
	var member *string
	if req.Member != nil {
		member = &req.Member.ID
	}
	return r.b.event(tx, &req.Team, model.EventRequestChanged, false, member, nil, wire.RequestChangedEventData{Request: *updated})
}
func (r *Runner) checkOrphans(ctx context.Context, stopping bool) error {
	for id, o := range r.orphans {
		s := ProcessState{}
		var e error
		if o.turn.PID != nil {
			s, e = r.opts.Processes.Inspect(ctx, *o.turn.PID)
			if e != nil {
				return e
			}
		}
		if s.Alive && o.birth != "" && s.Birth != o.birth {
			s.Alive = false
		} // PID was reused.
		timeout := r.opts.TurnTimeout
		if timeout <= 0 {
			var team *store.Team
			e = r.b.db.Read(ctx, func(tx *store.Tx) error { var e error; team, e = tx.GetTeam(r.team); return e })
			if e != nil {
				return e
			}
			timeout = time.Duration(team.TurnTimeoutSeconds) * time.Second
		}
		if s.Alive && (stopping || r.b.now().Sub(o.turn.StartedAt.Time) >= timeout) {
			if o.birth == "" {
				return model.NewError(model.ErrorInternal, "cannot safely cancel an orphan without its process birth identity")
			}
			if e = r.opts.Processes.Cancel(ctx, *o.turn.PID, o.birth); e != nil {
				return e
			}
			s, e = r.opts.Processes.Inspect(ctx, *o.turn.PID)
			if e != nil {
				return e
			}
		}
		if s.Alive {
			continue
		}
		e = r.write(ctx, &o.member, func(tx *store.Tx, t *store.Team) error {
			current, e := tx.GetTurn(o.turn.ID)
			if e != nil {
				return e
			}
			if current.Status != model.TurnStatusStarting && current.Status != model.TurnStatusRunning {
				return nil
			}
			now := r.b.now()
			if e = tx.UpdateTurn(current.ID, nil, store.TurnPatch{Status: ptr(model.TurnStatusLost), EndedAt: ptr(&now)}); e != nil {
				return e
			}
			updated, e := tx.GetTurn(current.ID)
			if e != nil {
				return e
			}
			if e = r.setActivity(tx, o.member, model.ActivityIdle); e != nil {
				return e
			}
			return r.b.event(tx, &t.ID, model.EventTurnFinished, true, &id, nil, wire.TurnFinishedEventData{Turn: *updated})
		})
		if e != nil && !errors.Is(e, errRunnerGeneration) {
			return e
		}
		delete(r.orphans, id)
	}
	return nil
}
func (r *Runner) setActivity(tx *store.Tx, m model.Member, a model.Activity) error {
	current, e := tx.GetMember(m.ID)
	if e != nil {
		return e
	}
	if current.Activity != nil && *current.Activity == a {
		return nil
	}
	if e = tx.SetMemberActivity(m.ID, &a, r.b.now()); e != nil {
		return e
	}
	return r.b.event(tx, &m.Team, model.EventMemberActivityChanged, false, &m.ID, nil, wire.MemberActivityChangedEventData{Member: ref(m), From: current.Activity, To: &a})
}
func (r *Runner) cleanLaunchFiles(ctx context.Context) error {
	known := map[string]string{}
	ended := map[string]bool{}
	e := r.b.db.Read(ctx, func(tx *store.Tx) error {
		ls, e := tx.ListLaunches("")
		if e != nil {
			return e
		}
		for _, l := range ls {
			m, e := tx.GetMember(l.MemberID)
			if e != nil {
				return e
			}
			known[l.ID] = m.Team
			if m.Team == r.team && l.EndedAt != nil {
				ended[l.ID] = true
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	root := filepath.Join(r.b.opts.StateDir, "launch")
	entries, e := os.ReadDir(root)
	if os.IsNotExist(e) {
		e = nil
	}
	if e != nil {
		return e
	}
	for _, entry := range entries {
		n := entry.Name()
		if len(n) < 26 {
			continue
		}
		id := n[:26]
		if model.ValidateULID(id) != nil {
			continue
		}
		if known[id] != "" && !ended[id] {
			continue
		}
		if e = r.opts.Files.Delete(ctx, id); e != nil {
			return e
		}
		if e = os.RemoveAll(filepath.Join(root, n)); e != nil {
			return e
		}
	}
	if r.opts.HomeDir != "" {
		for id := range ended {
			for _, suffix := range []string{"", ".json"} {
				e = os.Remove(filepath.Join(r.opts.HomeDir, ".lugarn", "conversations", id+suffix))
				if e != nil && !os.IsNotExist(e) {
					return e
				}
			}
		}
	}
	return nil
}
