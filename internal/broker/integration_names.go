package broker

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

// The immediate transaction serializes allocations across runners using this
// store. Herdr remains the final arbiter against clients using other stores.
func (i *Integration) reserveAgentName(ctx context.Context, tx *store.Tx, t *store.Team, m model.Member) (string, error) {
	current, err := tx.GetStoredMember(m.ID)
	if err != nil {
		return "", err
	}
	if current.HerdrAgent != nil {
		return *current.HerdrAgent, nil
	}
	names, err := tx.ReservedHerdrNames(t.HerdrServer)
	if err != nil {
		return "", err
	}
	agents, err := i.opts.Herdr(t.HerdrServer).Agents(ctx)
	if err != nil {
		return "", err
	}
	for _, a := range agents {
		names[a.Name] = true
	}
	for n := 1; n <= len(names)+1; n++ {
		name := herdr.CandidateName(m.Name, n)
		if !names[name] {
			return name, nil
		}
	}
	return "", model.NewError(model.ErrorNameTaken, "no free Herdr agent name")
}
