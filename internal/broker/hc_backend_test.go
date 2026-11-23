package broker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LexingtonStanley/lugarn-comms/internal/hc"
	"github.com/LexingtonStanley/lugarn-comms/internal/hc/hctest"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func init() { integrationHelpers["hc-peer"] = hctest.Serve }
func hcFake(t *testing.T) (*Integration, string, hctest.State) {
	t.Helper()
	b, _ := setup(t)
	binary, e := os.Executable()
	must(t, e)
	i, e := NewIntegration(b, IntegrationOptions{Binary: binary, HCBinary: binary, HomeDir: t.TempDir()})
	must(t, e)
	i.Attach()
	var s hctest.State
	raw, e := os.ReadFile("../hc/testdata/hello.json")
	must(t, e)
	must(t, json.Unmarshal(raw, &s.Hello))
	raw, e = os.ReadFile("../hc/testdata/terminal.json")
	must(t, e)
	var terminal hc.Terminal
	must(t, json.Unmarshal(raw, &terminal))
	terminal.Labels = i.hcLabels("t", "member")
	terminal.Labels["herdr.space"] = "sp" + strings.Repeat("a", 32)
	terminal.Labels["herdr.tab"] = "tb" + strings.Repeat("b", 32)
	terminal.Cwd = b.opts.StateDir
	terminal.Foreground = &hc.Foreground{PID: *terminal.PID, Name: "pi", Cwd: b.opts.StateDir}
	terminal.Status = &hc.Status{App: "pi", State: "idle"}
	s.Terminals = []hc.Terminal{terminal}
	path := filepath.Join(t.TempDir(), "peer.json")
	must(t, hctest.Write(path, s))
	t.Setenv("LUGARN_INTEGRATION_HELPER", "hc-peer")
	t.Setenv("LUGARN_HC_FAKE_STATE", path)
	t.Setenv("HC_TERMINAL_ID", "do-not-inherit")
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{HerdrServer: ptr("lugarn-test-fake"), BackendChoice: ptr(model.HerdrBackendChoiceHC), Backend: ptr(ptr(model.HerdrBackendHC)), HCCoreID: ptr(&s.Hello.Core.ID), HerdrWorkspace: ptr(ptr(terminal.Labels["herdr.space"]))}, b.now())
	}))
	return i, path, s
}

type hcFallback struct {
	HerdrAPI
	installed bool
}

