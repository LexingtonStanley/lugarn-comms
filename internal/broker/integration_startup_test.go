package broker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
				err := i.waitPaneMarker(ctx, id, marker)
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

type swallowedRunnerHerdr struct{ launcherHerdr }

func (*swallowedRunnerHerdr) RunCommand(context.Context, string, ...string) error { return nil }

func TestRunnerStartupTimeoutExplainsShellQuestion(t *testing.T) {
	b, _ := setup(t)
	runnerReady(t, b)
	h := &swallowedRunnerHerdr{launcherHerdr: launcherHerdr{b: b}}
	i, e := NewIntegration(b, IntegrationOptions{Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	waited := time.Duration(0)
	b.opts.Wait = func(_ context.Context, d time.Duration) error { waited += d; return nil }
	var team *store.Team
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error { var e error; team, e = tx.GetTeam("t"); return e }))
	failure := model.AsError(i.Launch(context.Background(), *team))
	if failure == nil || failure.Code != model.ErrorRunnerUnavailable || failure.Details["cause"] != "shell_not_ready" || !strings.Contains(failure.Message, "startup question") || waited != 15*time.Second {
		t.Fatal(failure, waited)
	}
	must(t, i.Close(context.Background(), *team, nil))
}

type swallowedBootstrapHerdr struct{ lifecycleHerdr }

func (*swallowedBootstrapHerdr) RunCommand(context.Context, string, ...string) error { return nil }

func TestHarnessStartupTimeoutRecordsShellCause(t *testing.T) {
	for _, windows := range []bool{false, true} {
		t.Run(map[bool]string{false: "posix", true: "windows"}[windows], func(t *testing.T) {
			b, r, m := headlessFixture(t, &headlessStub{})
			binary, e := os.Executable()
			must(t, e)
			client := &swallowedBootstrapHerdr{}
			i, e := NewIntegration(b, IntegrationOptions{Binary: fakePlainBrokerPath(binary), HomeDir: t.TempDir(), Herdr: func(string) HerdrAPI { return client }})
			must(t, e)
			i.windowsPanes = windows
			r.opts.Binary = func(model.Harness) (string, error) { return binary, nil }
			configureWindowsPaneMember(t, b, &m)
			b.opts.Wait = func(context.Context, time.Duration) error { return nil }
			l := &integrationLifecycle{i: i, r: r}
			failure := model.AsError(l.Open(context.Background(), m, model.LeaseCauseOpenTerminal))
			if failure == nil || failure.Code != model.ErrorLaunchFailed || failure.Details["cause"] != "shell_not_ready" {
				t.Fatal(failure)
			}
			assertLaunchFailureEvent(t, b, m.ID, "shell_not_ready")
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
