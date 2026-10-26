package policy

import (
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func gitHost() *fakeHost { return newHost(model.HostOSLinux).repo(folder, folder) }

func TestPlanApplyRevision(t *testing.T) {
	cases := []struct {
		name       string
		current    *Team
		ifRevision int64
		code       model.ErrorCode
		expected   int64
		actual     int64
		revision   int64
	}{
		{name: "create", ifRevision: 0, revision: 1},
		{name: "create over an existing team", current: existingTeam(model.TeamStateStopped), ifRevision: 0,
			code: model.ErrorRevisionConflict, expected: 0, actual: 4},
		{name: "update at the current revision", current: existingTeam(model.TeamStateStopped), ifRevision: 4, revision: 5},
		{name: "stale revision", current: existingTeam(model.TeamStateStopped), ifRevision: 3,
			code: model.ErrorRevisionConflict, expected: 3, actual: 4},
		{name: "revision from the future", current: existingTeam(model.TeamStateStopped), ifRevision: 9,
			code: model.ErrorRevisionConflict, expected: 9, actual: 4},
		{name: "update of a missing team", ifRevision: 2,
			code: model.ErrorRevisionConflict, expected: 2, actual: 0},
		{name: "negative", ifRevision: -1, code: model.ErrorInvalidRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, err := PlanApply(ApplyInput{Spec: baseSpec(), IfRevision: c.ifRevision,
				Facts: SpecFacts{Current: c.current}}, gitHost())
			if c.code == "" {
				requireNoError(t, err)
				equal(t, "revision", plan.Revision, c.revision)
				equal(t, "create", plan.Create, c.current == nil)
				return
			}
			requireCode(t, err, c.code)
			if c.code == model.ErrorRevisionConflict {
				equal(t, "details", err.Details, map[string]any{"team": "team1", "expected": c.expected, "actual": c.actual})
			}
		})
	}
}

func TestPlanApplyCheckOrder(t *testing.T) {
	bad := baseSpec()
	bad.Members[0].Folder = "relative"
	cur := existingTeam(model.TeamStateRunning)

	// A stale revision wins over an invalid spec.
	_, err := PlanApply(ApplyInput{Spec: bad, IfRevision: 1, Facts: SpecFacts{Current: cur}}, gitHost())
	requireCode(t, err, model.ErrorRevisionConflict)
	// An invalid spec wins over a taken name and a running team.
	bad.Members = append(bad.Members, model.MemberSpec{ID: "m-new", Name: "new", Role: model.RoleCustom,
		Harness: model.HarnessPi, Folder: folder, Profile: model.ProfileReadOnly, Mode: model.ModeHeadless, Brief: "b"})
	_, err = PlanApply(ApplyInput{Spec: bad, IfRevision: 4, Facts: SpecFacts{Current: cur, OtherTeamNames: []string{"login-fix"}}}, gitHost())
	requireCode(t, err, model.ErrorInvalidRequest)
	// A taken name wins over a running team.
	bad.Members[0].Folder = folder
	_, err = PlanApply(ApplyInput{Spec: bad, IfRevision: 4, Facts: SpecFacts{Current: cur, OtherTeamNames: []string{"login-fix"}}}, gitHost())
	requireCode(t, err, model.ErrorNameTaken)
	_, err = PlanApply(ApplyInput{Spec: bad, IfRevision: 4, Facts: SpecFacts{Current: cur}}, gitHost())
	requireCode(t, err, model.ErrorTeamRunning)
}

func TestPlanApplyCreate(t *testing.T) {
	spec := baseSpec()
	plan, err := PlanApply(ApplyInput{Spec: spec, Facts: SpecFacts{}}, gitHost())
	requireNoError(t, err)
	equal(t, "added", len(plan.Added), 2)
	equal(t, "added defaults", *plan.Added[0].Worktree, false)
	equal(t, "changed", plan.Changed, []MemberChange{})
	equal(t, "removed", plan.Removed, []model.MemberRef{})
	equal(t, "grid", plan.Grid, spec.Grid)
	equal(t, "limits", plan.Limits, ResolveLimits(nil))
	equal(t, "spec", plan.Spec, ApplyDefaults(spec))
	equal(t, "warnings", plan.Warnings, []Warning{})
}

