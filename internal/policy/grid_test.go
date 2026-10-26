package policy

import (
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestGridCan(t *testing.T) {
	g := NewGrid([]model.GridEdge{
		{From: "a", To: "b", Can: []model.GridCapability{model.GridCapabilityMessage, model.GridCapabilityWake}},
		{From: "b", To: "a", Can: []model.GridCapability{model.GridCapabilityMessage}},
		{From: "c", To: "a", Can: []model.GridCapability{model.GridCapabilityWake}}, // refused by ValidateSpec; grants only wake
	})
	msg, wake := model.GridCapabilityMessage, model.GridCapabilityWake
	cases := []struct {
		from, to string
		c        model.GridCapability
		want     bool
	}{
		{"a", "b", msg, true},
		{"a", "b", wake, true},
		{"b", "a", msg, true},
		{"b", "a", wake, false},
		{"a", "c", msg, false},
		{"c", "a", msg, false},
		{"c", "a", wake, true},
		{"a", "a", msg, false},
		{"a", "b", "spawn", false},
		// human's implicit row: message and wake everyone.
		{"human", "a", msg, true},
		{"human", "a", wake, true},
		{"human", "stranger", msg, true},
		{"human", "a", "spawn", false},
		// human's implicit column: everyone may message human, nobody wakes it.
		{"a", "human", msg, true},
		{"c", "human", msg, true},
		{"a", "human", wake, false},
		{"human", "human", msg, false},
	}
	for _, c := range cases {
		if got := g.Can(c.from, c.to, c.c); got != c.want {
			t.Errorf("Can(%s, %s, %s) = %v; want %v", c.from, c.to, c.c, got, c.want)
		}
	}
	equal(t, "CanList a→b", g.CanList("a", "b"), []model.GridCapability{msg, wake})
	equal(t, "CanList a→c", g.CanList("a", "c"), []model.GridCapability{})
	equal(t, "CanList a→human", g.CanList("a", "human"), []model.GridCapability{msg})
	var zero Grid
	equal(t, "zero grid", [3]bool{zero.Can("a", "b", msg), zero.Can("human", "a", wake), zero.Can("a", "human", msg)}, [3]bool{false, true, true})
}

func TestSpawnEdges(t *testing.T) {
	edges := SpawnEdges("lead", "web")
	equal(t, "edges", edges, []model.GridEdge{
		{From: "lead", To: "web", Can: []model.GridCapability{model.GridCapabilityMessage, model.GridCapabilityWake}},
		{From: "web", To: "lead", Can: []model.GridCapability{model.GridCapabilityMessage}},
	})
	base := NewGrid([]model.GridEdge{{From: "lead", To: "api", Can: []model.GridCapability{model.GridCapabilityMessage}}})
	g := base.With(edges...)
	for _, c := range []struct {
		from, to string
		c        model.GridCapability
		want     bool
	}{
		{"lead", "web", model.GridCapabilityMessage, true},
		{"lead", "web", model.GridCapabilityWake, true},
		{"web", "lead", model.GridCapabilityMessage, true},
		{"web", "lead", model.GridCapabilityWake, false},
		{"web", "api", model.GridCapabilityMessage, false}, // nothing to other workers
		{"api", "web", model.GridCapabilityMessage, false},
		{"lead", "api", model.GridCapabilityMessage, true},
	} {
		if got := g.Can(c.from, c.to, c.c); got != c.want {
			t.Errorf("Can(%s, %s, %s) = %v; want %v", c.from, c.to, c.c, got, c.want)
		}
	}
	if base.Can("lead", "web", model.GridCapabilityMessage) {
		t.Fatal("With modified its receiver")
	}
}