func (h hcFallback) Status(context.Context) (herdr.Status, error) {
	return herdr.Status{Installed: h.installed, Running: h.installed}, nil
}
func TestHCResolution(t *testing.T) {
	for _, tc := range []struct {
		name           string
		generation     int
		missing, herdr bool
		want           model.HerdrBackend
	}{
		{"hc", 1, false, true, model.HerdrBackendHC}, {"generation2", 2, false, true, model.HerdrBackendHerdr}, {"missing hc", 1, true, true, model.HerdrBackendHerdr}, {"neither", 1, true, false, ""}, {"unsupported only", 2, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i, path, s := hcFake(t)
			s.Hello.Generation = tc.generation
			must(t, hctest.Write(path, s))
			if tc.missing {
				i.opts.HCBinary = filepath.Join(t.TempDir(), "missing")
			}
			i.opts.Herdr = func(string) HerdrAPI { return hcFallback{installed: tc.herdr} }
			backend, _, e := i.resolveBackend(context.Background(), &store.Team{BackendChoice: model.HerdrBackendChoiceAuto, HerdrServer: "lugarn-test-fake"})
			if tc.want == "" {
				code(t, e, model.ErrorHerdrUnavailable)
				if tc.generation == 2 && model.AsError(e).Details["hc"] != "unsupported_generation" {
					t.Fatal(e)
				}
			} else {
				must(t, e)
				if backend == nil || *backend != tc.want {
					t.Fatal(backend)
				}
			}
		})
	}
}
func TestHCOwnershipRefusals(t *testing.T) {
	for _, change := range []string{"core", "home", "team", "pid", "address"} {
		t.Run(change, func(t *testing.T) {
			i, path, s := hcFake(t)
			terminal := s.Terminals[0]
			r := store.HCTerminal{Team: "t", Core: s.Hello.Core.ID, Address: terminal.Address, PID: *terminal.PID}
			switch change {
			case "core":
				s.Hello.Core.ID = "replacement"
			case "home":
				terminal.Labels["lugarn.home"] = "foreign"
			case "team":
				terminal.Labels["lugarn.team"] = "foreign"
			case "pid":
				terminal.PID = ptr(int64(98765))
			case "address":
				r.Address = "tmissing"
			}
			s.Terminals[0] = terminal
			must(t, hctest.Write(path, s))
			c, e := i.hcClient(context.Background(), "lugarn-test-fake")
			must(t, e)
			defer c.Close()
			if e = i.hcClose(context.Background(), c, r); change != "address" && e == nil {
				t.Fatal("ownership accepted")
			}
			state, e := hctest.Read(path)
			must(t, e)
			for _, call := range state.Calls {
				if call.Method == "terminal.kill" || call.Method == "terminal.rm" || call.Method == "terminal.send" {
					t.Fatal(call)
				}
			}
		})
	}
}
func TestHCNamesPlacementAndRace(t *testing.T) {
	for _, placement := range []string{"tabs", "workspaces"} {
		t.Run(placement, func(t *testing.T) {
			i, path, s := hcFake(t)
			b := i.b
			ctx := context.Background()
			runnerReady(t, b)
			must(t, b.db.Write(ctx, func(tx *store.Tx) error {
				if e := tx.UpdateTeam("t", store.TeamPatch{Placement: &placement}, b.now()); e != nil {
					return e
				}
				return tx.UpdateMember("writer", store.MemberPatch{Name: ptr("t7"), Harness: ptr(ptr(model.HarnessPi)), Mode: ptr(ptr(model.ModePane))}, b.now())
			}))
			r := i.Runner("t", RunnerOptions{Binary: func(model.Harness) (string, error) { return i.opts.Binary, nil }})
			must(t, r.acquire(ctx))
			var m *model.Member
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("writer"); return e }))
			must(t, r.opts.PaneLifecycle.Open(ctx, *m, model.LeaseCauseStart))
			state, e := hctest.Read(path)
			must(t, e)
			last := state.Terminals[len(state.Terminals)-1]
			if last.Name == nil || *last.Name != "t7-2" || !hcSpacePattern.MatchString(last.Labels["herdr.space"]) || !hcTabPattern.MatchString(last.Labels["herdr.tab"]) {
				t.Fatal(last)
			}
			if (last.Labels["herdr.space"] == s.Terminals[0].Labels["herdr.space"]) != (placement == "tabs") {
				t.Fatal(last.Labels)
			}
			for _, call := range state.Calls {
				if call.Method == "terminal.run" {
					var opts hc.RunOptions
					must(t, json.Unmarshal(call.Params, &opts))
					if !opts.NoParent || !opts.NoNotify || opts.Keep || opts.Env["HOME"] != os.Getenv("HOME") || opts.Env["PATH"] != completeSystemPath() {
						t.Fatal(opts)
					}
				}
			}
			// Herdr, not a preflight list, arbitrates the final name race.
			state.RunError = "name_taken"
			must(t, hctest.Write(path, state))
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("reviewer"); return e }))
			must(t, b.db.Write(ctx, func(tx *store.Tx) error {
				return tx.UpdateMember(m.ID, store.MemberPatch{Harness: ptr(ptr(model.HarnessPi)), Mode: ptr(ptr(model.ModePane))}, b.now())
			}))
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("reviewer"); return e }))
			code(t, r.opts.PaneLifecycle.Open(ctx, *m, model.LeaseCauseStart), model.ErrorNameTaken)
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("writer"); return e }))
			// With no subscription, the label-filtered poll still supplies status.
			obs, e := i.hcPanes.State(ctx, *m)
			must(t, e)
			if obs.Gone || obs.Activity != model.ActivityIdle {
				t.Fatal(obs)
			}
			must(t, i.closeHCMember(ctx, *m))
			state, e = hctest.Read(path)
			must(t, e)
			for _, terminal := range state.Terminals {
				if terminal.Address == *m.Lease.Pane {
					t.Fatal("member terminal was not removed")
				}
			}

		})
	}
}
func TestHCFolder(t *testing.T) {
	for input, want := range map[string]string{" . hi/a\\b<>:\"|?* . ": "hi-a-b-------", "CON": "fallback", "...": "fallback", "nul.txt": "fallback", "Team α": "Team α"} {
		if got := hcFolder(input, "fallback"); got != want {
			t.Fatalf("%q: %q != %q", input, got, want)
		}
	}
	got := hcFolder(strings.Repeat("界", 30), "fallback")
	if len(got) > 64 || !utf8.ValidString(got) {
		t.Fatal(got)
	}
}
func TestHCSessionDiscovery(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	session := "00000000-0000-4000-8000-000000000001"
	terminal := hc.Terminal{Foreground: &hc.Foreground{PID: 123, Cwd: cwd, Argv: []string{"claude", "--resume", session}}}
	if got := hcSession(home, terminal, model.HarnessClaude); got == nil || *got != session {
		t.Fatal(got)
	}
	dir := filepath.Join(home, ".claude", "sessions")
	must(t, os.MkdirAll(dir, 0700))
	other := "00000000-0000-4000-8000-000000000002"
	raw, _ := json.Marshal(map[string]any{"pid": 123, "cwd": cwd, "sessionId": other})
	must(t, os.WriteFile(filepath.Join(dir, "123.json"), raw, 0600))
	if got := hcSession(home, terminal, model.HarnessClaude); got == nil || *got != other {
		t.Fatal(got)
	}
	terminal.Foreground.Cwd = "different"
	terminal.Foreground.Argv = nil
	if got := hcSession(home, terminal, model.HarnessClaude); got != nil {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		h    model.Harness
		argv []string
		want bool
	}{{model.HarnessCodex, []string{"codex", "resume", session}, true}, {model.HarnessOpenCode, []string{"opencode", "-s", "ses_abc123"}, true}, {model.HarnessPi, []string{"pi", "--resume", session}, false}, {model.HarnessClaude, []string{"claude", "-r", "--bad"}, false}} {
		terminal.Foreground.Argv = tc.argv
		got := hcSession(home, terminal, tc.h)
		if (got != nil) != tc.want {
			t.Fatal(tc, got)
		}
	}
}
func TestHCAdoptionAndForget(t *testing.T) {
	i, path, s := hcFake(t)
	ctx := context.Background()
	v := s.Terminals[0]
	name := v.Address
	if v.Name != nil {
		name = *v.Name
	}
	spec := model.TeamSpec{Version: 1, ID: "adopted", Name: "adopted", Pattern: model.PatternCustom, Herdr: &model.HerdrSettings{Server: ptr("lugarn-test-fake"), Backend: ptr(model.HerdrBackendChoiceHC)}, Members: []model.MemberSpec{{ID: "outside", Name: "outside", Role: model.RoleCustom, Harness: model.HarnessPi, Mode: model.ModePane, Profile: model.ProfileWorkspace, Folder: v.Foreground.Cwd, Brief: "test"}}, Grid: []model.GridEdge{}}
	_, e := i.b.AdoptTeam(ctx, wire.AdoptTeamRequest{Spec: spec, Backend: ptr(model.HerdrBackendHC), Workspace: v.Labels["herdr.space"], Members: []wire.AdoptMember{{Member: "outside", Pane: v.Address, Agent: name}}})
	must(t, e)
	state, e := hctest.Read(path)
	must(t, e)
	if !reflect.DeepEqual(state.Terminals, s.Terminals) {
		t.Fatal("adoption mutated core")
	}
	lists := 0
	for _, call := range state.Calls {
		if call.Method == "terminal.list" {
			lists++
		} else if call.Method != "hello" {
			t.Fatal(call)
		}
	}
	if lists != 1 {
		t.Fatal(lists)
	}
	_, e = i.b.ForgetTeam(ctx, wire.ForgetTeamRequest{Team: "adopted", IfRevision: 1})
	must(t, e)
	state, e = hctest.Read(path)
	must(t, e)
	if !reflect.DeepEqual(state.Terminals, s.Terminals) {
		t.Fatal("forget changed external terminals")
	}
}

