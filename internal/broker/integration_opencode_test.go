package broker

import (
	"context"
	"os"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

type openCodeProbe struct{ fakeProbe }

func (openCodeProbe) Harnesses(context.Context) []model.HarnessInfo {
	return []model.HarnessInfo{{Harness: model.HarnessOpenCode, Installed: true, Supported: true, Headless: true, Version: ptr("2.1.4")}}
}
func TestOpenCodeSessionBeforePaneAndDelivery(t *testing.T) {
	b, _ := setup(t)
	b.opts.Probe = openCodeProbe{}
	ctx := context.Background()
	runnerReady(t, b)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		if e := tx.UpdateTeam("t", store.TeamPatch{HerdrWorkspace: ptr(ptr("workspace-owned"))}, b.now()); e != nil {
			return e
		}
		return tx.UpdateMember("writer", store.MemberPatch{Harness: ptr(ptr(model.HarnessOpenCode)), Mode: ptr(ptr(model.ModePane))}, b.now())
	}))
	h := &lifecycleHerdr{adapterHerdr: adapterHerdr{activity: model.ActivityIdle}}
	binary, e := os.Executable()
	must(t, e)
	i, e := NewIntegration(b, IntegrationOptions{Binary: binary, HomeDir: t.TempDir(), Models: map[model.Harness]string{model.HarnessOpenCode: "opencode-go/kimi-k3.1-pro"}, Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	r := i.Runner("t", RunnerOptions{Binary: func(model.Harness) (string, error) { return binary, nil }, Processes: &fakeProcesses{alive: true, birth: "fake"}})
	r.opts.Headless = &headlessStub{fn: func(s turnStart) {
		s.handle.complete(TurnOutcome{Acknowledged: true, Session: ptr("ses_fixture"), ExitCode: ptr(int64(0)), Text: "ready"})
	}}
	must(t, r.acquire(ctx))
	l := r.opts.PaneLifecycle.(*integrationLifecycle)
	message := sendToWriter(t, b, "Reply with the word ok.")
	m, e := l.current(ctx, "writer")
	must(t, e)
	must(t, l.Open(ctx, *m, model.LeaseCauseStart))
	m, e = l.current(ctx, m.ID)
	must(t, e)
	must(t, r.runPane(ctx, *m))
	must(t, b.db.Read(ctx, func(tx *store.Tx) error {
		turns, e := tx.ListTurns(m.ID, 10)
		if e != nil {
			return e
		}
		if len(turns) != 1 || len(turns[0].Consumed) != 0 || len(turns[0].AutoReplies) != 0 {
			t.Fatal("brief-only turn consumed/replied to messages", turns)
		}
		delivery, e := tx.GetDelivery(message.ID, m.ID)
		if e == nil && delivery.State != model.DeliveryStateDelivered {
			t.Fatal(delivery)
		}
		return e
	}))
}
