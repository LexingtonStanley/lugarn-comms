package broker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func writeCodexRollout(t *testing.T, id, cwd string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("CODEX_HOME"), "sessions", "2026", "10", "26")
	must(t, os.MkdirAll(dir, 0700))
	raw, e := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": id, "cwd": cwd}})
	must(t, e)
	must(t, os.WriteFile(filepath.Join(dir, "rollout-2026-10-26-"+id+".jsonl"), append(raw, '\n'), 0600))
}
func TestCodexWorkdirUsesRollout(t *testing.T) {
	b, _ := setup(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	a, bdir := t.TempDir(), t.TempDir()
	id := "00000000-0000-4000-8000-000000000001"
	m := model.Member{ID: "writer", Harness: ptr(model.HarnessCodex), Session: &id, Workdir: &a}
	failure := func(cause string) {
		t.Helper()
		e := model.AsError(b.assertCodexWorkdir(m))
		if e == nil || e.Code != model.ErrorLaunchFailed || e.Details["cause"] != cause {
			t.Fatal(e)
		}
	}
	failure("rollout_missing")
	writeCodexRollout(t, id, a)
	must(t, b.assertCodexWorkdir(m))
	m.Workdir = &bdir
	failure("workdir_changed")
	m.Workdir = &a
	must(t, os.Remove(a))
	failure("workdir_missing")
}

type pickerHerdr struct {
	lifecycleHerdr
	text string
}

func (h *pickerHerdr) ReadAgent(context.Context, string, int) (string, error) { return h.text, nil }
func TestCodexResumePickerClosesWithoutDelivery(t *testing.T) {
	panes := &panesStub{activity: model.ActivityIdle, active: true}
	b, r, m := paneFixture(t, panes)
	h := &pickerHerdr{text: "Working directory · resume\n1. Session\n2. Current"}
	i, e := NewIntegration(b, IntegrationOptions{Herdr: func(string) HerdrAPI { return h }})
	must(t, e)
	l := &integrationLifecycle{i: i, r: r}
	m.Harness = ptr(model.HarnessCodex)
	e = l.checkPane(context.Background(), m)
	if model.AsError(e).Details["cause"] != "resume_picker" || h.closed != 1 || len(panes.prompts) != 0 {
		t.Fatal(e, h.closed)
	}
}