func TestHCBringReusesTabAndForgetLeavesManaged(t *testing.T) {
	i, path, s := hcFake(t)
	ctx := context.Background()
	v := s.Terminals[0]
	name := v.Address
	if v.Name != nil {
		name = *v.Name
	}
	spec := model.TeamSpec{Version: 1, ID: "adopted", Name: "adopted", Pattern: model.PatternCustom, Herdr: &model.HerdrSettings{Server: ptr("lugarn-test-fake"), Backend: ptr(model.HerdrBackendChoiceHC)}, Members: []model.MemberSpec{{ID: "outside", Name: "outside", Role: model.RoleCustom, Harness: model.HarnessPi, Mode: model.ModePane, Profile: model.ProfileWorkspace, Folder: v.Foreground.Cwd, Brief: "test"}}, Grid: []model.GridEdge{}}
	_, e := i.b.AdoptTeam(ctx, wire.AdoptTeamRequest{Spec: spec, Backend: ptr(model.HerdrBackendHC), Workspace: v.Labels["herdr.space"], Members: []wire.AdoptMember{{Member: "outside", Pane: v.Address, Agent: name}}})
	must(t, e)
	must(t, i.b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("adopted", store.TeamPatch{State: ptr(model.TeamStateStarting)}, i.b.now())
	}))
	r := i.Runner("adopted", RunnerOptions{Binary: func(model.Harness) (string, error) { return i.opts.Binary, nil }})
	must(t, r.acquire(ctx))
	var member *model.Member
	must(t, i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; member, e = tx.GetMember("outside"); return e }))
	lifecycle := &integrationLifecycle{i: i, r: r}
	must(t, lifecycle.bringOntoComms(ctx, *member, model.Request{Args: map[string]any{"pane": v.Address, "terminal": v.Address, "expected_session": nil}}))
	state, e := hctest.Read(path)
	must(t, e)
	if len(state.Terminals) != 2 || state.Terminals[0].State != "finished" {
		t.Fatal(state.Terminals)
	}
	managed := state.Terminals[1]
	if managed.Labels["herdr.tab"] != v.Labels["herdr.tab"] || managed.Labels["herdr.space"] != v.Labels["herdr.space"] {
		t.Fatal(managed)
	}
	c, e := i.hcClient(ctx, "lugarn-test-fake")
	must(t, e)
	defer c.Close()
	runner, e := c.Run(ctx, hc.RunOptions{Argv: []string{"runner"}, Env: map[string]string{}, Labels: i.hcLabels("adopted", "runner")})
	must(t, e)
	must(t, i.b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.PutHCTerminal(store.HCTerminal{Team: "adopted", Core: s.Hello.Core.ID, Address: runner.Address, PID: *runner.PID}); e != nil {
			return e
		}
		return tx.Heartbeat("adopted", model.Runner{State: model.RunnerStateDown}, i.b.now())
	}))
	_, e = i.b.ForgetTeam(ctx, wire.ForgetTeamRequest{Team: "adopted", IfRevision: 1})
	must(t, e)
	state, e = hctest.Read(path)
	must(t, e)
	if len(state.Terminals) != 2 || state.Terminals[1].State != "live" {
		t.Fatal(state.Terminals)
	}
	for _, call := range state.Calls {
		if call.Method == "terminal.rm" {
			var params map[string]string
			must(t, json.Unmarshal(call.Params, &params))
			if params["terminal"] != runner.Address {
				t.Fatal("removed a member on forget", params)
			}
		}
	}
}

