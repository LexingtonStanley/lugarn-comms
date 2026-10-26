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

type panesStub struct {
	activity     model.Activity
	settled      model.Activity
	gone, active bool
	submitErr    error
	prompts      []string
	waited       bool
}

func (p *panesStub) State(context.Context, model.Member) (PaneObservation, error) {
	return PaneObservation{Activity: p.activity, Gone: p.gone}, nil
}
func (p *panesStub) Submit(_ context.Context, _ model.Member, prompt string) error {
	p.prompts = append(p.prompts, prompt)
	return p.submitErr
}
func (p *panesStub) WaitSettled(context.Context, model.Member, time.Duration) (PaneObservation, error) {
	p.waited = true
	return PaneObservation{Activity: p.settled}, nil
}
func (p *panesStub) WaitActivity(context.Context, model.Member, time.Duration) (bool, error) {
	return p.active, nil
}
func paneFixture(t *testing.T, p Panes) (*Broker, *Runner, model.Member) {
	t.Helper()
	b, r, m := headlessFixture(t, &headlessStub{})
	r.opts.Panes = p
	r.opts.HomeDir = t.TempDir()
	id, e := model.NewULID()
	must(t, e)
	must(t, b.db.Write(context.Background(), func(tx *store.Tx) error {
		if e := tx.EndLaunch(*m.Lease.Launch, model.LaunchEndReasonConverted, b.now()); e != nil {
			return e
		}
		if e := tx.InsertLaunch(store.Launch{ID: id, MemberID: m.ID, Generation: 2, Driver: model.ModePane, PaneID: ptr("pane"), StartedAt: b.now()}); e != nil {
			return e
		}
		if e := tx.CompareAndSwapLease(m.ID, model.DriverHeadless, 1, model.Lease{Driver: model.DriverPane, Generation: 2, Launch: &id, Pane: ptr("pane")}, b.now()); e != nil {
			return e
		}
		if e := tx.UpdateMember(m.ID, store.MemberPatch{Harness: ptr(ptr(model.HarnessClaude))}, b.now()); e != nil {
			return e
		}
		v, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		m = *v
		if e = r.session(tx, m, "00000000-0000-4000-8000-000000000001", "7KQ2M9XA", model.SessionSourceAssigned, id, false); e != nil {
			return e
		}
		v, e = tx.GetMember(m.ID)
		if e == nil {
			m = *v
		}
		return e
	}))
	return b, r, m
}
func TestRunnerPaneTransitions(t *testing.T) {
	for _, tt := range []struct {
		name     string
		activity model.Activity
		active   bool
		err      error
		want     model.DeliveryState
		reason   *model.DeliveryReason
	}{
		{"delivered", model.ActivityIdle, true, nil, model.DeliveryStateDelivered, nil},
		{"working", model.ActivityWorking, true, nil, model.DeliveryStateDelivered, nil},
		{"blocked", model.ActivityBlocked, false, nil, model.DeliveryStateHeld, ptr(model.DeliveryReasonRecipientBlocked)},
		{"unknown", model.ActivityUnknown, false, nil, model.DeliveryStateHeld, ptr(model.DeliveryReasonRecipientUnknown)},
		{"failed", model.ActivityDone, false, errors.New("submit failed"), model.DeliveryStateHeld, ptr(model.DeliveryReasonDeliveryFailed)},
		{"uncertain", model.ActivityIdle, false, nil, model.DeliveryStateUncertain, ptr(model.DeliveryReasonNoActivity)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &panesStub{activity: tt.activity, settled: model.ActivityIdle, active: tt.active, submitErr: tt.err}
			b, r, m := paneFixture(t, p)
			msg := sendToWriter(t, b, "message")
			must(t, r.runPane(context.Background(), m))
			must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
				d, e := tx.GetDelivery(msg.ID, m.ID)
				if e != nil {
					return e
				}
				if d.State != tt.want || (tt.reason != nil && (d.Reason == nil || *d.Reason != *tt.reason)) {
					t.Fatal(d)
				}
				return nil
			}))
			if tt.activity == model.ActivityWorking && !p.waited {
				t.Fatal("working pane wasn't waited on")
			}
			if tt.activity == model.ActivityBlocked || tt.activity == model.ActivityUnknown {
				p.activity = model.ActivityIdle
				p.active = true
				must(t, r.runPane(context.Background(), m))
				must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
					d, e := tx.GetDelivery(msg.ID, m.ID)
					if e == nil && d.State != model.DeliveryStateDelivered {
						t.Fatal(d)
					}
					return e
				}))
			} else {
				n := len(p.prompts)
				must(t, r.runPane(context.Background(), m))
				if len(p.prompts) != n {
					t.Fatal("terminal/failed delivery retried automatically")
				}
			}
		})
	}
}
func TestRunnerPaneOverflowAndPointerSession(t *testing.T) {
	p := &panesStub{activity: model.ActivityIdle, active: true}
	b, r, m := paneFixture(t, p)
	id := "00000000-0000-4000-8000-000000000002"
	root := filepath.Join(r.opts.HomeDir, ".lugarn", "conversations")
	must(t, os.MkdirAll(root, 0700))
	must(t, os.WriteFile(filepath.Join(root, *m.Lease.Launch), []byte("{\"session_id\":\""+id+"\"}\n{\"session_id\":\"00000000-0000-4000-8000-000000000003\",\"agent_id\":\"child\"}\n"), 0600))
	msg := sendToWriter(t, b, strings.Repeat("界", 4000))
	must(t, r.runPane(context.Background(), m))
	if len(p.prompts) != 1 || !strings.Contains(p.prompts[0], "read that file now") {
		t.Fatal(p.prompts)
	}
	path := filepath.Join(b.opts.StateDir, "launch", *m.Lease.Launch, "msg-"+msg.ID+"-1.md")
	raw, e := os.ReadFile(path)
	must(t, e)
	if !strings.Contains(string(raw), "Real Lugarn headers") || !strings.Contains(string(raw), strings.Repeat("界", 100)) {
		t.Fatal("overflow lost the sealed prompt")
	}
	must(t, b.db.Read(context.Background(), func(tx *store.Tx) error {
		v, e := tx.GetMember(m.ID)
		if e != nil {
			return e
		}
		if v.Session == nil || *v.Session != id || len(v.Sessions) != 2 || v.Sessions[1].Source != model.SessionSourcePointer {
			t.Fatal(v)
		}
		return nil
	}))
}

