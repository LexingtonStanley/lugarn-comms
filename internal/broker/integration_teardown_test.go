package broker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

type teardownHerdr struct {
	HerdrAPI
	closed []string
}

func (h *teardownHerdr) ClosePane(_ context.Context, pane string) error {
	h.closed = append(h.closed, pane)
	return nil
}
func TestIntegrationTeardownEndsOwnedLaunchArtifacts(t *testing.T) {
	b, _, m := headlessFixture(t, &headlessStub{})
	ctx := context.Background()
	home := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(home, ".lugarn", "conversations"), 0700))
	pointer := filepath.Join(home, ".lugarn", "conversations", *m.Lease.Launch)
	must(t, os.WriteFile(pointer, []byte("test pointer"), 0600))
	settings := filepath.Join(b.opts.StateDir, "launch", *m.Lease.Launch+".settings.json")
	must(t, os.WriteFile(settings, []byte("{}"), 0600))
	client := &teardownHerdr{}
	i, e := NewIntegration(b, IntegrationOptions{HomeDir: home, Herdr: func(string) HerdrAPI { return client }})
	must(t, e)
	bridge, dir, e := i.runnerBridge()
	must(t, e)
	defer os.RemoveAll(dir)
	must(t, i.savePlacement("t", runnerPlacement{Pane: "root-owned", BridgeDir: dir, Binary: bridge}))
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		team, e := tx.GetTeam("t")
		if e != nil {
			return e
		}
		runner := team.Runner
		runner.State = model.RunnerStateDown
		if e = tx.Heartbeat("t", runner, b.now()); e != nil {
			return e
		}
		return tx.UpdateTeam("t", store.TeamPatch{State: ptr(model.TeamStateStopping)}, b.now())
	}))
	var team *store.Team
	must(t, b.db.Read(ctx, func(tx *store.Tx) error { var e error; team, e = tx.GetTeam("t"); return e }))
	must(t, i.Close(ctx, *team, []model.Member{m}))
	if len(client.closed) != 1 || client.closed[0] != "root-owned" {
		t.Fatal(client.closed)
	}
	for _, path := range []string{pointer, settings, dir, filepath.Join(b.opts.StateDir, "launch", *m.Lease.Launch+".token")} {
		if _, e = os.Lstat(path); !os.IsNotExist(e) {
			t.Fatalf("artifact remains: %s: %v", path, e)
		}
	}
}