func TestHCAdoptionRejectsChangedBinding(t *testing.T) {
	for _, change := range []string{"space", "address", "name", "harness", "folder", "finished", "no_foreground"} {
		t.Run(change, func(t *testing.T) {
			i, _, s := hcFake(t)
			terminal := s.Terminals[0]
			space := terminal.Labels["herdr.space"]
			name := terminal.Address
			if terminal.Name != nil {
				name = *terminal.Name
			}
			address := terminal.Address
			folder := terminal.Foreground.Cwd
			switch change {
			case "space":
				terminal.Labels["herdr.space"] = "sp" + strings.Repeat("c", 32)
			case "address":
				terminal.Address = "remote/" + terminal.Address
			case "name":
				name = "someone-else"
			case "harness":
				terminal.Status = &hc.Status{App: "codex"}
			case "folder":
				folder = t.TempDir()
			case "finished":
				terminal.State = "finished"
			case "no_foreground":
				terminal.Foreground = nil
			}
			if _, e := i.validateHCExternal(context.Background(), space, address, name, model.HarnessPi, folder, terminal); e == nil {
				t.Fatal("changed external binding accepted")
			}
		})
	}
}

func TestHCCodexNoDaemonOnce(t *testing.T) {
	i, _, _ := hcFake(t)
	for _, supported := range []bool{false, true} {
		help := "other options"
		if supported {
			help += " --no-daemon"
		}
		t.Setenv("LUGARN_HC_FAKE_HELP", help)
		got := hcCodexArgs(context.Background(), i.opts.Binary, []string{"--no-daemon", "resume", "session"})
		count := 0
		for _, arg := range got {
			if arg == "--no-daemon" {
				count++
			}
		}
		if (count == 1) != supported || count > 1 {
			t.Fatal(got)
		}
	}
}

func TestHCHelloDoesNotGuessUnknownGeneration(t *testing.T) {
	i, _, _ := hcFake(t)
	for _, tc := range []struct {
		info hc.Info
		want *model.HerdrBackend
	}{
		{hc.Info{Installed: true, Running: ptr(false)}, nil},
		{hc.Info{Installed: true, Supported: ptr(true)}, ptr(model.HerdrBackendHC)},
		{hc.Info{Installed: true, Supported: ptr(false)}, ptr(model.HerdrBackendHerdr)},
		{hc.Info{Installed: false}, ptr(model.HerdrBackendHerdr)},
	} {
		i.opts.HCProbe = func(context.Context, string, string) hc.Info { return tc.info }
		result, e := i.b.Hello(context.Background(), wire.HelloRequest{Protocols: []int64{1}})
		must(t, e)
		if !reflect.DeepEqual(result.HerdrAuto, tc.want) {
			t.Fatal(result.HerdrAuto, tc.want)
		}
	}
}

