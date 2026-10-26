package broker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestAcceptMessages(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	msg, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "review"})
	must(t, e)
	if msg.State != model.DeliveryStateQueued {
		t.Fatal(msg)
	}
	_, e = b.ToolSend(ctx, c, wire.SendInput{To: "unknown", Text: "x"})
	code(t, e, model.ErrorUnknownMember)
	for _, to := range []string{"writer", "all"} {
		_, e = b.ToolSend(ctx, c, wire.SendInput{To: to, Text: "x"})
		code(t, e, model.ErrorInvalidRequest)
	}
	_, e = b.ToolReply(ctx, service.Caller{TeamID: "t", MemberID: "reviewer"}, wire.ReplyInput{ID: msg.ID, Text: "not received"})
	code(t, e, model.ErrorNotFound)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateDelivery(msg.ID, "reviewer", model.DeliveryStateQueued, store.DeliveryPatch{State: model.DeliveryStateDelivered, DeliveredAt: ptr(ptr(b.now())), UpdatedAt: b.now()})
	}))
	reply, e := b.ToolReply(ctx, service.Caller{TeamID: "t", MemberID: "reviewer"}, wire.ReplyInput{ID: msg.ID, Text: "reviewed"})
	must(t, e)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		m, e := tx.GetMessage(reply.ID)
		if e != nil {
			return e
		}
		if m.Round == nil || *m.Round != 1 || m.ReplyTo == nil || *m.ReplyTo != msg.ID {
			t.Fatal(m)
		}
		return nil
	}))
	// Denial commits both its delivery and policy event.
	must(t, b.db.Write(ctx, func(tx *store.Tx) error { return tx.ReplaceGrid("t", nil) }))
	denied, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "denied"})
	code(t, e, model.ErrorDenied)
	id := model.AsError(e).Details["message_id"].(string)
	if denied.ID != id {
		t.Fatal(denied)
	}
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		m, e := tx.GetMessage(id)
		if e != nil {
			return e
		}
		if m.Hop != nil || m.Deliveries[0].State != model.DeliveryStateDenied {
			t.Fatal(m)
		}
		es, e := tx.ReplayEvents(0, nil, 0)
		if e != nil {
			return e
		}
		if es[len(es)-1].Type != model.EventPolicyDenied {
			t.Fatal(es)
		}
		return nil
	}))
	app, e := b.Send(ctx, wire.SendRequest{Team: "t", All: ptr(true), Text: "everyone"})
	must(t, e)
	if !app.Message.Broadcast || len(app.Message.Deliveries) != 2 || app.Message.Deliveries[0].Wake {
		t.Fatal(app)
	}
	_, e = b.Send(ctx, wire.SendRequest{Team: "t", To: ptr("ghost"), Text: "x"})
	code(t, e, model.ErrorUnknownMember)
}
func TestHumanMessageLimits(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	fixed := time.Now()
	b.opts.Clock = func() time.Time { return fixed }
	_, e := b.ToolSend(ctx, c, wire.SendInput{To: "human", Text: strings.Repeat("界", 8001)})
	code(t, e, model.ErrorInvalidRequest)
	for i := 0; i < 6; i++ {
		out, e := b.ToolSend(ctx, c, wire.SendInput{To: "human", Text: "help"})
		must(t, e)
		must(t, b.db.Read(ctx, func(tx *store.Tx) error {
			m, e := tx.GetMessage(out.ID)
			if e != nil {
				return e
			}
			d := m.Deliveries[0]
			if d.Notify == nil || *d.Notify != (i < 5) || d.State != model.DeliveryStateDelivered || m.Hop != nil {
				t.Fatal(m)
			}
			return nil
		}))
	}
}
func TestTimelineCurrentDeliveries(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	m, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "x"})
	must(t, e)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateDelivery(m.ID, "reviewer", model.DeliveryStateQueued, store.DeliveryPatch{State: model.DeliveryStateHeld, Reason: ptr(ptr(model.DeliveryReasonPersonControl)), UpdatedAt: b.now()})
	}))
	page, e := b.Timeline(ctx, wire.TimelineRequest{Team: "t"})
	must(t, e)
	d := page.Events[0].Data.(*wire.MessageCreatedEventData)
	if d.Message.Deliveries[0].State != model.DeliveryStateHeld {
		t.Fatal(d)
	}
}
