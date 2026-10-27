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
	"path/filepath"
	"strings"
	"testing"
	"time"

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
			b.opts.Wait = func(context.Context, time.Duration) error { return nil }
			r.paneRetry(ctx, *m, errors.New("pane diagnostics "+launchSecret))
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

const publicTurnReason = "error: unknown option '--standalone'"
const hostTurnSecret = "SENTINEL_HOST_API_KEY_PRIVATE"

func init() {
	integrationHelpers["headless-private-failure"] = func() int {
		mode := os.Getenv("LUGARN_PRIVATE_FAILURE_MODE")
		reason := publicTurnReason
		if mode != "plain" {
			token, _ := os.ReadFile(os.Getenv("LUGARN_COMMS_TOKEN_FILE"))
			reason = os.Getenv("PRIVATE_TEST") + " " + os.Getenv("LUGARN_PRIVATE_API_KEY") + " " + strings.TrimSpace(string(token)) + " " + reason
		}
		switch mode {
		case "claude":
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "result", "is_error": true, "result": reason})
		case "codex":
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "turn.failed", "error": map[string]any{"message": reason}})
		case "opencode":
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "error", "error": map[string]any{"data": map[string]any{"message": reason}}})
		default:
			fmt.Fprintln(os.Stderr, "earlier stderr line")
			fmt.Fprintln(os.Stderr, reason)
			fmt.Fprintln(os.Stderr)
		}
		return 7
	}
}

func TestHeadlessFailureReasonSurvivesWithSecretsScrubbed(t *testing.T) {
	for _, mode := range []string{"claude", "codex", "opencode", "stderr", "plain"} {
		t.Run(mode, func(t *testing.T) {
			b, _ := setup(t)
			ctx := context.Background()
			t.Setenv("LUGARN_PRIVATE_API_KEY", hostTurnSecret)
			binary, e := os.Executable()
			must(t, e)
			kind := model.HarnessCodex
			if mode == "claude" {
				kind = model.HarnessClaude
			} else if mode == "opencode" {
				kind = model.HarnessOpenCode
				b.opts.Probe = openCodeProbe{}
			}
			must(t, b.db.Write(ctx, func(tx *store.Tx) error {
				return tx.UpdateMember("writer", store.MemberPatch{Harness: ptr(&kind)}, b.now())
			}))
			runnerReady(t, b)
			i, e := NewIntegration(b, IntegrationOptions{Binary: binary, HomeDir: t.TempDir(), ExtraEnv: map[string]string{
				"LUGARN_INTEGRATION_HELPER": "headless-private-failure", "LUGARN_PRIVATE_FAILURE_MODE": mode, "PRIVATE_TEST": launchSecret,
			}})
			must(t, e)
			r := i.Runner("t", RunnerOptions{Binary: func(model.Harness) (string, error) { return binary, nil }, Processes: &fakeProcesses{alive: true, birth: "fake"}})
			must(t, r.acquire(ctx))
			var member *model.Member
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; member, e = tx.GetMember("writer"); return e }))
			member, e = r.launchHeadless(ctx, *member, model.LeaseCauseStart)
			must(t, e)
			// This is the launch's own token, created in this test's scratch state.
			token, e := os.ReadFile(filepath.Join(b.opts.StateDir, "launch", *member.Lease.Launch+".token"))
			must(t, e)
			sendToWriter(t, b, "test failure diagnostics")
			must(t, r.runTurn(ctx, *member))
			want := publicTurnReason
			if mode != "plain" {
				want = "‹redacted› ‹redacted› ‹redacted› " + want
			}
			var turnError string
			must(t, b.db.Read(ctx, func(tx *store.Tx) error {
				turns, e := tx.ListTurns("writer", 10)
				if e != nil {
					return e
				}
				if len(turns) != 1 || turns[0].Status != model.TurnStatusFailed || turns[0].Error == nil {
					t.Fatalf("missing failed turn: %+v", turns)
				}
				turnError = *turns[0].Error
				if turnError != want {
					t.Fatalf("reason = %q, want %q", turnError, want)
				}
				events, e := tx.ReplayEvents(0, ptr("t"), 0)
				if e != nil {
					return e
				}
				found := false
				for _, event := range events {
					for _, secret := range []string{launchSecret, hostTurnSecret, strings.TrimSpace(string(token))} {
						if bytes.Contains(event.Data, []byte(secret)) {
							t.Fatal("secret entered durable event", event.Type)
						}
					}
					if event.Type == model.EventTurnFinished {
						var data struct {
							Turn model.Turn `json:"turn"`
						}
						must(t, json.Unmarshal(event.Data, &data))
						if data.Turn.Error == nil || *data.Turn.Error != want {
							t.Fatal("event lost useful failure reason")
						}
						found = true
					}
				}
				if !found {
					t.Fatal("missing turn.finished event")
				}
				return nil
			}))
			var output bytes.Buffer
			old := log.Writer()
			log.SetOutput(&output)
			defer log.SetOutput(old)
			b.opts.Wait = func(context.Context, time.Duration) error { return nil }
			r.paneRetry(ctx, *member, model.NewError(model.ErrorLaunchFailed, turnError))
			for _, secret := range []string{launchSecret, hostTurnSecret, strings.TrimSpace(string(token))} {
				if strings.Contains(output.String(), secret) {
					t.Fatal("secret entered runner log")
				}
			}
			if !strings.Contains(output.String(), publicTurnReason) {
				t.Fatal("logger lost failure reason")
			}
		})
	}
}