type hcLaunchCapture struct{ team store.Team }

func (c *hcLaunchCapture) Launch(_ context.Context, t store.Team) error { c.team = t; return nil }
func TestHCAutoChangeClearsPlacement(t *testing.T) {
	i, _, _ := hcFake(t)
	ctx := context.Background()
	capture := &hcLaunchCapture{}
	i.b.opts.Launcher = capture
	i.opts.HCBinary = filepath.Join(t.TempDir(), "missing")
	i.opts.Herdr = func(string) HerdrAPI { return hcFallback{installed: true} }
	must(t, i.b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopped), BackendChoice: ptr(model.HerdrBackendChoiceAuto)}, i.b.now())
	}))
	_, e := i.b.StartTeam(ctx, wire.StartTeamRequest{Team: "t"})
	must(t, e)
	if capture.team.HerdrWorkspace != nil || capture.team.Backend == nil || *capture.team.Backend != model.HerdrBackendHerdr {
		t.Fatal(capture.team)
	}
}
func TestHCBackendEditWhileRunningRefused(t *testing.T) {
	i, _, _ := hcFake(t)
	ctx := context.Background()
	got, e := i.b.GetTeam(ctx, wire.GetTeamRequest{Team: "t"})
	must(t, e)
	got.Spec.Herdr.Backend = ptr(model.HerdrBackendChoiceHerdr)
	_, e = i.b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: got.Spec, IfRevision: got.Revision})
	code(t, e, model.ErrorTeamRunning)
}

// return-headless keeps waiting through blocked, as on 0.9: the person may answer
// it within the window. Only the deadline ends the wait.
func TestHCReturnHeadlessWaitsThroughBlocked(t *testing.T) {
	i, path, s := hcFake(t)
	ctx := context.Background()
	v := s.Terminals[0]
	name := v.Address
	if v.Name != nil {
		name = *v.Name
	}
	spec := model.TeamSpec{Version: 1, ID: "adopted", Name: "adopted", Pattern: model.PatternCustom, Herdr: &model.HerdrSettings{Server: ptr("lugarn-test-fake"), Backend: ptr(model.HerdrBackendChoiceHC)}, Members: []model.MemberSpec{{ID: "outside", Name: "outside", Role: model.RoleCustom, Harness: model.HarnessPi, Mode: model.ModePane, Profile: model.ProfileWorkspace, Folder: v.Foreground.Cwd, Brief: "test"}}, Grid: []model.GridEdge{}}
	_, e := i.b.AdoptTeam(ctx, wire.AdoptTeamRequest{Spec: spec, Backend: ptr(model.HerdrBackendHC), Workspace: v.Labels["herdr.space"], Members: []wire.AdoptMember{{Member: "outside", Pane: v.Address, Agent: name}}})
	must(t, e)
	must(t, i.b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("adopted", store.TeamPatch{State: ptr(model.TeamStateStarting)}, i.b.now())
	}))
	r := i.Runner("adopted", RunnerOptions{Binary: func(model.Harness) (string, error) { return i.opts.Binary, nil }})
	must(t, r.acquire(ctx))
	var member *model.Member
	must(t, i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; member, e = tx.GetMember("outside"); return e }))
	lifecycle := &integrationLifecycle{i: i, r: r}
	must(t, lifecycle.bringOntoComms(ctx, *member, model.Request{Args: map[string]any{"pane": v.Address, "terminal": v.Address, "expected_session": nil}}))
	state, e := hctest.Read(path)
	must(t, e)
	state.Terminals[1].Status = &hc.Status{App: "pi", State: "blocked", Kind: "permission"}
	must(t, hctest.Write(path, state))
	must(t, i.b.db.Read(ctx, func(tx *store.Tx) error { var e error; member, e = tx.GetMember("outside"); return e }))
	if !hcMember(*member) {
		t.Fatal("brought member is not on hc", member.Herdr)
	}
	started := time.Now()
	e = i.waitHCIdle(ctx, *member, 2500*time.Millisecond)
	if model.AsError(e).Code != model.ErrorTimeout {
		t.Fatalf("blocked member: want timeout, got %v", e)
	}
	if time.Since(started) < 2*time.Second {
		t.Fatalf("gave up after %s, before the deadline", time.Since(started))
	}
}
