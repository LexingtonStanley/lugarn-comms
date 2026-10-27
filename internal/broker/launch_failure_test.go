package broker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func assertLaunchFailureEvent(t *testing.T, b *Broker, member, cause string) {
	t.Helper()
	found := false
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
		events, e := tx.ReplayEvents(0, ptr("t"), 0)
		if e != nil {
			return e
		}
		for _, event := range events {
			if event.Type != model.EventMemberStateChanged {
				continue
			}
			var data wire.MemberStateChangedEventData
			if e := model.DecodeStrict(event.Data, &data); e != nil {
				return e
			}
			if data.Member.ID != member {
				continue
			}
			if data.Reason != nil && *data.Reason == model.StopReasonLaunchFailed {
				if data.Error == nil || data.Error.Code != model.ErrorLaunchFailed || data.Error.Details["cause"] != cause {
					t.Fatalf("failure not durable: %s", event.Data)
				}
				found = true
			} else if data.Error != nil {
				t.Fatalf("non-failure carried error: %s", event.Data)
			}
		}
		return nil
	}))
	if !found {
		t.Fatal("no durable failed-launch event")
	}
}
func TestLaunchFailureEventRetainsSpecificCause(t *testing.T) {
	for _, cause := range []string{"workdir_missing", "resume_picker", "unresolvable"} {
		t.Run(cause, func(t *testing.T) {
			b, r, m := headlessFixture(t, &headlessStub{})
			detail := "cause"
			if cause == "unresolvable" {
				detail = "reason"
			}
			failure := model.NewError(model.ErrorHarnessUnavailable, "specific launch problem").WithDetail(detail, cause)
			if cause == "unresolvable" {
				failure = unresolvable(errors.New("native executable disappeared"))
			}
			returned := model.AsError(r.launchFailed(context.Background(), m, failure))
			if returned.Details["cause"] != cause {
				t.Fatal(returned)
			}
			assertLaunchFailureEvent(t, b, m.ID, cause)
			raw, e := json.Marshal(wire.MemberStateChangedEventData{})
			must(t, e)
			var fields map[string]json.RawMessage
			must(t, json.Unmarshal(raw, &fields))
			if string(fields["error"]) != "null" {
				t.Fatal("normal events must emit error:null", string(raw))
			}
		})
	}
}
func TestSpawnLaunchFailureEvent(t *testing.T) {
	b, caller := setup(t)
	spawning(t, b)
	b.opts.RequestTimeout = 5 * time.Second // Override the cap-test fixture's deliberate 1 ms timeout.
	runnerReady(t, b)
	i, e := NewIntegration(b, IntegrationOptions{HomeDir: t.TempDir()})
	must(t, e)
	r := i.Runner("t", RunnerOptions{Binary: func(model.Harness) (string, error) {
		return "", model.NewError(model.ErrorHarnessUnavailable, "missing native executable").WithDetail("reason", "unresolvable")
	}})
	ctx := context.Background()
	must(t, r.acquire(ctx))
	b.opts.Wait = func(ctx context.Context, _ time.Duration) error {
		var requests []model.Request
		must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; requests, e = tx.ListOpenRequests("t"); return e }))
		for _, req := range requests {
			if req.Status == model.RequestStatusPending {
				must(t, r.executeRequest(ctx, req))
			}
		}
		return nil
	}
	_, e = b.ToolSpawn(ctx, caller, wire.SpawnInput{Name: "broken", Harness: model.HarnessCodex, Task: "ok"})
	code(t, e, model.ErrorLaunchFailed)
	var m *model.Member
	must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetLiveMember("t", "broken"); return e }))
	assertLaunchFailureEvent(t, b, m.ID, "unresolvable")
}

func TestHeadlessLaunchChecksFailFromStarting(t *testing.T) {
	for _, check := range []string{"worktree", "binary", "workdir"} {
		t.Run(check, func(t *testing.T) {
			b, _ := setup(t)
			ctx := context.Background()
			must(t, b.db.Write(ctx, func(tx *store.Tx) error {
				return tx.SetMemberState("writer", model.MemberStateStopped, nil, nil, nil, b.now())
			}))
			runnerReady(t, b)
			r := NewRunner(b, "t", RunnerOptions{Files: fakeLaunchFiles{}, Binary: func(model.Harness) (string, error) { return "fake", nil }})
			must(t, r.acquire(ctx))
			var m *model.Member
			must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; m, e = tx.GetMember("writer"); return e }))
			failure := model.NewError(model.ErrorHarnessUnavailable, "launch check failed").WithDetail("cause", check)
			switch check {
			case "worktree":
				r.prepareMember = func(_ context.Context, m model.Member) (*model.Member, error) {
					if m.State != model.MemberStateStarting {
						t.Fatal("worktree preparation preceded starting", m.State)
					}
					return nil, failure
				}
			case "binary":
				r.opts.Binary = func(model.Harness) (string, error) { return "", failure }
			case "workdir":
				m.Workdir = ptr(t.TempDir() + "/missing")
				r.prepareMember = func(_ context.Context, current model.Member) (*model.Member, error) {
					current.Workdir = m.Workdir
					return &current, nil
				}
			}
			_, err := r.launchHeadless(ctx, *m, model.LeaseCauseStart)
			code(t, err, model.ErrorLaunchFailed)
			must(t, b.db.Read(ctx, func(tx *store.Tx) error {
				events, e := tx.ReplayEvents(0, ptr("t"), 0)
				if e != nil {
					return e
				}
				var transitions []wire.MemberStateChangedEventData
				for _, event := range events {
					if event.Type == model.EventMemberStateChanged {
						var data wire.MemberStateChangedEventData
						must(t, model.DecodeStrict(event.Data, &data))
						if data.Member.ID == m.ID {
							transitions = append(transitions, data)
						}
					}
				}
				if len(transitions) != 2 || transitions[0].From != model.MemberStateStopped || transitions[0].To != model.MemberStateStarting || transitions[0].Member.State != model.MemberStateStarting || transitions[1].From != model.MemberStateStarting || transitions[1].To != model.MemberStateStopped || transitions[1].Reason == nil || *transitions[1].Reason != model.StopReasonLaunchFailed || transitions[1].Error == nil {
					t.Fatalf("launch transitions: %+v", transitions)
				}
				current, e := tx.GetMember(m.ID)
				if e == nil && (current.State != model.MemberStateStopped || current.Lease.Driver != model.DriverNone) {
					t.Fatal(current)
				}
				return e
			}))
		})
	}
}
