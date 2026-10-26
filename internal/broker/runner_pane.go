package broker

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (r *Runner) paneHolds(ctx context.Context, m model.Member, reason *model.DeliveryReason) error {
	return r.write(ctx, &m, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning {
			return errRunnerGeneration
		}
		if h, e := tx.GetHold(m.ID); e == nil && reason != nil && h.ReleasedAt == nil && h.HeldUntil.After(r.b.now().Time) {
			return nil
		} else if e != nil && !missing(e) {
			return e
		}
		ds, e := tx.ListTeamDeliveries(r.team, model.DeliveryStateQueued, model.DeliveryStateHeld)
		if e != nil {
			return e
		}
		for _, item := range ds {
			d := item.Delivery
			if d.To.ID != m.ID {
				continue
			}
			if reason != nil && d.State == model.DeliveryStateQueued {
				if _, e = r.b.changeDelivery(tx, r.team, item.MessageID, d, model.DeliveryStateHeld, reason); e != nil {
					return e
				}
			}
			if reason == nil && d.State == model.DeliveryStateHeld && d.Reason != nil && (*d.Reason == model.DeliveryReasonRecipientBlocked || *d.Reason == model.DeliveryReasonRecipientUnknown) {
				if _, e = r.b.changeDelivery(tx, r.team, item.MessageID, d, model.DeliveryStateQueued, nil); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func (r *Runner) pointerSession(m model.Member) (string, error) {
	if r.opts.HomeDir == "" || m.Harness == nil || *m.Harness != model.HarnessClaude || m.Lease == nil || m.Lease.Launch == nil {
		return "", nil
	}
	path := filepath.Join(r.opts.HomeDir, ".lugarn", "conversations", *m.Lease.Launch)
	info, e := os.Lstat(path)
	if os.IsNotExist(e) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() {
		return "", nil
	}
	return harness.ReadPointer(r.opts.HomeDir, *m.Lease.Launch)
}

type runnerPanePoll struct {
	at         time.Time
	generation int64
}

// Pending work needs prompt observation. Otherwise keep idle activity/gone
// detection current without starting a Herdr process on every store poll.
func (r *Runner) observePane(ctx context.Context, m model.Member) (observe, work bool, err error) {
	err = r.b.db.Read(ctx, func(tx *store.Tx) error {
		queued, e := tx.NextDeliveries(m.ID, 1)
		if e != nil {
			return e
		}
		if len(queued) > 0 {
			work = true
			return nil
		}
		held, e := tx.ListTeamDeliveries(r.team, model.DeliveryStateHeld)
		if e != nil {
			return e
		}
		for _, item := range held {
			d := item.Delivery
			if d.To.ID == m.ID && d.Reason != nil && (*d.Reason == model.DeliveryReasonRecipientBlocked || *d.Reason == model.DeliveryReasonRecipientUnknown) {
				work = true
				break
			}
		}
		return nil
	})
	if err != nil {
		return false, false, err
	}
	now := r.b.opts.Clock()
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, ok := r.paneObserved[m.ID]
	if !work && ok && previous.generation == m.Lease.Generation && now.Sub(previous.at) < 5*time.Second {
		return false, false, nil
	}
	r.paneObserved[m.ID] = runnerPanePoll{at: now, generation: m.Lease.Generation}
	return true, work, nil
}

func (r *Runner) runPane(ctx context.Context, m model.Member) error {
	observe, work, e := r.observePane(ctx, m)
	if e != nil || !observe {
		return e
	}
	observation, e := r.opts.Panes.State(ctx, m)
	if e != nil {
		return r.paneRetry(ctx, m, e)
	}
	if observation.Gone {
		return r.opts.PaneLifecycle.Exited(ctx, m)
	}
	if work && observation.Activity == model.ActivityWorking {
		observation, e = r.opts.Panes.WaitSettled(ctx, m, r.opts.PaneTimeout)
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return r.paneRetry(ctx, m, e)
		}
		if observation.Gone {
			return r.opts.PaneLifecycle.Exited(ctx, m)
		}
		if observation.Activity == model.ActivityWorking {
			return nil
		}
	}
	if !observation.Activity.Valid() {
		observation.Activity = model.ActivityUnknown
	}
	if e = r.write(ctx, &m, func(tx *store.Tx, _ *store.Team) error { return r.setActivity(tx, m, observation.Activity) }); e != nil {
		return e
	}
	if !work {
		return nil
	}
	switch observation.Activity {
	case model.ActivityBlocked:
		return r.paneHolds(ctx, m, ptr(model.DeliveryReasonRecipientBlocked))
	case model.ActivityIdle, model.ActivityDone:
	default:
		return r.paneHolds(ctx, m, ptr(model.DeliveryReasonRecipientUnknown))
	}
	if r.checkPane != nil {
		if e = r.checkPane(ctx, m); e != nil {
			if model.AsError(e).Code == model.ErrorLaunchFailed {
				return nil
			}
			return r.paneRetry(ctx, m, e)
		}
	}
	if e = r.paneHolds(ctx, m, nil); e != nil {
		return e
	}
	pointer, e := r.pointerSession(m)
	if e != nil {
		return e
	}
	var items []store.DeliveryItem
	var prompt, seal string
	var current model.Member
	e = r.write(ctx, &m, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning {
			return errRunnerGeneration
		}
		v, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		current = *v
		if h, e := tx.GetHold(m.ID); e == nil && h.ReleasedAt == nil && h.HeldUntil.After(r.b.now().Time) {
			return nil
		} else if e != nil && !missing(e) {
			return e
		}
		requests, e := tx.ListOpenRequests(t.ID)
		if e != nil {
			return e
		}
		for _, req := range requests {
			if req.Member != nil && req.Member.ID == m.ID {
				return nil
			}
		}
		inflight, e := tx.ListTeamDeliveries(t.ID, model.DeliveryStateDelivering)
		if e != nil {
			return e
		}
		for _, d := range inflight {
			if d.Delivery.To.ID == m.ID {
				return nil
			}
		}
		queued, e := tx.NextDeliveries(m.ID, 0)
		if e != nil || len(queued) == 0 {
			return e
		}
		if pointer != "" && (current.Session == nil || pointer != *current.Session) {
			seal, e = model.NewSeal()
			if e != nil {
				return e
			}
			if e = r.session(tx, current, pointer, seal, model.SessionSourcePointer, *m.Lease.Launch, false); e != nil {
				return e
			}
			v, e = tx.GetMember(m.ID)
			if e != nil {
				return e
			}
			current = *v
		}
		if current.Session == nil {
			return invalid("pane delivery needs a known session; C2d must complete session-before-a-pane")
		}
		session, e := tx.GetMemberSession(m.ID, *current.Session)
		if e != nil {
			return e
		}
		seal = session.Seal
		prompt, e = r.prompt(tx, t, current, queued, seal)
		if e != nil {
			return e
		}
		items, e = tx.PickUpDeliveries(m.ID, 0, ptr(model.DeliveryViaPane), nil, &m.Lease.Generation, r.b.now())
		if e != nil {
			return e
		}
		for _, item := range items {
			if e = r.b.event(tx, &t.ID, model.EventDeliveryChanged, false, &m.ID, &item.MessageID, wire.DeliveryChangedEventData{Message: item.MessageID, From: model.DeliveryStateQueued, Delivery: item.Delivery}); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil || len(items) == 0 {
		return e
	}
	if !policy.FitsInline(prompt) {
		path := filepath.Join(r.b.opts.StateDir, "launch", *m.Lease.Launch, policy.OverflowFileName(items[0].MessageID, items[0].Delivery.Attempts))
		if e = os.MkdirAll(filepath.Dir(path), 0700); e == nil {
			e = statedir.WriteSecret(path, []byte(prompt))
		}
		if e != nil {
			return r.finishPane(ctx, current, items, model.DeliveryStateHeld, ptr(model.DeliveryReasonDeliveryFailed))
		}
		prompt = policy.OverflowPrompt(len(items), path, seal)
	}
	if e = r.opts.Panes.Submit(ctx, current, prompt); e != nil {
		if ctx.Err() != nil {
			return r.cancelledPane(ctx, current, items, false)
		}
		return r.finishPane(ctx, current, items, model.DeliveryStateHeld, ptr(model.DeliveryReasonDeliveryFailed))
	}
	activity, e := r.opts.Panes.WaitActivity(ctx, current, r.opts.PaneTimeout)
	if ctx.Err() != nil {
		return r.cancelledPane(ctx, current, items, activity && e == nil)
	}
	if e != nil || !activity {
		return r.finishPane(ctx, current, items, model.DeliveryStateUncertain, ptr(model.DeliveryReasonNoActivity))
	}
	return r.finishPane(ctx, current, items, model.DeliveryStateDelivered, nil)
}
func (r *Runner) finishPane(ctx context.Context, m model.Member, items []store.DeliveryItem, state model.DeliveryState, reason *model.DeliveryReason) error {
	return r.write(ctx, &m, func(tx *store.Tx, _ *store.Team) error {
		for _, item := range items {
			d, e := tx.GetDelivery(item.MessageID, m.ID)
			if e != nil {
				return e
			}
			if d.State != model.DeliveryStateDelivering {
				continue
			}
			if _, e = r.b.changeDelivery(tx, r.team, item.MessageID, *d, state, reason); e != nil {
				return e
			}
		}
		if state == model.DeliveryStateDelivered && m.Session != nil {
			s, e := tx.GetMemberSession(m.ID, *m.Session)
			if e != nil {
				return e
			}
			return tx.MarkSessionBriefed(m.ID, s.Ordinal, r.b.now())
		}
		return nil
	})
}

func (r *Runner) cancelledPane(ctx context.Context, m model.Member, items []store.DeliveryItem, confirmed bool) error {
	if !r.stopping.Load() {
		return nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	if confirmed {
		return r.finishPane(cleanup, m, items, model.DeliveryStateDelivered, nil)
	}
	return r.finishPane(cleanup, m, items, model.DeliveryStateUncertain, ptr(model.DeliveryReasonNoActivity))
}

// Observation failures are recipient-local; they never surrender the team lease.
func (r *Runner) paneRetry(ctx context.Context, m model.Member, err error) error {
	log.Printf("pane observation for %s: %v; retrying", m.ID, model.AsError(err))
	_ = r.b.opts.Wait(ctx, time.Second)
	return nil
}
