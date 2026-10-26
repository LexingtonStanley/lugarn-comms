package policy

import (
	"fmt"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// spawnBase passes every check: lead (workspace, spawning on with defaults)
// spawns web on codex in a running team of 3 live agents.
func spawnBase() (SpawnInput, *fakeHost) {
	lead := agent("L", "lead", model.RoleOrchestrator, model.HarnessClaude, workdir("/home/lex/code/flare"))
	cfg := ResolveSpawn(&model.SpawnSettings{Enabled: true, Brief: ptr("You are {{member}}, started by {{spawner}}.")})
	in := SpawnInput{
		Name: "web", Harness: model.HarnessCodex, Task: "Build the login page.",
		TeamState: model.TeamStateRunning, RunnerUp: true, Caller: lead, Settings: cfg,
		Live: []model.Member{
			humanMember("H"), lead,
			agent("A", "api", model.RoleWorker, model.HarnessCodex, spawnedBy(lead)),
			agent("R", "reviewer", model.RoleReviewer, model.HarnessCodex),
		},
		Running: 1, SpawnedThisRun: 1,
	}
	return in, newHost(model.HostOSLinux).repo("/home/lex/code/flare", "/home/lex/code/flare")
}

// spawnChecks breaks each check in order, without touching any other check.
var spawnChecks = []struct {
	code   model.ErrorCode
	policy bool
	brk    func(*SpawnInput, *fakeHost)
}{
	{model.ErrorTeamNotRunning, false, func(in *SpawnInput, _ *fakeHost) { in.TeamState = model.TeamStateStopping }},
	{model.ErrorRunnerUnavailable, false, func(in *SpawnInput, _ *fakeHost) { in.RunnerUp = false }},
	{model.ErrorNestingNotAllowed, true, func(in *SpawnInput, _ *fakeHost) {
		in.Caller.Origin, in.Caller.SpawnedBy = model.MemberOriginSpawn, &model.MemberRef{ID: "X", Name: "boss"}
	}},
	{model.ErrorSpawnDisabled, true, func(in *SpawnInput, _ *fakeHost) { in.Settings.Enabled = false }},
	{model.ErrorInvalidRequest, false, func(in *SpawnInput, _ *fakeHost) { in.Task = "" }},
	{model.ErrorNameTaken, true, func(in *SpawnInput, _ *fakeHost) { in.Name = "reviewer" }},
	{model.ErrorHarnessNotAllowed, true, func(in *SpawnInput, _ *fakeHost) { in.Settings.Harnesses = []model.Harness{model.HarnessClaude} }},
	{model.ErrorHarnessUnavailable, false, func(in *SpawnInput, h *fakeHost) { h.issues[model.HarnessCodex] = model.HarnessIssueShimOnly }},
	{model.ErrorProfileTooHigh, true, func(in *SpawnInput, _ *fakeHost) { in.Profile = ptr(model.ProfileBypass) }},
	{model.ErrorSpawnCapReached, true, func(in *SpawnInput, _ *fakeHost) { in.Running = 3 }},
	{model.ErrorSpawnCapReached, true, func(in *SpawnInput, _ *fakeHost) { in.SpawnedThisRun = 8 }},
	{model.ErrorSpawnCapReached, true, func(in *SpawnInput, _ *fakeHost) {
		for i := len(in.Live) - 1; i < MaxLiveAgents; i++ {
			in.Live = append(in.Live, agent(fmt.Sprintf("X%d", i), fmt.Sprintf("extra-%d", i), model.RoleCustom, model.HarnessPi))
		}
	}},
}

func TestSpawnCheckOrder(t *testing.T) {
	in, host := spawnBase()
	d := Spawn(in, host)
	requireNoError(t, d.Err)

	caps := []model.SpawnCap{model.SpawnCapRunningAtOnce, model.SpawnCapPerRun, model.SpawnCapTeamSize}
	for i, c := range spawnChecks {
		// Breaking check i and every later one: check i still wins.
		for _, alone := range []bool{true, false} {
			in, host := spawnBase()
			c.brk(&in, host)
			if !alone {
				for _, later := range spawnChecks[i+1:] {
					later.brk(&in, host)
				}
			}
			d := Spawn(in, host)
			if d.Plan != nil || d.Err == nil || d.Err.Code != c.code {
				t.Fatalf("check %d (alone %v): got %v; want %s", i+1, alone, d.Err, c.code)
			}
			if i >= 9 && d.Err.Details["cap"] != caps[i-9] {
				t.Fatalf("check %d: cap %v; want %s", i+1, d.Err.Details["cap"], caps[i-9])
			}
			if got := len(d.Events) == 1; got != c.policy {
				t.Fatalf("check %d: policy.denied written %v; want %v", i+1, got, c.policy)
			}
			if c.policy {
				e := d.Events[0]
				equal(t, "event", jsonOf(t, e.Data), map[string]any{"action": "spawn",
					"actor": map[string]any{"id": "L", "name": "lead"}, "target": nil, "message_id": nil, "error": jsonOf(t, d.Err)})
				equal(t, "event row", [3]any{e.Type, *e.Member, e.Message == nil}, [3]any{model.EventPolicyDenied, "L", true})
			}
		}
	}
}

func TestSpawnErrorDetails(t *testing.T) {
	cases := []struct {
		name    string
		brk     func(*SpawnInput, *fakeHost)
		message string
		details map[string]any
	}{
		{"running_at_once", func(in *SpawnInput, _ *fakeHost) { in.Running = 3 },
			"lead already has 3 workers running (running at once: 3)",
			map[string]any{"cap": model.SpawnCapRunningAtOnce, "limit": 3, "current": 3}},
		{"per_run", func(in *SpawnInput, _ *fakeHost) { in.Settings.PerRun, in.SpawnedThisRun = 2, 2 },
			"lead has spawned 2 workers this run (per run: 2)",
			map[string]any{"cap": model.SpawnCapPerRun, "limit": 2, "current": 2}},
		{"team_size", spawnChecks[11].brk,
			"the team already has 24 live agent members (team size: 24)",
			map[string]any{"cap": model.SpawnCapTeamSize, "limit": 24, "current": 24}},
		{"profile_too_high", func(in *SpawnInput, _ *fakeHost) { in.Profile = ptr(model.ProfileBypass) },
			"bypass is above lead's highest spawn profile, workspace",
			map[string]any{"requested": model.ProfileBypass, "highest": model.ProfileWorkspace}},
		{"harness_not_allowed", func(in *SpawnInput, _ *fakeHost) {
			in.Settings.Harnesses = []model.Harness{model.HarnessClaude, model.HarnessOpenCode}
		}, "lead may not spawn codex workers (allowed: claude, opencode)",
			map[string]any{"harness": model.HarnessCodex, "allowed": []model.Harness{model.HarnessClaude, model.HarnessOpenCode}}},
		{"harness_unavailable", func(_ *SpawnInput, h *fakeHost) { h.issues[model.HarnessCodex] = model.HarnessIssueUnsupportedVersion },
			"codex isn't available on this host (unsupported_version)",
			map[string]any{"harness": model.HarnessCodex, "reason": model.HarnessIssueUnsupportedVersion}},
		{"name_taken", func(in *SpawnInput, _ *fakeHost) { in.Name = "api" },
			"api is already a member of this team", map[string]any{"name": "api"}},
		{"nesting_not_allowed", spawnChecks[2].brk, "lead was spawned, and spawned workers can't spawn", map[string]any{}},
		{"spawn_disabled", spawnChecks[3].brk, "lead can't spawn workers", map[string]any{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, host := spawnBase()
			c.brk(&in, host)
			d := Spawn(in, host)
			if d.Err == nil {
				t.Fatal("no error")
			}
			equal(t, "message", d.Err.Message, c.message)
			equal(t, "details", d.Err.Details, c.details)
		})
	}
}

func TestSpawnInputChecks(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*SpawnInput)
		paths []string
	}{
		{"name pattern", func(in *SpawnInput) { in.Name = "Web" }, []string{"/name"}},
		{"name human", func(in *SpawnInput) { in.Name = "human" }, []string{"/name"}},
		{"name all", func(in *SpawnInput) { in.Name = "all" }, []string{"/name"}},
		{"name too long", func(in *SpawnInput) { in.Name = "w" + fmt.Sprintf("%032d", 0) }, []string{"/name"}},
		{"harness", func(in *SpawnInput) { in.Harness = "gpt" }, []string{"/harness"}},
		{"task too long", func(in *SpawnInput) { in.Task = fmt.Sprintf("%065537d", 0) }, []string{"/task"}},
		{"profile", func(in *SpawnInput) { in.Profile = ptr(model.Profile("root")) }, []string{"/profile"}},
		{"mode", func(in *SpawnInput) { in.Mode = ptr(model.Mode("tab")) }, []string{"/mode"}},
		{"all at once", func(in *SpawnInput) { in.Name, in.Harness, in.Task = "", "", "" }, []string{"/name", "/harness", "/task"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, host := spawnBase()
			c.edit(&in)
			d := Spawn(in, host)
			requireCode(t, d.Err, model.ErrorInvalidRequest)
			equal(t, "paths", fieldErrorPaths(t, d.Err), c.paths)
		})
	}
	// A removed member's name is free again.
	in, host := spawnBase()
	in.Live = append(in.Live, agent("G", "web", model.RoleWorker, model.HarnessCodex, removed()))
	requireNoError(t, Spawn(in, host).Err)
}

