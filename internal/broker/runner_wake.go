package broker

import (
	"context"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func (r *Runner) mayWake(tx *store.Tx, m model.Member) (bool, error) {
	current, e := tx.GetMember(m.ID)
	if e != nil {
		return false, e
	}
	m = *current
	deliveries, e := tx.NextDeliveries(m.ID, 0)
	if e != nil {
		return false, e
	}
	agentWake := false
	for _, item := range deliveries {
		if !item.Delivery.Wake {
			continue
		}
		message, e := tx.GetMessage(item.MessageID)
		if e != nil {
			return false, e
		}
		// A launch failure consumes the old wake trigger. A new message may
		// try again; the person's resend requeues an old delivery with a new
		// updated_at. Open-terminal is an explicit request and bypasses waking.
		if m.StopReason != nil && *m.StopReason == model.StopReasonLaunchFailed && m.StoppedAt != nil &&
			!message.CreatedAt.After(m.StoppedAt.Time) && !item.Delivery.UpdatedAt.After(m.StoppedAt.Time) {
			continue
		}
		from, e := tx.GetMember(message.From.ID)
		if e != nil {
			return false, e
		}
		if from.Kind == model.MemberKindHuman {
			return true, nil
		}
		agentWake = true
	}
	if !agentWake {
		return false, nil
	}
	if m.SpawnedBy == nil {
		return true, nil
	}
	settings, e := tx.GetSpawnSettings(m.SpawnedBy.ID)
	if e != nil && !missing(e) {
		return false, e
	}
	limit := int64(model.DefaultSpawnRunningAtOnce)
	if settings != nil {
		limit = settings.RunningAtOnce
	}
	members, e := tx.ListMembers(m.Team, true)
	if e != nil {
		return false, e
	}
	var running int64
	for _, v := range members {
		if v.SpawnedBy != nil && v.SpawnedBy.ID == m.SpawnedBy.ID && v.State != model.MemberStateStopped {
			running++
		}
	}
	return running < limit, nil
}
func (r *Runner) wakeMember(ctx context.Context, m model.Member) error {
	var allowed bool
	e := r.b.db.Read(ctx, func(tx *store.Tx) error { var e error; allowed, e = r.mayWake(tx, m); return e })
	if e != nil || !allowed {
		return e
	}
	mode := m.Mode
	if m.LastDriver != nil {
		mode = m.LastDriver
	}
	if mode != nil && *mode == model.ModeHeadless {
		_, e = r.launchHeadless(ctx, m, model.LeaseCauseWake)
	} else {
		e = r.openPane(ctx, m, model.LeaseCauseWake)
	}
	if e != nil && model.AsError(e).Code == model.ErrorLaunchFailed {
		return nil
	}
	return e
}
func (r *Runner) openPane(ctx context.Context, m model.Member, cause model.LeaseCause) error {
	var starting *model.Member
	e := r.write(ctx, &m, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning {
			return errRunnerGeneration
		}
		if cause == model.LeaseCauseWake {
			allowed, e := r.mayWake(tx, m)
			if e != nil {
				return e
			}
			if !allowed {
				return nil
			}
		}
		if e := r.memberState(tx, m, model.MemberStateStarting, nil); e != nil {
			return e
		}
		var e error
		starting, e = tx.GetMember(m.ID)
		return e
	})
	if e != nil || starting == nil {
		return e
	}
	if e = r.opts.PaneLifecycle.Open(ctx, *starting, cause); e != nil {
		return r.launchFailed(ctx, *starting, e)
	}
	return nil
}
func (r *Runner) takeStart(m model.Member) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.startMembers[m.ID] {
		delete(r.startMembers, m.ID)
		return true
	}
	return false
}
