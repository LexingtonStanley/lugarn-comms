package broker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestStopTeamStoreCleanup(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	must(t, statedir.Ensure(b.opts.StateDir))
	id, token := launch(t, b, "writer")
	must(t, statedir.WriteSecret(filepath.Join(b.opts.StateDir, "launch", id+".token"), []byte(token)))
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.CompareAndSwapLease("writer", model.DriverNone, 0, model.Lease{Driver: model.DriverHeadless, Generation: 1, Launch: &id}, b.now())
	}))
	m, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "queued"})
	must(t, e)
	uncertain, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "maybe"})
	must(t, e)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateDelivery(uncertain.ID, "reviewer", model.DeliveryStateQueued, store.DeliveryPatch{State: model.DeliveryStateUncertain, Reason: ptr(ptr(model.DeliveryReasonNoActivity)), UpdatedAt: b.now()})
	}))
	_, e = b.Hold(ctx, wire.HoldRequest{Team: "t", Member: "reviewer", TTLSeconds: ptr(int64(20))})
	must(t, e)
	_, e = b.StopMember(ctx, wire.StopMemberRequest{Team: "t", Member: "reviewer"})
	must(t, e)
	out, e := b.StopTeam(ctx, wire.StopTeamRequest{Team: "t"})
	must(t, e)
	if out.Expired != 1 || out.Team.State != model.TeamStateStopped {
		t.Fatal(out)
	}
	_, e = os.Stat(filepath.Join(b.opts.StateDir, "launch", id+".token"))
	if !os.IsNotExist(e) {
		t.Fatal("token file remains", e)
	}
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		d, e := tx.GetDelivery(m.ID, "reviewer")
		if e != nil {
			return e
		}
		if d.State != model.DeliveryStateExpired {
			t.Fatal(d)
		}
		d, e = tx.GetDelivery(uncertain.ID, "reviewer")
		if e != nil {
			return e
		}
		if d.State != model.DeliveryStateUncertain {
			t.Fatal(d)
		}
		lease, e := tx.GetLease("writer")
		if e != nil {
			return e
		}
		if lease.Lease.Driver != model.DriverNone || lease.Lease.Generation != 2 {
			t.Fatal(lease)
		}
		req, e := tx.ListOpenRequests("t")
		if len(req) != 0 {
			t.Fatal(req)
		}
		tok, e := tx.GetTokenByHash(model.HashToken(token))
		if tok.RevokedAt == nil {
			t.Fatal(tok)
		}
		return e
	}))
	_, e = b.Authenticate(ctx, "writer", &service.Token{Bytes: []byte(token)}, model.FaceCLI)
	code(t, e, model.ErrorTeamNotRunning)
	again, e := b.StopTeam(ctx, wire.StopTeamRequest{Team: "t"})
	must(t, e)
	if again.Expired != 0 {
		t.Fatal(again)
	}
}
func TestStartTeamSeam(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	out, e := b.StartTeam(ctx, wire.StartTeamRequest{Team: "t"})
	must(t, e)
	if !out.AlreadyRunning {
		t.Fatal(out)
	}
	_, e = b.StopTeam(ctx, wire.StopTeamRequest{Team: "t"})
	must(t, e)
	var before int64
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		var err error
		before, err = tx.CurrentEventSeq()
		return err
	}))
	_, e = b.StartTeam(ctx, wire.StartTeamRequest{Team: "t"})
	code(t, e, model.ErrorRunnerUnavailable)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		events, err := tx.ReplayEvents(before, ptr("t"), 0)
		if err != nil {
			return err
		}
		var transitions []wire.TeamStateChangedEventData
		for _, event := range events {
			if event.Type != model.EventTeamStateChanged {
				continue
			}
			var data wire.TeamStateChangedEventData
			if err := json.Unmarshal(event.Data, &data); err != nil {
				return err
			}
			transitions = append(transitions, data)
		}
		want := [][2]model.TeamState{{model.TeamStateStopped, model.TeamStateStarting}, {model.TeamStateStarting, model.TeamStateStopped}}
		if len(transitions) != len(want) {
			t.Fatalf("failed start transitions: %#v", transitions)
		}
		for i, transition := range transitions {
			if transition.From != want[i][0] || transition.To != want[i][1] || transition.Team.State != transition.To {
				t.Fatalf("transition %d: %#v, want %v", i, transition, want[i])
			}
		}
		return nil
	}))
	if model.AsError(e).Details["reason"] != "not_built" {
		t.Fatal(e)
	}
	s, e := b.Status(ctx, wire.StatusRequest{})
	must(t, e)
	if s.Teams[0].State != model.TeamStateStopped || s.Teams[0].Run != 2 {
		t.Fatal(s)
	}
	_, e = b.StartHerdr(ctx, wire.StartHerdrRequest{})
	code(t, e, model.ErrorHerdrUnavailable)
}
func TestAppRequestSeams(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	_, e := b.OpenTerminal(ctx, wire.OpenTerminalRequest{Team: "t", Member: "human"})
	code(t, e, model.ErrorInvalidRequest)
	_, e = b.OpenTerminal(ctx, wire.OpenTerminalRequest{Team: "t", Member: "writer", IfGeneration: ptr(int64(2))})
	code(t, e, model.ErrorLeaseConflict)
	r, e := b.OpenTerminal(ctx, wire.OpenTerminalRequest{Team: "t", Member: "writer"})
	must(t, e)
	if r.Request.Status != model.RequestStatusPending {
		t.Fatal(r)
	}
	_, e = b.StopMember(ctx, wire.StopMemberRequest{Team: "t", Member: "writer"})
	code(t, e, model.ErrorLeaseConflict)
	_, e = b.ReturnHeadless(ctx, wire.ReturnHeadlessRequest{Team: "t", Member: "reviewer"})
	code(t, e, model.ErrorLeaseConflict)
	old := model.Time{Time: time.Now().Add(-time.Minute)}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.Heartbeat("t", model.Runner{State: model.RunnerStateUp, HeartbeatAt: &old}, b.now())
	}))
	_, e = b.StopMember(ctx, wire.StopMemberRequest{Team: "t", Member: "reviewer"})
	code(t, e, model.ErrorRunnerUnavailable)
}