func TestProfiles(t *testing.T) {
	ro, ws, by := model.ProfileReadOnly, model.ProfileWorkspace, model.ProfileBypass
	equal(t, "ranks", []int{ProfileRank(ro), ProfileRank(ws), ProfileRank(by), ProfileRank("x")}, []int{0, 1, 2, -1})
	cases := []struct {
		max, caller, highest, def model.Profile
	}{
		{ws, ws, ws, ws}, // the defaults
		{by, by, by, ws}, // the person's tick: bypass allowed, workspace by default
		{by, ws, ws, ws}, // never above the spawner's own profile
		{ws, by, ws, ws},
		{ro, by, ro, ro},
		{by, ro, ro, ro},
		{ws, ro, ro, ro},
	}
	for _, c := range cases {
		cfg := SpawnConfig{MaxProfile: c.max}
		if got := HighestWorkerProfile(cfg, c.caller); got != c.highest {
			t.Errorf("highest(max %s, caller %s) = %s; want %s", c.max, c.caller, got, c.highest)
		}
		if got := DefaultWorkerProfile(cfg, c.caller); got != c.def {
			t.Errorf("default(max %s, caller %s) = %s; want %s", c.max, c.caller, got, c.def)
		}
		// Every requested profile up to the highest passes check 9; above it fails.
		for _, p := range []model.Profile{ro, ws, by} {
			in, host := spawnBase()
			in.Settings.MaxProfile, in.Caller.Profile, in.Profile = c.max, ptr(c.caller), ptr(p)
			d := Spawn(in, host)
			if ok := ProfileRank(p) <= ProfileRank(c.highest); ok != (d.Err == nil) {
				t.Errorf("max %s caller %s requested %s: %v", c.max, c.caller, p, d.Err)
			}
		}
	}
}

