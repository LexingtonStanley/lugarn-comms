package broker

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (i *Integration) launchHC(ctx context.Context, t store.Team) error {
	c, e := i.hcClient(ctx, t.HerdrServer)
	if e != nil {
		return e
	}
	defer c.Close()
	if t.HCCoreID == nil || *t.HCCoreID != c.Hello.Core.ID {
		return model.NewError(model.ErrorLeaseConflict, "hc core changed during start")
	}
	terminals, e := c.List(ctx, nil)
	if e != nil {
		return e
	}
	space := ""
	if t.HerdrWorkspace != nil {
		for _, v := range terminals {
			if v.State == "live" && v.Labels["herdr.space"] == *t.HerdrWorkspace {
				space = *t.HerdrWorkspace
				break
			}
		}
	}
	if t.Adopted && t.HerdrWorkspace != nil {
		space = *t.HerdrWorkspace
	}
	if space == "" {
		space, e = hcID("sp")
		if e != nil {
			return e
		}
	}
	tab, e := hcID("tb")
	if e != nil {
		return e
	}
	labels := i.hcLabels(t.ID, "runner")
	if !t.Adopted {
		labels["herdr.space"] = space
		labels["herdr.tab"] = tab
	}
	cwd, e := i.hcRunnerFolder(t)
	if e != nil {
		return e
	}
	binary, dir, e := i.runnerBridge()
	if e != nil {
		return e
	}
	// Commit the space before starting the runner; it may acquire immediately.
	if e = i.b.db.Write(ctx, func(tx *store.Tx) error {
		current, e := tx.GetTeam(t.ID)
		if e != nil {
			return e
		}
		if current.Run != t.Run || current.State != model.TeamStateStarting {
			return errRunnerLost
		}
		return tx.UpdateTeam(t.ID, store.TeamPatch{HerdrWorkspace: ptr(&space)}, i.b.now())
	}); e != nil {
		return e
	}
	terminal, e := c.Run(ctx, hc.RunOptions{Argv: []string{binary, "run", "--team", t.ID}, Cwd: cwd, Env: i.hcEnvironment(i.rootEnv()), Labels: labels})
	if e != nil {
		return e
	}
	if terminal.PID == nil {
		return model.NewError(model.ErrorRunnerUnavailable, "hc runner has no pid")
	}
	record := store.HCTerminal{Team: t.ID, Core: c.Hello.Core.ID, Address: terminal.Address, PID: *terminal.PID, Space: space, Tab: tab}
	if e = i.b.db.Write(ctx, func(tx *store.Tx) error { return tx.PutHCTerminal(record) }); e != nil {
		_ = i.hcClose(context.WithoutCancel(ctx), c, record)
		return e
	}
	if e = i.savePlacement(t.ID, runnerPlacement{Workspace: space, Pane: terminal.Address, BridgeDir: dir, Binary: binary}); e != nil {
		return e
	}
	waiting, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for {
		current, e := i.team(waiting, t.ID)
		if e != nil {
			return e
		}
		if current.Runner.State == model.RunnerStateUp && current.Runner.Pane != nil && *current.Runner.Pane == terminal.Address && i.b.fresh(current) {
			return nil
		}
		if e = i.b.opts.Wait(waiting, 50*time.Millisecond); e != nil {
			return model.NewError(model.ErrorRunnerUnavailable, "hc runner did not heartbeat").WithDetail("cause", "runner_no_heartbeat")
		}
	}
}
func (l *integrationLifecycle) openHC(ctx context.Context, m model.Member, starting *model.Member, t store.Team, launch harness.Launch, binary string, args []string, cause model.LeaseCause) error {
	c, e := l.i.hcClient(ctx, t.HerdrServer)
	if e != nil {
		return e
	}
	defer c.Close()
	if t.HCCoreID == nil || *t.HCCoreID != c.Hello.Core.ID {
		return model.NewError(model.ErrorLeaseConflict, "hc core changed")
	}
	if launch.Harness == model.HarnessCodex {
		args = hcCodexArgs(ctx, binary, args)
	}

	env, e := harness.Environment(launch)
	if e != nil {
		return e
	}
	// terminal.run merges env and has no unset operation. Empty content is
	// absent to OpenCode, so it cannot override the launch config file.
	if launch.Harness == model.HarnessOpenCode {
		env["OPENCODE_CONFIG_CONTENT"] = ""
	}
	space := *t.HerdrWorkspace
	if t.Placement == "workspaces" {
		space, e = hcID("sp")
		if e != nil {
			return e
		}
	}
	tab, e := hcID("tb")
	if e != nil {
		return e
	}
	if m.External != nil && t.Placement == "tabs" {
		old, err := l.i.hcRecord(ctx, m)
		if err != nil {
			return err
		}
		tab = old.Tab
	}
	labels := l.i.hcLabels(t.ID, "member")
	labels["lugarn.member"] = m.ID
	labels["lugarn.launch"] = launch.LaunchID
	labels["herdr.space"] = space
	labels["herdr.tab"] = tab
	terminal, e := c.Run(ctx, hc.RunOptions{Argv: append([]string{binary}, args...), Cwd: launch.WorkDir, Env: l.i.hcEnvironment(env), Labels: labels, Name: starting.HerdrName})
	if e != nil {
		if hc.IsKind(e, "name_taken") {
			return model.NewError(model.ErrorNameTaken, "hc name is held by a live terminal").WithDetail("name", *starting.HerdrName)
		}
		return e
	}
	if terminal.PID == nil {
		return model.NewError(model.ErrorLaunchFailed, "hc terminal has no pid")
	}
	record := store.HCTerminal{Team: t.ID, Core: c.Hello.Core.ID, Address: terminal.Address, Member: &m.ID, Launch: &launch.LaunchID, PID: *terminal.PID, Space: space, Tab: tab}
	// Persist before the lease handoff so stop/reconciliation can clean a failed launch.
	if e = l.i.b.db.Write(ctx, func(tx *store.Tx) error { return tx.PutHCTerminal(record) }); e != nil {
		_ = l.i.hcClose(context.WithoutCancel(ctx), c, record)
		return e
	}
	success := false
	defer func() {
		if !success {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = l.i.hcClose(cleanup, c, record)
		}
	}()
	e = l.r.write(ctx, starting, func(tx *store.Tx, _ *store.Team) error {
		if e := tx.UpdateMember(m.ID, store.MemberPatch{HerdrWorkspace: ptr(&space)}, l.i.b.now()); e != nil {
			return e
		}
		next := *starting.Lease
		next.Pane = &terminal.Address
		if e := tx.CompareAndSwapLease(m.ID, next.Driver, next.Generation, next, l.i.b.now()); e != nil {
			return e
		}
		if e := l.r.memberState(tx, *starting, model.MemberStateRunning, nil); e != nil {
			return e
		}
		if e := l.r.setActivity(tx, *starting, terminal.Activity()); e != nil {
			return e
		}
		updated, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		return l.i.b.event(tx, &m.Team, model.EventLeaseChanged, true, &m.ID, nil, wire.LeaseChangedEventData{Member: *updated, From: m.Lease.Driver, To: model.DriverPane, Generation: updated.Lease.Generation, Cause: cause})
	})
	success = e == nil
	return e
}

func hcCodexArgs(ctx context.Context, binary string, args []string) []string {
	// The shared launch table already adds this flag for 0.9. Rebuild it once
	// for hc using the installed binary's help; never duplicate a clap flag.
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != "--no-daemon" {
			out = append(out, arg)
		}
	}
	help, done := context.WithTimeout(ctx, 5*time.Second)
	defer done()
	cmd := exec.CommandContext(help, binary, "--help")
	cmd.Env = hc.CleanEnv(os.Environ())
	raw, err := cmd.Output()
	if err == nil && strings.Contains(string(raw), "--no-daemon") {
		out = append([]string{"--no-daemon"}, out...)
	}
	return out
}
