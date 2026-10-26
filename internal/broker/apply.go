package broker

import (
	"context"
	"sort"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (b *Broker) spec(tx *store.Tx, t *store.Team) (model.TeamSpec, error) {
	out := model.TeamSpec{Version: t.SpecVersion, ID: t.ID, Name: t.Name, Pattern: t.Pattern, Herdr: &model.HerdrSettings{Server: &t.HerdrServer}, Limits: &model.Limits{HopBudget: ptr(int(t.HopBudget)), ReviewRounds: ptr(int(t.ReviewRounds)), PerRecipient: ptr(int(t.PerRecipient)), HeadlessTurns: ptr(int(t.HeadlessTurns)), TurnTimeoutSeconds: ptr(int(t.TurnTimeoutSeconds))}, Members: []model.MemberSpec{}}
	ms, e := tx.ListMembers(t.ID, false)
	if e != nil {
		return out, e
	}
	var specs []store.StoredMember
	for _, m := range ms {
		if m.Origin != model.MemberOriginSpec {
			continue
		}
		s, e := tx.GetStoredMember(m.ID)
		if e != nil {
			return out, e
		}
		specs = append(specs, *s)
	}
	sort.SliceStable(specs, func(i, j int) bool {
		a, b := specs[i].Position, specs[j].Position
		if a == nil {
			return false
		}
		return b == nil || *a < *b
	})
	for _, s := range specs {
		m := s.Member
		spawn, e := tx.GetSpawnSettings(m.ID)
		if e != nil && !missing(e) {
			return out, e
		}
		out.Members = append(out.Members, model.MemberSpec{ID: m.ID, Name: m.Name, Role: *m.Role, Harness: *m.Harness, Folder: *m.Folder, Worktree: ptr(s.Worktree), Profile: *m.Profile, Mode: *m.Mode, Brief: *s.Brief, Task: s.Task, Spawn: spawnSpec(spawn)})
	}
	out.Grid, e = b.grid(tx, t.ID)
	return out, e
}
func (b *Broker) GetTeam(ctx context.Context, r wire.GetTeamRequest) (wire.GetTeamResult, error) {
	var out wire.GetTeamResult
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		t, e := tx.GetTeam(r.Team)
		if e != nil {
			return e
		}
		out.Revision = t.Revision
		out.Spec, e = b.spec(tx, t)
		if e != nil {
			return e
		}
		out.Cursor, e = b.cursor(tx)
		return e
	})
	return out, e
}
func (b *Broker) ApplyTeam(ctx context.Context, r wire.ApplyTeamRequest) (wire.ApplyTeamResult, error) {
	var out wire.ApplyTeamResult
	h := b.host(ctx)
	if e := b.contact(ctx, &r.Spec.ID); e != nil && !missing(e) {
		return out, e
	}
	e := b.db.Write(ctx, func(tx *store.Tx) error {
		facts := policy.SpecFacts{}
		t, e := tx.GetTeam(r.Spec.ID)
		if e != nil && !missing(e) {
			return e
		}
		if e == nil {
			s, e := b.spec(tx, t)
			if e != nil {
				return e
			}
			ms, e := tx.ListMembers(t.ID, true)
			if e != nil {
				return e
			}
			facts.Current = &policy.Team{ID: t.ID, Revision: t.Revision, State: t.State, Spec: s, Members: ms}
		}
		ts, e := tx.ListTeams()
		if e != nil {
			return e
		}
		for _, v := range ts {
			if v.ID != r.Spec.ID {
				facts.OtherTeamNames = append(facts.OtherTeamNames, v.Name)
			}
		}
		for _, s := range r.Spec.Members {
			m, e := tx.GetMember(s.ID)
			if e != nil && !missing(e) {
				return e
			}
			if e == nil && m.Team != r.Spec.ID {
				facts.ForeignMemberIDs = append(facts.ForeignMemberIDs, m.ID)
			}
		}
		p, pe := policy.PlanApply(policy.ApplyInput{Spec: r.Spec, IfRevision: r.IfRevision, Facts: facts}, h)
		if err := hostError(h); err != nil {
			return err
		}
		if pe != nil {
			return pe
		}
		s := p.Spec
		now := b.now()
		l := p.Limits
		if p.Create {
			if e = tx.CreateTeam(store.Team{ID: s.ID, Name: s.Name, Pattern: s.Pattern, SpecVersion: 1, Revision: p.Revision, State: model.TeamStateStopped, HerdrServer: *s.Herdr.Server, HopBudget: int64(l.HopBudget), ReviewRounds: int64(l.ReviewRounds), PerRecipient: int64(l.PerRecipient), HeadlessTurns: int64(l.HeadlessTurns), TurnTimeoutSeconds: int64(l.TurnTimeoutSeconds), CreatedAt: now, UpdatedAt: now}); e != nil {
				return e
			}
		} else {
			if e = tx.UpdateTeam(s.ID, store.TeamPatch{Name: &s.Name, Pattern: &s.Pattern, HerdrServer: s.Herdr.Server, HopBudget: ptr(int64(l.HopBudget)), ReviewRounds: ptr(int64(l.ReviewRounds)), PerRecipient: ptr(int64(l.PerRecipient)), HeadlessTurns: ptr(int64(l.HeadlessTurns)), TurnTimeoutSeconds: ptr(int64(l.TurnTimeoutSeconds))}, now); e != nil {
				return e
			}
			if _, e = tx.BumpTeamRevision(s.ID, r.IfRevision, now); e != nil {
				return e
			}
		}
		if _, e = tx.TouchAppContact(&s.ID, now); e != nil {
			return e
		}
		// Remove first and stage renamed names so a valid swap is atomic even with
		// the live-name unique index. Temporary names are never observable.
		for _, m := range p.Removed {
			if e = tx.RemoveMember(m.ID, model.RemovedReasonRemovedFromSpec, now); e != nil {
				return e
			}
		}
		for _, change := range p.Changed {
			for _, f := range change.Fields {
				if f == "name" {
					id, e := b.opts.ID()
					if e != nil {
						return model.AsError(e)
					}
					if e = tx.UpdateMember(change.ID, store.MemberPatch{Name: ptr("tmp-" + id)}, now); e != nil {
						return e
					}
				}
			}
		}
		added := []string{}
		if p.Create {
			id, e := b.opts.ID()
			if e != nil {
				return model.AsError(e)
			}
			if e = tx.InsertMember(store.MemberParams{ID: id, TeamID: s.ID, Name: "human", Kind: model.MemberKindHuman, Origin: model.MemberOriginImplicit, State: model.MemberStateRunning, CreatedAt: &now, UpdatedAt: &now}); e != nil {
				return e
			}
			added = append(added, id)
			if e = tx.PutHopCounter(store.HopCounter{TeamID: s.ID, Allowance: int64(l.HopBudget), UpdatedAt: now}); e != nil {
				return e
			}
		}
		newIDs := map[string]bool{}
		for _, m := range p.Added {
			newIDs[m.ID] = true
		}
		for i, m := range s.Members {
			pos := int64(i)
			if newIDs[m.ID] {
				if e = tx.InsertMember(store.MemberParams{ID: m.ID, TeamID: s.ID, Name: m.Name, Kind: model.MemberKindAgent, Role: &m.Role, Origin: model.MemberOriginSpec, Position: &pos, Harness: &m.Harness, Profile: &m.Profile, Mode: &m.Mode, Folder: &m.Folder, Worktree: *m.Worktree, Brief: &m.Brief, Task: m.Task, State: model.MemberStateStopped, CreatedAt: &now, UpdatedAt: &now}); e != nil {
					return e
				}
				if e = tx.InsertLease(m.ID, model.Lease{Driver: model.DriverNone}, now); e != nil {
					return e
				}
				added = append(added, m.ID)
			} else {
				if e = tx.UpdateMember(m.ID, store.MemberPatch{Name: &m.Name, Role: ptr(&m.Role), Position: ptr(&pos), Harness: ptr(&m.Harness), Profile: ptr(&m.Profile), Mode: ptr(&m.Mode), Folder: ptr(&m.Folder), Worktree: m.Worktree, Brief: ptr(&m.Brief), Task: &m.Task}, now); e != nil {
					return e
				}
			}
			if m.Spawn == nil {
				if e = tx.DeleteSpawnSettings(m.ID); e != nil {
					return e
				}
			} else {
				cfg := policy.ResolveSpawn(m.Spawn)
				if e = tx.PutSpawnSettings(store.SpawnSettings{MemberID: m.ID, Enabled: cfg.Enabled, RunningAtOnce: int64(cfg.RunningAtOnce), PerRun: int64(cfg.PerRun), Harnesses: m.Spawn.Harnesses, Mode: cfg.Mode, Folder: cfg.Folder, MaxProfile: cfg.MaxProfile, Brief: m.Spawn.Brief}); e != nil {
					return e
				}
			}
		}
		edges, e := b.storeEdges(tx, s.ID, s.Grid)
		if e != nil {
			return e
		}
		if e = tx.ReplaceGrid(s.ID, edges); e != nil {
			return e
		}
		for _, m := range p.Removed {
			v, e := tx.GetMember(m.ID)
			if e != nil {
				return e
			}
			if e = b.event(tx, &s.ID, model.EventMemberRemoved, false, &m.ID, nil, wire.MemberRemovedEventData{Member: *v}); e != nil {
				return e
			}
		}
		for _, id := range added {
			v, e := tx.GetMember(id)
			if e != nil {
				return e
			}
			if e = b.event(tx, &s.ID, model.EventMemberAdded, true, &id, nil, wire.MemberAddedEventData{Member: *v}); e != nil {
				return e
			}
		}
		t, e = tx.GetTeam(s.ID)
		if e != nil {
			return e
		}
		out.Team, e = b.status(tx, t)
		if e != nil {
			return e
		}
		out.Warnings = p.Warnings
		return b.event(tx, &s.ID, model.EventTeamApplied, true, nil, nil, wire.TeamAppliedEventData{Team: out.Team})
	})
	return out, e
}
func (b *Broker) storeEdges(tx *store.Tx, team string, edges []model.GridEdge) ([]store.GridEdge, error) {
	out := []store.GridEdge{}
	for _, v := range edges {
		from, e := tx.GetLiveMember(team, v.From)
		if e != nil {
			return nil, e
		}
		to, e := tx.GetLiveMember(team, v.To)
		if e != nil {
			return nil, e
		}
		out = append(out, store.GridEdge{TeamID: team, FromID: from.ID, ToID: to.ID, Can: v.Can})
	}
	return out, nil
}
