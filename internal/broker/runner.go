package broker

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

var errRunnerLost = model.NewError(model.ErrorLeaseConflict, "runner ownership changed")
var errRunnerGeneration = errors.New("runner member generation changed")

// Runner drives one team. Construct a new value for every run; Run is single-use.
// Cancelling its context detaches live turns; a team stop cancels them instead.
type Runner struct {
	b     *Broker
	team  string
	opts  RunnerOptions
	owner model.Runner
	used  atomic.Bool
	mu    sync.Mutex
	busy  map[string]bool
}

// NewRunner constructs a runner over b's store and transaction helpers. It does
// not acquire ownership or start processes until Run. The caller owns the store.
func NewRunner(b *Broker, team string, opts RunnerOptions) *Runner {
	d := NotBuiltRunnerDrivers{}
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
	return &Runner{b: b, team: team, opts: opts, busy: map[string]bool{}}
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
		r.owner.HeartbeatAt = &now
		previous := t.Runner.State
		if e := tx.Heartbeat(t.ID, r.owner, now); e != nil {
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

// Run acquires the team runner lease and maintains its heartbeat until ctx ends
// or the team stops. Cancellation never records completion for a live turn.
func (r *Runner) Run(ctx context.Context) error {
	if !r.used.CompareAndSwap(false, true) {
		return invalid("Runner.Run is single-use")
	}
	if e := r.acquire(ctx); e != nil {
		return e
	}
	tick := time.NewTicker(r.opts.PollInterval)
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if time.Since(last) >= r.opts.HeartbeatInterval {
				if e := r.heartbeat(ctx); e != nil {
					if ctx.Err() != nil {
						return nil
					}
					return e
				}
				last = time.Now()
			}
			var stop bool
			e := r.b.db.Read(ctx, func(tx *store.Tx) error {
				t, e := tx.GetTeam(r.team)
				if e != nil {
					return e
				}
				if !r.owns(t) {
					return errRunnerLost
				}
				stop = t.State == model.TeamStateStopping || t.State == model.TeamStateStopped
				return nil
			})
			if e != nil {
				if ctx.Err() != nil {
					return nil
				}
				return e
			}
			if stop {
				return nil
			}
		}
	}
}
