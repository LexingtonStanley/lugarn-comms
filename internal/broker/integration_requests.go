package broker

import (
	"context"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (l *integrationLifecycle) current(ctx context.Context, id string) (*model.Member, error) {
	var m *model.Member
	e := l.i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember(id); return e })
	return m, e
}
func (l *integrationLifecycle) Execute(ctx context.Context, req model.Request) error {
	if req.Member == nil {
		return invalid("request needs a member")
	}
	m, e := l.current(ctx, req.Member.ID)
	if e != nil {
		return e
	}
	if m.Team != l.r.team {
		return invalid("request member is in another team")
	}
	switch req.Kind {
	case model.RequestKindSpawn:
		memberID := m.ID
		m, e = l.i.prepareWorktree(ctx, l.r, *m)
		if e == nil {
			if m.Mode != nil && *m.Mode == model.ModeHeadless {
				_, e = l.r.launchHeadless(ctx, *m, model.LeaseCauseStart)
			} else {
				e = l.Open(ctx, *m, model.LeaseCauseStart)
			}
		}
		if e != nil {
			current, _ := l.current(context.WithoutCancel(ctx), memberID)
			if current != nil && (current.StopReason == nil || *current.StopReason != model.StopReasonLaunchFailed) {
				_ = l.r.launchFailed(context.WithoutCancel(ctx), *current, e)
			}
			return model.NewError(model.ErrorLaunchFailed, "spawn launch failed").WithDetail("cause", e.Error())
		}
		return nil
	case model.RequestKindStopMember:
		reason := model.StopReasonPerson
		if value, ok := req.Args["reason"].(string); ok && value == string(model.StopReasonSpawner) {
			reason = model.StopReasonSpawner
		}
		return l.stopMember(ctx, *m, reason, model.LeaseCauseStop)
	case model.RequestKindOpenTerminal:
		cancel, _ := req.Args["cancel_turn"].(bool)
		timeout := 30 * time.Minute
		_ = l.i.b.db.Read(ctx, func(tx *store.Tx) error {
			t, e := tx.GetTeam(m.Team)
			if e == nil {
				timeout = time.Duration(t.TurnTimeoutSeconds) * time.Second
			}
			return e
		})
		waiting, done := context.WithTimeout(ctx, timeout)
		defer done()
		if e = l.waitRecipient(waiting, *m, cancel); e != nil {
			return e
		}
		m, e = l.current(ctx, m.ID)
		if e != nil {
			return e
		}
		return l.Open(ctx, *m, model.LeaseCauseOpenTerminal)
	case model.RequestKindReturnHeadless:
		return l.toHeadless(ctx, *m, model.LeaseCauseReturnHeadless, true)
	default:
		return invalid("unknown runner request")
	}
}
func (l *integrationLifecycle) waitRecipient(ctx context.Context, m model.Member, cancel bool) error {
	if cancel {
		if e := l.r.CancelTurn(m.ID); e != nil {
			return e
		}
	}
	for {
		l.r.mu.Lock()
		busy := l.r.busy[m.ID]
		l.r.mu.Unlock()
		var turn *model.Turn
		delivering := false
		e := l.i.b.db.Read(ctx, func(tx *store.Tx) error {
			var e error
			turn, e = tx.GetLiveTurn(m.ID)
			if e != nil && !missing(e) {
				return e
			}
			items, e := tx.ListTeamDeliveries(m.Team, model.DeliveryStateDelivering)
			if e != nil {
				return e
			}
			for _, item := range items {
				if item.Delivery.To.ID == m.ID {
					delivering = true
				}
			}
			return nil
		})
		if e != nil {
			return e
		}
		if cancel && turn != nil && turn.PID != nil {
			birth := l.r.processBirth(*turn)
			if birth != "" {
				if e = cancelOwnedTree(ctx, *turn.PID, birth); e != nil {
					return e
				}
			}
		}
		if !busy && turn == nil && !delivering {
			return nil
		}
		if e = l.i.b.opts.Wait(ctx, 50*time.Millisecond); e != nil {
			return model.NewError(model.ErrorTimeout, "member did not finish its in-flight work")
		}
	}
}
func (l *integrationLifecycle) closePane(ctx context.Context, m model.Member) error {
	if m.Lease == nil || m.Lease.Pane == nil {
		return nil
	}
	server, _ := paneIdentity(m)
	client := l.i.opts.Herdr(server)
	var workspace string
	e := l.i.b.db.Read(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(m.Team)
		if e == nil && t.HerdrWorkspace != nil {
			workspace = *t.HerdrWorkspace
		}
		return e
	})
	if e != nil {
		return e
	}
	if e = l.i.closeOwnedPane(ctx, server, workspace, *m.Lease.Pane); e != nil {
		return e
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, name := paneIdentity(m)
		_, e = client.Agent(ctx, name)
		if herdr.IsGone(e) || herdr.IsUnavailable(e) {
			return nil
		}
		if e != nil {
			return e
		}
		p, e := client.Pane(ctx, *m.Lease.Pane)
		if herdr.IsGone(e) || e == nil && p.Agent == "" {
			return nil
		}
		if e != nil {
			return e
		}
		if time.Now().After(deadline) {
			return model.NewError(model.ErrorTimeout, "pane agent still exists after closing its tab")
		}
		if e = l.i.b.opts.Wait(ctx, 50*time.Millisecond); e != nil {
			return e
		}
	}
}
func (l *integrationLifecycle) stopMember(ctx context.Context, m model.Member, reason model.StopReason, cause model.LeaseCause) error {
	stop, done := context.WithTimeout(ctx, 30*time.Second)
	defer done()
	// A launch may have recorded a pane while the stop request waited.
	// Decide what to close from the post-wait snapshot, never the initial one.
	if cause == model.LeaseCauseStop {
		if e := l.waitRecipient(stop, m, true); e != nil {
			return e
		}
	}
	current, e := l.current(stop, m.ID)
	if e != nil {
		return e
	}
	m = *current
	if e := l.i.capturePointer(stop, m); e != nil {
		return e
	}
	if m.Lease != nil && m.Lease.Driver == model.DriverPane {
		if e := l.closePane(stop, m); e != nil {
			return e
		}
	}
	e = l.r.write(stop, &m, func(tx *store.Tx, _ *store.Team) error {
		now := l.i.b.now()
		if m.Lease != nil && m.Lease.Driver != model.DriverNone {
			if m.Lease.Launch != nil {
				launch, e := tx.GetLaunch(*m.Lease.Launch)
				if e != nil {
					return e
				}
				if launch.EndedAt == nil {
					if e = tx.EndLaunch(launch.ID, model.LaunchEndReasonStopped, now); e != nil {
						return e
					}
				}
			}
			if _, e := tx.RevokeMemberTokens(m.ID, now); e != nil {
				return e
			}
			next := model.Lease{Driver: model.DriverNone, Generation: m.Lease.Generation + 1}
			if e := tx.CompareAndSwapLease(m.ID, m.Lease.Driver, m.Lease.Generation, next, now); e != nil {
				return e
			}
		}
		if e := l.r.memberState(tx, m, model.MemberStateStopped, &reason); e != nil {
			return e
		}
		updated, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		if m.Lease == nil || m.Lease.Driver == model.DriverNone {
			return nil
		}
		return l.i.b.event(tx, &m.Team, model.EventLeaseChanged, true, &m.ID, nil, wire.LeaseChangedEventData{Member: *updated, From: m.Lease.Driver, To: model.DriverNone, Generation: updated.Lease.Generation, Cause: cause})
	})
	if e != nil {
		return e
	}
	return l.r.cleanLaunchFiles(stop)
}
func (l *integrationLifecycle) toHeadless(ctx context.Context, m model.Member, cause model.LeaseCause, wait bool) error {
	if m.Lease == nil || m.Lease.Driver != model.DriverPane {
		return model.NewError(model.ErrorLeaseConflict, "member has no pane")
	}
	if wait {
		if e := l.waitRecipient(ctx, m, false); e != nil {
			return e
		}
		server, name := paneIdentity(m)
		_, e := l.i.opts.Herdr(server).Wait(ctx, name, []model.Activity{model.ActivityIdle, model.ActivityDone}, 300*time.Second)
		if e != nil {
			return e
		}
	}
	if e := l.i.capturePointer(ctx, m); e != nil {
		return e
	}
	if wait {
		if e := l.closePane(ctx, m); e != nil {
			return e
		}
	}
	current, e := l.current(ctx, m.ID)
	if e != nil {
		return e
	}
	ready, e := l.r.launchHeadless(ctx, *current, cause)
	if e != nil {
		return e
	}
	if e = l.r.paneHolds(ctx, *ready, nil); e != nil {
		return e
	}
	e = l.r.write(ctx, ready, func(tx *store.Tx, _ *store.Team) error {
		items, e := tx.ListTeamDeliveries(m.Team, model.DeliveryStateHeld)
		if e != nil {
			return e
		}
		for _, item := range items {
			if item.Delivery.To.ID == m.ID && item.Delivery.Reason != nil && *item.Delivery.Reason == model.DeliveryReasonPersonControl {
				if _, e = l.i.b.changeDelivery(tx, m.Team, item.MessageID, item.Delivery, model.DeliveryStateQueued, nil); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	return l.r.cleanLaunchFiles(ctx)
}
