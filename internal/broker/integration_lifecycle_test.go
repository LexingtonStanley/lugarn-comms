package broker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

type lifecycleHerdr struct {
	adapterHerdr
	env    map[string]string
	args   []string
	closed int
}

func (h *lifecycleHerdr) OpenTab(_ context.Context, _ string, _ string, _ string, env map[string]string) (herdr.Tab, error) {
	h.env = env
	return herdr.Tab{ID: "tab-owned", RootPane: "pane-owned", Workspace: "workspace-owned"}, nil
}
func (h *lifecycleHerdr) CloseTab(context.Context, string) error { h.closed++; return nil }
func (h *lifecycleHerdr) RunCommand(_ context.Context, _ string, args ...string) error {
	if len(args) > 1 && args[1] == "pane-launch" {
		return nil
	}
	return os.WriteFile(filepath.Join(filepath.Dir(h.env["LUGARN_COMMS_PANE_CONFIG"]), "shell-ready"), []byte("ready"), 0600)
}
func (h *lifecycleHerdr) StartAgent(_ context.Context, o herdr.StartOptions) (herdr.Agent, error) {
	h.args = o.Args
	return herdr.Agent{Activity: model.ActivityBlocked}, nil
}
func TestIntegrationPaneLaunchAndSessionBeforePane(t *testing.T) {
	for _, harness := range []model.Harness{model.HarnessClaude, model.HarnessCodex} {
		t.Run(string(harness), func(t *testing.T) {
			b, _ := setup(t)
			t.Setenv("CODEX_HOME", t.TempDir())
			ctx := context.Background()
			must(t, b.db.Write(ctx, func(tx *store.Tx) error {
				if e := tx.UpdateTeam("t", store.TeamPatch{HerdrWorkspace: ptr(ptr("workspace-owned"))}, b.now()); e != nil {
					return e
				}
				return tx.UpdateMember("writer", store.MemberPatch{Harness: ptr(&harness), Mode: ptr(ptr(model.ModePane))}, b.now())
			}))
			runnerReady(t, b)
			client := &lifecycleHerdr{}
			binary, e := os.Executable()
			must(t, e)
			integration, e := NewIntegration(b, IntegrationOptions{Binary: binary, HomeDir: t.TempDir(), Herdr: func(string) HerdrAPI { return client }})
			must(t, e)
			r := integration.Runner("t", RunnerOptions{})
			r.opts.Files = fakeLaunchFiles{}
			r.opts.Binary = func(model.Harness) (string, error) { return binary, nil }
			r.opts.Processes = &fakeProcesses{alive: true, birth: "fake"}
			var prompt string
			r.opts.Headless = &headlessStub{fn: func(s turnStart) {
				prompt = s.prompt
				if harness == model.HarnessCodex {
					writeCodexRollout(t, "00000000-0000-4000-8000-000000000001", s.launch.Workdir)
				}
				s.handle.complete(TurnOutcome{Acknowledged: true, Session: ptr("00000000-0000-4000-8000-000000000001"), ExitCode: ptr(int64(0)), Text: "ready"})
			}}
			must(t, r.acquire(ctx))
			sendToWriter(t, b, "task must wait for pane")
			var m *model.Member
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("writer"); return e }))
			if harness == model.HarnessCodex {
				m, e = r.launchHeadless(ctx, *m, model.LeaseCauseStart)
				must(t, e)
			}
			must(t, r.opts.PaneLifecycle.Open(ctx, *m, model.LeaseCauseStart))
			must(t, b.db.Read(ctx, func(tx *store.Tx) error {
				v, e := tx.GetMember(m.ID)
				if e != nil {
					return e
				}
				if v.State != model.MemberStateRunning || v.Lease.Driver != model.DriverPane || v.Activity == nil || *v.Activity != model.ActivityBlocked || v.Herdr == nil || v.Herdr.Agent == nil || *v.Herdr.Agent != herdr.AgentName("t", "writer") {
					t.Fatal(v)
				}
				queued, e := tx.NextDeliveries(m.ID, 0)
				if e != nil {
					return e
				}
				if len(queued) != 1 {
					t.Fatal("brief-only turn consumed task")
				}
				turns, e := tx.ListTurns(m.ID, 10)
				if e != nil {
					return e
				}
				if harness == model.HarnessCodex {
					if len(turns) != 1 || len(turns[0].Consumed) != 0 || len(turns[0].AutoReplies) != 0 || !strings.HasSuffix(prompt, policy.WaitForTaskLine) {
						t.Fatal(turns, prompt)
					}
				} else if len(turns) != 0 {
					t.Fatal("Claude needlessly ran a first turn")
				}
				return nil
			}))
			var before, after *model.Member
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; before, e = tx.GetMember("writer"); return e }))
			if e := r.opts.PaneLifecycle.Open(ctx, *before, model.LeaseCauseOpenTerminal); e == nil {
				t.Fatal("expected refusal")
			}
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; after, e = tx.GetMember("writer"); return e }))
			if after.State != before.State || after.Lease.Generation != before.Lease.Generation {
				t.Fatal("refusal changed healthy member", after)
			}
		})
	}
}

// Windows fakes observe the generated capsule instead of starting a shell.
func (h *lifecycleHerdr) Agents(context.Context) ([]herdr.Agent, error) {
	raw, e := os.ReadFile(h.env["LUGARN_COMMS_PANE_CONFIG"])
	if e != nil {
		return nil, e
	}
	var config paneLaunchCapsule
	if e = json.Unmarshal(raw, &config); e != nil {
		return nil, e
	}
	return []herdr.Agent{{Kind: string(config.Harness), Pane: "pane-owned", Workspace: "workspace-owned", Activity: model.ActivityBlocked}}, nil
}
func (h *lifecycleHerdr) RenameAgent(context.Context, string, string) error {
	raw, e := os.ReadFile(h.env["LUGARN_COMMS_PANE_CONFIG"])
	if e != nil {
		return e
	}
	var config paneLaunchCapsule
	if e = json.Unmarshal(raw, &config); e != nil {
		return e
	}
	h.args = config.Args
	return nil
}