type lifecycleStub struct {
	exits  int
	member string
}

func (p *lifecycleStub) Open(context.Context, model.Member, model.LeaseCause) error {
	return runnerNotBuilt()
}
func (p *lifecycleStub) Exited(_ context.Context, m model.Member) error {
	p.exits++
	p.member = m.ID
	return nil
}
func TestRunnerReconcileGonePaneUsesLifecycle(t *testing.T) {
	p := &panesStub{gone: true}
	_, r, m := paneFixture(t, p)
	lifecycle := &lifecycleStub{}
	r.opts.PaneLifecycle = lifecycle
	must(t, r.reconcile(context.Background()))
	if lifecycle.exits != 1 || lifecycle.member != m.ID {
		t.Fatal(lifecycle)
	}
}

type blockingActivityPanes struct {
	panesStub
	submitted chan struct{}
}

func (p *blockingActivityPanes) Submit(context.Context, model.Member, string) error {
	close(p.submitted)
	return nil
}
func (p *blockingActivityPanes) WaitActivity(ctx context.Context, _ model.Member, _ time.Duration) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}
func TestRunnerStoppingMakesPaneSubmissionUncertain(t *testing.T) {
	p := &blockingActivityPanes{panesStub: panesStub{activity: model.ActivityIdle}, submitted: make(chan struct{})}
	b, r, m := paneFixture(t, p)
	message := sendToWriter(t, b, "task")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	workCtx, stop := context.WithCancel(ctx)
	r.worker(workCtx, m.ID, func(ctx context.Context) error { return r.runPane(ctx, m) })
	select {
	case <-p.submitted:
	case <-ctx.Done():
		t.Fatal("pane prompt was not submitted")
	}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopping)}, b.now())
	}))
	r.stopping.Store(true)
	stop()
	must(t, r.stop(ctx))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		d, e := tx.GetDelivery(message.ID, m.ID)
		if e == nil && (d.State != model.DeliveryStateUncertain || *d.Reason != model.DeliveryReasonNoActivity) {
			t.Fatal(d)
		}
		return e
	}))
}
