package broker

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// Authenticate checks the token on each call and commits rejection audit events.
func (b *Broker) Authenticate(ctx context.Context, id string, token *service.Token, face model.Face) (service.Caller, error) {
	var caller service.Caller
	var refusal error
	err := b.db.Write(ctx, func(tx *store.Tx) error {
		var team, member *string
		m, e := tx.GetMember(id)
		if e != nil && !missing(e) {
			return e
		}
		if e == nil {
			team = &m.Team
			member = &m.ID
		}
		reject := func(f model.AuthFailure, err error) error {
			refusal = err
			var claimed *string
			if id != "" {
				claimed = &id
			}
			return b.event(tx, team, model.EventAuthRejected, false, member, nil, wire.AuthRejectedEventData{ClaimedMember: claimed, Failure: f, Face: face})
		}
		if id == "" || token == nil || len(token.Bytes) == 0 {
			return reject(model.AuthFailureNoToken, model.NewError(model.ErrorUnauthenticated, "a member id and readable token are required"))
		}
		// Hash the actual bytes, never trust a caller-supplied digest.
		tok, e := tx.GetTokenByHash(model.HashToken(string(token.Bytes)))
		if e != nil && !missing(e) {
			return e
		}
		if e != nil || tok.MemberID != id || m == nil {
			return reject(model.AuthFailureBadToken, model.NewError(model.ErrorUnauthenticated, "token doesn't identify this member"))
		}
		t, e := tx.GetTeam(m.Team)
		if e != nil {
			return e
		}
		if e = b.reconcileRunner(tx, t); e != nil {
			return e
		}
		if tok.RevokedAt != nil {
			if t.State == model.TeamStateStopped {
				refusal = model.NewError(model.ErrorTeamNotRunning, "the team is stopped")
				return nil
			}
			return reject(model.AuthFailureRevoked, model.NewError(model.ErrorUnauthenticated, "this launch has ended").WithDetail("reason", "launch_ended"))
		}
		caller = service.Caller{MemberID: id, TeamID: m.Team, LaunchID: tok.LaunchID}
		return nil
	})
	if err != nil {
		return service.Caller{}, err
	}
	return caller, refusal
}