func TestPlanApplyDiff(t *testing.T) {
	cur := existingTeam(model.TeamStateStopped)
	// The stored grid has a spawn edge for web; the new spec keeps one of the
	// two and drops the other, which revokes it.
	cur.Spec.Grid = append(cur.Spec.Grid, SpawnEdges("writer", "web")...)
	spec := baseSpec()
	spec.Name = "login-fix-2"
	spec.Pattern = model.PatternCustom
	spec.Herdr = &model.HerdrSettings{Server: ptr("work")}
	spec.Limits = &model.Limits{HopBudget: ptr(32)}
	spec.Members[0].Name = "author"
	spec.Members[0].Worktree = ptr(true)
	spec.Members[0].Task = ptr("Fix it.")
	spec.Members[0].Spawn = &model.SpawnSettings{Enabled: true, Brief: ptr("w")}
	spec.Members[1] = model.MemberSpec{ID: "m-critic", Name: "critic", Role: model.RoleReviewer, Harness: model.HarnessOpenCode,
		Folder: folder, Profile: model.ProfileReadOnly, Mode: model.ModeHeadless, Brief: "b"}
	spec.Grid = []model.GridEdge{
		{From: "author", To: "critic", Can: []model.GridCapability{model.GridCapabilityMessage}},
		{From: "author", To: "web", Can: []model.GridCapability{model.GridCapabilityMessage, model.GridCapabilityWake}},
	}

	plan, err := PlanApply(ApplyInput{Spec: spec, IfRevision: 4, Facts: SpecFacts{Current: cur}}, gitHost())
	requireNoError(t, err)
	equal(t, "team fields", plan.TeamFields, []string{"/name", "/pattern", "/herdr/server", "/limits"})
	equal(t, "added", []string{plan.Added[0].ID}, []string{"m-critic"})
	equal(t, "changed", plan.Changed, []MemberChange{{ID: "m-writer", Name: "author",
		Fields: []string{"name", "worktree", "task", "spawn"}}})
	equal(t, "removed", plan.Removed, []model.MemberRef{{ID: "m-reviewer", Name: "reviewer"}})
	equal(t, "grid is exactly the spec's", plan.Grid, spec.Grid)
	equal(t, "limits", plan.Limits.HopBudget, 32)
	equal(t, "revision", plan.Revision, int64(5))

	// An apply that changes nothing still bumps the revision and lists nothing.
	plan, err = PlanApply(ApplyInput{Spec: baseSpec(), IfRevision: 4, Facts: SpecFacts{Current: existingTeam(model.TeamStateRunning)}}, gitHost())
	requireNoError(t, err)
	equal(t, "no-op", [4]int{len(plan.Added), len(plan.Changed), len(plan.Removed), len(plan.TeamFields)}, [4]int{})
	equal(t, "no-op revision", plan.Revision, int64(5))
}

func TestPlanApplyMemberFields(t *testing.T) {
	cases := []struct {
		field string
		edit  func(*model.MemberSpec)
	}{
		{"name", func(m *model.MemberSpec) { m.Name = "editor" }},
		{"role", func(m *model.MemberSpec) { m.Role = model.RoleCustom }},
		{"harness", func(m *model.MemberSpec) { m.Harness = model.HarnessCodex }},
		{"folder", func(m *model.MemberSpec) { m.Folder = "/home/lex/code/other" }},
		{"worktree", func(m *model.MemberSpec) { m.Worktree = ptr(true) }},
		{"profile", func(m *model.MemberSpec) { m.Profile = model.ProfileBypass }},
		{"mode", func(m *model.MemberSpec) { m.Mode = model.ModeHeadless }},
		{"brief", func(m *model.MemberSpec) { m.Brief = "New brief." }},
		{"task", func(m *model.MemberSpec) { m.Task = ptr("A task.") }},
		{"spawn", func(m *model.MemberSpec) { m.Spawn = &model.SpawnSettings{} }},
	}
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			spec := baseSpec()
			c.edit(&spec.Members[0])
			spec.Grid = []model.GridEdge{}
			plan, err := PlanApply(ApplyInput{Spec: spec, IfRevision: 4, Facts: SpecFacts{Current: existingTeam(model.TeamStateStopped)}}, gitHost())
			requireNoError(t, err)
			equal(t, "fields", plan.Changed[0].Fields, []string{c.field})
		})
	}
	// Writing a default out changes nothing: worktree false, herdr default,
	// limits at their defaults.
	spec := baseSpec()
	spec.Members[0].Worktree = ptr(false)
	spec.Herdr = &model.HerdrSettings{Server: ptr("default")}
	spec.Limits = &model.Limits{HopBudget: ptr(16), TurnTimeoutSeconds: ptr(1800)}
	plan, err := PlanApply(ApplyInput{Spec: spec, IfRevision: 4, Facts: SpecFacts{Current: existingTeam(model.TeamStateRunning)}}, gitHost())
	requireNoError(t, err)
	equal(t, "defaults written out", [2]int{len(plan.Changed), len(plan.TeamFields)}, [2]int{})
}

