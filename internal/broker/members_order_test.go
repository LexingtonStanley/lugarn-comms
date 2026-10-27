package broker

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestMembersOrderByCreationTimeThenID(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	early := model.Time{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	tie := model.Time{Time: early.Add(time.Second)}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		// Insert ties backwards and use names that sort opposite to ids.
		for _, p := range []store.MemberParams{
			{ID: "b-order-tie", Name: "order-alpha", CreatedAt: &tie},
			{ID: "a-order-tie", Name: "order-zulu", CreatedAt: &tie},
			{ID: "z-order-early", Name: "order-middle", CreatedAt: &early},
		} {
			p.TeamID, p.Kind, p.Origin, p.State = "t", model.MemberKindAgent, model.MemberOriginSpec, model.MemberStateStopped
			p.Role, p.Harness = ptr(model.RoleWorker), ptr(model.HarnessCodex)
			p.Profile, p.Mode = ptr(model.ProfileReadOnly), ptr(model.ModeHeadless)
			p.Folder, p.Brief = ptr(b.opts.StateDir), ptr("order fixture")
			if e := tx.InsertMember(p); e != nil {
				return e
			}
		}
		return tx.RemoveMember("b-order-tie", model.RemovedReasonRemovedFromSpec, b.now())
	}))
	for _, removed := range []bool{false, true} {
		out, e := b.Members(ctx, wire.MembersRequest{Team: "t", IncludeRemoved: &removed})
		must(t, e)
		want := []string{"z-order-early", "a-order-tie"}
		if removed {
			want = append(want, "b-order-tie")
		}
		var got []string
		human := false
		for n, m := range out.Members {
			human = human || m.Kind == model.MemberKindHuman
			if n < len(want) {
				got = append(got, m.ID)
			}
			if n > 0 {
				previous := out.Members[n-1]
				if previous.CreatedAt.After(m.CreatedAt.Time) || previous.CreatedAt.Equal(m.CreatedAt.Time) && previous.ID > m.ID {
					t.Fatalf("members out of order: %s before %s", previous.ID, m.ID)
				}
			}
		}
		if !reflect.DeepEqual(got, want) || !human || len(out.Members) != len(want)+3 {
			t.Fatal(removed, got, want, human, len(out.Members))
		}
	}
}
