package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

const folder = "/home/lex/code/flare"

// baseSpec is a valid writer–reviewer spec with nothing optional set.
func baseSpec() model.TeamSpec {
	return model.TeamSpec{
		Version: 1, ID: "team1", Name: "login-fix", Pattern: model.PatternWriterReviewer,
		Members: []model.MemberSpec{
			{ID: "m-writer", Name: "writer", Role: model.RoleWriter, Harness: model.HarnessClaude, Folder: folder,
				Profile: model.ProfileWorkspace, Mode: model.ModePane, Brief: "You are {{member}}."},
			{ID: "m-reviewer", Name: "reviewer", Role: model.RoleReviewer, Harness: model.HarnessCodex, Folder: folder,
				Profile: model.ProfileReadOnly, Mode: model.ModeHeadless, Brief: "You review."},
		},
		Grid: []model.GridEdge{
			{From: "writer", To: "reviewer", Can: []model.GridCapability{model.GridCapabilityMessage, model.GridCapabilityWake}},
			{From: "reviewer", To: "writer", Can: []model.GridCapability{model.GridCapabilityMessage}},
		},
	}
}

func TestResolveDefaults(t *testing.T) {
	equal(t, "nil limits", ResolveLimits(nil), Limits{16, 3, 1, 3, 1800})
	equal(t, "partial limits", ResolveLimits(&model.Limits{HopBudget: ptr(24), HeadlessTurns: ptr(2)}), Limits{24, 3, 1, 2, 1800})
	equal(t, "absent spawn", ResolveSpawn(nil), SpawnConfig{RunningAtOnce: 3, PerRun: 8, Mode: model.ModeHeadless,
		Folder: model.SpawnFolderWorktree, MaxProfile: model.ProfileWorkspace})
	equal(t, "enabled spawn", ResolveSpawn(&model.SpawnSettings{Enabled: true, Brief: ptr("b"), PerRun: ptr(2),
		Harnesses: &[]model.Harness{model.HarnessCodex}, MaxProfile: ptr(model.ProfileBypass)}),
		SpawnConfig{Enabled: true, RunningAtOnce: 3, PerRun: 2, Harnesses: []model.Harness{model.HarnessCodex},
			Mode: model.ModeHeadless, Folder: model.SpawnFolderWorktree, MaxProfile: model.ProfileBypass, Brief: "b"})
}

func TestApplyDefaults(t *testing.T) {
	in := baseSpec()
	in.Members[0].Spawn = &model.SpawnSettings{Enabled: true, Brief: ptr("worker brief")}
	in.Grid = nil
	out := ApplyDefaults(in)

	equal(t, "herdr.server", *out.Herdr.Server, "default")
	equal(t, "limits", ResolveLimits(out.Limits), Limits{16, 3, 1, 3, 1800})
	for _, l := range []*int{out.Limits.HopBudget, out.Limits.ReviewRounds, out.Limits.PerRecipient,
		out.Limits.HeadlessTurns, out.Limits.TurnTimeoutSeconds} {
		if l == nil {
			t.Fatal("a limit was left unset")
		}
	}
	equal(t, "worktree", *out.Members[0].Worktree, false)
	s := out.Members[0].Spawn
	equal(t, "spawn", [6]any{*s.RunningAtOnce, *s.PerRun, *s.Mode, *s.Folder, *s.MaxProfile, *s.Brief},
		[6]any{3, 8, model.ModeHeadless, model.SpawnFolderWorktree, model.ProfileWorkspace, "worker brief"})
	if s.Harnesses != nil {
		t.Fatal("null harnesses must stay null: every supported harness")
	}
	in2 := baseSpec()
	in2.Members[1].Spawn = &model.SpawnSettings{Enabled: true, Brief: ptr("b"), Harnesses: &[]model.Harness{model.HarnessCodex}}
	out2 := ApplyDefaults(in2)
	(*out2.Members[1].Spawn.Harnesses)[0] = model.HarnessPi
	equal(t, "harnesses copied", (*in2.Members[1].Spawn.Harnesses)[0], model.HarnessCodex)
	if out.Members[1].Spawn != nil {
		t.Fatal("absent spawn settings must stay absent")
	}
	if out.Grid == nil || len(out.Grid) != 0 {
		t.Fatal("a nil grid becomes empty")
	}
	// The input is untouched, and the output shares nothing with it.
	if in.Herdr != nil || in.Limits != nil || in.Members[0].Worktree != nil || in.Members[0].Spawn.RunningAtOnce != nil {
		t.Fatal("ApplyDefaults modified its input")
	}
	*out.Members[0].Spawn.Brief = "changed"
	if *in.Members[0].Spawn.Brief != "worker brief" {
		t.Fatal("ApplyDefaults shares pointers with its input")
	}
	// Given values win, and defaulting twice changes nothing.
	in.Herdr = &model.HerdrSettings{Server: ptr("work")}
	in.Members[1].Worktree = ptr(true)
	again := ApplyDefaults(ApplyDefaults(in))
	equal(t, "given server", *again.Herdr.Server, "work")
	equal(t, "given worktree", *again.Members[1].Worktree, true)
	equal(t, "idempotent", ApplyDefaults(again), again)
}

