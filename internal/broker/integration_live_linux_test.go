//go:build linux

package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type liveIntegration struct {
	b                     *Broker
	integration           *Integration
	binary, session, repo string
	ctx                   context.Context
	client                *herdr.Client
}

func (f *liveIntegration) member(t *testing.T, team, name string) model.Member {
	t.Helper()
	var m *model.Member
	must(t, f.b.db.Read(f.ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetLiveMember(team, name); return e }))
	return *m
}
func (f *liveIntegration) wait(t *testing.T, label string, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-f.ctx.Done():
			t.Fatalf("%s: %v", label, f.ctx.Err())
		case <-deadline.C:
			t.Fatalf("%s timed out", label)
		case <-tick.C:
		}
	}
}
func (f *liveIntegration) command(t *testing.T, m model.Member, args ...string) []byte {
	t.Helper()
	argv := append([]string{"--member", m.ID, "--token-file", filepath.Join(f.b.opts.StateDir, "launch", *m.Lease.Launch+".token")}, args...)
	cmd := exec.CommandContext(f.ctx, f.binary, argv...)
	cmd.Env = append(os.Environ(), "LUGARN_COMMS_HOME="+f.b.opts.StateDir, "LUGARN_COMMS_POINTER_HOME="+f.integration.opts.HomeDir)
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	raw, e := cmd.Output()
	t.Logf("lugarn-comms --member %s --token-file <launch/%s.token> %q -> %s (stderr=%s)", m.ID, *m.Lease.Launch, args, raw, &diagnostic)
	if e != nil {
		t.Fatalf("agent command: %v", e)
	}
	return raw
}
func (f *liveIntegration) applyStart(t *testing.T, id string, members []model.MemberSpec, hop int, grid []model.GridEdge) {
	t.Helper()
	spec := model.TeamSpec{Version: 1, ID: id, Name: id, Pattern: model.PatternCustom, Herdr: &model.HerdrSettings{Server: &f.session}, Limits: &model.Limits{HopBudget: &hop, ReviewRounds: ptr(100), TurnTimeoutSeconds: ptr(60)}, Members: members, Grid: grid}
	_, e := f.b.ApplyTeam(f.ctx, wire.ApplyTeamRequest{Spec: spec})
	must(t, e)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, e := f.b.StopTeam(cleanup, wire.StopTeamRequest{Team: id})
		if e != nil {
			t.Errorf("cleanup stop-team %s: %v", id, e)
		}
	})
	out, e := f.b.StartTeam(f.ctx, wire.StartTeamRequest{Team: id})
	must(t, e)
	t.Logf("app start-team %s: state=%s runner=%v pane=%v", id, out.Team.State, out.Team.Runner.PID, out.Team.Runner.Pane)
}
func (f *liveIntegration) request(t *testing.T, req model.Request) model.Request {
	t.Helper()
	var current *model.Request
	f.wait(t, "request "+string(req.Kind), 90*time.Second, func() bool {
		must(t, f.b.db.Read(f.ctx, func(tx *store.Tx) error { var e error; current, e = tx.GetRequest(req.ID); return e }))
		return current.Status == model.RequestStatusSucceeded || current.Status == model.RequestStatusFailed
	})
	raw, _ := json.Marshal(current)
	t.Logf("request result %s", raw)
	return *current
}
func (f *liveIntegration) reportTurns(t *testing.T, team string) {
	t.Helper()
	must(t, f.b.db.Read(f.ctx, func(tx *store.Tx) error {
		ms, e := tx.ListMembers(team, false)
		if e != nil {
			return e
		}
		for _, m := range ms {
			turns, e := tx.ListTurns(m.ID, 20)
			if e != nil {
				return e
			}
			for _, turn := range turns {
				raw, _ := json.Marshal(turn)
				t.Logf("turn %s", raw)
			}
		}
		return nil
	}))
}
func (f *liveIntegration) stopAndCheck(t *testing.T, team string) {
	t.Helper()
	var launches []store.Launch
	var panes []string
	var pids []int64
	must(t, f.b.db.Read(f.ctx, func(tx *store.Tx) error {
		var e error
		launches, e = tx.ListLaunches(team)
		if e != nil {
			return e
		}
		ms, e := tx.ListMembers(team, true)
		if e != nil {
			return e
		}
		for _, m := range ms {
			turns, e := tx.ListTurns(m.ID, 20)
			if e != nil {
				return e
			}
			for _, turn := range turns {
				if turn.PID != nil {
					pids = append(pids, *turn.PID)
				}
			}
			if m.Lease != nil && m.Lease.Pane != nil {
				panes = append(panes, *m.Lease.Pane)
			}
		}
		state, e := tx.GetTeam(team)
		if e == nil && state.Runner.PID != nil {
			pids = append(pids, *state.Runner.PID)
		}
		if e == nil && state.Runner.Pane != nil {
			panes = append(panes, *state.Runner.Pane)
		}
		return e
	}))
	births := map[int64]string{}
	for _, pid := range pids {
		state, e := (nativeProcesses{}).Inspect(f.ctx, pid)
		must(t, e)
		if state.Alive {
			births[pid] = state.Birth
		}
	}
	out, e := f.b.StopTeam(f.ctx, wire.StopTeamRequest{Team: team})
	must(t, e)
	if out.Team.State != model.TeamStateStopped {
		t.Fatal(out)
	}
	f.wait(t, "owned processes to exit", 5*time.Second, func() bool {
		for pid, birth := range births {
			state, e := (nativeProcesses{}).Inspect(f.ctx, pid)
			must(t, e)
			if state.Alive && state.Birth == birth {
				return false
			}
		}
		return true
	})
	t.Logf("verified %d recorded live process identities exited", len(births))
	for _, pane := range panes {
		exists, e := f.client.PaneExists(f.ctx, pane)
		must(t, e)
		if exists {
			t.Fatalf("owned pane %s survived stop", pane)
		}
	}
	for _, launch := range launches {
		for _, suffix := range []string{"", ".token", ".json", ".settings.json", ".opencode.json"} {
			if _, e := os.Lstat(filepath.Join(f.b.opts.StateDir, "launch", launch.ID+suffix)); !os.IsNotExist(e) {
				t.Fatalf("launch artifact remains: %s%s: %v", launch.ID, suffix, e)
			}
		}
		if _, e := os.Lstat(filepath.Join(f.integration.opts.HomeDir, ".lugarn", "conversations", launch.ID)); !os.IsNotExist(e) {
			t.Fatalf("pointer remains: %s", launch.ID)
		}
	}
	t.Logf("app stop-team %s: stopped; %d launch ids and %d owned panes clean", team, len(launches), len(panes))
}
func liveMember(id, name string, h model.Harness, folder string, mode model.Mode) model.MemberSpec {
	return model.MemberSpec{ID: id, Name: name, Harness: h, Role: model.RoleWorker, Folder: folder, Profile: model.ProfileReadOnly, Mode: mode, Brief: "Reply with only the word ok. Do not use tools or change files."}
}