func TestSpawnPlan(t *testing.T) {
	in, host := spawnBase()
	d := Spawn(in, host)
	requireNoError(t, d.Err)
	equal(t, "plan", *d.Plan, SpawnPlan{
		Name: "web", Harness: model.HarnessCodex, Profile: model.ProfileWorkspace, Mode: model.ModeHeadless,
		Role: model.RoleWorker, Brief: "You are {{member}}, started by {{spawner}}.", Folder: "/home/lex/code/flare",
		Worktree: true, Edges: SpawnEdges("lead", "web"), Task: "Build the login page.",
	})
	equal(t, "no events", d.Events, []Event{})

	// mode: the setting's default, or the call's choice either way.
	in.Settings.Mode = model.ModePane
	equal(t, "default mode", Spawn(in, host).Plan.Mode, model.ModePane)
	in.Mode = ptr(model.ModeHeadless)
	equal(t, "chosen mode", Spawn(in, host).Plan.Mode, model.ModeHeadless)

	// The worker's folder is the spawner's workdir (its worktree, if it has one).
	in, host = spawnBase()
	in.Caller.Workdir = ptr("/home/lex/code/flare-lugarn/t-lead")
	equal(t, "workdir", Spawn(in, host).Plan.Folder, "/home/lex/code/flare-lugarn/t-lead")
}

