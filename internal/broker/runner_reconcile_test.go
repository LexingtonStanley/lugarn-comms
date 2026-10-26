package broker

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type fakeProcesses struct {
	mu        sync.Mutex
	alive     bool
	birth     string
	cancelled int
}

func (p *fakeProcesses) Inspect(context.Context, int64) (ProcessState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return ProcessState{Alive: p.alive, Birth: p.birth}, nil
}
func (p *fakeProcesses) Cancel(_ context.Context, _ int64, birth string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if birth != "" && birth == p.birth {
		p.alive = false
		p.cancelled++
	}
	return nil
}

type fakeLaunchFiles struct{}

func (fakeLaunchFiles) Write(context.Context, TurnLaunch) error { return nil }
func (fakeLaunchFiles) Delete(context.Context, string) error    { return nil }
func installRunnerTurn(t *testing.T, b *Broker) (model.Turn, string) {
	t.Helper()
	ctx := context.Background()
	id, _ := model.NewULID()
	launchID, _ := launch(t, b, "writer")
	var turn model.Turn
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.CompareAndSwapLease("writer", model.DriverNone, 0, model.Lease{Driver: model.DriverHeadless, Generation: 1, Launch: &launchID}, b.now()); e != nil {
			return e
		}
		turn = model.Turn{ID: id, Team: "t", Member: model.MemberRef{ID: "writer", Name: "writer"}, Launch: launchID, LeaseGeneration: 1, Harness: model.HarnessCodex, PID: ptr(int64(333)), Status: model.TurnStatusStarting, StartedAt: b.now()}
		if e := tx.InsertTurn(store.TurnParams{Turn: turn, MemberID: "writer", Seal: ptr("7KQ2M9XA")}); e != nil {
			return e
		}
		return nil
	}))
	return turn, launchID
}
func TestRunnerReconcileDeliveriesTurnsRequestsAndFiles(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	must(t, statedir.Ensure(b.opts.StateDir))
	msg, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "in flight"})
	must(t, e)
	turn, _ := installRunnerTurn(t, b)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.UpdateDelivery(msg.ID, "reviewer", model.DeliveryStateQueued, store.DeliveryPatch{State: model.DeliveryStateDelivering, UpdatedAt: b.now()}); e != nil {
			return e
		}
		t, e := tx.GetTeam("t")
		if e != nil {
			return e
		}
		m, e := tx.GetMember("reviewer")
		if e != nil {
			return e
		}
		h, e := tx.GetLiveMember("t", "human")
		if e != nil {
			return e
		}
		req, e := b.request(tx, t, model.RequestKindStopMember, *m, *h, map[string]any{})
		if e != nil {
			return e
		}
		now := b.now()
		return tx.UpdateRequest(req.ID, req.Status, store.RequestPatch{Status: model.RequestStatusRunning, StartedAt: ptr(&now)})
	}))
	ended, _ := launch(t, b, "reviewer")
	must(t, b.db.Write(ctx, func(tx *store.Tx) error { return tx.EndLaunch(ended, model.LaunchEndReasonStopped, b.now()) }))
	unknown, e := model.NewULID()
	must(t, e)
	home := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(home, ".lugarn", "conversations"), 0700))
	for _, id := range []string{ended, unknown} {
		must(t, statedir.WriteSecret(filepath.Join(b.opts.StateDir, "launch", id+".token"), []byte("secret")))
	}
	pointer := filepath.Join(home, ".lugarn", "conversations", ended)
	must(t, os.WriteFile(pointer, []byte("pointer"), 0600))
	runnerReady(t, b)
	r := NewRunner(b, "t", RunnerOptions{Processes: &fakeProcesses{}, Files: fakeLaunchFiles{}, HomeDir: home})
	must(t, r.acquire(ctx))
	must(t, r.reconcile(ctx))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		d, e := tx.GetDelivery(msg.ID, "reviewer")
		if e != nil {
			return e
		}
		if d.State != model.DeliveryStateUncertain || *d.Reason != model.DeliveryReasonRunnerRestart {
			t.Fatal(d)
		}
		v, e := tx.GetTurn(turn.ID)
		if e != nil {
			return e
		}
		if v.Status != model.TurnStatusLost || len(v.AutoReplies) != 0 {
			t.Fatal(v)
		}
		requests, e := tx.ListOpenRequests("t")
		if len(requests) != 0 {
			t.Fatal(requests)
		}
		return e
	}))
	for _, p := range []string{pointer, filepath.Join(b.opts.StateDir, "launch", ended+".token"), filepath.Join(b.opts.StateDir, "launch", unknown+".token")} {
		if _, e := os.Stat(p); !os.IsNotExist(e) {
			t.Fatalf("not cleaned: %s: %v", p, e)
		}
	}
}
func TestRunnerReconcileLiveOrphanAndPIDReuse(t *testing.T) {
	for _, reused := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "reused"}[reused], func(t *testing.T) {
			b, _ := setup(t)
			must(t, statedir.Ensure(b.opts.StateDir))
			turn, _ := installRunnerTurn(t, b)
			p := &fakeProcesses{alive: true, birth: "original"}
			r := NewRunner(b, "t", RunnerOptions{Processes: p})
			must(t, r.recordProcess(context.Background(), turn, *turn.PID))
			if reused {
				p.birth = "replacement"
			}
			runnerReady(t, b)
			must(t, r.acquire(context.Background()))
			must(t, r.reconcile(context.Background()))
			if !reused {
				if len(r.orphans) != 1 {
					t.Fatal("live orphan wasn't retained")
				}
				p.mu.Lock()
				p.alive = false
				p.mu.Unlock()
				must(t, r.checkOrphans(context.Background(), false))
			}
			must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
				v, e := tx.GetTurn(turn.ID)
				if e != nil {
					return e
				}
				if v.Status != model.TurnStatusLost {
					t.Fatal(v)
				}
				return nil
			}))
			if p.cancelled != 0 {
				t.Fatal("killed an exited or reused process")
			}
		})
	}
}

func TestRunnerOrphanTimeoutCancelsVerifiedProcess(t *testing.T) {
	b, _ := setup(t)
	must(t, statedir.Ensure(b.opts.StateDir))
	turn, _ := installRunnerTurn(t, b)
	processes := &fakeProcesses{alive: true, birth: "orphan"}
	r := NewRunner(b, "t", RunnerOptions{Processes: processes, TurnTimeout: time.Millisecond})
	must(t, r.recordProcess(context.Background(), turn, *turn.PID))
	runnerReady(t, b)
	must(t, r.acquire(context.Background()))
	now := b.now().Add(time.Second)
	b.opts.Clock = func() time.Time { return now }
	must(t, r.reconcile(context.Background()))
	if processes.cancelled != 1 || len(r.orphans) != 0 {
		t.Fatalf("cancelled=%d, orphans=%d", processes.cancelled, len(r.orphans))
	}
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
		v, e := tx.GetTurn(turn.ID)
		if e == nil && v.Status != model.TurnStatusLost {
			t.Fatal(v)
		}
		return e
	}))
}

func TestRunnerReconcileInterruptedHeadlessLaunch(t *testing.T) {
	b, r, m := headlessFixture(t, &headlessStub{})
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.SetMemberState(m.ID, model.MemberStateStarting, nil, nil, nil, b.now())
	}))
	must(t, r.reconcile(ctx))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		v, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		if v.State != model.MemberStateStopped || v.StopReason == nil || *v.StopReason != model.StopReasonLaunchFailed || v.Lease.Driver != model.DriverNone {
			t.Fatal(v)
		}
		return nil
	}))
}
