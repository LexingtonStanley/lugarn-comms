package broker

import (
	"context"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// accept writes a policy decision in the caller's immediate transaction. A
// denial is returned separately so its message and audit event can commit.
func (b *Broker) accept(tx *store.Tx, t *store.Team, from model.Member, to *model.Member, all bool, text string, reply *string, kind model.MessageKind) (model.Message, []policy.DeliveryPlan, *model.Error, error) {
	var zero model.Message
	live, e := tx.ListMembers(t.ID, false)
	if e != nil {
		return zero, nil, nil, e
	}
	edges, e := b.grid(tx, t.ID)
	if e != nil {
		return zero, nil, nil, e
	}
	budget, e := b.budget(tx, t, from.Kind == model.MemberKindHuman)
	if e != nil {
		return zero, nil, nil, e
	}
	found := reply == nil
	if reply != nil {
		m, e := tx.GetMessage(*reply)
		if e != nil && !missing(e) {
			return zero, nil, nil, e
		}
		if e == nil && m.Team == t.ID {
			if from.Kind == model.MemberKindHuman {
				found = true
			} else {
				for _, d := range m.Deliveries {
					if d.To.ID == from.ID && (d.State == model.DeliveryStateDelivered || d.State == model.DeliveryStateUncertain) {
						found = true
					}
				}
			}
		}
	}
	id, e := b.opts.ID()
	if e != nil {
		return zero, nil, nil, model.AsError(e)
	}
	now := b.now()
	// Count before insertion so earlier commits with the same timestamp count.
	recent, e := tx.CountMessagesToHuman(from.ID, now)
	if e != nil {
		return zero, nil, nil, e
	}
	plan, pe := policy.Accept(policy.MessageInput{ID: id, Kind: kind, From: from, To: to, All: all, Live: live, Text: text, ReplyTo: reply, ReplyFound: found, Grid: policy.NewGrid(edges), Budget: budget, ToHumanRecent: int(recent)})
	if pe != nil {
		return zero, nil, nil, pe
	}
	if plan.Outcome == policy.OutcomeSkipped {
		return zero, nil, nil, nil
	}
	m := model.Message{ID: id, Team: t.ID, From: ref(from), Kind: plan.Kind, Broadcast: plan.Broadcast, ReplyTo: plan.ReplyTo, Text: plan.Text, Hop: plan.Hop, Round: plan.Round, CreatedAt: now, Deliveries: []model.Delivery{}}
	if from.Kind == model.MemberKindAgent {
		turn, e := tx.GetLiveTurn(from.ID)
		if e != nil && !missing(e) {
			return zero, nil, nil, e
		}
		if e == nil {
			m.Turn = &turn.ID
		}
	}
	if _, e = tx.InsertMessage(store.MessageParams{Message: m, SenderID: from.ID, Run: t.Run}); e != nil {
		return zero, nil, nil, e
	}
	for _, d := range plan.Deliveries {
		v := model.Delivery{To: d.To, State: d.State, Reason: d.Reason, Via: d.Via, Wake: d.Wake, Notify: d.Notify, UpdatedAt: now}
		if v.State == model.DeliveryStateDelivered {
			v.DeliveredAt = &now
		}
		if e = tx.InsertDelivery(store.DeliveryParams{Delivery: v, MessageID: id, RecipientID: d.To.ID, CreatedAt: &now}); e != nil {
			return zero, nil, nil, e
		}
		m.Deliveries = append(m.Deliveries, v)
	}
	if e = b.writeBudget(tx, t.ID, plan.Budget); e != nil {
		return zero, nil, nil, e
	}
	if e = b.event(tx, &t.ID, model.EventMessageCreated, true, &from.ID, &id, wire.MessageCreatedEventData{Message: m}); e != nil {
		return zero, nil, nil, e
	}
	if e = b.policyEvents(tx, t.ID, plan.Events); e != nil {
		return zero, nil, nil, e
	}
	if e = b.budgetDeliveryEvents(tx, t.ID, plan.Budget); e != nil {
		return zero, nil, nil, e
	}
	return m, plan.Deliveries, plan.Err, nil
}
func (b *Broker) writeBudget(tx *store.Tx, team string, r policy.BudgetResult) error {
	now := b.now()
	var exhausted *model.Time
	if r.Hops.Exhausted {
		exhausted = &now
	}
	if e := tx.PutHopCounter(store.HopCounter{TeamID: team, Used: r.Hops.Used, Allowance: r.Hops.Allowance, ExhaustedAt: exhausted, UpdatedAt: now}); e != nil {
		return e
	}
	for _, v := range r.Rounds {
		exhausted = nil
		if v.Exhausted {
			exhausted = &now
		}
		if e := tx.PutRoundCounter(store.RoundCounter{TeamID: team, WriterID: v.Writer.ID, ReviewerID: v.Reviewer.ID, Used: v.Used, Allowance: v.Allowance, ExhaustedAt: exhausted, UpdatedAt: now}); e != nil {
			return e
		}
	}
	for _, v := range r.Held {
		if v.Renumbered {
			if e := tx.RenumberMessage(v.Message, v.Hop, v.Round); e != nil {
				return e
			}
		}
		if v.Moved {
			if e := tx.UpdateDelivery(v.Message, v.To.ID, model.DeliveryStateHeld, store.DeliveryPatch{State: v.State, Reason: &v.Reason, UpdatedAt: now}); e != nil {
				return e
			}
		}
	}
	return nil
}
func (b *Broker) budgetDeliveryEvents(tx *store.Tx, team string, r policy.BudgetResult) error {
	for _, v := range r.Held {
		if !v.Moved {
			continue
		}
		d, e := tx.GetDelivery(v.Message, v.To.ID)
		if e != nil {
			return e
		}
		if e = b.event(tx, &team, model.EventDeliveryChanged, false, &v.To.ID, &v.Message, wire.DeliveryChangedEventData{Message: v.Message, From: model.DeliveryStateHeld, Delivery: *d}); e != nil {
			return e
		}
	}
	return nil
}
func (b *Broker) Send(ctx context.Context, r wire.SendRequest) (wire.SendResult, error) {
	var out wire.SendResult
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	var refusal *model.Error
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		if e = running(t); e != nil {
			return e
		}
		all := r.All != nil && *r.All
		if all == (r.To != nil) {
			return invalid("give either to or all: true")
		}
		from, e := tx.GetLiveMember(t.ID, policy.HumanName)
		if e != nil {
			return e
		}
		var to *model.Member
		if r.To != nil {
			to, e = b.member(tx, t.ID, *r.To)
			if e != nil {
				return e
			}
		}
		out.Message, _, refusal, e = b.accept(tx, t, *from, to, all, r.Text, r.ReplyTo, model.MessageKindMessage)
		return e
	})
	if e != nil {
		return out, e
	}
	if refusal != nil {
		return out, refusal
	}
	return out, nil
}
func sendOutput(m model.Message, ds []policy.DeliveryPlan) wire.SendOutput {
	if len(ds) == 0 {
		return wire.SendOutput{}
	}
	d := ds[0]
	return wire.SendOutput{ID: m.ID, To: d.To, State: d.State, Reason: d.Reason, WillWake: d.WillWake}
}
func (b *Broker) ToolSend(ctx context.Context, c service.Caller, r wire.SendInput) (wire.SendOutput, error) {
	var out wire.SendOutput
	var refusal *model.Error
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, m, e := b.caller(tx, c)
		if e != nil {
			return e
		}
		live, e := tx.ListMembers(t.ID, false)
		if e != nil {
			return e
		}
		to, all, pe := policy.ResolveRecipient(live, r.To)
		if pe != nil {
			return pe
		}
		msg, ds, pe, e := b.accept(tx, t, *m, to, all, r.Text, r.ReplyTo, model.MessageKindMessage)
		if e != nil {
			return e
		}
		refusal = pe
		out = sendOutput(msg, ds)
		return nil
	})
	if e != nil {
		return out, e
	}
	if refusal != nil {
		return out, refusal
	}
	return out, nil
}
func (b *Broker) ToolReply(ctx context.Context, c service.Caller, r wire.ReplyInput) (wire.ReplyOutput, error) {
	var out wire.ReplyOutput
	var refusal *model.Error
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		t, m, e := b.caller(tx, c)
		if e != nil {
			return e
		}
		orig, e := tx.GetMessage(r.ID)
		if e != nil {
			return e
		}
		if orig.Team != t.ID {
			return model.NewError(model.ErrorNotFound, "message not found")
		}
		to, e := b.member(tx, t.ID, orig.From.ID)
		if e != nil {
			return e
		}
		msg, ds, pe, e := b.accept(tx, t, *m, to, false, r.Text, &r.ID, model.MessageKindReply)
		if e != nil {
			return e
		}
		refusal = pe
		out = sendOutput(msg, ds)
		return nil
	})
	if e != nil {
		return out, e
	}
	if refusal != nil {
		return out, refusal
	}
	return out, nil
}
