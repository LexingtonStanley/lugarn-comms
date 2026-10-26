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
		r.mu.Lock()
		r.paneObserved[m.ID] = runnerPanePoll{at: r.b.opts.Clock(), generation: m.Lease.Generation}
		r.mu.Unlock()
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
			if e = r.cancelTurnProcess(ctx, o.turn); e != nil {
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
		// A dead/reused root PID can still have descendants in its recorded job.
		// Only that job may be signalled here, never the replacement PID.
		if _, e = r.terminateTurnJob(ctx, o.turn); e != nil {
			return e
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
	root := filepath.Join(r.b.opts.StateDir, "launch")
	info, e := os.Lstat(root)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	var entries []os.DirEntry
	if e == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return model.NewError(model.ErrorInternal, "launch path is not a directory")
		}
		entries, e = os.ReadDir(root)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	// List before reading the store: any launch whose files were listed must
	// already have committed its row. Later files are left for a future pass.
	return r.cleanListedLaunchFiles(ctx, root, entries)
}

func (r *Runner) cleanListedLaunchFiles(ctx context.Context, root string, entries []os.DirEntry) error {
	known := map[string]string{}
	ended := map[string]bool{}
	e := r.b.db.Read(ctx, func(tx *store.Tx) error {
		launches, e := tx.ListLaunches("")
		if e != nil {
			return e
		}
		for _, launch := range launches {
			member, e := tx.GetMember(launch.MemberID)
			if e != nil {
				return e
			}
			known[launch.ID] = member.Team
			if member.Team == r.team && launch.EndedAt != nil {
				ended[launch.ID] = true
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	for _, entry := range entries {
		name := entry.Name()
		id, directory, ok := cleanupLaunchName(name)
		if !ok {
			continue
		}
		path := filepath.Join(root, name)
		info, e := os.Lstat(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		link := info.Mode()&os.ModeSymlink != 0
		if !link && (directory && !info.IsDir() || !directory && !info.Mode().IsRegular()) {
			continue
		}
		if known[id] != "" {
			if !ended[id] {
				continue
			}
		} else if r.b.opts.Clock().Sub(info.ModTime()) <= 60*time.Second {
			continue
		}
		// Delete only this listed entry. A broad Files.Delete(id) could remove
		// a fresh sibling file or follow a link through a driver adapter.
		if link || !directory {
			e = os.Remove(path)
		} else {
			e = os.RemoveAll(path)
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return r.cleanPointerFiles(ended)
}

func cleanupLaunchName(name string) (id string, directory, ok bool) {
	if len(name) < 26 {
		return "", false, false
	}
	id = name[:26]
	if model.ValidateULID(id) != nil {
		return "", false, false
	}
	switch name[26:] {
	case "":
		return id, true, true
	case ".token", ".json", ".settings.json", ".opencode.json":
		return id, false, true
	default:
		return "", false, false
	}
}

func (r *Runner) cleanPointerFiles(ended map[string]bool) error {
	if r.opts.HomeDir == "" {
		return nil
	}
	// These parents may be user-managed links. Do not traverse one to reach a
	// pointer outside the explicitly supplied home directory.
	root := r.opts.HomeDir
	for _, name := range []string{".lugarn", "conversations"} {
		root = filepath.Join(root, name)
		info, e := os.Lstat(root)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
	}
	for id := range ended {
		if model.ValidateULID(id) != nil {
			continue
		}
		path := filepath.Join(root, id)
		info, e := os.Lstat(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return nil
}
