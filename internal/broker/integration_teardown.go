package broker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

// Close waits for cooperative runner shutdown, then removes only this team's
// recorded processes/panes and ended launch artifacts. No process name is used.
func (i *Integration) Close(ctx context.Context, t store.Team, members []model.Member) error {
	placement, e := i.placement(t.ID)
	if e != nil {
		return e
	}
	deadline := time.Now().Add(25 * time.Second)
	for {
		var up bool
		e = i.b.db.Read(ctx, func(tx *store.Tx) error {
			team, e := tx.GetTeam(t.ID)
			if e == nil {
				up = team.Runner.State == model.RunnerStateUp && team.Runner.PID != nil && i.b.fresh(team)
			}
			return e
		})
		if e != nil {
			return e
		}
		if !up || time.Now().After(deadline) {
			break
		}
		if e = i.b.opts.Wait(ctx, 100*time.Millisecond); e != nil {
			return e
		}
	}
	// Save a Claude /clear before its launch and pointer disappear.
	for _, m := range members {
		if e = i.capturePointer(ctx, m); e != nil {
			return e
		}
	}
	helper := NewRunner(i.b, t.ID, RunnerOptions{HomeDir: i.opts.HomeDir})
	for _, m := range members {
		var turn *model.Turn
		e = i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; turn, e = tx.GetLiveTurn(m.ID); return e })
		if e != nil && !missing(e) {
			return e
		}
		if turn != nil && turn.PID != nil {
			state, e := (nativeProcesses{}).Inspect(ctx, *turn.PID)
			if e != nil {
				return e
			}
			if state.Alive {
				birth := helper.processBirth(*turn)
				if birth == "" {
					return model.NewError(model.ErrorInternal, "cannot terminate an unverified turn process").WithDetail("turn", turn.ID)
				}
				if e = cancelOwnedTree(ctx, *turn.PID, birth); e != nil {
					return e
				}
			}
		}
	}
	client := i.opts.Herdr(t.HerdrServer)
	for _, m := range members {
		if m.Lease != nil && m.Lease.Pane != nil {
			if e = client.ClosePane(ctx, *m.Lease.Pane); e != nil {
				return e
			}
		}
	}
	if placement.PID > 0 && placement.Birth != "" {
		if e = cancelOwnedTree(ctx, placement.PID, placement.Birth); e != nil {
			return e
		}
	}
	root := t.Runner.Pane
	if root == nil && placement.Pane != "" {
		root = &placement.Pane
	}
	if root != nil {
		if e = client.ClosePane(ctx, *root); e != nil {
			return e
		}
	}
	e = i.b.db.Write(ctx, func(tx *store.Tx) error {
		launches, e := tx.ListLaunches(t.ID)
		if e != nil {
			return e
		}
		for _, l := range launches {
			if l.EndedAt == nil {
				if e = tx.EndLaunch(l.ID, model.LaunchEndReasonStopped, i.b.now()); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	if e = helper.cleanLaunchFiles(ctx); e != nil {
		return e
	}
	if placement.BridgeDir != "" {
		// A sidecar is not authority to recursively remove an arbitrary host path.
		if filepath.Clean(filepath.Dir(placement.BridgeDir)) != filepath.Clean(os.TempDir()) || !strings.HasPrefix(filepath.Base(placement.BridgeDir), "lugarn-runner-") {
			return model.NewError(model.ErrorInternal, "invalid native runner bridge directory")
		}
		for until := time.Now().Add(3 * time.Second); ; {
			e = os.RemoveAll(placement.BridgeDir)
			if e == nil || time.Now().After(until) {
				break
			}
			if e = i.b.opts.Wait(ctx, 50*time.Millisecond); e != nil {
				return e
			}
		}
		if e != nil {
			return e
		}
	}
	if e = os.Remove(i.placementPath(t.ID)); e != nil && !os.IsNotExist(e) {
		return e
	}
	return nil
}
func (i *Integration) capturePointer(ctx context.Context, m model.Member) error {
	if i.opts.HomeDir == "" || m.Harness == nil || *m.Harness != model.HarnessClaude || m.Lease == nil || m.Lease.Launch == nil {
		return nil
	}
	helper := NewRunner(i.b, m.Team, RunnerOptions{HomeDir: i.opts.HomeDir})
	id, e := helper.pointerSession(m)
	if e != nil || id == "" || m.Session != nil && *m.Session == id {
		return e
	}
	seal, e := model.NewSeal()
	if e != nil {
		return e
	}
	return i.b.db.Write(ctx, func(tx *store.Tx) error {
		current, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		if current.Lease == nil || current.Lease.Generation != m.Lease.Generation {
			return errRunnerGeneration
		}
		return helper.session(tx, *current, id, seal, model.SessionSourcePointer, *m.Lease.Launch, false)
	})
}
