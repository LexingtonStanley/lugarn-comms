package broker

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"testing"
)

func TestApplyAndGet(t *testing.T) {
	b, _ := setup(t)
	ctx := context.Background()
	s := model.TeamSpec{Version: 1, ID: "new", Name: "new-team", Pattern: model.PatternCustom, Members: []model.MemberSpec{{ID: "a", Name: "alpha", Role: model.RoleWorker, Harness: model.HarnessCodex, Folder: t.TempDir(), Profile: model.ProfileWorkspace, Mode: model.ModeHeadless, Brief: "brief"}, {ID: "b", Name: "beta", Role: model.RoleWorker, Harness: model.HarnessClaude, Folder: t.TempDir(), Profile: model.ProfileReadOnly, Mode: model.ModeHeadless, Brief: "brief"}}, Grid: []model.GridEdge{{From: "alpha", To: "beta", Can: []model.GridCapability{model.GridCapabilityMessage}}}}
	out, e := b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: s})
	must(t, e)
	if out.Team.Revision != 1 || out.Team.Counts.Members != 2 {
		t.Fatal(out)
	}
	g, e := b.GetTeam(ctx, wire.GetTeamRequest{Team: s.ID})
	must(t, e)
	if g.Spec.Limits == nil || g.Spec.Members[0].Worktree == nil || len(g.Spec.Grid) != 1 {
		t.Fatal(g)
	}
	_, e = b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: s})
	code(t, e, model.ErrorRevisionConflict)
	s.Members[0].Name = "beta"
	s.Members[1].Name = "alpha"
	s.Grid[0].From = "beta"
	s.Grid[0].To = "alpha"
	_, e = b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: s, IfRevision: 1})
	must(t, e)
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam(s.ID, store.TeamPatch{State: ptr(model.TeamStateRunning)}, b.now())
	}))
	s.Members[0].Folder = t.TempDir()
	_, e = b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: s, IfRevision: 2})
	code(t, e, model.ErrorTeamRunning)
	g, e = b.GetTeam(ctx, wire.GetTeamRequest{Team: s.ID})
	must(t, e)
	if g.Revision != 2 {
		t.Fatal(g)
	}
	must(t, b.db.Write(ctx, func(tx *store.Tx) error {
		return tx.UpdateTeam(s.ID, store.TeamPatch{State: ptr(model.TeamStateStopped)}, b.now())
	}))
	s = g.Spec
	s.Members = s.Members[:1]
	s.Grid = []model.GridEdge{}
	_, e = b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: s, IfRevision: 2})
	must(t, e)
	s.Members = append(s.Members, g.Spec.Members[1])
	_, e = b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: s, IfRevision: 3})
	code(t, e, model.ErrorInvalidRequest)
	s.ID = "other"
	s.Name = "another"
	_, e = b.ApplyTeam(ctx, wire.ApplyTeamRequest{Spec: s})
	code(t, e, model.ErrorInvalidRequest)
}
