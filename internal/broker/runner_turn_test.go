package broker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type turnStub struct {
	mu        sync.Mutex
	acks      chan TurnAcknowledgement
	out       chan TurnOutcome
	acked     bool
	session   *string
	cancelled int
	once      sync.Once
}

func newTurnStub() *turnStub {
	return &turnStub{acks: make(chan TurnAcknowledgement, 1), out: make(chan TurnOutcome, 1)}
}
func (*turnStub) PID() int64                                     { return 333 }
func (h *turnStub) Acknowledgements() <-chan TurnAcknowledgement { return h.acks }
func (h *turnStub) Wait() TurnOutcome                            { return <-h.out }
func (h *turnStub) acknowledge(id string) {
	h.mu.Lock()
	h.acked = true
	h.session = &id
	h.mu.Unlock()
	h.acks <- TurnAcknowledgement{Session: &id}
	close(h.acks)
}
func (h *turnStub) complete(o TurnOutcome) {
	h.once.Do(func() {
		h.mu.Lock()
		o.Acknowledged = o.Acknowledged || h.acked
		if o.Session == nil {
			o.Session = h.session
		}
		h.mu.Unlock()
		h.out <- o
	})
}
func (h *turnStub) Cancel() error {
	h.mu.Lock()
	h.cancelled++
	h.mu.Unlock()
	h.complete(TurnOutcome{Cancelled: true, ExitCode: ptr(int64(1))})
	return nil
}

type turnStart struct {
	launch TurnLaunch
	prompt string
	handle *turnStub
}
type headlessStub struct {
	started chan turnStart
	fn      func(turnStart)
	err     error
}

