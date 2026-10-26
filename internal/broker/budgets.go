package broker

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (b *Broker) ContinueBudget(ctx context.Context, r wire.ContinueBudgetRequest) (wire.ContinueBudgetResult, error) {
	var out wire.ContinueBudgetResult
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		if e = running(t); e != nil {
			return e
		}
		s, e := b.budget(tx, t, true)
		if e != nil {
			return e
		}
		human, e := tx.GetLiveMember(t.ID, "human")
		if e != nil {
			return e
		}
		kind := model.BudgetKindHops
		if r.Kind != nil {
			kind = *r.Kind
		}
		var result policy.BudgetResult
		switch kind {
		case model.BudgetKindHops:
			if r.Writer != nil || r.Reviewer != nil {
				return invalid("writer and reviewer require kind rounds")
			}
			var pe *model.Error
			result, pe = policy.ContinueHops(s, r.Amount, ref(*human))
			if pe != nil {
				return pe
			}
		case model.BudgetKindRounds:
			if (r.Writer == nil) != (r.Reviewer == nil) {
				return invalid("give both writer and reviewer, or neither")
			}
			var writer, reviewer *model.Member
			if r.Writer != nil {
				writer, e = b.member(tx, t.ID, *r.Writer)
				if e != nil {
					return e
				}
				reviewer, e = b.member(tx, t.ID, *r.Reviewer)
				if e != nil {
					return e
				}
			}
			var pe *model.Error
			result, pe = policy.ContinueRounds(s, writer, reviewer, r.Amount, ref(*human))
			if pe != nil {
				return pe
			}
		default:
			return invalid("unknown budget kind")
		}
		if e = b.writeBudget(tx, t.ID, result); e != nil {
			return e
		}
		if e = b.policyEvents(tx, t.ID, result.Events); e != nil {
			return e
		}
		if e = b.budgetDeliveryEvents(tx, t.ID, result); e != nil {
			return e
		}
		out = wire.ContinueBudgetResult{Hops: result.Hops, Rounds: result.Rounds, Released: result.Released}
		return nil
	})
	return out, e
}

func (b *Broker) changeDelivery(tx *store.Tx, team, message string, d model.Delivery, state model.DeliveryState, reason *model.DeliveryReason) (model.Delivery, error) {
	now := b.now()
	var delivered *model.Time
	if state == model.DeliveryStateDelivered {
		delivered = &now
	}
	e := tx.UpdateDelivery(message, d.To.ID, d.State, store.DeliveryPatch{State: state, Reason: &reason, DeliveredAt: &delivered, UpdatedAt: now})
	if e != nil {
		return model.Delivery{}, e
	}
	next, e := tx.GetDelivery(message, d.To.ID)
	if e != nil {
		return model.Delivery{}, e
	}
	e = b.event(tx, &team, model.EventDeliveryChanged, false, &d.To.ID, &message, wire.DeliveryChangedEventData{Message: message, From: d.State, Delivery: *next})
	return *next, e
}
func (b *Broker) ResolveDelivery(ctx context.Context, r wire.ResolveDeliveryRequest) (wire.ResolveDeliveryResult, error) {
	var out wire.ResolveDeliveryResult
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		recipient, e := tx.GetMember(r.Member)
		if e != nil {
			return e
		}
		// Historical uncertain deliveries remain resolvable after the recipient
		// leaves the live roster; member identities are never deleted.
		if recipient.Team != r.Team {
			return model.NewError(model.ErrorUnknownMember, "recipient is not in this team")
		}
		m, e := tx.GetMessage(r.Message)
		if e != nil {
			return e
		}
		if m.Team != t.ID {
			return model.NewError(model.ErrorNotFound, "message not found in this team")
		}
		d, e := tx.GetDelivery(r.Message, r.Member)
		if e != nil {
			return e
		}
		var state model.DeliveryState
		var reason *model.DeliveryReason
		legal := false
		switch r.Action {
		case model.ResolveActionResend:
			if e = running(t); e != nil {
				return e
			}
			state = model.DeliveryStateQueued
			legal = d.State == model.DeliveryStateUncertain || (d.State == model.DeliveryStateHeld && d.Reason != nil && *d.Reason == model.DeliveryReasonDeliveryFailed)
		case model.ResolveActionMarkDelivered:
			state = model.DeliveryStateDelivered
			legal = d.State == model.DeliveryStateUncertain
		case model.ResolveActionCancel:
			state = model.DeliveryStateExpired
			reason = ptr(model.DeliveryReasonCancelled)
			legal = d.State == model.DeliveryStateQueued || d.State == model.DeliveryStateHeld || d.State == model.DeliveryStateUncertain
		}
		if !legal {
			return invalid("cannot resolve this delivery with that action").WithDetail("state", d.State)
		}
		out.Message = r.Message
		out.Delivery, e = b.changeDelivery(tx, t.ID, r.Message, *d, state, reason)
		return e
	})
	return out, e
}
func (b *Broker) Hold(ctx context.Context, r wire.HoldRequest) (wire.HoldResult, error) {
	var out wire.HoldResult
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		m, e := b.member(tx, r.Team, r.Member)
		if e != nil {
			return e
		}
		var ttl *int
		if r.TTLSeconds != nil {
			if *r.TTLSeconds < policy.HoldMinSeconds || *r.TTLSeconds > policy.HoldMaxSeconds {
				return invalid("ttl_seconds must be 10–120")
			}
			ttl = ptr(int(*r.TTLSeconds))
		}
		p, pe := policy.Hold(b.now().Time, t.State, *m, ttl, r.Release != nil && *r.Release)
		if pe != nil {
			return pe
		}
		out.Member = ref(*m)
		if !p.Release {
			until := model.Time{Time: *p.HeldUntil}
			out.HeldUntil = &until
			return tx.PutHold(store.Hold{MemberID: m.ID, TeamID: t.ID, Since: b.now(), HeldUntil: until})
		}
		if e = tx.ReleaseHold(m.ID, b.now()); e != nil && !missing(e) {
			return e
		}
		ds, e := tx.ListTeamDeliveries(t.ID, model.DeliveryStateHeld)
		if e != nil {
			return e
		}
		for _, d := range ds {
			if d.Delivery.To.ID == m.ID && d.Delivery.Reason != nil && *d.Delivery.Reason == model.DeliveryReasonPersonControl {
				if _, e = b.changeDelivery(tx, t.ID, d.MessageID, d.Delivery, model.DeliveryStateQueued, nil); e != nil {
					return e
				}
				out.Released++
			}
		}
		return nil
	})
	return out, e
}