func TestValidateSpecValueRules(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(*model.TeamSpec)
		paths []string
	}{
		{"valid", func(*model.TeamSpec) {}, nil},
		{"version missing", func(s *model.TeamSpec) { s.Version = 0 }, []string{"/version"}},
		{"id", func(s *model.TeamSpec) { s.ID = "has space" }, []string{"/id"}},
		{"id too long", func(s *model.TeamSpec) { s.ID = strings.Repeat("a", 65) }, []string{"/id"}},
		{"name upper", func(s *model.TeamSpec) { s.Name = "Login" }, []string{"/name"}},
		{"name too long", func(s *model.TeamSpec) { s.Name = strings.Repeat("a", 49) }, []string{"/name"}},
		{"pattern", func(s *model.TeamSpec) { s.Pattern = "star" }, []string{"/pattern"}},
		{"herdr server", func(s *model.TeamSpec) { s.Herdr = &model.HerdrSettings{Server: ptr("a b")} }, []string{"/herdr/server"}},
		{"limits", func(s *model.TeamSpec) {
			s.Limits = &model.Limits{HopBudget: ptr(0), ReviewRounds: ptr(101), PerRecipient: ptr(2),
				HeadlessTurns: ptr(25), TurnTimeoutSeconds: ptr(59)}
		}, []string{"/limits/hop_budget", "/limits/review_rounds", "/limits/per_recipient", "/limits/headless_turns", "/limits/turn_timeout_seconds"}},
		{"limits at bounds", func(s *model.TeamSpec) {
			s.Limits = &model.Limits{HopBudget: ptr(1000), ReviewRounds: ptr(1), PerRecipient: ptr(1),
				HeadlessTurns: ptr(24), TurnTimeoutSeconds: ptr(86400)}
		}, nil},
		{"no members", func(s *model.TeamSpec) { s.Members = nil; s.Grid = []model.GridEdge{} }, []string{"/members"}},
		{"member fields", func(s *model.TeamSpec) {
			m := &s.Members[1]
			m.ID, m.Role, m.Harness, m.Profile, m.Mode, m.Brief, m.Task = "", "boss", "gpt", "root", "tab", "", ptr("")
			s.Grid = []model.GridEdge{}
		}, []string{"/members/1/id", "/members/1/role", "/members/1/harness", "/members/1/profile", "/members/1/mode", "/members/1/brief", "/members/1/task"}},
		{"reserved member names", func(s *model.TeamSpec) {
			s.Members[0].Name, s.Members[1].Name = "human", "all"
			s.Grid = []model.GridEdge{}
		}, []string{"/members/0/name", "/members/1/name"}},
		{"folder empty", func(s *model.TeamSpec) { s.Members[0].Folder = "" }, []string{"/members/0/folder"}},
		{"folder too long", func(s *model.TeamSpec) { s.Members[0].Folder = "/" + strings.Repeat("é", 4096) }, []string{"/members/0/folder"}},
		{"folder at 4096", func(s *model.TeamSpec) { s.Members[0].Folder = "/" + strings.Repeat("é", 4095) }, nil},
		{"brief over 65536", func(s *model.TeamSpec) { s.Members[0].Brief = strings.Repeat("ü", 65537) }, []string{"/members/0/brief"}},
		{"brief at 65536", func(s *model.TeamSpec) { s.Members[0].Brief = strings.Repeat("ü", 65536) }, nil},
		{"brief invalid utf-8", func(s *model.TeamSpec) { s.Members[0].Brief = "\xff" }, []string{"/members/0/brief"}},
		{"null task", func(s *model.TeamSpec) { s.Members[0].Task = nil }, nil},
		{"spawn values", func(s *model.TeamSpec) {
			s.Members[0].Spawn = &model.SpawnSettings{Enabled: true, RunningAtOnce: ptr(0), PerRun: ptr(25),
				Harnesses: &[]model.Harness{model.HarnessCodex, "gpt", model.HarnessCodex},
				Mode:      ptr(model.Mode("tab")), Folder: ptr(model.SpawnFolder("copy")), MaxProfile: ptr(model.Profile("root"))}
		}, []string{"/members/0/spawn/running_at_once", "/members/0/spawn/per_run", "/members/0/spawn/harnesses/1",
			"/members/0/spawn/harnesses/2", "/members/0/spawn/mode", "/members/0/spawn/folder", "/members/0/spawn/max_profile",
			"/members/0/spawn/brief"}},
		{"spawn empty harnesses", func(s *model.TeamSpec) {
			s.Members[0].Spawn = &model.SpawnSettings{Enabled: true, Brief: ptr("b"), Harnesses: &[]model.Harness{}}
		}, []string{"/members/0/spawn/harnesses"}},
		{"spawn disabled needs no brief", func(s *model.TeamSpec) { s.Members[0].Spawn = &model.SpawnSettings{} }, nil},
		{"spawn empty brief", func(s *model.TeamSpec) {
			s.Members[0].Spawn = &model.SpawnSettings{Brief: ptr("")}
		}, []string{"/members/0/spawn/brief"}},
		{"grid missing", func(s *model.TeamSpec) { s.Grid = nil }, []string{"/grid"}},
		{"grid empty is fine", func(s *model.TeamSpec) { s.Grid = []model.GridEdge{} }, nil},
		{"grid can", func(s *model.TeamSpec) {
			s.Grid[0].Can = nil
			s.Grid[1].Can = []model.GridCapability{"spawn", model.GridCapabilityMessage, model.GridCapabilityMessage}
		}, []string{"/grid/0/can", "/grid/1/can/0", "/grid/1/can/2"}},
		{"wake needs message", func(s *model.TeamSpec) {
			s.Grid[1].Can = []model.GridCapability{model.GridCapabilityWake}
		}, []string{"/grid/1/can"}},
		{"grid bad names", func(s *model.TeamSpec) { s.Grid[0].From, s.Grid[0].To = "Writer", "" }, []string{"/grid/0/from", "/grid/0/to"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := baseSpec()
			c.edit(&spec)
			err := ValidateSpec(spec, SpecFacts{}, model.HostOSLinux)
			if c.paths == nil {
				requireNoError(t, err)
				return
			}
			requireCode(t, err, model.ErrorInvalidRequest)
			equal(t, "paths", fieldErrorPaths(t, err), c.paths)
		})
	}
}

