package broker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/gitwt"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestTimelineZeroAndPaging(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: fmt.Sprint(i)})
		must(t, e)
	}
	zero, e := b.cursorAt(0)
	must(t, e)
	p, e := b.Timeline(ctx, wire.TimelineRequest{Team: "t", After: &zero, Limit: ptr(int64(1))})
	must(t, e)
	if len(p.Events) != 1 || p.HasMoreBefore || !p.HasMoreAfter {
		t.Fatal(p)
	}
	first := p.Events[0].Cursor
	p, e = b.Timeline(ctx, wire.TimelineRequest{Team: "t", After: &first, Limit: ptr(int64(1))})
	must(t, e)
	if len(p.Events) != 1 || !p.HasMoreBefore || !p.HasMoreAfter {
		t.Fatal(p)
	}
	p, e = b.Timeline(ctx, wire.TimelineRequest{Team: "t", Before: &zero})
	must(t, e)
	if len(p.Events) != 0 || !p.HasMoreAfter {
		t.Fatal(p)
	}
}

type launcherFunc func(context.Context, store.Team) error

func (f launcherFunc) Launch(ctx context.Context, t store.Team) error { return f(ctx, t) }
func TestCancelledStartCleansStore(t *testing.T) {
	b, _ := setup(t)
	_, e := b.StopTeam(context.Background(), wire.StopTeamRequest{Team: "t"})
	must(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.opts.Launcher = launcherFunc(func(context.Context, store.Team) error { cancel(); return context.Canceled })
	_, e = b.StartTeam(ctx, wire.StartTeamRequest{Team: "t"})
	if e == nil {
		t.Fatal("cancelled start succeeded")
	}
	s, e := b.Status(context.Background(), wire.StatusRequest{})
	must(t, e)
	if s.Teams[0].State != model.TeamStateStopped {
		t.Fatal(s)
	}
}

type brokenGit struct{ fakeGit }

func (brokenGit) RepoInfo(context.Context, string) (gitwt.Info, error) {
	return gitwt.Info{}, errors.New("git inspection failed")
}
func TestPolicyHostErrorsAreNotOutsideGit(t *testing.T) {
	b, _ := setup(t)
	b.opts.Host = nil
	b.opts.Git = &brokenGit{}
	_, e := b.ApplyTeam(context.Background(), wire.ApplyTeamRequest{Spec: model.TeamSpec{Version: 1, ID: "new", Name: "new", Pattern: model.PatternCustom, Members: []model.MemberSpec{{ID: "a", Name: "worker", Role: model.RoleWorker, Harness: model.HarnessCodex, Folder: t.TempDir(), Worktree: ptr(true), Profile: model.ProfileWorkspace, Mode: model.ModeHeadless, Brief: "brief"}}, Grid: []model.GridEdge{}}})
	code(t, e, model.ErrorInternal)
}
func addWorker(t *testing.T, b *Broker, id, name string, state model.MemberState) {
	t.Helper()
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		return tx.InsertMember(store.MemberParams{ID: id, TeamID: "t", Name: name, Kind: model.MemberKindAgent, Origin: model.MemberOriginSpawn, SpawnedBy: ptr("writer"), SpawnedRun: ptr(int64(1)), Role: ptr(model.RoleWorker), Harness: ptr(model.HarnessCodex), Profile: ptr(model.ProfileWorkspace), Mode: ptr(model.ModeHeadless), Folder: ptr(t.TempDir()), Brief: ptr("brief"), State: state})
	}))
}

type unavailableHost struct{ fakeHost }