func TestIntegrationLiveFlows(t *testing.T) {
	if os.Getenv("LUGARN_INTEGRATION_LIVE") != "1" {
		t.Skip("opt in to tiny live model runs using the person's own config")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	herdrBin, e := exec.LookPath("herdr")
	must(t, e)
	binary := filepath.Join(t.TempDir(), "lugarn-comms")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/lugarn-comms")
	raw, e := build.CombinedOutput()
	if e != nil {
		t.Fatalf("build: %s %v", raw, e)
	}
	state := t.TempDir()
	must(t, statedir.Ensure(state))
	t.Setenv("LUGARN_COMMS_HOME", state)
	pointerHome := t.TempDir()
	t.Setenv("LUGARN_COMMS_POINTER_HOME", pointerHome)
	// HOME, XDG config homes, and provider authentication are deliberately inherited.
	db, e := store.Open(ctx, filepath.Join(state, "comms.db"))
	must(t, e)
	defer db.Close()
	b := New(db, Options{StateDir: state, Probe: hostprobe.New()})
	session := privateIntegrationSession(t)
	client := herdr.New(herdrBin, session)
	status, e := client.Status(ctx)
	must(t, e)
	if status.Running {
		t.Fatal("fresh session already exists; refusing to touch it")
	}
	integration, e := NewIntegration(b, IntegrationOptions{Binary: binary, HerdrBinary: herdrBin, HomeDir: pointerHome, Models: map[model.Harness]string{model.HarnessClaude: "haiku", model.HarnessCodex: "gpt-6-luna", model.HarnessOpenCode: "opencode-go/iceman-contributor-free"}})
	must(t, e)
	integration.Attach()
	started, e := b.StartHerdr(ctx, wire.StartHerdrRequest{Server: &session})
	must(t, e)
	t.Logf("app start-herdr server=%s started=%v method=%v", session, started.Started, started.Method)
	defer stopIntegrationServer(t, herdrBin, session, 0)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	must(t, os.Mkdir(repo, 0700))
	git := func(args ...string) {
		t.Helper()
		out, e := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("git: %s: %v", out, e)
		}
	}
	git("init")
	git("symbolic-ref", "HEAD", "refs/heads/main")
	git("-c", "user.name=Lugarn test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial")
	f := &liveIntegration{b: b, integration: integration, binary: binary, session: session, repo: repo, ctx: ctx, client: client}
	t.Run("a-headless-budget", func(t *testing.T) {
		writer := liveMember("live-writer", "writer", model.HarnessClaude, repo, model.ModeHeadless)
		writer.Role = model.RoleWriter
		reviewer := liveMember("live-reviewer", "reviewer", model.HarnessCodex, repo, model.ModeHeadless)
		reviewer.Role = model.RoleReviewer
		f.applyStart(t, "live-a", []model.MemberSpec{writer, reviewer}, 1, []model.GridEdge{{From: "writer", To: "reviewer", Can: []model.GridCapability{model.GridCapabilityMessage}}, {From: "reviewer", To: "writer", Can: []model.GridCapability{model.GridCapabilityMessage}}})
		f.wait(t, "headless launch", 20*time.Second, func() bool {
			return f.member(t, "live-a", "writer").State == model.MemberStateRunning && f.member(t, "live-a", "reviewer").State == model.MemberStateRunning
		})
		f.command(t, f.member(t, "live-a", "writer"), "send", "reviewer", "Reply with the word ok.")
		waitHeld := func() {
			t.Helper()
			f.wait(t, "hop budget hold", 90*time.Second, func() bool {
				held, failed := false, false
				must(t, b.db.Read(ctx, func(tx *store.Tx) error {
					items, e := tx.ListTeamDeliveries("live-a", model.DeliveryStateHeld)
					if e != nil {
						return e
					}
					for _, item := range items {
						if item.Delivery.Reason != nil && *item.Delivery.Reason == model.DeliveryReasonBudget {
							held = true
						} else {
							failed = true
						}
					}
					return nil
				}))
				if failed {
					f.reportTurns(t, "live-a")
					t.Fatal("a turn failed before reaching the budget hold")
				}
				return held
			})
		}
		waitHeld()
		f.reportTurns(t, "live-a")
		continued, e := b.ContinueBudget(ctx, wire.ContinueBudgetRequest{Team: "live-a", Amount: ptr(int64(1))})
		must(t, e)
		if continued.Released != 1 {
			t.Fatal(continued)
		}
		t.Logf("app continue-budget: released=%d allowance=%d", continued.Released, continued.Hops.Allowance)
		waitHeld()
		f.reportTurns(t, "live-a")
		f.stopAndCheck(t, "live-a")
	})
	t.Run("b-opencode-pane", func(t *testing.T) {
		f.applyStart(t, "live-b", []model.MemberSpec{liveMember("live-opencode", "worker", model.HarnessOpenCode, repo, model.ModePane)}, 2, []model.GridEdge{})
		var member model.Member
		f.wait(t, "OpenCode session-before-pane", 100*time.Second, func() bool {
			member = f.member(t, "live-b", "worker")
			return member.State == model.MemberStateStopped || member.State == model.MemberStateRunning && member.Lease.Driver == model.DriverPane
		})
		f.reportTurns(t, "live-b")
		if member.State != model.MemberStateRunning {
			t.Fatalf("OpenCode pane launch stopped: %v", member.StopReason)
		}
		_, e := b.Send(ctx, wire.SendRequest{Team: "live-b", To: &member.ID, Text: "Reply with the word ok."})
		must(t, e)
		f.wait(t, "OpenCode pane delivery", 45*time.Second, func() bool {
			var done bool
			must(t, b.db.Read(ctx, func(tx *store.Tx) error {
				events, e := tx.ListTeamDeliveries("live-b")
				if e != nil {
					return e
				}
				done = len(events) > 0 && events[0].Delivery.State == model.DeliveryStateDelivered
				return nil
			}))
			return done
		})
		f.stopAndCheck(t, "live-b")
	})
	t.Run("c-claude-conversion", func(t *testing.T) {
		f.applyStart(t, "live-c", []model.MemberSpec{liveMember("live-claude", "worker", model.HarnessClaude, repo, model.ModeHeadless)}, 2, []model.GridEdge{})
		f.wait(t, "Claude headless launch", 20*time.Second, func() bool { return f.member(t, "live-c", "worker").State == model.MemberStateRunning })
		// Assigning a Claude session is enough to open it; no paid warm-up turn.
		member := f.member(t, "live-c", "worker")
		opened, e := b.OpenTerminal(ctx, wire.OpenTerminalRequest{Team: "live-c", Member: member.ID})
		must(t, e)
		result := f.request(t, opened.Request)
		if result.Status != model.RequestStatusSucceeded {
			t.Fatal(result.Error)
		}
		member = f.member(t, "live-c", "worker")
		t.Logf("Claude pane activity=%v session=%v", member.Activity, member.Session)
		if member.Activity != nil && *member.Activity == model.ActivityBlocked {
			t.Skip("Claude trust question is blocked; return-headless must wait for the person, no keys injected")
		}
		returned, e := b.ReturnHeadless(ctx, wire.ReturnHeadlessRequest{Team: "live-c", Member: member.ID})
		must(t, e)
		result = f.request(t, returned.Request)
		if result.Status != model.RequestStatusSucceeded {
			t.Fatal(result.Error)
		}
		member = f.member(t, "live-c", "worker")
		if member.Lease.Driver != model.DriverHeadless {
			t.Fatal(member)
		}
		f.stopAndCheck(t, "live-c")
	})
	t.Run("d-spawn-worktree-stop", func(t *testing.T) {
		parent := liveMember("live-parent", "parent", model.HarnessCodex, repo, model.ModeHeadless)
		parent.Profile = model.ProfileWorkspace
		parent.Spawn = &model.SpawnSettings{Enabled: true, Brief: ptr("Reply with only the word ok. Do not use tools or change files.")}
		f.applyStart(t, "live-d", []model.MemberSpec{parent}, 1, []model.GridEdge{})
		f.wait(t, "spawner launch", 20*time.Second, func() bool { return f.member(t, "live-d", "parent").State == model.MemberStateRunning })
		caller := f.member(t, "live-d", "parent")
		raw := f.command(t, caller, "spawn", "worker", "claude", "Reply with the word ok. Do not change files.", "--profile", "workspace")
		var spawned wire.SpawnOutput
		must(t, json.Unmarshal(raw, &spawned))
		if spawned.Member.Worktree == nil {
			t.Fatal("spawn did not create a worktree")
		}
		f.wait(t, "spawned worker turn", 90*time.Second, func() bool {
			var done bool
			must(t, b.db.Read(ctx, func(tx *store.Tx) error {
				turns, e := tx.ListTurns(spawned.Member.ID, 1)
				done = len(turns) > 0 && turns[0].Status != model.TurnStatusStarting && turns[0].Status != model.TurnStatusRunning
				return e
			}))
			return done
		})
		f.reportTurns(t, "live-d")
		f.command(t, caller, "stop", "worker")
		f.stopAndCheck(t, "live-d")
		removed, e := b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "live-d", Paths: []string{spawned.Member.Worktree.Path}})
		must(t, e)
		t.Logf("app remove-worktrees: %+v", removed)
	})
	t.Run("e-empty-idle-teardown", func(t *testing.T) {
		f.applyStart(t, "live-e", []model.MemberSpec{liveMember("idle-claude", "claude", model.HarnessClaude, repo, model.ModeHeadless), liveMember("idle-codex", "codex", model.HarnessCodex, repo, model.ModeHeadless)}, 1, []model.GridEdge{})
		f.wait(t, "idle leases", 20*time.Second, func() bool {
			return f.member(t, "live-e", "claude").State == model.MemberStateRunning && f.member(t, "live-e", "codex").State == model.MemberStateRunning
		})
		f.stopAndCheck(t, "live-e")
	})

}