func TestValidateSpecUnsupportedVersion(t *testing.T) {
	spec := baseSpec()
	spec.Version = 2
	err := ValidateSpec(spec, SpecFacts{}, model.HostOSLinux)
	requireCode(t, err, model.ErrorUnsupportedVersion)
	equal(t, "supported", err.Details["supported"], []int{1})
}

// existingTeam is team1 as stored: the base spec, with a live spawned worker
// web, a removed spec member old, and human.
func existingTeam(st model.TeamState) *Team {
	spec := ApplyDefaults(baseSpec())
	writer := agent("m-writer", "writer", model.RoleWriter, model.HarnessClaude)
	return &Team{
		ID: "team1", Revision: 4, State: st, Spec: spec,
		Members: []model.Member{
			humanMember("01HUMAN0000000000000000000"),
			writer,
			agent("m-reviewer", "reviewer", model.RoleReviewer, model.HarnessCodex),
			agent("01WEB00000000000000000000W", "web", model.RoleWorker, model.HarnessCodex, spawnedBy(writer)),
			agent("01GONE0000000000000000000G", "gone", model.RoleWorker, model.HarnessCodex, spawnedBy(writer), removed()),
			agent("m-old", "old", model.RoleCustom, model.HarnessPi, removed()),
		},
	}
}

