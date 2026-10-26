package broker

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func TestTwoRunnersSeparateStores(t *testing.T) {
	b, _ := setup(t)
	runnerReady(t, b)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db2, e := store.Open(ctx, filepath.Join(b.opts.StateDir, "comms.db"))
	must(t, e)
	defer db2.Close()
	b2 := New(db2, Options{StateDir: b.opts.StateDir, Host: fakeHost{}, Probe: fakeProbe{}})
	opts := RunnerOptions{PID: 101, Files: fakeLaunchFiles{}, Processes: &fakeProcesses{alive: false}, PollInterval: 20 * time.Millisecond, HeartbeatInterval: 100 * time.Millisecond, Binary: func(model.Harness) (string, error) { return "fake", nil }}
	r1 := NewRunner(b, "t", opts)
	firstCtx, stopFirst := context.WithCancel(ctx)
	defer stopFirst()
	firstDone := make(chan error, 1)
	go func() { firstDone <- r1.Run(firstCtx) }()
	eventually(t, func() bool {
		var ready bool
		must(t, b2.db.Read(ctx, func(tx *store.Tx) error {
			team, e := tx.GetTeam("t")
			if e != nil {
				return e
			}
			m, e := tx.GetMember("writer")
			if e != nil {
				return e
			}
			ready = team.Runner.HeartbeatAt != nil && team.Runner.StartedAt != nil && team.Runner.HeartbeatAt.After(team.Runner.StartedAt.Time) && m.State == model.MemberStateRunning && m.Lease.Driver == model.DriverHeadless
			return nil
		}))
		return ready
	})
	secondOpts := opts
	secondOpts.PID = 202
	r2 := NewRunner(b2, "t", secondOpts)
	code(t, r2.Run(ctx), model.ErrorLeaseConflict)
	stopFirst()
	must(t, <-firstDone)
	// A detached turn is discovered through the second store after the old
	// runner's heartbeat expires. It must never be replayed or auto-replied.
	message := sendToWriter(t, b, "orphan task")
	var m *model.Member
	must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("writer"); return e }))
	work, e := r1.prepareTurn(ctx, *m)
	must(t, e)
	if work == nil {
		t.Fatal("failed to reserve orphan turn")
	}
	old := model.Time{Time: time.Now().Add(-time.Minute)}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		team, e := tx.GetTeam("t")
		if e != nil {
			return e
		}
		runner := team.Runner
		runner.HeartbeatAt = &old
		return tx.Heartbeat("t", runner, b.now())
	}))
	third := NewRunner(b2, "t", secondOpts)
	thirdCtx, stopThird := context.WithCancel(ctx)
	defer stopThird()
	thirdDone := make(chan error, 1)
	go func() { thirdDone <- third.Run(thirdCtx) }()
	eventually(t, func() bool {
		var lost bool
		must(t, b.db.Read(ctx, func(tx *store.Tx) error {
			turn, e := tx.GetTurn(work.turn.ID)
			if e != nil {
				return e
			}
			lost = turn.Status == model.TurnStatusLost
			return nil
		}))
		return lost
	})
	code(t, r1.heartbeat(ctx), model.ErrorLeaseConflict)
	code(t, r1.finishTurn(ctx, work, TurnOutcome{Acknowledged: true, Session: ptr("late"), ExitCode: ptr(int64(0)), Text: "must not land"}, false), model.ErrorLeaseConflict)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		team, e := tx.GetTeam("t")
		if e != nil {
			return e
		}
		if *team.Runner.PID != 202 {
			t.Fatal(team)
		}
		delivery, e := tx.GetDelivery(message.ID, "writer")
		if e != nil {
			return e
		}
		if delivery.State != model.DeliveryStateUncertain || *delivery.Reason != model.DeliveryReasonRunnerRestart {
			t.Fatal(delivery)
		}
		turn, e := tx.GetTurn(work.turn.ID)
		if e == nil && len(turn.AutoReplies) != 0 {
			t.Fatal(turn)
		}
		return e
	}))
	stopThird()
	must(t, <-thirdDone)
}
