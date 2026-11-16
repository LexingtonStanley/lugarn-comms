package broker

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
)

// Each members call observes one set of lists for its team's server. Nothing
// is cached across polls or used to authorize adoption/conversion actions.
type externalSnapshot struct {
	workspaces map[string]herdr.Workspace
	panes      map[string]herdr.Pane
	agents     map[string]herdr.Agent
}

func (i *Integration) listExternal(ctx context.Context, server string) (externalSnapshot, error) {
	s := externalSnapshot{workspaces: map[string]herdr.Workspace{}, panes: map[string]herdr.Pane{}, agents: map[string]herdr.Agent{}}
	c := i.opts.Herdr(server)
	ws, e := c.Workspaces(ctx)
	if e != nil {
		return s, e
	}
	ps, e := c.Panes(ctx)
	if e != nil {
		return s, e
	}
	as, e := c.Agents(ctx)
	if e != nil {
		return s, e
	}
	for _, w := range ws {
		s.workspaces[w.ID] = w
	}
	for _, p := range ps {
		s.panes[p.ID] = p
	}
	for _, a := range as {
		s.agents[a.Name] = a
	}
	return s, nil
}