func (unavailableHost) HarnessIssue(model.Harness) model.HarnessIssue {
	return model.HarnessIssueNotInstalled
}
func TestSpawnRefusals(t *testing.T) {
	for _, tt := range []struct {
		name      string
		want      model.ErrorCode
		configure func(*testing.T, *Broker, *service.Caller, *wire.SpawnInput)
	}{
		{"stopped", model.ErrorTeamNotRunning, func(t *testing.T, b *Broker, _ *service.Caller, _ *wire.SpawnInput) {
			must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
				return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopped)}, b.now())
			}))
		}},
		{"runner", model.ErrorRunnerUnavailable, func(t *testing.T, b *Broker, _ *service.Caller, _ *wire.SpawnInput) {
			must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
				return tx.Heartbeat("t", model.Runner{State: model.RunnerStateDown}, b.now())
			}))
		}},
		{"nesting", model.ErrorNestingNotAllowed, func(t *testing.T, b *Broker, c *service.Caller, _ *wire.SpawnInput) {
			addWorker(t, b, "child", "child", model.MemberStateRunning)
			c.MemberID = "child"
		}},
		{"disabled", model.ErrorSpawnDisabled, func(t *testing.T, b *Broker, _ *service.Caller, _ *wire.SpawnInput) {
			must(t, b.db.Write(context.Background(), func(tx *store.Tx) error { return tx.DeleteSpawnSettings("writer") }))
		}},
		{"invalid", model.ErrorInvalidRequest, func(_ *testing.T, _ *Broker, _ *service.Caller, r *wire.SpawnInput) { r.Name = "all" }},
		{"name", model.ErrorNameTaken, func(_ *testing.T, _ *Broker, _ *service.Caller, r *wire.SpawnInput) { r.Name = "reviewer" }},
		{"harness", model.ErrorHarnessNotAllowed, func(t *testing.T, b *Broker, _ *service.Caller, _ *wire.SpawnInput) {
			must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
				s, e := tx.GetSpawnSettings("writer")
				if e != nil {
					return e
				}
				s.Harnesses = ptr([]model.Harness{model.HarnessClaude})
				return tx.PutSpawnSettings(*s)
			}))
		}},
		{"host", model.ErrorHarnessUnavailable, func(_ *testing.T, b *Broker, _ *service.Caller, _ *wire.SpawnInput) { b.opts.Host = unavailableHost{} }},
		{"profile", model.ErrorProfileTooHigh, func(_ *testing.T, _ *Broker, _ *service.Caller, r *wire.SpawnInput) {
			r.Profile = ptr(model.ProfileBypass)
		}},
		{"running_cap", model.ErrorSpawnCapReached, func(t *testing.T, b *Broker, _ *service.Caller, _ *wire.SpawnInput) {
			addWorker(t, b, "child", "child", model.MemberStateStarting)
		}},
		{"run_cap", model.ErrorSpawnCapReached, func(t *testing.T, b *Broker, _ *service.Caller, _ *wire.SpawnInput) {
			addWorker(t, b, "one", "one", model.MemberStateStopped)
			addWorker(t, b, "two", "two", model.MemberStateStopped)
		}},
		{"team_cap", model.ErrorSpawnCapReached, func(t *testing.T, b *Broker, _ *service.Caller, _ *wire.SpawnInput) {
			must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
				for i := 0; i < 22; i++ {
					id := fmt.Sprintf("extra%d", i)
					if e := tx.InsertMember(store.MemberParams{ID: id, TeamID: "t", Name: id, Kind: model.MemberKindAgent, Origin: model.MemberOriginSpec, Role: ptr(model.RoleWorker), Harness: ptr(model.HarnessCodex), Profile: ptr(model.ProfileReadOnly), Mode: ptr(model.ModeHeadless), Folder: ptr(t.TempDir()), Brief: ptr("brief"), State: model.MemberStateStopped}); e != nil {
						return e
					}
				}
				return nil
			}))
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, c := setup(t)
			spawning(t, b)
			r := wire.SpawnInput{Name: "newworker", Harness: model.HarnessCodex, Task: "task"}
			tt.configure(t, b, &c, &r)
			_, e := b.ToolSpawn(context.Background(), c, r)
			code(t, e, tt.want)
			policyRefusal := model.ExitCode(tt.want) >= 20 && model.ExitCode(tt.want) <= 26
			must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
				es, e := tx.ReplayEvents(0, ptr("t"), 0)
				if e != nil {
					return e
				}
				if policyRefusal && (len(es) != 1 || es[0].Type != model.EventPolicyDenied) {
					t.Fatal(es)
				}
				if !policyRefusal && len(es) != 0 {
					t.Fatal(es)
				}
				return nil
			}))
		})
	}
}
func TestSpawnRequestCompletion(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			b, c := setup(t)
			spawning(t, b)
			b.opts.RequestTimeout = time.Second
			b.opts.Wait = func(ctx context.Context, _ time.Duration) error {
				return b.db.Write(ctx, func(tx *store.Tx) error {
					rs, e := tx.ListOpenRequests("t")
					if e != nil {
						return e
					}
					r := rs[0]
					now := b.now()
					p := store.RequestPatch{Status: model.RequestStatusSucceeded, FinishedAt: ptr(&now)}
					state := model.MemberStateRunning
					var reason *model.StopReason
					if failed {
						state = model.MemberStateStopped
						reason = ptr(model.StopReasonLaunchFailed)
						p.Status = model.RequestStatusFailed
						p.Error = ptr(model.NewError(model.ErrorLaunchFailed, "fake runner failure"))
					}
					if e = tx.SetMemberState(r.Member.ID, state, reason, nil, nil, now); e != nil {
						return e
					}
					return tx.UpdateRequest(r.ID, r.Status, p)
				})
			}
			out, e := b.ToolSpawn(context.Background(), c, wire.SpawnInput{Name: "child", Harness: model.HarnessCodex, Task: "task"})
			if failed {
				code(t, e, model.ErrorLaunchFailed)
			} else {
				must(t, e)
				if out.Member.State != model.MemberStateRunning || out.TaskMessage == "" {
					t.Fatal(out)
				}
			}
		})
	}
}

