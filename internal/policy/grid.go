package policy

import "github.com/LexingtonStanley/lugarn-comms/internal/model"

// Reserved member names.
const (
	HumanName = "human"
	AllName   = "all"
)

// Grid answers what one member may do to another. It is keyed by member name,
// as the spec's grid is; the service layer builds it from the stored grid and
// the live roster. human's row and column are implicit and never stored:
// human may message and wake every member, and every member may message
// human. Nobody may act on itself. The zero Grid denies everything except
// human's implicit cells.
type Grid struct {
	caps map[gridKey]gridCaps
}

type gridKey struct{ from, to string }

type gridCaps struct{ message, wake bool }

// NewGrid builds a grid from edges. A wake without message on the same edge
// grants nothing beyond what's listed; ValidateSpec refuses such edges.
func NewGrid(edges []model.GridEdge) Grid {
	g := Grid{caps: map[gridKey]gridCaps{}}
	g.add(edges)
	return g
}

func (g *Grid) add(edges []model.GridEdge) {
	for _, e := range edges {
		k := gridKey{e.From, e.To}
		c := g.caps[k]
		for _, cap := range e.Can {
			switch cap {
			case model.GridCapabilityMessage:
				c.message = true
			case model.GridCapabilityWake:
				c.wake = true
			}
		}
		g.caps[k] = c
	}
}

// With returns a copy of g with edges added. g is unchanged.
func (g Grid) With(edges ...model.GridEdge) Grid {
	out := Grid{caps: make(map[gridKey]gridCaps, len(g.caps)+len(edges))}
	for k, v := range g.caps {
		out.caps[k] = v
	}
	out.add(edges)
	return out
}

// Can reports whether from may use capability c on to.
func (g Grid) Can(from, to string, c model.GridCapability) bool {
	if from == to {
		return false
	}
	if from == HumanName {
		return c == model.GridCapabilityMessage || c == model.GridCapabilityWake
	}
	if to == HumanName {
		return c == model.GridCapabilityMessage
	}
	caps := g.caps[gridKey{from, to}]
	switch c {
	case model.GridCapabilityMessage:
		return caps.message
	case model.GridCapabilityWake:
		return caps.wake
	}
	return false
}

// CanList returns what from may do to to, message before wake: team()'s
// members[].can and the roster's "you may" list.
func (g Grid) CanList(from, to string) []model.GridCapability {
	out := []model.GridCapability{}
	for _, c := range []model.GridCapability{model.GridCapabilityMessage, model.GridCapabilityWake} {
		if g.Can(from, to, c) {
			out = append(out, c)
		}
	}
	return out
}

// SpawnEdges are the edges a spawn adds: spawner → worker message and wake,
// worker → spawner message. Nothing to other workers.
func SpawnEdges(spawner, worker string) []model.GridEdge {
	return []model.GridEdge{
		{From: spawner, To: worker, Can: []model.GridCapability{model.GridCapabilityMessage, model.GridCapabilityWake}},
		{From: worker, To: spawner, Can: []model.GridCapability{model.GridCapabilityMessage}},
	}
}
