package broker

import (
	"context"
	"errors"
	"fmt"
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
	var failures []error
	record := func(step string, e error) {
		if e != nil {
			failures = append(failures, fmt.Errorf("%s: %w", step, e))
		}
	}
	placement, e := i.placement(t.ID)
	record("read runner placement "+i.placementPath(t.ID), e)
	deadline := time.Now().Add(30 * time.Second)
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
			record("read runner state team "+t.ID, e)
			break
		}
		if !up || time.Now().After(deadline) {
			break
		}
		if e = i.b.opts.Wait(ctx, 100*time.Millisecond); e != nil {
			record("wait for runner team "+t.ID, e)
			break
		}
	}
	for _, m := range members {
		record("capture pointer member "+m.ID, i.capturePointer(ctx, m))
	}
	helper := NewRunner(i.b, t.ID, RunnerOptions{HomeDir: i.opts.HomeDir})
	for _, m := range members {
		var turn *model.Turn
		e = i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; turn, e = tx.GetLiveTurn(m.ID); return e })
		if e != nil && !missing(e) {
			record("read live turn member "+m.ID, e)
			continue
		}
		if turn != nil {
			pid := int64(0)
			if turn.PID != nil {
				pid = *turn.PID
			}
			record(fmt.Sprintf("kill turn %s tree pid %d", turn.ID, pid), helper.cancelTurnProcess(ctx, *turn))
		}
	}
	workspace := placement.Workspace
	if t.HerdrWorkspace != nil {
		workspace = *t.HerdrWorkspace
	}
	for _, m := range members {
		if m.Lease != nil && m.Lease.Pane != nil {
			record("close member "+m.ID+" pane "+*m.Lease.Pane, i.closeOwnedPane(ctx, t.HerdrServer, workspace, *m.Lease.Pane))
		}
	}
	if placement.PID > 0 && placement.Birth != "" {
		record(fmt.Sprintf("kill runner tree pid %d", placement.PID), cancelOwnedTree(ctx, placement.PID, placement.Birth))
	}
	root := t.Runner.Pane
	if root == nil && placement.Pane != "" {
		root = &placement.Pane
	}
	if root != nil {
		record("close runner pane "+*root, i.closeOwnedPane(ctx, t.HerdrServer, workspace, *root))
	}
	record("end launches team "+t.ID, i.b.db.Write(ctx, func(tx *store.Tx) error {
		launches, e := tx.ListLaunches(t.ID)
		if e != nil {
			return e
		}
		for _, l := range launches {
			if l.EndedAt == nil {
				if e = tx.EndLaunch(l.ID, model.LaunchEndReasonStopped, i.b.now()); e != nil {
					return fmt.Errorf("launch %s: %w", l.ID, e)
				}
			}
		}
		return nil
	}))
	record("remove launch and pointer files team "+t.ID+" state "+i.b.opts.StateDir, helper.cleanLaunchFiles(ctx))
	if placement.BridgeDir != "" {
		// A sidecar never authorizes removing an arbitrary host directory.
		if (filepath.Clean(filepath.Dir(placement.BridgeDir)) != filepath.Clean(os.TempDir()) && filepath.Clean(filepath.Dir(placement.BridgeDir)) != filepath.Clean(i.b.opts.StateDir)) || !strings.HasPrefix(filepath.Base(placement.BridgeDir), "lugarn-runner-") {
			record("validate bridge "+placement.BridgeDir, model.NewError(model.ErrorInternal, "invalid native runner bridge directory"))
		} else {
			for until := time.Now().Add(3 * time.Second); ; {
				e = os.RemoveAll(placement.BridgeDir)
				if e == nil || time.Now().After(until) {
					break
				}
				if waitErr := i.b.opts.Wait(ctx, 50*time.Millisecond); waitErr != nil {
					record("wait to remove bridge "+placement.BridgeDir, waitErr)
					break
				}
			}
			record("remove bridge "+placement.BridgeDir, e)
		}
	}
	if e = os.Remove(i.placementPath(t.ID)); e != nil && !os.IsNotExist(e) {
		record("remove runner placement "+i.placementPath(t.ID), e)
	}
	return errors.Join(failures...)
}
func (i *Integration) capturePointer(ctx context.Context, m model.Member) error {
	if i.opts.HomeDir == "" || m.Harness == nil || *m.Harness != model.HarnessClaude || m.Lease == nil || m.Lease.Launch == nil {
		return nil
	}
	helper := NewRunner(i.b, m.Team, RunnerOptions{HomeDir: i.opts.HomeDir})
	id, e := helper.pointerSession(m)
	if e != nil || id == "" {
		return e
	}
	if e = i.b.rememberOrigin(m, id); e != nil {
		return e
	}
	if m.Session != nil && *m.Session == id {
		return nil
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
