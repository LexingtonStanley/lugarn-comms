package broker

import (
	"context"
	"encoding/json"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/version"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func (b *Broker) Hello(ctx context.Context, r wire.HelloRequest) (wire.HelloResult, error) {
	var out wire.HelloResult
	if e := b.contact(ctx, nil); e != nil {
		return out, e
	}
	found := false
	for _, p := range r.Protocols {
		found = found || p == 1
	}
	if !found {
		return out, model.NewError(model.ErrorUnsupportedVersion, "no common protocol").WithDetail("supported", []int{1})
	}
	h, e := b.opts.Probe.Host()
	if e != nil {
		return out, model.AsError(e)
	}
	herdr, e := b.opts.Probe.Herdr(ctx, model.DefaultHerdrServer)
	if e != nil {
		herdr.Running = false
	}
	out = wire.HelloResult{Broker: wire.HelloResultBroker{Version: version.Version, Commit: ptr(version.Commit), Protocol: 1, Protocols: []int64{1}}, Schemas: wire.HelloResultSchemas{TeamSpec: []int64{1}, Store: int64(b.db.Version())}, Capabilities: []string{"watch", "resolve-delivery", "hold", "worktrees"}, Host: wire.HelloResultHost{ID: b.db.HostID(), OS: h.OS, Arch: h.Arch, Hostname: h.Hostname, StateDir: b.opts.StateDir}, Herdr: herdr, Harnesses: b.opts.Probe.Harnesses(ctx)}
	if b.integration != nil {
		out.Capabilities = append(out.Capabilities, "spawn", "open-terminal", "return-headless", "start-herdr")
	}
	if l := b.opts.Probe.Logind(ctx); l != nil {
		out.Host.Logind = &wire.LogindInfo{KillUserProcesses: l.KillUserProcesses, Linger: l.Linger}
	}
	return out, nil
}
func (b *Broker) Status(ctx context.Context, r wire.StatusRequest) (wire.StatusResult, error) {
	out := wire.StatusResult{Teams: []model.TeamStatus{}}
	if e := b.contact(ctx, r.Team); e != nil {
		return out, e
	}
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		ts, e := tx.ListTeams()
		if e != nil {
			return e
		}
		for i := range ts {
			if r.Team != nil && ts[i].ID != *r.Team {
				continue
			}
			s, e := b.status(tx, &ts[i])
			if e != nil {
				return e
			}
			out.Teams = append(out.Teams, s)
		}
		out.Cursor, e = b.cursor(tx)
		return e
	})
	return out, e
}
func (b *Broker) Members(ctx context.Context, r wire.MembersRequest) (wire.MembersResult, error) {
	var out wire.MembersResult
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		var e error
		out.Members, e = tx.ListMembers(r.Team, r.IncludeRemoved != nil && *r.IncludeRemoved)
		if e != nil {
			return e
		}
		out.Cursor, e = b.cursor(tx)
		return e
	})
	return out, e
}
func (b *Broker) wireEvent(e store.Event) (wire.Event, error) {
	c, err := model.MakeCursor(b.db.HostID(), e.Seq)
	if err != nil {
		return wire.Event{}, err
	}
	raw, err := json.Marshal(struct {
		Type     model.EventType `json:"type"`
		Cursor   string          `json:"cursor"`
		At       model.Time      `json:"at"`
		Team     *string         `json:"team"`
		Timeline bool            `json:"timeline"`
		Data     json.RawMessage `json:"data"`
	}{e.Type, c, e.At, e.TeamID, e.Timeline, e.Data})
	if err != nil {
		return wire.Event{}, model.AsError(err)
	}
	var v wire.Event
	err = json.Unmarshal(raw, &v)
	if err != nil {
		return v, model.AsError(err)
	}
	return v, nil
}
func (b *Broker) Timeline(ctx context.Context, r wire.TimelineRequest) (wire.TimelineResult, error) {
	out := wire.TimelineResult{Events: []wire.Event{}}
	if e := b.contact(ctx, &r.Team); e != nil {
		return out, e
	}
	if r.Before != nil && r.After != nil {
		return out, invalid("give before or after, not both")
	}
	limit := 50
	if r.Limit != nil {
		limit = int(*r.Limit)
	}
	if limit < 1 || limit > 200 {
		return out, invalid("limit must be 1–200")
	}
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		before, e := b.parseCursor(tx, r.Before)
		if e != nil {
			return e
		}
		after, e := b.parseCursor(tx, r.After)
		if e != nil {
			return e
		}
		var p *store.TimelinePage
		// Store's zero means "no bound". The protocol also permits an
		// explicit cursor at zero, which means before/after an empty log.
		if (r.Before != nil && before == 0) || (r.After != nil && after == 0) {
			events, err := tx.ReplayEvents(0, &r.Team, 0)
			if err != nil {
				return err
			}
			p = &store.TimelinePage{}
			for _, event := range events {
				if !event.Timeline {
					continue
				}
				if r.Before != nil {
					p.HasMoreAfter = true
					break
				}
				if len(p.Events) == limit {
					p.HasMoreAfter = true
					break
				}
				p.Events = append(p.Events, event)
			}
		} else {
			p, e = tx.Timeline(r.Team, before, after, limit)
			if e != nil {
				return e
			}
		}
		out.HasMoreBefore = p.HasMoreBefore
		out.HasMoreAfter = p.HasMoreAfter
		for _, s := range p.Events {
			v, e := b.wireEvent(s)
			if e != nil {
				return e
			}
			if v.Type == model.EventMessageCreated {
				d := v.Data.(*wire.MessageCreatedEventData)
				m, e := tx.GetMessage(d.Message.ID)
				if e != nil {
					return e
				}
				d.Message = *m
			}
			out.Events = append(out.Events, v)
		}
		return nil
	})
	return out, e
}
func (b *Broker) caller(tx *store.Tx, c service.Caller) (*store.Team, *model.Member, error) {
	t, e := tx.GetTeam(c.TeamID)
	if e != nil {
		return nil, nil, e
	}
	if e = running(t); e != nil {
		return nil, nil, e
	}
	m, e := b.member(tx, c.TeamID, c.MemberID)
	return t, m, e
}
func (b *Broker) ToolInbox(ctx context.Context, c service.Caller, r wire.InboxInput) (wire.InboxOutput, error) {
	out := wire.InboxOutput{Messages: []wire.InboxMessage{}}
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		_, _, e := b.caller(tx, c)
		if e != nil {
			return e
		}
		dir := model.InboxDirectionIn
		if r.Direction != nil {
			dir = *r.Direction
		}
		limit := 20
		if r.Limit != nil {
			limit = int(*r.Limit)
		}
		if limit < 1 || limit > 100 {
			return invalid("limit must be 1–100")
		}
		before := ""
		if r.Before != nil {
			before = *r.Before
			m, e := tx.GetMessage(before)
			if e != nil {
				return e
			}
			if m.Team != c.TeamID {
				return model.NewError(model.ErrorNotFound, "message not found")
			}
		}
		ms, more, e := tx.Inbox(c.MemberID, dir, limit, before)
		if e != nil {
			return e
		}
		out.HasMore = more
		for _, m := range ms {
			for _, d := range m.Deliveries {
				if m.From.ID != c.MemberID && d.To.ID != c.MemberID {
					continue
				}
				out.Messages = append(out.Messages, wire.InboxMessage{ID: m.ID, From: m.From.Name, To: d.To.Name, Kind: m.Kind, ReplyTo: m.ReplyTo, Text: m.Text, CreatedAt: m.CreatedAt, State: d.State, Reason: d.Reason})
			}
		}
		return nil
	})
	return out, e
}
func spawnSpec(s *store.SpawnSettings) *model.SpawnSettings {
	if s == nil {
		return nil
	}
	return &model.SpawnSettings{Enabled: s.Enabled, RunningAtOnce: ptr(int(s.RunningAtOnce)), PerRun: ptr(int(s.PerRun)), Harnesses: s.Harnesses, Mode: &s.Mode, Folder: &s.Folder, MaxProfile: &s.MaxProfile, Brief: s.Brief}
}
func (b *Broker) ToolTeam(ctx context.Context, c service.Caller, _ wire.TeamInput) (wire.TeamOutput, error) {
	var out wire.TeamOutput
	h := b.host(ctx)
	e := b.db.Read(ctx, func(tx *store.Tx) error {
		t, m, e := b.caller(tx, c)
		if e != nil {
			return e
		}
		out.Team = wire.TeamOutputTeam{ID: t.ID, Name: t.Name, Pattern: t.Pattern, State: t.State, Run: t.Run}
		out.Me = wire.TeamOutputMe{ID: m.ID, Name: m.Name, Role: *m.Role, Harness: *m.Harness, Profile: *m.Profile, Mode: *m.Mode}
		if m.SpawnedBy != nil {
			out.Me.SpawnedBy = &m.SpawnedBy.Name
		}
		s, e := tx.GetSpawnSettings(m.ID)
		if e != nil && !missing(e) {
			return e
		}
		cfg := policy.ResolveSpawn(spawnSpec(s))
		out.Me.CanSpawn = cfg.Enabled && m.Origin != model.MemberOriginSpawn
		edges, e := b.grid(tx, t.ID)
		if e != nil {
			return e
		}
		g := policy.NewGrid(edges)
		ms, e := tx.ListMembers(t.ID, false)
		if e != nil {
			return e
		}
		out.Members = []wire.TeamMember{}
		for _, v := range ms {
			if v.ID == m.ID {
				continue
			}
			x := wire.TeamMember{ID: v.ID, Name: v.Name, Kind: v.Kind, Role: v.Role, Harness: v.Harness, Mode: v.Mode, State: v.State, Activity: v.Activity, Can: g.CanList(m.Name, v.Name), SpawnedByMe: v.SpawnedBy != nil && v.SpawnedBy.ID == m.ID}
			if v.SpawnedBy != nil {
				x.SpawnedBy = &v.SpawnedBy.Name
			}
			out.Members = append(out.Members, x)
		}
		budget, e := b.budget(tx, t, false)
		if e != nil {
			return e
		}
		out.Hops = wire.TeamOutputHops{Used: budget.Hops.Used, Allowance: budget.Hops.Allowance}
		if out.Me.CanSpawn {
			count, e := tx.GetSpawnCount(m.ID)
			if e != nil {
				return e
			}
			hs := []model.Harness{}
			for _, v := range (model.Harness("")).Values() {
				allowed := cfg.Harnesses == nil
				for _, x := range cfg.Harnesses {
					allowed = allowed || v == x
				}
				if allowed && h.HarnessIssue(v) == "" {
					hs = append(hs, v)
				}
			}
			out.Spawn = &wire.SpawnStatus{Running: count.Running, RunningAtOnce: int64(cfg.RunningAtOnce), SpawnedThisRun: count.ThisRun, PerRun: int64(cfg.PerRun), Harnesses: hs, Mode: cfg.Mode, Folder: cfg.Folder, MaxProfile: policy.HighestWorkerProfile(cfg, *m.Profile)}
		}
		return nil
	})
	return out, e
}