func (s *headlessStub) Start(_ context.Context, l TurnLaunch, prompt string) (TurnHandle, error) {
	if s.err != nil {
		return nil, s.err
	}
	h := newTurnStub()
	start := turnStart{l, prompt, h}
	if s.started != nil {
		s.started <- start
	}
	if s.fn != nil {
		s.fn(start)
	}
	return h, nil
}
func headlessFixture(t *testing.T, driver Headless) (*Broker, *Runner, model.Member) {
	t.Helper()
	b, _ := setup(t)
	runnerReady(t, b)
	r := NewRunner(b, "t", RunnerOptions{Headless: driver, Files: fakeLaunchFiles{}, Processes: &fakeProcesses{alive: true, birth: "fake"}, Binary: func(model.Harness) (string, error) { return "fake", nil }, PollInterval: 5 * time.Millisecond})
	must(t, r.acquire(context.Background()))
	var m *model.Member
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error { var e error; m, e = tx.GetMember("writer"); return e }))
	ready, e := r.launchHeadless(context.Background(), *m, model.LeaseCauseStart)
	must(t, e)
	return b, r, *ready
}
func sendToWriter(t *testing.T, b *Broker, text string) wire.SendOutput {
	t.Helper()
	out, e := b.ToolSend(context.Background(), service.Caller{TeamID: "t", MemberID: "reviewer"}, wire.SendInput{To: "writer", Text: text})
	must(t, e)
	return out
}
func TestRunnerTurnOutcomes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		out      TurnOutcome
		timeout  time.Duration
		want     model.TurnStatus
		delivery model.DeliveryState
	}{
		{"ack on exit", TurnOutcome{Acknowledged: true, Session: ptr("thread"), ExitCode: ptr(int64(0)), Usage: &model.Usage{InputTokens: ptr(int64(7))}}, 0, model.TurnStatusSucceeded, model.DeliveryStateDelivered},
		{"before ack", TurnOutcome{ExitCode: ptr(int64(1)), Error: "failed before ack"}, 0, model.TurnStatusFailed, model.DeliveryStateHeld},
		{"cancelled", TurnOutcome{Cancelled: true}, 0, model.TurnStatusCancelled, model.DeliveryStateUncertain},
		{"timeout", TurnOutcome{}, 10 * time.Millisecond, model.TurnStatusTimedOut, model.DeliveryStateUncertain},
	} {
		t.Run(tt.name, func(t *testing.T) {
			driver := &headlessStub{fn: func(s turnStart) {
				if tt.timeout == 0 {
					s.handle.complete(tt.out)
				}
			}}
			b, r, m := headlessFixture(t, driver)
			r.opts.TurnTimeout = tt.timeout
			msg := sendToWriter(t, b, "task")
			must(t, r.runTurn(context.Background(), m))
			must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
				ts, e := tx.ListTurns(m.ID, 10)
				if e != nil {
					return e
				}
				if len(ts) != 1 || ts[0].Status != tt.want || ts[0].PID == nil || len(ts[0].Consumed) != 1 {
					t.Fatal(ts)
				}
				d, e := tx.GetDelivery(msg.ID, m.ID)
				if e != nil {
					return e
				}
				if d.State != tt.delivery {
					t.Fatal(d)
				}
				if tt.out.Usage != nil && ts[0].Usage == nil {
					t.Fatal("missing usage")
				}
				return nil
			}))
		})
	}
}
func TestRunnerTurnAcknowledgementAndGenerationFence(t *testing.T) {
	driver := &headlessStub{started: make(chan turnStart, 1)}
	b, r, m := headlessFixture(t, driver)
	msg := sendToWriter(t, b, "task")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.runTurn(ctx, m) }()
	start := <-driver.started
	start.handle.acknowledge("thread-one")
	eventually(t, func() bool {
		var delivered bool
		must(t, b.db.Read(ctx, func(tx *store.Tx) error {
			d, e := tx.GetDelivery(msg.ID, m.ID)
			if e == nil {
				delivered = d.State == model.DeliveryStateDelivered
			}
			return e
		}))
		return delivered
	})
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.CompareAndSwapLease(m.ID, model.DriverHeadless, m.Lease.Generation, model.Lease{Driver: model.DriverNone, Generation: m.Lease.Generation + 1}, b.now())
	}))
	start.handle.complete(TurnOutcome{ExitCode: ptr(int64(0)), Text: "late reply"})
	e := <-done
	code(t, e, model.ErrorLeaseConflict)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		ts, e := tx.ListTurns(m.ID, 10)
		if e == nil && (ts[0].Status != model.TurnStatusRunning || len(ts[0].AutoReplies) != 0) {
			t.Fatal(ts)
		}
		return e
	}))
}
func TestRunnerCancellationDetachesTurn(t *testing.T) {
	driver := &headlessStub{started: make(chan turnStart, 1)}
	b, r, m := headlessFixture(t, driver)
	sendToWriter(t, b, "task")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.runTurn(ctx, m) }()
	start := <-driver.started
	cancel()
	must(t, <-done)
	start.handle.mu.Lock()
	count := start.handle.cancelled
	start.handle.mu.Unlock()
	if count != 0 {
		t.Fatal("runner cancellation killed the turn")
	}
	start.handle.complete(TurnOutcome{ExitCode: ptr(int64(0))})
}
func TestRunnerStartFailureAndHeadlessCap(t *testing.T) {
	driver := &headlessStub{err: errors.New("start failed")}
	b, r, m := headlessFixture(t, driver)
	msg := sendToWriter(t, b, "task")
	must(t, r.runTurn(context.Background(), m))
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
		d, e := tx.GetDelivery(msg.ID, m.ID)
		if e == nil && (d.State != model.DeliveryStateHeld || *d.Reason != model.DeliveryReasonDeliveryFailed) {
			t.Fatal(d)
		}
		return e
	}))
	_, e := b.ResolveDelivery(context.Background(), wire.ResolveDeliveryRequest{Team: "t", Message: msg.ID, Member: m.ID, Action: model.ResolveActionResend})
	must(t, e)
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{HeadlessTurns: ptr(int64(1))}, b.now())
	}))
	w, e := r.prepareTurn(context.Background(), m)
	must(t, e)
	if w == nil {
		t.Fatal("no turn reserved")
	}
	again, e := r.prepareTurn(context.Background(), m)
	must(t, e)
	if again != nil {
		t.Fatal("overlapping recipient turn")
	}
}
