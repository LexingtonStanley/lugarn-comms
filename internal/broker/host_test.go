package broker

import (
	"context"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type countingProbe struct {
	fakeProbe
	harnessCalls int
}

func (p *countingProbe) Harnesses(ctx context.Context) []model.HarnessInfo {
	p.harnessCalls++
	return p.fakeProbe.Harnesses(ctx)
}

func TestTeamProbesHarnessesOnlyForSpawner(t *testing.T) {
	b, caller := setup(t)
	probe := &countingProbe{}
	b.opts.Host = nil
	b.opts.Probe = probe
	out, err := b.ToolTeam(context.Background(), caller, wire.TeamInput{})
	must(t, err)
	if out.Me.CanSpawn || probe.harnessCalls != 0 {
		t.Fatalf("non-spawner: can_spawn=%v, harness probes=%d", out.Me.CanSpawn, probe.harnessCalls)
	}
	spawning(t, b)
	out, err = b.ToolTeam(context.Background(), caller, wire.TeamInput{})
	must(t, err)
	if !out.Me.CanSpawn || probe.harnessCalls != 1 {
		t.Fatalf("spawner: can_spawn=%v, harness probes=%d", out.Me.CanSpawn, probe.harnessCalls)
	}
}
