package broker

import (
	"context"
	"encoding/json"
	"errors"
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

func TestRunnerAppStopCancelsRunningTurn(t *testing.T) {
	b, _ := setup(t)
	runnerReady(t, b)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	driver := &headlessStub{started: make(chan turnStart, 2)}
	r := NewRunner(b, "t", RunnerOptions{Headless: driver, Files: fakeLaunchFiles{}, Processes: &fakeProcesses{alive: true, birth: "fake"}, Binary: func(model.Harness) (string, error) { return "fake", nil }, PollInterval: 5 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond})
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	eventually(t, func() bool {
		var ready bool
		must(t, b.db.Read(ctx, func(tx *store.Tx) error {
			m, e := tx.GetMember("writer")
			if e == nil {
				ready = m.Lease.Driver == model.DriverHeadless && m.State == model.MemberStateRunning
			}
			return e
		}))
		return ready
	})
	_, e := b.Send(ctx, wire.SendRequest{Team: "t", To: ptr("writer"), Text: "start work"})
	must(t, e)
	var start turnStart
	select {
	case start = <-driver.started:
	case <-ctx.Done():
		t.Fatal("turn never started")
	}
	_, e = b.StopTeam(ctx, wire.StopTeamRequest{Team: "t"})
	must(t, e)
	select {
	case e = <-done:
		must(t, e)
	case <-ctx.Done():
		t.Fatal("runner failed to exit after app stop")
	}
	start.handle.mu.Lock()
	cancelled := start.handle.cancelled
	start.handle.mu.Unlock()
	if cancelled == 0 {
		t.Fatal("app stop did not cancel the process")
	}
}

type requestExecutorFunc func(context.Context, model.Request) error

func (f requestExecutorFunc) Execute(ctx context.Context, req model.Request) error {
	return f(ctx, req)
}
func TestRunnerPendingRequestDoesNotBlockOtherRecipient(t *testing.T) {
	driver := &headlessStub{fn: func(s turnStart) {
		s.handle.complete(TurnOutcome{Acknowledged: true, Session: ptr("thread"), ExitCode: ptr(int64(0))})
	}}
	b, r, m := headlessFixture(t, driver)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, e := b.StopMember(ctx, wire.StopMemberRequest{Team: "t", Member: "reviewer"})
	must(t, e)
	entered := make(chan struct{})
	r.opts.Requests = requestExecutorFunc(func(ctx context.Context, req model.Request) error {
		if req.Status != model.RequestStatusRunning {
			return errors.New("executor received a non-running request")
		}
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	_, e = b.Send(ctx, wire.SendRequest{Team: "t", To: ptr(m.ID), Text: "unrelated task"})
	must(t, e)
	stop, e := r.pass(ctx)
	must(t, e)
	if stop {
		t.Fatal("runner unexpectedly stopped")
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("request wasn't dispatched")
	}
	eventually(t, func() bool {
		var done bool
		must(t, b.db.Read(ctx, func(tx *store.Tx) error {
			turns, e := tx.ListTurns(m.ID, 1)
			if e == nil {
				done = len(turns) > 0 && turns[0].Status == model.TurnStatusSucceeded
			}
			return e
		}))
		return done
	})
	cancel()
	r.workers.Wait()
}

func TestRunnerStoppingMakesInFlightDeliveryUncertain(t *testing.T) {
	driver := &headlessStub{started: make(chan turnStart, 1)}
	b, r, m := headlessFixture(t, driver)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	message := sendToWriter(t, b, "task")
	workCtx, stop := context.WithCancel(ctx)
	r.worker(workCtx, m.ID, func(ctx context.Context) error { return r.runTurn(ctx, m) })
	select {
	case <-driver.started:
	case <-ctx.Done():
		t.Fatal("turn never started")
	}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopping)}, b.now())
	}))
	r.stopping.Store(true)
	stop()
	must(t, r.stop(ctx))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		delivery, e := tx.GetDelivery(message.ID, m.ID)
		if e != nil {
			return e
		}
		if delivery.State != model.DeliveryStateUncertain || delivery.Reason == nil || *delivery.Reason != model.DeliveryReasonTurnCancelled {
			t.Fatal(delivery)
		}
		turns, e := tx.ListTurns(m.ID, 1)
		if e == nil && turns[0].Status != model.TurnStatusCancelled {
			t.Fatal(turns)
		}
		return e
	}))
}