func TestValidateSpecRules(t *testing.T) {
	cases := []struct {
		name  string
		os    model.HostOS
		edit  func(*model.TeamSpec)
		facts func(*SpecFacts)
		paths []string
	}{
		{name: "1 duplicate id and name", edit: func(s *model.TeamSpec) {
			s.Members = append(s.Members, s.Members[0])
		}, paths: []string{"/members/2/id", "/members/2/name"}},
		{name: "1 name of a live spawned member", edit: func(s *model.TeamSpec) {
			s.Members[1].Name = "web"
			s.Grid = []model.GridEdge{}
		}, paths: []string{"/members/1/name"}},
		{name: "1 name of a removed spawned member is free", edit: func(s *model.TeamSpec) {
			s.Members[1].Name = "gone"
			s.Grid = []model.GridEdge{}
		}},
		{name: "2 id of another team", facts: func(f *SpecFacts) { f.ForeignMemberIDs = []string{"m-reviewer"} },
			paths: []string{"/members/1/id"}},
		{name: "2 id of a spawned member", edit: func(s *model.TeamSpec) { s.Members[1].ID = "01WEB00000000000000000000W" },
			paths: []string{"/members/1/id"}},
		{name: "2 id of a removed spawned member", edit: func(s *model.TeamSpec) { s.Members[1].ID = "01GONE0000000000000000000G" },
			paths: []string{"/members/1/id"}},
		{name: "2 id of human", edit: func(s *model.TeamSpec) { s.Members[1].ID = "01HUMAN0000000000000000000" },
			paths: []string{"/members/1/id"}},
		{name: "2 id of a removed spec member", edit: func(s *model.TeamSpec) { s.Members[1].ID = "m-old" },
			paths: []string{"/members/1/id"}},
		{name: "3 grid to a live spawned member", edit: func(s *model.TeamSpec) {
			s.Grid = append(s.Grid, model.GridEdge{From: "writer", To: "web", Can: []model.GridCapability{model.GridCapabilityMessage}})
		}},
		{name: "3 grid names", edit: func(s *model.TeamSpec) {
			s.Grid = append(s.Grid,
				model.GridEdge{From: "writer", To: "gone", Can: []model.GridCapability{model.GridCapabilityMessage}},
				model.GridEdge{From: "nobody", To: "writer", Can: []model.GridCapability{model.GridCapabilityMessage}},
				model.GridEdge{From: "human", To: "writer", Can: []model.GridCapability{model.GridCapabilityMessage}},
				model.GridEdge{From: "writer", To: "human", Can: []model.GridCapability{model.GridCapabilityMessage}},
				model.GridEdge{From: "writer", To: "writer", Can: []model.GridCapability{model.GridCapabilityMessage}},
				model.GridEdge{From: "writer", To: "reviewer", Can: []model.GridCapability{model.GridCapabilityMessage}},
			)
		}, paths: []string{"/grid/2/to", "/grid/3/from", "/grid/4/from", "/grid/5/to", "/grid/6", "/grid/7"}},
		{name: "4 relative folder", edit: func(s *model.TeamSpec) { s.Members[0].Folder = "code/flare" },
			paths: []string{"/members/0/folder"}},
		{name: "4 Windows folder on Linux", edit: func(s *model.TeamSpec) { s.Members[0].Folder = `C:\code\flare` },
			paths: []string{"/members/0/folder"}},
		{name: "4 Windows folders on Windows", os: model.HostOSWindows, edit: func(s *model.TeamSpec) {
			s.Members[0].Folder, s.Members[1].Folder = `C:\code\flare`, `\\nas\code\flare`
		}},
		{name: "4 POSIX folder on Windows", os: model.HostOSWindows, paths: []string{"/members/0/folder", "/members/1/folder"}},
		{name: "6 at 24 live agents", edit: func(s *model.TeamSpec) { addMembers(s, 21) }},
		{name: "6 over 24 live agents", edit: func(s *model.TeamSpec) { addMembers(s, 22) }, paths: []string{"/members"}},
		{name: "6 over 24 spec members reports once", edit: func(s *model.TeamSpec) { addMembers(s, 23) }, paths: []string{"/members"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec := baseSpec()
			if c.edit != nil {
				c.edit(&spec)
			}
			facts := SpecFacts{Current: existingTeam(model.TeamStateStopped)}
			if c.facts != nil {
				c.facts(&facts)
			}
			os := c.os
			if os == "" {
				os = model.HostOSLinux
			}
			err := ValidateSpec(spec, facts, os)
			if c.paths == nil {
				requireNoError(t, err)
				return
			}
			requireCode(t, err, model.ErrorInvalidRequest)
			equal(t, "paths", fieldErrorPaths(t, err), c.paths)
		})
	}
}

