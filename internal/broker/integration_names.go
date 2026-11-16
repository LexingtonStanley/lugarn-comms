package broker

import (
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

// The immediate transaction serializes allocations across runners using this
// store. Herdr remains the final arbiter against clients using other stores.
func (i *Integration) reserveAgentName(tx *store.Tx, t *store.Team, m model.Member, agents []herdr.Agent) (string, error) {
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
	for _, a := range agents {
		names[a.Name] = true
	}
	// Truncation can make the unsuffixed name equal a later candidate. Count
	// distinct candidates so duplicates cannot exhaust the pigeonhole bound.
	seen := map[string]bool{}
	for n := 1; len(seen) <= len(names); n++ {
		name := herdr.CandidateName(m.Name, n)
		if seen[name] {
			continue
		}
		seen[name] = true
		if !names[name] {
			return name, nil
		}
	}
	return "", model.NewError(model.ErrorNameTaken, "no free Herdr agent name").WithDetail("name", m.Name)
}
