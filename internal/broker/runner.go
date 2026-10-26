package broker

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

var errRunnerLost = model.NewError(model.ErrorLeaseConflict, "runner ownership changed")
var errRunnerGeneration = model.NewError(model.ErrorLeaseConflict, "runner member generation changed")

// Runner drives one team. Construct a new value for every run; Run is single-use.
// Cancelling its context detaches live turns; a team stop cancels them instead.
type Runner struct {
	workers      sync.WaitGroup
	stopping     atomic.Bool
	workerErrors chan error
	wake         chan struct{}
	handles      map[string]TurnHandle
	orphans      map[string]runnerOrphan
	b            *Broker
	team         string
	opts         RunnerOptions
	owner        model.Runner
	used         atomic.Bool
	mu           sync.Mutex
	busy         map[string]bool
}

// NewRunner constructs a runner over b's store and transaction helpers. It does
// not acquire ownership or start processes until Run. The caller owns the store.
func NewRunner(b *Broker, team string, opts RunnerOptions) *Runner {
	d := NotBuiltRunnerDrivers{}
	if opts.Processes == nil {
		opts.Processes = nativeProcesses{}
	}
	if opts.Headless == nil {
		opts.Headless = d
	}
	if opts.Files == nil {
		opts.Files = d
	}
	if opts.Panes == nil {
		opts.Panes = d
	}
	if opts.Requests == nil {
		opts.Requests = d
	}
	if opts.PaneLifecycle == nil {
		opts.PaneLifecycle = d
	}
	if opts.PID == 0 {
		opts.PID = int64(os.Getpid())
	}
	if opts.PollInterval <= 0 || opts.PollInterval > 250*time.Millisecond {
		opts.PollInterval = 250 * time.Millisecond
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 5 * time.Second
	}
	if opts.PaneTimeout <= 0 {
		opts.PaneTimeout = 30 * time.Second
	}
	if opts.Binary == nil {
		opts.Binary = b.opts.Probe.Binary
	}
	return &Runner{b: b, team: team, opts: opts, busy: map[string]bool{}, orphans: map[string]runnerOrphan{}, workerErrors: make(chan error, 64), wake: make(chan struct{}, 1), handles: map[string]TurnHandle{}}
}
func (r *Runner) owns(t *store.Team) bool {
	return t.Runner.PID != nil && r.owner.PID != nil && *t.Runner.PID == *r.owner.PID && t.Runner.StartedAt != nil && r.owner.StartedAt != nil && t.Runner.StartedAt.Equal(r.owner.StartedAt.Time)
}

// write fences every mutation by runner identity, and optionally member lease.
// The read/check and writes share BEGIN IMMEDIATE; no external I/O belongs here.
func (r *Runner) write(ctx context.Context, m *model.Member, fn func(*store.Tx, *store.Team) error) error {
	return r.b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.team)
		if e != nil {
			return e
		}
		if !r.owns(t) {
			return errRunnerLost
		}
		if m != nil {
			current, e := tx.GetMember(m.ID)
			if e != nil {
				return e
			}
			if m.Lease == nil || current.Lease == nil || current.Lease.Generation != m.Lease.Generation || current.Lease.Driver != m.Lease.Driver {
				return errRunnerGeneration
			}
		}
		return fn(tx, t)
	})
}
func (r *Runner) acquire(ctx context.Context) error {
	return r.b.db.Write(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.team)
		if e != nil {
			return e
		}
		if t.State != model.TeamStateStarting && t.State != model.TeamStateRunning {
			return model.NewError(model.ErrorTeamNotRunning, "runner needs a starting or running team")
		}
		if r.b.fresh(t) {
			return model.NewError(model.ErrorLeaseConflict, "another runner has a fresh heartbeat")
		}
		now := r.b.now()
		old := t.Runner.State
		r.owner = model.Runner{State: model.RunnerStateUp, PID: &r.opts.PID, Pane: r.opts.Pane, StartedAt: &now, HeartbeatAt: &now}
		if e = tx.Heartbeat(t.ID, r.owner, now); e != nil {
			return e
		}
		t.Runner = r.owner
		s, e := r.b.status(tx, t)
		if e != nil {
			return e
		}
		if e = r.b.event(tx, &t.ID, model.EventRunnerStateChanged, true, nil, nil, wire.RunnerStateChangedEventData{Team: s, From: old, To: model.RunnerStateUp}); e != nil {
			return e
		}
		if t.State == model.TeamStateStarting {
			if e = tx.UpdateTeam(t.ID, store.TeamPatch{State: ptr(model.TeamStateRunning)}, now); e != nil {
				return e
			}
			t.State = model.TeamStateRunning
			s, e = r.b.status(tx, t)
			if e != nil {
				return e
			}
			return r.b.event(tx, &t.ID, model.EventTeamStateChanged, true, nil, nil, wire.TeamStateChangedEventData{Team: s, From: model.TeamStateStarting, To: model.TeamStateRunning})
		}
		return nil
	})
}
func (r *Runner) heartbeat(ctx context.Context) error {
	return r.write(ctx, nil, func(tx *store.Tx, t *store.Team) error {
		now := r.b.now()
		owner := r.owner
		owner.HeartbeatAt = &now
		previous := t.Runner.State
		if e := tx.Heartbeat(t.ID, owner, now); e != nil {
			return e
		}
		if previous != model.RunnerStateUp {
			t.Runner = r.owner
			s, e := r.b.status(tx, t)
			if e != nil {
				return e
			}
			return r.b.event(tx, &t.ID, model.EventRunnerStateChanged, true, nil, nil, wire.RunnerStateChangedEventData{Team: s, From: previous, To: model.RunnerStateUp})
		}
		return nil
	})
}

// Run acquires the runner lease, reconciles, then schedules recipient work.
// Cancellation detaches turns; team stopping cancels them and ends headless leases.
func (r *Runner) Run(ctx context.Context) error {
	if !r.used.CompareAndSwap(false, true) {
		return invalid("Runner.Run is single-use")
	}
	if r.b.opts.StateDir == "" {
		return invalid("runner requires an explicit state directory")
	}
	if e := r.acquire(ctx); e != nil {
		return e
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatDone := make(chan struct{})
	go func() { defer close(heartbeatDone); r.heartbeats(workCtx) }()
	defer func() { cancel(); <-heartbeatDone }()
	if e := r.reconcile(workCtx); e != nil {
		if ctx.Err() != nil {
			return nil
		}
		return model.AsError(e)
	}
	tick := time.NewTicker(r.opts.PollInterval)
	defer tick.Stop()
	for {
		stop, e := r.pass(workCtx)
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return model.AsError(e)
		}
		if stop {
			r.stopping.Store(true)
			cancel()
			cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
			defer done()
			return r.stop(cleanup)
		}
		select {
		case <-ctx.Done():
			return nil
		case e := <-r.workerErrors:
			if ctx.Err() != nil {
				return nil
			}
			return model.AsError(e)
		case <-tick.C:
		case <-r.wake:
		}
	}
}
