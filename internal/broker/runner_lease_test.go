package broker

import (
	"context"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func runnerReady(t *testing.T, b *Broker) {
	t.Helper()
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		if e := tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStarting)}, b.now()); e != nil {
			return e
		}
		return tx.Heartbeat("t", model.Runner{State: model.RunnerStateDown}, b.now())
	}))
}
func TestRunnerLeaseAndHeartbeat(t *testing.T) {
	b, _ := setup(t)
	runnerReady(t, b)
	ctx := context.Background()
	r := NewRunner(b, "t", RunnerOptions{PID: 101, Pane: ptr("pane")})
	must(t, r.acquire(ctx))
	other := NewRunner(b, "t", RunnerOptions{PID: 102})
	code(t, other.acquire(ctx), model.ErrorLeaseConflict)
	must(t, r.heartbeat(ctx))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		team, e := tx.GetTeam("t")
		if e != nil {
			return e
		}
		if team.State != model.TeamStateRunning || *team.Runner.PID != 101 || *team.Runner.Pane != "pane" {
			t.Fatal(team)
		}
		es, e := tx.ReplayEvents(0, ptr("t"), 0)
		if len(es) != 2 {
			t.Fatal(es)
		}
		return e
	}))
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		now := model.Time{Time: b.now().Add(time.Second)}
		return tx.Heartbeat("t", model.Runner{State: model.RunnerStateUp, PID: ptr(int64(102)), StartedAt: &now, HeartbeatAt: &now}, now)
	}))
	code(t, r.heartbeat(ctx), model.ErrorLeaseConflict)
}
func TestRunnerRefusesStoppedTeam(t *testing.T) {
	b, _ := setup(t)
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopped)}, b.now())
	}))
	code(t, NewRunner(b, "t", RunnerOptions{}).Run(context.Background()), model.ErrorTeamNotRunning)
}

func TestRunnerAcquiresImmediatelyAfterStopTeam(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	now := b.now().Time
	b.opts.Clock = func() time.Time { return now }
	_, err := b.StopTeam(ctx, wire.StopTeamRequest{Team: "t"})
	must(t, err)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		team, err := tx.GetTeam("t")
		if err != nil {
			return err
		}
		if team.Runner.State != model.RunnerStateDown || !b.fresh(team) {
			t.Fatal("test requires a down runner with a fresh retained heartbeat")
		}
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStarting)}, b.now())
	}))
	must(t, NewRunner(b, "t", RunnerOptions{PID: 202}).acquire(ctx))
}
