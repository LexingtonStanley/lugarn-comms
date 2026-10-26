package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

const launchSecret = "SENTINEL_PRIVATE_LAUNCH_ENV_ARGV_STDERR"

func init() {
	integrationHelpers["private-launch-failure"] = func() int { fmt.Fprintln(os.Stderr, os.Getenv("PRIVATE_TEST")); return 7 }
}

type privateLaunchHerdr struct {
	lifecycleHerdr
	step string
	t    *testing.T
}

func (h *privateLaunchHerdr) OpenTab(ctx context.Context, w, n, d string, env map[string]string) (herdr.Tab, error) {
	if env["PRIVATE_TEST"] != launchSecret {
		h.t.Fatal("launch environment sentinel missing")
	}
	if h.step == "pane" {
		return herdr.Tab{}, errors.New("environment " + launchSecret)
	}
	return h.lifecycleHerdr.OpenTab(ctx, w, n, d, env)
}
func (h *privateLaunchHerdr) RunCommand(ctx context.Context, p string, args ...string) error {
	if h.step == "shell" {
		return fmt.Errorf("bootstrap argv %s", launchSecret)
	}
	return h.lifecycleHerdr.RunCommand(ctx, p, args...)
}
func (h *privateLaunchHerdr) StartAgent(_ context.Context, o herdr.StartOptions) (herdr.Agent, error) {
	if !strings.Contains(strings.Join(o.Args, " "), launchSecret) {
		h.t.Fatal("launch argv sentinel missing")
	}
	binary, e := os.Executable()
	if e != nil {
		return herdr.Agent{}, e
	}
	cmd := exec.Command(binary, o.Args...)
	cmd.Env = append(os.Environ(), "LUGARN_INTEGRATION_HELPER=private-launch-failure", "PRIVATE_TEST="+launchSecret)
	_, e = cmd.Output()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || !bytes.Contains(exit.Stderr, []byte(launchSecret)) {
		h.t.Fatal("fake harness did not fail with private stderr", e)
	}
	return herdr.Agent{}, fmt.Errorf("harness output %s: %w", exit.Stderr, e)
}
func assertPrivateFailure(t *testing.T, b *Broker, err error, runnerLog string) {
	t.Helper()
	if err == nil {
		t.Fatal("wanted launch failure")
	}
	raw, e := json.Marshal(model.AsError(err))
	must(t, e)
	if strings.Contains(err.Error()+string(raw)+runnerLog, launchSecret) {
		t.Fatal("returned error or runner log leaked launch data")
	}
	found := false
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
		events, e := tx.ReplayEvents(0, ptr("t"), 0)
		if e != nil {
			return e
		}
		for _, event := range events {
			if bytes.Contains(event.Data, []byte(launchSecret)) {
				t.Fatal("event leaked launch data", event.Type)
			}
			if event.Type == model.EventMemberStateChanged && bytes.Contains(event.Data, []byte(`"launch_failed"`)) {
				found = true
			}
		}
		return nil
	}))
	if !found {
		t.Fatal("no launch failure event")
	}
}
func TestPaneFailurePrivacyAcrossLaunchSteps(t *testing.T) {
	for _, step := range []string{"pane", "shell", "harness"} {
		t.Run(step, func(t *testing.T) {
			b, _ := setup(t)
			ctx := context.Background()
			must(t, b.db.Write(ctx, func(tx *store.Tx) error {
				if e := tx.UpdateTeam("t", store.TeamPatch{HerdrWorkspace: ptr(ptr("workspace-owned"))}, b.now()); e != nil {
					return e
				}
				return tx.UpdateMember("writer", store.MemberPatch{Harness: ptr(ptr(model.HarnessClaude)), Mode: ptr(ptr(model.ModePane))}, b.now())
			}))
			runnerReady(t, b)
			h := &privateLaunchHerdr{step: step, t: t}
			binary, e := os.Executable()
			must(t, e)
			i, e := NewIntegration(b, IntegrationOptions{Binary: binary, HomeDir: t.TempDir(), Models: map[model.Harness]string{model.HarnessClaude: launchSecret}, ExtraEnv: map[string]string{"PRIVATE_TEST": launchSecret}, Herdr: func(string) HerdrAPI { return h }})
			must(t, e)
			// Exercise the clean shell adapter on every host; the capsule is tested separately.
			i.windowsPanes = false
			r := i.Runner("t", RunnerOptions{Binary: func(model.Harness) (string, error) { return binary, nil }})
			must(t, r.acquire(ctx))
			var m *model.Member
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("writer"); return e }))
			var output bytes.Buffer
			old := log.Writer()
			log.SetOutput(&output)
			defer log.SetOutput(old)
			err := r.opts.PaneLifecycle.Open(ctx, *m, model.LeaseCauseStart)
			if err != nil {
				log.Print(err)
			}
			assertPrivateFailure(t, b, err, output.String())
		})
	}
}

type privateLaunchFiles struct{ fakeLaunchFiles }

func (privateLaunchFiles) Write(context.Context, TurnLaunch) error {
	return errors.New("file contents " + launchSecret)
}
func TestHeadlessFailurePrivacyBeforeLaunch(t *testing.T) {
	for _, step := range []string{"worktree", "resolve", "files"} {
		t.Run(step, func(t *testing.T) {
			b, _ := setup(t)
			runnerReady(t, b)
			r := NewRunner(b, "t", RunnerOptions{Files: fakeLaunchFiles{}, Binary: func(model.Harness) (string, error) { return "fake", nil }})
			ctx := context.Background()
			must(t, r.acquire(ctx))
			var current *model.Member
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; current, e = tx.GetMember("writer"); return e }))
			m := *current
			switch step {
			case "worktree":
				r.prepareMember = func(context.Context, model.Member) (*model.Member, error) {
					return nil, errors.New("git output " + launchSecret)
				}
			case "resolve":
				r.opts.Binary = func(model.Harness) (string, error) { return "", fmt.Errorf("exec argv %s", launchSecret) }
			case "files":
				r.opts.Files = privateLaunchFiles{}
			}
			_, err := r.launchHeadless(context.Background(), m, model.LeaseCauseStart)
			var output bytes.Buffer
			fmt.Fprintln(&output, err)
			assertPrivateFailure(t, b, err, output.String())
		})
	}
}
