package broker

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestMain(m *testing.M) {
	if os.Getenv("LUGARN_INTEGRATION_HELPER") == "exit-seven" {
		os.Exit(7)
	}
	if os.Getenv("LUGARN_INTEGRATION_HELPER") == "codex" {
		_, _ = os.Stdout.WriteString("{\"type\":\"thread.started\",\"thread_id\":\"00000000-0000-4000-8000-000000000001\"}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"ok\"}}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestIntegrationHeadlessAdapter(t *testing.T) {
	b, _ := setup(t)
	bin, e := os.Executable()
	must(t, e)
	i, e := NewIntegration(b, IntegrationOptions{Binary: bin, HomeDir: t.TempDir(), ExtraEnv: map[string]string{"LUGARN_INTEGRATION_HELPER": "codex"}})
	must(t, e)
	id, e := model.NewULID()
	must(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, e := i.adapter.Start(ctx, TurnLaunch{Harness: model.HarnessCodex, Profile: model.ProfileReadOnly, Workdir: t.TempDir(), StateDir: b.opts.StateDir, LaunchID: id, MemberID: "member", Binary: bin}, "tiny fake prompt")
	must(t, e)
	cancel() // This is the runner's startup context, not a request to kill the turn.
	out := h.Wait()
	if !out.Acknowledged || out.Text != "ok" || out.Session == nil || out.ExitCode == nil || *out.ExitCode != 0 {
		t.Fatal(out)
	}
	ack := <-h.Acknowledgements()
	if ack.Session == nil || *ack.Session != *out.Session {
		t.Fatal(ack)
	}
}

type adapterHerdr struct {
	HerdrAPI
	activity model.Activity
	prompted bool
	states   []model.Activity
}

func (h *adapterHerdr) Agent(context.Context, string) (herdr.Agent, error) {
	return herdr.Agent{Activity: h.activity}, nil
}
func (h *adapterHerdr) Prompt(context.Context, string, string) (herdr.Agent, error) {
	h.prompted = true
	h.activity = model.ActivityWorking
	return herdr.Agent{Activity: h.activity}, nil
}
func (h *adapterHerdr) Wait(_ context.Context, _ string, states []model.Activity, _ time.Duration) (herdr.Agent, error) {
	h.states = states
	return herdr.Agent{Activity: model.ActivityIdle}, nil
}
func TestIntegrationPaneAdapterObservesActivity(t *testing.T) {
	b, _ := setup(t)
	client := &adapterHerdr{activity: model.ActivityIdle}
	i, e := NewIntegration(b, IntegrationOptions{Herdr: func(string) HerdrAPI { return client }})
	must(t, e)
	m := model.Member{ID: "member", Team: "team", Name: "writer"}
	obs, e := i.panes.State(context.Background(), m)
	must(t, e)
	if obs.Activity != model.ActivityIdle {
		t.Fatal(obs)
	}
	must(t, i.panes.Submit(context.Background(), m, "message"))
	confirmed, e := i.panes.WaitActivity(context.Background(), m, time.Second)
	must(t, e)
	if !confirmed || !client.prompted {
		t.Fatal("activity was not confirmed")
	}
}