func TestPlanApplyTeamRunning(t *testing.T) {
	cases := []struct {
		name   string
		edit   func(*model.TeamSpec)
		fields []string // nil: allowed while running
	}{
		{"grid", func(s *model.TeamSpec) { s.Grid = s.Grid[:1] }, nil},
		{"limits", func(s *model.TeamSpec) { s.Limits = &model.Limits{HopBudget: ptr(40), ReviewRounds: ptr(5)} }, nil},
		{"brief", func(s *model.TeamSpec) { s.Members[0].Brief = "new" }, nil},
		{"task", func(s *model.TeamSpec) { s.Members[1].Task = ptr("new") }, nil},
		{"spawn settings", func(s *model.TeamSpec) { s.Members[0].Spawn = &model.SpawnSettings{Enabled: true, Brief: ptr("w")} }, nil},
		{"profile", func(s *model.TeamSpec) { s.Members[1].Profile = model.ProfileWorkspace }, nil},
		{"mode", func(s *model.TeamSpec) { s.Members[1].Mode = model.ModePane }, nil},
		{"role", func(s *model.TeamSpec) { s.Members[1].Role = model.RoleCustom }, nil},
		{"pattern", func(s *model.TeamSpec) { s.Pattern = model.PatternCustom }, nil},
		{"team name", func(s *model.TeamSpec) { s.Name = "other" }, []string{"/name"}},
		{"herdr server", func(s *model.TeamSpec) { s.Herdr = &model.HerdrSettings{Server: ptr("work")} }, []string{"/herdr/server"}},
		{"rename", func(s *model.TeamSpec) { s.Members[1].Name = "critic"; s.Grid = []model.GridEdge{} }, []string{"/members/1/name"}},
		{"harness", func(s *model.TeamSpec) { s.Members[1].Harness = model.HarnessOpenCode }, []string{"/members/1/harness"}},
		{"folder", func(s *model.TeamSpec) { s.Members[1].Folder = "/srv/x" }, []string{"/members/1/folder"}},
		{"worktree flag", func(s *model.TeamSpec) { s.Members[1].Worktree = ptr(true) }, []string{"/members/1/worktree"}},
		{"add", func(s *model.TeamSpec) {
			s.Members = append(s.Members, model.MemberSpec{ID: "m-3", Name: "third", Role: model.RoleCustom, Harness: model.HarnessPi,
				Folder: folder, Profile: model.ProfileReadOnly, Mode: model.ModeHeadless, Brief: "b"})
		}, []string{"/members/2"}},
		{"remove", func(s *model.TeamSpec) { s.Members = s.Members[:1]; s.Grid = []model.GridEdge{} }, []string{"/members"}},
		{"several", func(s *model.TeamSpec) {
			s.Name = "other"
			s.Members[0].Harness, s.Members[0].Folder = model.HarnessCodex, "/srv/x"
			s.Members = s.Members[:1]
			s.Grid = []model.GridEdge{}
		}, []string{"/name", "/members/0/harness", "/members/0/folder", "/members"}},
	}
	for _, st := range []model.TeamState{model.TeamStateStarting, model.TeamStateRunning, model.TeamStateStopping, model.TeamStateStopped} {
		for _, c := range cases {
			t.Run(string(st)+"/"+c.name, func(t *testing.T) {
				spec := baseSpec()
				c.edit(&spec)
				_, err := PlanApply(ApplyInput{Spec: spec, IfRevision: 4, Facts: SpecFacts{Current: existingTeam(st)}}, gitHost())
				if c.fields == nil || st == model.TeamStateStopped {
					requireNoError(t, err)
					return
				}
				requireCode(t, err, model.ErrorTeamRunning)
				equal(t, "fields", err.Details["fields"], c.fields)
			})
		}
	}
}

func TestPlanApplyNextLaunch(t *testing.T) {
	spec := baseSpec()
	spec.Members[0].Profile = model.ProfileBypass
	spec.Members[0].Mode = model.ModeHeadless
	spec.Members[1].Profile = model.ProfileWorkspace

	cur := existingTeam(model.TeamStateRunning)
	cur.Members[2].State = model.MemberStateStopped // reviewer
	host := gitHost()
	host.issues[model.HarnessClaude] = model.HarnessIssueUnsupportedVersion

	plan, err := PlanApply(ApplyInput{Spec: spec, IfRevision: 4, Facts: SpecFacts{Current: cur}}, host)
	requireNoError(t, err)
	equal(t, "changed", plan.Changed, []MemberChange{
		{ID: "m-writer", Name: "writer", Fields: []string{"profile", "mode"}, NextLaunch: true},
		{ID: "m-reviewer", Name: "reviewer", Fields: []string{"profile"}},
	})
	equal(t, "warnings", len(plan.Warnings), 2)
	equal(t, "first", plan.Warnings[0].Code, model.WarningCodeHarnessUnavailable)
	w := plan.Warnings[1]
	equal(t, "next_launch", [2]any{w.Code, *w.Member}, [2]any{model.WarningCodeNextLaunch, "m-writer"})
	equal(t, "message", w.Message, "writer is running: its profile from workspace to bypass and its mode from pane to headless apply from its next launch")

	// A starting member's launch is already under way: next launch too.
	cur.Members[1].State = model.MemberStateStarting
	plan, _ = PlanApply(ApplyInput{Spec: spec, IfRevision: 4, Facts: SpecFacts{Current: cur}}, gitHost())
	equal(t, "starting", plan.Changed[0].NextLaunch, true)
}
