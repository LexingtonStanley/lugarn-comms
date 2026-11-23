package broker

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"os"
	"time"
)

func (i *Integration) closeHCMember(ctx context.Context, m model.Member) error {
	r, e := i.hcRecord(ctx, m)
	if missing(e) {
		return nil
	}
	if e != nil {
		return e
	}
	t, e := i.team(ctx, m.Team)
	if e != nil {
		return e
	}
	c, e := i.hcClient(ctx, t.HerdrServer)
	if e != nil {
		return e
	}
	defer c.Close()
	// A replaced core's addresses are gone. Never mutate the replacement.
	if c.Hello.Core.ID != r.Core {
		return nil
	}
	_, e = i.hcOwned(ctx, c, r, false)
	if e != nil && model.AsError(e).Code == model.ErrorLeaseConflict {
		return nil
	}
	return i.hcClose(ctx, c, r)
}
func (i *Integration) closeHC(ctx context.Context, t store.Team, members []model.Member) error {
	var failures teardownFailures
	record := func(step string, e error) {
		if e != nil {
			failures = append(failures, teardownStep{step, e})
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		current, e := i.team(ctx, t.ID)
		if e != nil {
			return e
		}
		if current.Runner.State != model.RunnerStateUp || !i.b.fresh(current) || time.Now().After(deadline) {
			break
		}
		if e = i.b.opts.Wait(ctx, 100*time.Millisecond); e != nil {
			return e
		}
	}
	helper := NewRunner(i.b, t.ID, RunnerOptions{HomeDir: i.opts.HomeDir})
	for _, m := range members {
		if m.External != nil {
			continue
		}
		record("capture pointer "+m.ID, i.capturePointer(ctx, m))
		var turn *model.Turn
		e := i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; turn, e = tx.GetLiveTurn(m.ID); return e })
		if e != nil && !missing(e) {
			record("read turn "+m.ID, e)
		}
		if turn != nil {
			record("cancel turn "+turn.ID, helper.cancelTurnProcess(ctx, *turn))
		}
	}
	var records []store.HCTerminal
	if e := i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; records, e = tx.HCTerminals(t.ID); return e }); e != nil {
		return e
	}
	// Connecting may start a stopped core. That is deliberate here: a restarted
	// core adopts programs its holders kept, so they can be found and stopped.
	// A failed connect is recorded like any other step, and the launch cleanup
	// below still runs, as on 0.9; stop-team then reports it and can be retried.
	if c, e := i.hcClient(ctx, t.HerdrServer); e != nil {
		record("connect to hc core", e)
	} else {
		defer c.Close()
		for _, r := range records {
			if r.External || r.Core != c.Hello.Core.ID || t.Forgotten && r.Member != nil {
				continue
			}
			_, e := i.hcOwned(ctx, c, r, false)
			if hc.IsGone(e) || e != nil && model.AsError(e).Code == model.ErrorLeaseConflict {
				continue
			}
			if e != nil {
				record("inspect hc terminal "+r.Address, e)
				continue
			}
			record("close hc terminal "+r.Address, i.hcClose(ctx, c, r))
		}
	}
	record("end hc launches", i.b.db.Write(ctx, func(tx *store.Tx) error {
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
	}))
	record("clean launch files", helper.cleanLaunchFiles(ctx))
	if e := os.Remove(i.placementPath(t.ID)); e != nil && !os.IsNotExist(e) {
		record("remove runner placement", e)
	}
	i.sweepRunnerBridges()
	if len(failures) > 0 {
		return failures
	}
	return nil
}
func (l *integrationLifecycle) exitedHC(ctx context.Context, m model.Member) error {
	reason, cause := model.StopReasonDied, model.LeaseCauseDied
	r, e := l.i.hcRecord(ctx, m)
	if e == nil {
		t, err := l.i.team(ctx, m.Team)
		if err != nil {
			return err
		}
		c, err := l.i.hcClient(ctx, t.HerdrServer)
		if err == nil {
			defer c.Close()
			v, err := l.i.hcOwned(ctx, c, r, false)
			if err == nil && v.State == "finished" && v.ExitCode != nil {
				reason, cause = model.StopReasonExited, model.LeaseCausePaneExit
			}
		}
	}
	if m.Mode != nil && *m.Mode == model.ModeHeadless {
		if e = l.i.closeHCMember(ctx, m); e != nil {
			return e
		}
		return l.toHeadless(ctx, m, cause, false)
	}
	return l.stopMember(ctx, m, reason, cause)
}
