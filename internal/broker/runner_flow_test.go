package broker

import (
	"context"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestRunnerWriterReviewerFlow(t *testing.T) {
	b, _ := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec := model.TeamSpec{Version: 1, ID: "flow", Name: "flow", Pattern: model.PatternWriterReviewer, Limits: &model.Limits{HopBudget: ptr(3)}, Members: []model.MemberSpec{
		{ID: "flow-writer", Name: "writer", Role: model.RoleWriter, Harness: model.HarnessCodex, Folder: t.TempDir(), Profile: model.ProfileWorkspace, Mode: model.ModeHeadless, Brief: "You are {{member}} in {{team}}.\n{{roster}}"},
		{ID: "flow-reviewer", Name: "reviewer", Role: model.RoleReviewer, Harness: model.HarnessCodex, Folder: t.TempDir(), Profile: model.ProfileReadOnly, Mode: model.ModeHeadless, Brief: "Review for the writer.\n{{tools}}"},
	}, Grid: []model.GridEdge{{From: "writer", To: "reviewer", Can: []model.GridCapability{model.GridCapabilityMessage}}, {From: "reviewer", To: "writer", Can: []model.GridCapability{model.GridCapabilityMessage}}}}
	_, e := b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: spec})
	must(t, e)
	// Stand in only for C2d's runner launcher. The real runner owns launches,
	// turns, deliveries, sessions and auto-replies from this point onward.
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if _, e := tx.BumpTeamRun(spec.ID, b.now()); e != nil {
			return e
		}
		return tx.UpdateTeam(spec.ID, store.TeamPatch{State: ptr(model.TeamStateStarting), StartedAt: ptr(ptr(b.now()))}, b.now())
	}))
	driver := &headlessStub{fn: func(s turnStart) {
		session := "00000000-0000-4000-8000-000000000001"
		if s.launch.MemberID == "flow-reviewer" {
			session = "00000000-0000-4000-8000-000000000002"
		}
		s.handle.complete(TurnOutcome{Acknowledged: true, Session: &session, ExitCode: ptr(int64(0)), Text: "response from " + s.launch.MemberID})
	}}
	r := NewRunner(b, spec.ID, RunnerOptions{Headless: driver, Files: fakeLaunchFiles{}, Processes: &fakeProcesses{alive: true, birth: "fake"}, Binary: func(model.Harness) (string, error) { return "fake", nil }, PollInterval: 5 * time.Millisecond, HeartbeatInterval: 20 * time.Millisecond})
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- r.Run(runCtx) }()
	defer func() { stop(); must(t, <-done) }()
	eventually(t, func() bool {
		var ready bool
		must(t, b.db.Read(ctx, func(tx *store.Tx) error {
			members, e := tx.ListMembers(spec.ID, false)
			if e != nil {
				return e
			}
			ready = true
			for _, m := range members {
				if m.Kind == model.MemberKindAgent && (m.State != model.MemberStateRunning || m.Lease == nil || m.Lease.Driver != model.DriverHeadless) {
					ready = false
				}
			}
			return nil
		}))
		return ready
	})
	_, e = b.ToolSend(ctx, service.Caller{TeamID: spec.ID, MemberID: "flow-writer"}, wire.SendInput{To: "reviewer", Text: "review this change"})
	must(t, e)
	waitHeld := func(hop int64) {
		t.Helper()
		eventually(t, func() bool {
			var ready bool
			must(t, b.db.Read(ctx, func(tx *store.Tx) error {
				h, e := tx.GetHopCounter(spec.ID)
				if e != nil {
					return e
				}
				held, e := tx.ListTeamDeliveries(spec.ID, model.DeliveryStateHeld)
				if e != nil {
					return e
				}
				ready = h.Used == hop && len(held) == 1 && held[0].Delivery.Reason != nil && *held[0].Delivery.Reason == model.DeliveryReasonBudget
				return nil
			}))
			return ready
		})
	}
	waitHeld(4)
	continuation, e := b.ContinueBudget(ctx, wire.ContinueBudgetRequest{Team: spec.ID, Amount: ptr(int64(2))})
	must(t, e)
	if continuation.Released != 1 || continuation.Hops.Allowance != 5 {
		t.Fatal(continuation)
	}
	waitHeld(6)
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		members, e := tx.ListMembers(spec.ID, false)
		if e != nil {
			return e
		}
		for _, m := range members {
			if m.Kind == model.MemberKindHuman {
				continue
			}
			if len(m.Sessions) != 1 {
				t.Fatal(m)
			}
			turns, e := tx.ListTurns(m.ID, 100)
			if e != nil {
				return e
			}
			if len(turns) == 0 {
				t.Fatal("exchange did not run both members")
			}
			for _, turn := range turns {
				if turn.Status != model.TurnStatusSucceeded || len(turn.Consumed) != 1 || len(turn.AutoReplies) != 1 {
					t.Fatal(turn)
				}
			}
		}
		return nil
	}))
}
