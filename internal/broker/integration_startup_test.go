package broker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

func TestPaneBootstrapWait(t *testing.T) {
	for _, marker := range []string{"shell-ready", "pane-started"} {
		for _, outcome := range []string{"missing", "invalid", "ready", "cancelled"} {
			t.Run(marker+"/"+outcome, func(t *testing.T) {
				b, _ := setup(t)
				i, e := NewIntegration(b, IntegrationOptions{})
				must(t, e)
				id := cleanupID(t)
				root := filepath.Join(b.opts.StateDir, "launch", id)
				must(t, os.MkdirAll(root, 0700))
				if outcome == "invalid" {
					must(t, os.WriteFile(filepath.Join(root, marker), []byte("incomplete"), 0600))
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				waited := time.Duration(0)
				b.opts.Wait = func(_ context.Context, d time.Duration) error {
					waited += d
					if outcome == "cancelled" {
						cancel()
						return ctx.Err()
					}
					if outcome == "ready" && waited >= 60*time.Millisecond {
						return os.WriteFile(filepath.Join(root, marker), []byte("ready"), 0600)
					}
					return nil
				}
				err := i.waitPaneMarker(ctx, id, marker, &launcherHerdr{}, "pane")
				switch outcome {
				case "ready":
					must(t, err)
					if waited != 60*time.Millisecond {
						t.Fatal(waited)
					}
				case "cancelled":
					if !errors.Is(err, context.Canceled) {
						t.Fatal(err)
					}
				default:
					failure := model.AsError(err)
					if failure == nil || failure.Code != model.ErrorLaunchFailed || failure.Details["cause"] != "shell_not_ready" || waited != 20*time.Second {
						t.Fatal(failure, waited)
					}
				}
			})
		}
	}
}

type swallowedRunnerHerdr struct {
	launcherHerdr
	broker    bool
	inspected int
}

func (h *swallowedRunnerHerdr) ProcessInfo(ctx context.Context, pane string) (herdr.ProcessInfo, error) {
	if ctx.Err() != nil || pane != "p-owned" {
		return herdr.ProcessInfo{}, errors.New("invalid inspection")
	}
	h.inspected++
	if h.broker {
		return herdr.ProcessInfo{Foreground: []herdr.ForegroundProcess{{Argv: []string{h.argv[0], "run"}}}}, nil
	}
	return h.launcherHerdr.ProcessInfo(ctx, pane)
}

func (h *swallowedRunnerHerdr) RunCommand(_ context.Context, _ string, argv ...string) error {
	h.argv = argv
	return nil
}

func TestRunnerStartupTimeoutDistinguishesBroker(t *testing.T) {
	for _, broker := range []bool{false, true} {
		t.Run(map[bool]string{false: "shell", true: "broker"}[broker], func(t *testing.T) {
			b, _ := setup(t)
			runnerReady(t, b)
			h := &swallowedRunnerHerdr{launcherHerdr: launcherHerdr{b: b}, broker: broker}
			i, e := NewIntegration(b, IntegrationOptions{Herdr: func(string) HerdrAPI { return h }})
			must(t, e)
			waited := time.Duration(0)
			b.opts.Wait = func(_ context.Context, d time.Duration) error { waited += d; return nil }
			var team *store.Team
			must(t, b.db.Read(context.Background(), func(tx *store.Tx) error { var e error; team, e = tx.GetTeam("t"); return e }))
			failure := model.AsError(i.Launch(context.Background(), *team))
			cause := "shell_not_ready"
			if broker {
				cause = "runner_no_heartbeat"
			}
			if failure == nil || failure.Code != model.ErrorRunnerUnavailable || failure.Details["cause"] != cause || waited != 15*time.Second || h.inspected != 1 {
				t.Fatal(failure, waited, h.inspected)
			}
			if !broker && !strings.Contains(failure.Message, "startup question") {
				t.Fatal(failure)
			}
			must(t, i.Close(context.Background(), *team, nil))
		})
	}
}

type swallowedBootstrapHerdr struct {
	lifecycleHerdr
	binary    string
	inspected int
}

func (h *swallowedBootstrapHerdr) ProcessInfo(ctx context.Context, pane string) (herdr.ProcessInfo, error) {
	if ctx.Err() != nil || pane != "pane-owned" {
		return herdr.ProcessInfo{}, errors.New("invalid inspection")
	}
	h.inspected++
	if h.binary != "" {
		return herdr.ProcessInfo{Foreground: []herdr.ForegroundProcess{{Argv: []string{h.binary, "pane-shell"}}}}, nil
	}
	return h.lifecycleHerdr.ProcessInfo(ctx, pane)
}

func (*swallowedBootstrapHerdr) RunCommand(context.Context, string, ...string) error { return nil }

func TestHarnessStartupTimeoutRecordsShellCause(t *testing.T) {
	for _, broker := range []bool{false, true} {
		for _, windows := range []bool{false, true} {
			t.Run(map[bool]string{false: "shell/", true: "broker/"}[broker]+map[bool]string{false: "posix", true: "windows"}[windows], func(t *testing.T) {
				b, r, m := headlessFixture(t, &headlessStub{})
				binary, e := os.Executable()
				must(t, e)
				client := &swallowedBootstrapHerdr{}
				i, e := NewIntegration(b, IntegrationOptions{Binary: fakePlainBrokerPath(binary), HomeDir: t.TempDir(), Herdr: func(string) HerdrAPI { return client }})
				must(t, e)
				i.windowsPanes = windows
				if broker {
					client.binary = i.opts.Binary
				}
				r.opts.Binary = func(model.Harness) (string, error) { return binary, nil }
				configureWindowsPaneMember(t, b, &m)
				b.opts.Wait = func(context.Context, time.Duration) error { return nil }
				l := &integrationLifecycle{i: i, r: r}
				failure := model.AsError(l.Open(context.Background(), m, model.LeaseCauseOpenTerminal))
				cause := "shell_not_ready"
				if broker {
					cause = "bootstrap_no_marker"
				}
				if failure == nil || failure.Code != model.ErrorLaunchFailed || failure.Details["cause"] != cause || client.inspected != 1 {
					t.Fatal(failure)
				}
				assertLaunchFailureEvent(t, b, m.ID, cause)
				if len(client.args) != 0 {
					t.Fatal("started a harness without a bootstrap acknowledgement")
				}
				must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
					current, e := tx.GetMember(m.ID)
					if e == nil && (current.State != model.MemberStateStopped || current.Lease.Driver != model.DriverNone) {
						t.Fatal(current)
					}
					return e
				}))
			})
		}
	}
}