func TestSpawnFolderDecision(t *testing.T) {
	cases := []struct {
		folder  model.SpawnFolder
		profile model.Profile
		inGit   bool
		want    bool
	}{
		{model.SpawnFolderWorktree, model.ProfileWorkspace, true, true},
		{model.SpawnFolderWorktree, model.ProfileBypass, true, true},
		{model.SpawnFolderWorktree, model.ProfileReadOnly, true, false},
		{model.SpawnFolderWorktree, model.ProfileWorkspace, false, false},
		{model.SpawnFolderShared, model.ProfileWorkspace, true, false},
		{model.SpawnFolderShared, model.ProfileBypass, true, false},
	}
	for _, c := range cases {
		if got := SpawnUsesWorktree(c.folder, c.profile, c.inGit); got != c.want {
			t.Errorf("SpawnUsesWorktree(%s, %s, %v) = %v", c.folder, c.profile, c.inGit, got)
		}
		in, host := spawnBase()
		in.Settings.Folder, in.Settings.MaxProfile, in.Caller.Profile, in.Profile = c.folder, model.ProfileBypass, ptr(model.ProfileBypass), ptr(c.profile)
		if !c.inGit {
			host = newHost(model.HostOSLinux).dir("/home/lex/code/flare")
		}
		if got := Spawn(in, host).Plan.Worktree; got != c.want {
			t.Errorf("spawn %s %s in git %v: worktree %v", c.folder, c.profile, c.inGit, got)
		}
	}
}

func TestStop(t *testing.T) {
	lead := agent("L", "lead", model.RoleOrchestrator, model.HarnessClaude)
	other := agent("O", "other", model.RoleOrchestrator, model.HarnessClaude)
	web := agent("W", "web", model.RoleWorker, model.HarnessCodex, spawnedBy(lead))
	spec := agent("R", "reviewer", model.RoleReviewer, model.HarnessCodex)

	equal(t, "ok", Stop(lead, web, true), StopDecision{Request: true, Events: []Event{}})
	stopped := web
	stopped.State, stopped.StopReason = model.MemberStateStopped, ptr(model.StopReasonSpawner)
	equal(t, "already stopped", Stop(lead, stopped, false), StopDecision{AlreadyStopped: true, Events: []Event{}})
	d := Stop(lead, web, false)
	requireCode(t, d.Err, model.ErrorRunnerUnavailable)

	for _, c := range []struct {
		name           string
		caller, target model.Member
	}{
		{"another spawner's worker", other, web},
		{"a spec member", lead, spec},
		{"itself", lead, lead},
		{"its spawner", web, lead},
		{"a stopped worker of another", other, stopped},
	} {
		d := Stop(c.caller, c.target, true)
		requireCode(t, d.Err, model.ErrorDenied)
		equal(t, c.name, d.Err.Details, map[string]any{"reason": "not_spawner"})
		equal(t, c.name+" event", jsonOf(t, d.Events[0].Data)["target"], map[string]any{"id": c.target.ID, "name": c.target.Name})
		equal(t, c.name+" action", d.Events[0].Data.(wire.PolicyDeniedEventData).Action, model.PolicyActionStop)
	}
}

