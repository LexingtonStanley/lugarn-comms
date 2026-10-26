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

func TestRunnerWakeLimitsAndHumanOverride(t *testing.T) {
	b, _ := setup(t)
	spawning(t, b)
	addWorker(t, b, "busy", "busy", model.MemberStateRunning)
	addWorker(t, b, "child", "child", model.MemberStateStopped)
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.InsertLease("child", model.Lease{Driver: model.DriverNone}, b.now()); e != nil {
			return e
		}
		return tx.AddGridEdges("t", []store.GridEdge{{TeamID: "t", FromID: "writer", ToID: "child", Can: []model.GridCapability{model.GridCapabilityMessage, model.GridCapabilityWake}}})
	}))
	_, e := b.ToolSend(ctx, service.Caller{TeamID: "t", MemberID: "writer"}, wire.SendInput{To: "child", Text: "agent wake"})
	must(t, e)
	runnerReady(t, b)
	r := NewRunner(b, "t", RunnerOptions{Files: fakeLaunchFiles{}, Binary: func(model.Harness) (string, error) { return "fake", nil }})
	must(t, r.acquire(ctx))
	var child *model.Member
	must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; child, e = tx.GetMember("child"); return e }))
	must(t, r.wakeMember(ctx, *child))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		v, e := tx.GetMember("child")
		if e == nil && v.State != model.MemberStateStopped {
			t.Fatal("agent bypassed worker cap")
		}
		return e
	}))
	_, e = b.Send(ctx, wire.SendRequest{Team: "t", To: ptr("child"), Text: "human wake"})
	must(t, e)
	must(t, r.wakeMember(ctx, *child))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		v, e := tx.GetMember("child")
		if e != nil {
			return e
		}
		if v.State != model.MemberStateRunning || v.Lease.Driver != model.DriverHeadless || v.Lease.Generation != 1 {
			t.Fatal(v)
		}
		return nil
	}))
}
func TestRunnerWakeLastDriverAndDefaultPaneFailure(t *testing.T) {
	b, r, m := headlessFixture(t, &headlessStub{})
	ctx := context.Background()
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.EndLaunch(*m.Lease.Launch, model.LaunchEndReasonStopped, b.now()); e != nil {
			return e
		}
		if e := tx.CompareAndSwapLease(m.ID, model.DriverHeadless, m.Lease.Generation, model.Lease{Driver: model.DriverNone, Generation: m.Lease.Generation + 1}, b.now()); e != nil {
			return e
		}
		if e := tx.UpdateMember(m.ID, store.MemberPatch{LastDriver: ptr(ptr(model.ModePane))}, b.now()); e != nil {
			return e
		}
		return tx.SetMemberState(m.ID, model.MemberStateStopped, ptr(model.StopReasonPerson), nil, nil, b.now())
	}))
	sendToWriter(t, b, "no wake edge")
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		v, e := tx.GetMember(m.ID)
		if e == nil {
			m = *v
		}
		return e
	}))
	must(t, r.wakeMember(ctx, m))
	_, e := b.Send(ctx, wire.SendRequest{Team: "t", To: &m.ID, Text: "wake to previous pane"})
	must(t, e)
	must(t, r.wakeMember(ctx, m))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		v, e := tx.GetMember(m.ID)
		if e == nil && (v.State != model.MemberStateStopped || v.StopReason == nil || *v.StopReason != model.StopReasonLaunchFailed) {
			t.Fatal(v)
		}
		return e
	}))
}

type failingPaneLauncher struct {
	NotBuiltRunnerDrivers
	attempts int
}

func (p *failingPaneLauncher) Open(context.Context, model.Member, model.LeaseCause) error {
	p.attempts++
	return runnerNotBuilt()
}

func TestRunnerLaunchFailureNeedsNewWake(t *testing.T) {
	b, caller := setup(t)
	ctx := context.Background()
	now := b.now().Time
	b.opts.Clock = func() time.Time { return now }
	old, err := b.ToolSend(ctx, caller, wire.SendInput{To: "reviewer", Text: "old wake"})
	must(t, err)
	_, err = b.Send(ctx, wire.SendRequest{Team: "t", To: ptr("reviewer"), Text: "old human wake"})
	must(t, err)
	now = now.Add(time.Second)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if err := tx.UpdateMember("reviewer", store.MemberPatch{Mode: ptr(ptr(model.ModePane)), LastDriver: ptr(ptr(model.ModePane))}, b.now()); err != nil {
			return err
		}
		return tx.SetMemberState("reviewer", model.MemberStateStopped, ptr(model.StopReasonLaunchFailed), nil, ptr(ptr(b.now())), b.now())
	}))
	runnerReady(t, b)
	launcher := &failingPaneLauncher{}
	r := NewRunner(b, "t", RunnerOptions{PaneLifecycle: launcher})
	must(t, r.acquire(ctx))
	pass := func() {
		t.Helper()
		var m *model.Member
		must(t, b.db.Read(ctx, func(tx *store.Tx) error { var err error; m, err = tx.GetMember("reviewer"); return err }))
		must(t, r.workMember(ctx, *m))
	}
	for i := 0; i < 3; i++ {
		pass()
	}
	if launcher.attempts != 0 {
		t.Fatalf("old wake retried a failed launch %d times", launcher.attempts)
	}
	for want := 1; want <= 2; want++ {
		now = now.Add(time.Second)
		_, err := b.ToolSend(ctx, caller, wire.SendInput{To: "reviewer", Text: "new wake"})
		must(t, err)
		pass()
		if launcher.attempts != want {
			t.Fatalf("attempts=%d, want %d", launcher.attempts, want)
		}
		// Preserve an old queued wake, as an interrupted/external launcher may do.
		must(t, b.db.Write(ctx, func(tx *store.Tx) error {
			d, err := tx.GetDelivery(old.ID, "reviewer")
			if err != nil {
				return err
			}
			return tx.UpdateDelivery(old.ID, "reviewer", d.State, store.DeliveryPatch{State: model.DeliveryStateQueued, Reason: ptr((*model.DeliveryReason)(nil)), UpdatedAt: b.now()})
		}))
		for i := 0; i < 3; i++ {
			pass()
		}
		if launcher.attempts != want {
			t.Fatalf("same wake retried every pass: attempts=%d, want %d", launcher.attempts, want)
		}
	}
	now = now.Add(time.Second)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		d, err := tx.GetDelivery(old.ID, "reviewer")
		if err != nil {
			return err
		}
		return tx.UpdateDelivery(old.ID, "reviewer", d.State, store.DeliveryPatch{State: model.DeliveryStateHeld, Reason: ptr(ptr(model.DeliveryReasonDeliveryFailed)), UpdatedAt: b.now()})
	}))
	_, err = b.ResolveDelivery(ctx, wire.ResolveDeliveryRequest{Team: "t", Message: old.ID, Member: "reviewer", Action: model.ResolveActionResend})
	must(t, err)
	pass()
	if launcher.attempts != 3 {
		t.Fatalf("person's resend did not retry: attempts=%d", launcher.attempts)
	}

}