func addMembers(s *model.TeamSpec, n int) {
	for i := range n {
		m := s.Members[1]
		m.ID, m.Name = fmt.Sprintf("extra-%d", i), fmt.Sprintf("extra-%d", i)
		s.Members = append(s.Members, m)
	}
}

func TestValidateSpecNameTaken(t *testing.T) {
	spec := baseSpec()
	facts := SpecFacts{OtherTeamNames: []string{"auth-rewrite", "login-fix"}}
	err := ValidateSpec(spec, facts, model.HostOSLinux)
	requireCode(t, err, model.ErrorNameTaken)
	equal(t, "details", err.Details, map[string]any{"name": "login-fix"})

	// invalid_request comes first, so the app sees every field problem at once.
	spec.Members[0].Folder = "relative"
	requireCode(t, ValidateSpec(spec, facts, model.HostOSLinux), model.ErrorInvalidRequest)

	// The team keeping its own name is fine: only other teams' names count.
	requireNoError(t, ValidateSpec(baseSpec(), SpecFacts{OtherTeamNames: []string{"auth-rewrite"}}, model.HostOSLinux))
}

func TestValidateSpecErrorShape(t *testing.T) {
	spec := baseSpec()
	spec.Name, spec.Members[0].Folder = "Bad", "rel"
	err := ValidateSpec(spec, SpecFacts{}, model.HostOSLinux)
	requireCode(t, err, model.ErrorInvalidRequest)
	got := jsonOf(t, err)
	errs := got["details"].(map[string]any)["errors"].([]any)
	if len(errs) != 2 || errs[0].(map[string]any)["path"] != "/name" || errs[0].(map[string]any)["message"] == "" {
		t.Fatalf("details.errors = %v", errs)
	}
	if !strings.HasPrefix(err.Message, "invalid team spec: /name: ") || !strings.HasSuffix(err.Message, "(and 1 more)") {
		t.Fatalf("message = %q", err.Message)
	}
}

func TestSpecExamplesValidate(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "schema", "examples", "team-spec", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no team-spec examples: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var spec model.TeamSpec
		if err := model.DecodeStrict(data, &spec); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if err := ValidateSpec(spec, SpecFacts{}, model.HostOSLinux); err != nil {
			t.Errorf("%s: %s %v", filepath.Base(f), err.Message, err.Details)
		}
	}
}

func TestSpecWarnings(t *testing.T) {
	spec := baseSpec()
	spec.Members[0].Worktree = ptr(true)
	spec.Members[1].Spawn = &model.SpawnSettings{Enabled: true, Brief: ptr("b")}
	third := spec.Members[1]
	third.ID, third.Name, third.Folder, third.Spawn = "m-third", "third", "/nowhere", nil
	spec.Members = append(spec.Members, third)

	host := newHost(model.HostOSLinux).dir(folder) // exists, outside git
	host.issues[model.HarnessCodex] = model.HarnessIssueNotInstalled
	ws := specWarnings(ApplyDefaults(spec), host)
	var got []string
	for _, w := range ws {
		got = append(got, string(w.Code)+" "+*w.Member)
		if w.Message == "" {
			t.Errorf("%s has no message", w.Code)
		}
	}
	equal(t, "warnings", got, []string{
		"folder_not_git m-writer",
		"folder_not_git m-reviewer",
		"harness_unavailable m-reviewer",
		"folder_missing m-third",
		"harness_unavailable m-third",
	})

	// In git: no folder_not_git. A spawner with folder shared: none either.
	host = newHost(model.HostOSLinux).repo(folder, folder)
	spec.Members = spec.Members[:2]
	equal(t, "in git", specWarnings(ApplyDefaults(spec), host), []Warning(nil))
	host = newHost(model.HostOSLinux).dir(folder)
	spec.Members[0].Worktree = nil
	spec.Members[1].Spawn.Folder = ptr(model.SpawnFolderShared)
	equal(t, "shared spawner", specWarnings(ApplyDefaults(spec), host), []Warning(nil))
	spec.Members[1].Spawn = &model.SpawnSettings{Enabled: false, Folder: ptr(model.SpawnFolderWorktree)}
	equal(t, "spawning off", specWarnings(ApplyDefaults(spec), host), []Warning(nil))
	if slices.ContainsFunc(ws, func(w Warning) bool { return w.Code == model.WarningCodeNextLaunch }) {
		t.Fatal("next_launch comes from the apply diff, not the spec alone")
	}
}
