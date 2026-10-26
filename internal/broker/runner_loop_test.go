package broker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("condition did not become true")
		case <-tick.C:
		}
	}
}
func TestRunnerHoldsLapseAndRelease(t *testing.T) {
	for _, release := range []bool{false, true} {
		t.Run(map[bool]string{false: "lapse", true: "release"}[release], func(t *testing.T) {
			b, c := setup(t)
			ctx := context.Background()
			now := b.now().Time
			b.opts.Clock = func() time.Time { return now }
			msg, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "held"})
			must(t, e)
			_, e = b.Hold(ctx, wire.HoldRequest{Team: "t", Member: "reviewer", TTLSeconds: ptr(int64(10))})
			must(t, e)
			runnerReady(t, b)
			r := NewRunner(b, "t", RunnerOptions{})
			must(t, r.acquire(ctx))
			var m *model.Member
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("reviewer"); return e }))
			active, e := r.syncHolds(ctx, *m)
			must(t, e)
			if !active {
				t.Fatal("hold not active")
			}
			if release {
				must(t, b.db.Write(ctx, func(tx *store.Tx) error { return tx.ReleaseHold(m.ID, b.now()) }))
			} else {
				now = now.Add(11 * time.Second)
			}
			active, e = r.syncHolds(ctx, *m)
			must(t, e)
			if active {
				t.Fatal("hold still active")
			}
			must(t, b.db.Read(ctx, func(tx *store.Tx) error {
				d, e := tx.GetDelivery(msg.ID, m.ID)
				if e == nil && d.State != model.DeliveryStateQueued {
					t.Fatal(d)
				}
				return e
			}))
		})
	}
}
func TestRunnerDefaultRequestStatusEvents(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	request, e := b.StopMember(ctx, wire.StopMemberRequest{Team: "t", Member: "writer"})
	must(t, e)
	runnerReady(t, b)
	r := NewRunner(b, "t", RunnerOptions{})
	must(t, r.acquire(ctx))
	must(t, r.executeRequest(ctx, request.Request))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		v, e := tx.GetRequest(request.Request.ID)
		if e != nil {
			return e
		}
		if v.Status != model.RequestStatusFailed || v.Error.Details["reason"] != "not_built" {
			t.Fatal(v)
		}
		es, e := tx.ReplayEvents(0, ptr("t"), 0)
		if e != nil {
			return e
		}
		statuses := []model.RequestStatus{}
		for _, ev := range es {
			if ev.Type != model.EventRequestChanged {
				continue
			}
			var d wire.RequestChangedEventData
			if e = json.Unmarshal(ev.Data, &d); e != nil {
				return e
			}
			statuses = append(statuses, d.Request.Status)
		}
		if len(statuses) != 3 || statuses[1] != model.RequestStatusRunning || statuses[2] != model.RequestStatusFailed {
			t.Fatal(statuses)
		}
		return nil
	}))
}
func TestRunnerLoopStops(t *testing.T) {
	b, _ := setup(t)
	runnerReady(t, b)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := NewRunner(b, "t", RunnerOptions{PollInterval: 5 * time.Millisecond})
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	eventually(t, func() bool {
		var up bool
		must(t, b.db.Read(ctx, func(tx *store.Tx) error {
			team, e := tx.GetTeam("t")
			if e == nil {
				up = team.Runner.State == model.RunnerStateUp
			}
			return e
		}))
		return up
	})
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopping)}, b.now())
	}))
	select {
	case e := <-done:
		must(t, e)
	case <-ctx.Done():
		t.Fatal("runner failed to stop")
	}
}