type processSnapshotHerdr struct {
	HerdrAPI
	info      herdr.ProcessInfo
	err       error
	inspected bool
}

func (h *processSnapshotHerdr) ProcessInfo(ctx context.Context, _ string) (herdr.ProcessInfo, error) {
	h.inspected = ctx.Err() == nil
	deadline, bounded := ctx.Deadline()
	if !bounded || time.Until(deadline) > 2*time.Second {
		panic("unbounded inspection")
	}
	return h.info, h.err
}
func TestStartupTimeoutUsesBrokerEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		process    herdr.ForegroundProcess
		inspectErr error
		want       string
	}{
		{name: "broker argv", process: herdr.ForegroundProcess{Argv: []string{"/tmp/broker", "run"}}, want: "runner_no_heartbeat"},
		{name: "broker argv0", process: herdr.ForegroundProcess{Argv0: "/tmp/broker"}, want: "runner_no_heartbeat"},
		{name: "broker name only", process: herdr.ForegroundProcess{Name: "broker"}, want: "runner_no_heartbeat"},
		{name: "shell", process: herdr.ForegroundProcess{Name: "zsh"}, want: "shell_not_ready"},
		{name: "unrelated command", process: herdr.ForegroundProcess{Argv: []string{"/usr/bin/sleep", "100"}}, want: "shell_not_ready"},
		{name: "same name other executable", process: herdr.ForegroundProcess{Name: "broker", Argv: []string{"/unrelated/broker"}}, want: "shell_not_ready"},
		{name: "missing process info", inspectErr: errors.New("unavailable"), want: "shell_not_ready"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h := &processSnapshotHerdr{info: herdr.ProcessInfo{Foreground: []herdr.ForegroundProcess{scenario.process}}, err: scenario.inspectErr}
			failure := startupTimeout(context.Background(), model.ErrorRunnerUnavailable, h, "pane", "/tmp/broker", "runner_no_heartbeat")
			if !h.inspected || failure.Code != model.ErrorRunnerUnavailable || failure.Details["cause"] != scenario.want {
				t.Fatal(failure, h.inspected)
			}
		})
	}
}