func TestHold(t *testing.T) {
	now := time.Date(2026, 10, 26, 14, 5, 12, 654_000_000, time.UTC)
	lead := agent("L", "lead", model.RoleOrchestrator, model.HarnessClaude)
	d, err := Hold(now, model.TeamStateRunning, lead, ptr(30), false)
	requireNoError(t, err)
	equal(t, "held until", model.FormatTime(*d.HeldUntil), "2026-10-26T14:05:42.654Z")
	equal(t, "not a release", d.Release, false)

	for _, ttl := range []int{10, 120} {
		_, err := Hold(now, model.TeamStateRunning, lead, ptr(ttl), false)
		requireNoError(t, err)
	}
	for _, ttl := range []int{9, 121, 0, -10} {
		_, err := Hold(now, model.TeamStateRunning, lead, ptr(ttl), false)
		requireCode(t, err, model.ErrorInvalidRequest)
	}
	d, err = Hold(now, model.TeamStateRunning, lead, nil, true)
	requireNoError(t, err)
	equal(t, "release", d, HoldDecision{Release: true})

	stopped := lead
	stopped.State = model.MemberStateStopped
	_, err = Hold(now, model.TeamStateRunning, stopped, ptr(30), false)
	requireCode(t, err, model.ErrorInvalidRequest)
	_, err = Hold(now, model.TeamStateRunning, stopped, nil, true)
	requireNoError(t, err)

	_, err = Hold(now, model.TeamStateStopped, lead, ptr(30), false)
	requireCode(t, err, model.ErrorTeamNotRunning)
	_, err = Hold(now, model.TeamStateRunning, humanMember("H"), ptr(30), false)
	requireCode(t, err, model.ErrorInvalidRequest)
	_, err = Hold(now, model.TeamStateRunning, lead, ptr(30), true)
	requireCode(t, err, model.ErrorInvalidRequest)
	_, err = Hold(now, model.TeamStateRunning, lead, nil, false)
	requireCode(t, err, model.ErrorInvalidRequest)
}

func TestSpawnHeadlessHarness(t *testing.T) {
	cases := []struct {
		name     string
		settings model.Mode
		mode     *model.Mode
		harness  model.Harness
		msg      string // "" passes
	}{
		{"pi, mode omitted, default headless", model.ModeHeadless, nil, model.HarnessPi,
			"pi can't run headless, and lead's workers default to headless: pass mode: pane"},
		{"pi, mode headless", model.ModePane, ptr(model.ModeHeadless), model.HarnessPi, "pi can't run headless: pass mode: pane"},
		{"pi, mode pane", model.ModeHeadless, ptr(model.ModePane), model.HarnessPi, ""},
		{"pi, mode omitted, default pane", model.ModePane, nil, model.HarnessPi, ""},
		{"codex headless", model.ModeHeadless, nil, model.HarnessCodex, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in, host := spawnBase()
			in.Settings.Mode, in.Mode, in.Harness = c.settings, c.mode, c.harness
			d := Spawn(in, host)
			if c.msg == "" {
				requireNoError(t, d.Err)
				return
			}
			requireCode(t, d.Err, model.ErrorInvalidRequest)
			equal(t, "message", d.Err.Message, "mode: "+c.msg)
			equal(t, "paths", fieldErrorPaths(t, d.Err), []string{"/mode"})
			equal(t, "no policy.denied", d.Events, []Event{})
		})
	}

	// It is check 5: it wins over a taken name (6) and a disallowed harness (7).
	in, host := spawnBase()
	in.Harness, in.Name, in.Settings.Harnesses = model.HarnessPi, "api", []model.Harness{model.HarnessCodex}
	requireCode(t, Spawn(in, host).Err, model.ErrorInvalidRequest)
	// The host decides which harnesses can run headless.
	in, host = spawnBase()
	host.headless[model.HarnessCodex] = false
	requireCode(t, Spawn(in, host).Err, model.ErrorInvalidRequest)
	// An invalid mode is reported as such, not as headless.
	in, host = spawnBase()
	in.Harness, in.Mode = model.HarnessPi, ptr(model.Mode("tab"))
	equal(t, "invalid mode", Spawn(in, host).Err.Details["errors"].([]FieldError)[0].Message, "must be one of [headless pane]")
}
