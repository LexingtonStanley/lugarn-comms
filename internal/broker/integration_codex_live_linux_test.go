//go:build linux

package broker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

// This test-only proxy bypasses the launch cwd guarantee without adding a
// production bypass: it changes the actual pane cwd and drops explicit --cd.
type wrongCWDHerdr struct {
	HerdrAPI
	cwd string
}

func (h wrongCWDHerdr) OpenTab(ctx context.Context, w, n, _ string, env map[string]string) (herdr.Tab, error) {
	return h.HerdrAPI.OpenTab(ctx, w, n, h.cwd, env)
}
func (h wrongCWDHerdr) StartAgent(ctx context.Context, o herdr.StartOptions) (herdr.Agent, error) {
	var args []string
	for n := 0; n < len(o.Args); n++ {
		if o.Args[n] == "--cd" {
			n++
			continue
		}
		args = append(args, o.Args[n])
	}
	o.Args = args
	return h.HerdrAPI.StartAgent(ctx, o)
}
func TestIntegrationLiveCodexWorkdir(t *testing.T) {
	if os.Getenv("LUGARN_INTEGRATION_CODEX_CWD_LIVE") != "1" {
		t.Skip("explicit cheap Codex cwd/picker probe opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	b, _ := setup(t)
	b.opts.Probe = hostprobe.New()
	herdrBin, e := exec.LookPath("herdr")
	must(t, e)
	binary := filepath.Join(t.TempDir(), "lugarn-comms")
	raw, e := exec.Command("go", "build", "-o", binary, "../../cmd/lugarn-comms").CombinedOutput()
	if e != nil {
		t.Fatalf("build: %s %v", raw, e)
	}
	session := privateIntegrationSession(t)
	started, e := herdr.StartServer(ctx, herdrBin, session)
	if started.Started {
		defer stopIntegrationServer(t, herdrBin, session, 0)
	}
	must(t, e)
	c := herdr.New(herdrBin, session)
	a, bdir := filepath.Join(t.TempDir(), "a"), t.TempDir()
	must(t, os.Mkdir(a, 0700))
	ws, e := c.CreateWorkspace(ctx, "codex-workdir-probe", b.opts.StateDir, nil)
	must(t, e)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.UpdateTeam("t", store.TeamPatch{HerdrServer: &session, HerdrWorkspace: ptr(&ws.ID)}, b.now()); e != nil {
			return e
		}
		return tx.UpdateMember("writer", store.MemberPatch{Folder: ptr(&a)}, b.now())
	}))
	runnerReady(t, b)
	i, e := NewIntegration(b, IntegrationOptions{Binary: binary, HerdrBinary: herdrBin, HomeDir: t.TempDir(), Models: map[model.Harness]string{model.HarnessCodex: "gpt-6-luna"}, Herdr: func(string) HerdrAPI { return wrongCWDHerdr{HerdrAPI: c, cwd: bdir} }})
	must(t, e)
	r := i.Runner("t", RunnerOptions{TurnTimeout: 60 * time.Second})
	must(t, r.acquire(ctx))
	l := r.opts.PaneLifecycle.(*integrationLifecycle)
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 35*time.Second)
		defer done()
		var team *store.Team
		var members []model.Member
		_ = b.db.Write(cleanup, func(tx *store.Tx) error {
			team, _ = tx.GetTeam("t")
			members, _ = tx.ListMembers("t", true)
			runner := team.Runner
			runner.State = model.RunnerStateDown
			return tx.Heartbeat("t", runner, b.now())
		})
		if team != nil {
			if e := i.Close(cleanup, *team, members); e != nil {
				t.Logf("cleanup: %v", e)
			}
		}
	}()
	m, e := l.current(ctx, "writer")
	must(t, e)
	m, e = r.launchHeadless(ctx, *m, model.LeaseCauseStart)
	must(t, e)
	turn, e := r.prepareTurnKind(ctx, *m, true)
	must(t, e)
	if turn == nil {
		t.Fatal("no brief turn")
	}
	must(t, r.runPreparedTurn(ctx, turn))
	m, e = l.current(ctx, m.ID)
	must(t, e)
	if m.Session == nil {
		t.Fatal("Codex did not return a thread")
	}
	t.Logf("gpt-6-luna brief-only turn session=%s", *m.Session)
	must(t, os.Remove(a))
	e = l.Open(ctx, *m, model.LeaseCauseOpenTerminal)
	if model.AsError(e).Details["cause"] != "workdir_missing" {
		t.Fatalf("removed A: %v", e)
	}
	t.Log("removed rollout cwd A: launch_failed cause=workdir_missing")
	must(t, os.Mkdir(a, 0700))
	must(t, l.Open(ctx, *m, model.LeaseCauseOpenTerminal))
	m, e = l.current(ctx, m.ID)
	must(t, e)
	e = l.checkPane(ctx, *m)
	if model.AsError(e).Details["cause"] != "resume_picker" {
		t.Fatalf("test-only cwd B: %v", e)
	}
	t.Log("test-only pane cwd B: launch_failed cause=resume_picker; pane closed; no delivery submitted")
}