func TestStoppedTeamAndMissingResources(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	_, e := b.StopTeam(ctx, wire.StopTeamRequest{Team: "t"})
	must(t, e)
	checks := []func() error{
		func() error { _, e := b.Send(ctx, wire.SendRequest{Team: "t", To: ptr("writer"), Text: "x"}); return e },
		func() error { _, e := b.ContinueBudget(ctx, wire.ContinueBudgetRequest{Team: "t"}); return e },
		func() error {
			_, e := b.Hold(ctx, wire.HoldRequest{Team: "t", Member: "writer", TTLSeconds: ptr(int64(10))})
			return e
		},
		func() error {
			_, e := b.OpenTerminal(ctx, wire.OpenTerminalRequest{Team: "t", Member: "writer"})
			return e
		},
		func() error {
			_, e := b.ReturnHeadless(ctx, wire.ReturnHeadlessRequest{Team: "t", Member: "writer"})
			return e
		},
		func() error { _, e := b.StopMember(ctx, wire.StopMemberRequest{Team: "t", Member: "writer"}); return e },
		func() error { _, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "x"}); return e },
		func() error { _, e := b.ToolReply(ctx, c, wire.ReplyInput{ID: "missing", Text: "x"}); return e },
		func() error { _, e := b.ToolInbox(ctx, c, wire.InboxInput{}); return e },
		func() error { _, e := b.ToolTeam(ctx, c, wire.TeamInput{}); return e },
		func() error { _, e := b.ToolStop(ctx, c, wire.StopInput{Member: "reviewer"}); return e },
	}
	for i, check := range checks {
		t.Run(fmt.Sprint(i), func(t *testing.T) { code(t, check(), model.ErrorTeamNotRunning) })
	}
	_, e = b.GetTeam(ctx, wire.GetTeamRequest{Team: "missing"})
	code(t, e, model.ErrorNotFound)
	_, e = b.Status(ctx, wire.StatusRequest{Team: ptr("missing")})
	code(t, e, model.ErrorNotFound)
	_, e = b.Timeline(ctx, wire.TimelineRequest{Team: "missing"})
	code(t, e, model.ErrorNotFound)
	_, e = b.Worktrees(ctx, wire.WorktreesRequest{Team: "missing"})
	code(t, e, model.ErrorNotFound)
	_, e = b.RemoveWorktrees(ctx, wire.RemoveWorktreesRequest{Team: "missing", Paths: []string{"x"}})
	code(t, e, model.ErrorNotFound)
}

func TestResolveHistoricalRecipient(t *testing.T) {
	b, c := setup(t)
	ctx := context.Background()
	m, e := b.ToolSend(ctx, c, wire.SendInput{To: "reviewer", Text: "x"})
	must(t, e)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.UpdateDelivery(m.ID, "reviewer", model.DeliveryStateQueued, store.DeliveryPatch{State: model.DeliveryStateUncertain, Reason: ptr(ptr(model.DeliveryReasonNoActivity)), UpdatedAt: b.now()}); e != nil {
			return e
		}
		return tx.RemoveMember("reviewer", model.RemovedReasonRemovedFromSpec, b.now())
	}))
	out, e := b.ResolveDelivery(ctx, wire.ResolveDeliveryRequest{Team: "t", Message: m.ID, Member: "reviewer", Action: model.ResolveActionMarkDelivered})
	must(t, e)
	if out.Delivery.State != model.DeliveryStateDelivered {
		t.Fatal(out)
	}
}
