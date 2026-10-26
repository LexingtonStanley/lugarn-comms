package broker

import (
	"context"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type runnerTurn struct {
	turn         model.Turn
	member       model.Member
	items        []store.DeliveryItem
	seal, prompt string
	seq          int64
	timeout      time.Duration
}

// CancelTurn lets a C2d request adapter cancel this runner's current member turn.
// It never cancels an orphan PID by name or guesses a process identity.
func (r *Runner) CancelTurn(member string) error {
	r.mu.Lock()
	h := r.handles[member]
	r.mu.Unlock()
	if h == nil {
		return nil
	}
	return h.Cancel()
}
func (r *Runner) prepareTurn(ctx context.Context, m model.Member) (*runnerTurn, error) {
	var queued bool
	if e := r.b.db.Read(ctx, func(tx *store.Tx) error { items, e := tx.NextDeliveries(m.ID, 1); queued = len(items) > 0; return e }); e != nil || !queued {
		return nil, e
	}
	var w *runnerTurn
	e := r.write(ctx, &m, func(tx *store.Tx, t *store.Team) error {
		if t.State != model.TeamStateRunning {
			return nil
		}
		current, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		if current.State != model.MemberStateRunning || current.Lease.Driver != model.DriverHeadless {
			return nil
		}
		if h, e := tx.GetHold(m.ID); e == nil && h.ReleasedAt == nil && h.HeldUntil.After(r.b.now().Time) {
			return nil
		} else if e != nil && !missing(e) {
			return e
		}
		if _, e = tx.GetLiveTurn(m.ID); e == nil {
			return nil
		} else if !missing(e) {
			return e
		}
		n, e := tx.CountLiveTurns(t.ID)
		if e != nil {
			return e
		}
		if n >= t.HeadlessTurns {
			return nil
		}
		rs, e := tx.ListOpenRequests(t.ID)
		if e != nil {
			return e
		}
		for _, req := range rs {
			if req.Member != nil && req.Member.ID == m.ID {
				return nil
			}
		}
		items, e := tx.NextDeliveries(m.ID, 0)
		if e != nil {
			return e
		}
		if len(items) == 0 {
			return nil
		}
		seal, e := model.NewSeal()
		if e != nil {
			return e
		}
		if current.Session != nil {
			s, e := tx.GetMemberSession(m.ID, *current.Session)
			if e != nil {
				return e
			}
			seal = s.Seal
		}
		id, e := r.b.opts.ID()
		if e != nil {
			return e
		}
		turn := model.Turn{ID: id, Team: t.ID, Member: ref(m), Launch: *current.Lease.Launch, LeaseGeneration: current.Lease.Generation, Harness: *current.Harness, SessionIDStart: current.Session, Status: model.TurnStatusStarting, StartedAt: r.b.now()}
		if e = tx.InsertTurn(store.TurnParams{Turn: turn, MemberID: m.ID, Seal: &seal}); e != nil {
			return e
		}
		picked, e := tx.PickUpDeliveries(m.ID, 0, ptr(model.DeliveryViaHeadless), &id, &turn.LeaseGeneration, r.b.now())
		if e != nil {
			return e
		}
		for _, d := range picked {
			if e = r.b.event(tx, &t.ID, model.EventDeliveryChanged, false, &m.ID, &d.MessageID, wire.DeliveryChangedEventData{Message: d.MessageID, From: model.DeliveryStateQueued, Delivery: d.Delivery}); e != nil {
				return e
			}
		}
		if e = r.b.event(tx, &t.ID, model.EventTurnStarted, false, &m.ID, nil, wire.TurnStartedEventData{Turn: turn}); e != nil {
			return e
		}
		seq, e := tx.CurrentEventSeq()
		if e != nil {
			return e
		}
		texts := []string{}
		for _, d := range picked {
			msg, e := tx.GetMessage(d.MessageID)
			if e != nil {
				return e
			}
			texts = append(texts, msg.Text)
		}
		timeout := r.opts.TurnTimeout
		if timeout <= 0 {
			timeout = time.Duration(t.TurnTimeoutSeconds) * time.Second
		}
		w = &runnerTurn{turn: turn, member: *current, items: picked, seal: seal, prompt: strings.Join(texts, "\n\n"), seq: seq, timeout: timeout}
		return nil
	})
	return w, e
}
func (r *Runner) runTurn(ctx context.Context, m model.Member) error {
	w, e := r.prepareTurn(ctx, m)
	if e != nil || w == nil {
		return e
	}
	binary, e := r.opts.Binary(*m.Harness)
	if e != nil {
		return r.finishTurn(ctx, w, TurnOutcome{Error: e.Error()}, false)
	}
	if ctx.Err() != nil {
		return nil
	}
	desc := TurnLaunch{Harness: *m.Harness, Profile: *m.Profile, Workdir: *m.Workdir, StateDir: r.b.opts.StateDir, LaunchID: w.turn.Launch, MemberID: m.ID, Binary: binary, Session: w.turn.SessionIDStart}
	if m.Harness != nil && *m.Harness == model.HarnessClaude && m.Session != nil {
		_ = r.b.db.Read(ctx, func(tx *store.Tx) error {
			s, e := tx.GetMemberSession(m.ID, *m.Session)
			if e == nil {
				desc.NewSession = s.Source == model.SessionSourceAssigned && s.BriefedAt == nil
			}
			return e
		})
	}
	h, e := r.opts.Headless.Start(context.WithoutCancel(ctx), desc, w.prompt)
	if e != nil {
		return r.finishTurn(ctx, w, TurnOutcome{Error: e.Error()}, false)
	}
	r.mu.Lock()
	r.handles[m.ID] = h
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.handles, m.ID); r.mu.Unlock() }()
	done := make(chan TurnOutcome, 1)
	go func() { done <- h.Wait() }()
	// Persist identity even if the runner context just ended, so the next runner
	// can distinguish an orphan from a reused PID. This does not signal the child.
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	e = r.write(persist, &m, func(tx *store.Tx, _ *store.Team) error {
		return tx.UpdateTurn(w.turn.ID, &w.turn.LeaseGeneration, store.TurnPatch{PID: ptr(ptr(h.PID()))})
	})
	if e == nil {
		e = r.recordProcess(persist, w.turn, h.PID())
	}
	cancel()
	if e != nil {
		return e
	}
	acked := false
	acks := h.Acknowledgements()
	timer := time.NewTimer(w.timeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			if !r.stopping.Load() {
				return nil
			}
			if e = h.Cancel(); e != nil {
				return e
			}
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
			defer cancel()
			select {
			case out := <-done:
				out.Cancelled = true
				if acked {
					out.Acknowledged = true
				}
				return r.finishTurn(cleanup, w, out, false)
			case <-cleanup.Done():
				return model.NewError(model.ErrorTimeout, "cancelled harness did not exit")
			}
		case <-timer.C:
			if e = h.Cancel(); e != nil {
				return e
			}
			select {
			case out := <-done:
				out.Cancelled = true
				if acked {
					out.Acknowledged = true
				}
				return r.finishTurn(ctx, w, out, true)
			case <-ctx.Done():
				return nil
			case <-time.After(20 * time.Second):
				return model.NewError(model.ErrorTimeout, "timed-out harness did not exit")
			}
		case ack, ok := <-acks:
			if !ok {
				acks = nil
				continue
			}
			if !acked {
				if e = r.acknowledge(ctx, w, ack.Session); e != nil {
					return e
				}
				acked = true
			}
		case out := <-done:
			if acked {
				out.Acknowledged = true
			}
			return r.finishTurn(ctx, w, out, false)
		}
	}
}
func (r *Runner) acknowledge(ctx context.Context, w *runnerTurn, session *string) error {
	return r.write(ctx, &w.member, func(tx *store.Tx, _ *store.Team) error { return r.acknowledgeTx(tx, w, session) })
}
func (r *Runner) acknowledgeTx(tx *store.Tx, w *runnerTurn, session *string) error {
	current, e := tx.GetTurn(w.turn.ID)
	if e != nil {
		return e
	}
	if current.Status != model.TurnStatusStarting && current.Status != model.TurnStatusRunning {
		return errRunnerGeneration
	}
	if session == nil {
		session = w.member.Session
	}
	if session != nil {
		if e = r.session(tx, w.member, *session, w.seal, model.SessionSourceReported, w.turn.Launch, true); e != nil {
			return e
		}
	} else if w.member.Session != nil {
		record, err := tx.GetMemberSession(w.member.ID, *w.member.Session)
		if err != nil {
			return err
		}
		if e = tx.MarkSessionBriefed(w.member.ID, record.Ordinal, r.b.now()); e != nil {
			return e
		}
	}
	if e = tx.UpdateTurn(current.ID, &current.LeaseGeneration, store.TurnPatch{Status: ptr(model.TurnStatusRunning), SessionIDEnd: &session}); e != nil {
		return e
	}
	for _, item := range w.items {
		d, e := tx.GetDelivery(item.MessageID, w.member.ID)
		if e != nil {
			return e
		}
		if d.State == model.DeliveryStateDelivering {
			if _, e = r.b.changeDelivery(tx, r.team, item.MessageID, *d, model.DeliveryStateDelivered, nil); e != nil {
				return e
			}
		}
	}
	return r.setActivity(tx, w.member, model.ActivityWorking)
}
func (r *Runner) finishTurn(ctx context.Context, w *runnerTurn, out TurnOutcome, timedOut bool) error {
	return r.write(ctx, &w.member, func(tx *store.Tx, _ *store.Team) error {
		current, e := tx.GetTurn(w.turn.ID)
		if e != nil {
			return e
		}
		if current.Status != model.TurnStatusStarting && current.Status != model.TurnStatusRunning {
			return nil
		}
		if out.Acknowledged && current.Status == model.TurnStatusStarting {
			if e = r.acknowledgeTx(tx, w, out.Session); e != nil {
				return e
			}
		}
		if out.Session != nil {
			if e = r.session(tx, w.member, *out.Session, w.seal, model.SessionSourceReported, w.turn.Launch, out.Acknowledged); e != nil {
				return e
			}
		}
		status := OutcomeStatus(out, timedOut)
		if !out.Acknowledged {
			state, reason := model.DeliveryStateHeld, model.DeliveryReasonDeliveryFailed
			if out.Cancelled || timedOut {
				state, reason = model.DeliveryStateUncertain, model.DeliveryReasonTurnCancelled
			}
			for _, item := range w.items {
				d, e := tx.GetDelivery(item.MessageID, w.member.ID)
				if e != nil {
					return e
				}
				if d.State == model.DeliveryStateDelivering {
					if _, e = r.b.changeDelivery(tx, r.team, item.MessageID, *d, state, &reason); e != nil {
						return e
					}
				}
			}
		}
		now := r.b.now()
		var failure *string
		if out.Error != "" {
			failure = &out.Error
		}
		patch := store.TurnPatch{Status: &status, ExitCode: &out.ExitCode, Error: &failure, Usage: &out.Usage, EndedAt: ptr(&now)}
		if out.Session != nil {
			patch.SessionIDEnd = &out.Session
		}
		if out.UsageRaw != nil {
			patch.UsageRaw = &out.UsageRaw
		}
		if e = tx.UpdateTurn(current.ID, &current.LeaseGeneration, patch); e != nil {
			return e
		}
		if e = r.setActivity(tx, w.member, model.ActivityIdle); e != nil {
			return e
		}
		updated, e := tx.GetTurn(current.ID)
		if e != nil {
			return e
		}
		return r.b.event(tx, &r.team, model.EventTurnFinished, status != model.TurnStatusSucceeded, &w.member.ID, nil, wire.TurnFinishedEventData{Turn: *updated})
	})
}
