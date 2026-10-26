package broker

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"testing"
)

func TestBudgetContinueAndReset(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{HopBudget: ptr(int64(1))}, b.now())
	}))
	var ids []string
	for i := 0; i < 4; i++ {
		m, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "x"})
		must(t, e)
		ids = append(ids, m.ID)
		if (m.State == model.DeliveryStateQueued) != (i == 0) {
			t.Fatal(m)
		}
	}
	out, e := b.ContinueBudget(ctx, wire.ContinueBudgetRequest{Team: "t", Amount: ptr(int64(1))})
	must(t, e)
	if out.Released != 1 || !out.Hops.Exhausted {
		t.Fatal(out)
	}
	_, e = b.Send(ctx, wire.SendRequest{Team: "t", To: ptr("reviewer"), Text: "reset"})
	must(t, e)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		m, e := tx.GetMessage(ids[2])
		if e != nil {
			return e
		}
		if *m.Hop != 1 || m.Deliveries[0].State != model.DeliveryStateQueued {
			t.Fatal(m)
		}
		m, e = tx.GetMessage(ids[3])
		if e != nil {
			return e
		}
		if *m.Hop != 2 || m.Deliveries[0].State != model.DeliveryStateHeld {
			t.Fatal(m)
		}
		return nil
	}))
	for _, r := range []wire.ContinueBudgetRequest{{Team: "t", Amount: ptr(int64(0))}, {Team: "t", Kind: ptr(model.BudgetKindRounds), Writer: ptr("writer")}, {Team: "t", Kind: ptr(model.BudgetKindRounds), Writer: ptr("reviewer"), Reviewer: ptr("writer")}} {
		_, e = b.ContinueBudget(ctx, r)
		code(t, e, model.ErrorInvalidRequest)
	}
}
func TestResolveDeliveryStates(t *testing.T) {
	for _, v := range []struct {
		state  model.DeliveryState
		reason *model.DeliveryReason
		action model.ResolveAction
		want   model.DeliveryState
		ok     bool
	}{{model.DeliveryStateUncertain, ptr(model.DeliveryReasonNoActivity), model.ResolveActionResend, model.DeliveryStateQueued, true}, {model.DeliveryStateUncertain, ptr(model.DeliveryReasonNoActivity), model.ResolveActionMarkDelivered, model.DeliveryStateDelivered, true}, {model.DeliveryStateHeld, ptr(model.DeliveryReasonDeliveryFailed), model.ResolveActionResend, model.DeliveryStateQueued, true}, {model.DeliveryStateHeld, ptr(model.DeliveryReasonBudget), model.ResolveActionResend, "", false}, {model.DeliveryStateQueued, nil, model.ResolveActionCancel, model.DeliveryStateExpired, true}, {model.DeliveryStateQueued, nil, model.ResolveActionMarkDelivered, "", false}} {
		t.Run(string(v.state)+"/"+string(v.action), func(t *testing.T) {
			b, c := setup(t)
			ctx := context.Background()
			m, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "x"})
			must(t, e)
			if v.state != model.DeliveryStateQueued {
				must(t, b.db.Write(ctx, func(tx *store.Tx) error {
					return tx.UpdateDelivery(m.ID, "reviewer", model.DeliveryStateQueued, store.DeliveryPatch{State: v.state, Reason: &v.reason, UpdatedAt: b.now()})
				}))
			}
			out, e := b.ResolveDelivery(ctx, wire.ResolveDeliveryRequest{Team: "t", Message: m.ID, Member: "reviewer", Action: v.action})
			if v.ok {
				must(t, e)
				if out.Delivery.State != v.want {
					t.Fatal(out)
				}
			} else {
				code(t, e, model.ErrorInvalidRequest)
			}
		})
	}
}
func TestHolds(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	for _, r := range []wire.HoldRequest{{Team: "t", Member: "human", TTLSeconds: ptr(int64(20))}, {Team: "t", Member: "writer", TTLSeconds: ptr(int64(9))}, {Team: "t", Member: "writer"}} {
		_, e := b.Hold(ctx, r)
		code(t, e, model.ErrorInvalidRequest)
	}
	h, e := b.Hold(ctx, wire.HoldRequest{Team: "t", Member: "reviewer", TTLSeconds: ptr(int64(20))})
	must(t, e)
	if h.HeldUntil == nil {
		t.Fatal(h)
	}
	m, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "x"})
	must(t, e)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateDelivery(m.ID, "reviewer", model.DeliveryStateQueued, store.DeliveryPatch{State: model.DeliveryStateHeld, Reason: ptr(ptr(model.DeliveryReasonPersonControl)), UpdatedAt: b.now()})
	}))
	h, e = b.Hold(ctx, wire.HoldRequest{Team: "t", Member: "reviewer", Release: ptr(true)})
	must(t, e)
	if h.HeldUntil != nil || h.Released != 1 {
		t.Fatal(h)
	}
	_, e = b.Hold(ctx, wire.HoldRequest{Team: "t", Member: "reviewer", Release: ptr(true)})
	must(t, e)
}
