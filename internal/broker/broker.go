// Package broker joins the transactional store, pure policy and host adapters.
// Runner and pane side effects are injected so C2 can supply them independently.
package broker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/gitwt"
	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/policy"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// Git is the host worktree interface. Ownership checks always precede Remove.
type Git interface {
	RepoInfo(context.Context, string) (gitwt.Info, error)
	BranchExists(context.Context, string, string) (bool, error)
	State(context.Context, string) (gitwt.GitState, error)
	Compare(context.Context, string, string, string) (BranchStatus, error)
	Remove(context.Context, string, string, string) (gitwt.BranchOutcome, error)
}

// Options supplies host dependencies and deterministic time/IDs for tests.
// Nil dependencies use production defaults. Host, when supplied, overrides the
// policy adapter; Probe still supplies hello's host information.
type Options struct {
	StateDir string
	Host     policy.Host
	Probe    hostprobe.Probe
	Git      Git
	Clock    func() time.Time
	ID       func() (string, error)
	// Wait pauses request polling. Cancellation must interrupt it.
	Wait           func(context.Context, time.Duration) error
	RequestTimeout time.Duration
}

// Broker implements the app, tools and authenticator service contracts.
type Broker struct {
	db   *store.Store
	opts Options
}

// New constructs a broker over an already opened store. The caller owns Close.
func New(db *store.Store, opts Options) *Broker {
	if opts.Probe == nil {
		opts.Probe = hostprobe.New()
	}
	if opts.Git == nil {
		opts.Git = realGit{}
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.ID == nil {
		opts.ID = func() (string, error) { return model.NewULIDAt(opts.Clock()) }
	}
	if opts.Wait == nil {
		opts.Wait = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	if opts.RequestTimeout == 0 {
		opts.RequestTimeout = 60 * time.Second
	}
	return &Broker{db: db, opts: opts}
}
func ptr[T any](v T) *T                  { return &v }
func ref(m model.Member) model.MemberRef { return model.MemberRef{ID: m.ID, Name: m.Name} }
func (b *Broker) now() model.Time {
	return model.Time{Time: b.opts.Clock().UTC().Truncate(time.Millisecond)}
}
func invalid(s string) *model.Error { return model.NewError(model.ErrorInvalidRequest, s) }
func running(t *store.Team) error {
	if t.State != model.TeamStateRunning {
		return model.NewError(model.ErrorTeamNotRunning, "the team isn't running")
	}
	return nil
}
func missing(err error) bool {
	if err == nil {
		return false
	}
	c := model.AsError(err).Code
	return c == model.ErrorNotFound || c == model.ErrorUnknownMember
}
func (b *Broker) cursor(tx *store.Tx) (string, error) {
	s, e := tx.CurrentEventSeq()
	if e != nil {
		return "", e
	}
	return model.MakeCursor(b.db.HostID(), s)
}
func (b *Broker) parseCursor(tx *store.Tx, c *string) (int64, error) {
	if c == nil {
		return 0, nil
	}
	s, e := model.ParseCursor(*c, b.db.HostID())
	if e != nil {
		return 0, e
	}
	max, e := tx.CurrentEventSeq()
	if e != nil {
		return 0, e
	}
	if s > max {
		return 0, model.NewError(model.ErrorCursorInvalid, "cursor is ahead of this database")
	}
	return s, nil
}
func (b *Broker) event(tx *store.Tx, team *string, typ model.EventType, timeline bool, member, message *string, data any) error {
	raw, e := json.Marshal(data)
	if e != nil {
		return model.AsError(e)
	}
	_, e = tx.AppendEvent(store.Event{TeamID: team, Type: typ, Timeline: timeline, MemberID: member, MessageID: message, At: b.now(), Data: raw})
	return e
}
func (b *Broker) policyEvents(tx *store.Tx, team string, events []policy.Event) error {
	for _, e := range events {
		if err := b.event(tx, &team, e.Type, e.Timeline, e.Member, e.Message, e.Data); err != nil {
			return err
		}
	}
	return nil
}
func (b *Broker) member(tx *store.Tx, team, id string) (*model.Member, error) {
	m, e := tx.GetMember(id)
	if e != nil {
		return nil, e
	}
	if m.Team != team || m.RemovedAt != nil {
		return nil, model.NewError(model.ErrorUnknownMember, "no live member with that id in this team")
	}
	return m, nil
}
func (b *Broker) fresh(t *store.Team) bool {
	return t.Runner.HeartbeatAt != nil && b.now().Sub(t.Runner.HeartbeatAt.Time) < 15*time.Second
}

// contact also records the first observation of a stale runner, once, across processes.
func (b *Broker) contact(ctx context.Context, team *string) error {
	return b.db.Write(ctx, func(tx *store.Tx) error {
		var ts []store.Team
		if team != nil {
			t, e := tx.GetTeam(*team)
			if e != nil {
				return e
			}
			ts = append(ts, *t)
		} else {
			var e error
			ts, e = tx.ListTeams()
			if e != nil {
				return e
			}
		}
		if _, e := tx.TouchAppContact(team, b.now()); e != nil {
			return e
		}
		for _, t := range ts {
			if e := b.reconcileRunner(tx, &t); e != nil {
				return e
			}
		}
		return nil
	})
}
func (b *Broker) reconcileRunner(tx *store.Tx, t *store.Team) error {
	if t.State != model.TeamStateRunning || t.Runner.State != model.RunnerStateUp || b.fresh(t) {
		return nil
	}
	t.Runner.State = model.RunnerStateDown
	if e := tx.Heartbeat(t.ID, t.Runner, b.now()); e != nil {
		return e
	}
	s, e := b.status(tx, t)
	if e != nil {
		return e
	}
	return b.event(tx, &t.ID, model.EventRunnerStateChanged, true, nil, nil, wire.RunnerStateChangedEventData{Team: s, From: model.RunnerStateUp, To: model.RunnerStateDown})
}
func (b *Broker) status(tx *store.Tx, t *store.Team) (model.TeamStatus, error) {
	s := model.TeamStatus{ID: t.ID, Name: t.Name, Pattern: t.Pattern, Revision: t.Revision, Run: t.Run, State: t.State, Runner: t.Runner, StartedAt: t.StartedAt, StoppedAt: t.StoppedAt, LastAppContactAt: t.LastAppContactAt}
	budget, e := b.budget(tx, t, false)
	if e != nil {
		return s, e
	}
	s.Hops = budget.Hops
	s.Rounds = budget.Rounds
	ms, e := tx.ListMembers(t.ID, false)
	if e != nil {
		return s, e
	}
	for _, m := range ms {
		if m.Kind == model.MemberKindAgent {
			s.Counts.Members++
			if m.State == model.MemberStateRunning {
				s.Counts.Running++
			}
		}
	}
	if s.Counts.HeadlessTurns, e = tx.CountLiveTurns(t.ID); e != nil {
		return s, e
	}
	for _, v := range []struct {
		state model.DeliveryState
		out   *int64
	}{{model.DeliveryStateQueued, &s.Counts.Queued}, {model.DeliveryStateHeld, &s.Counts.Held}, {model.DeliveryStateUncertain, &s.Counts.Uncertain}} {
		if *v.out, e = tx.CountDeliveries(t.ID, v.state); e != nil {
			return s, e
		}
	}
	return s, nil
}
func (b *Broker) budget(tx *store.Tx, t *store.Team, held bool) (policy.BudgetState, error) {
	s := policy.BudgetState{Hops: model.Hops{Base: t.HopBudget, Allowance: t.HopBudget}, ReviewRounds: t.ReviewRounds, Rounds: []model.Rounds{}}
	h, e := tx.GetHopCounter(t.ID)
	if e != nil && !missing(e) {
		return s, e
	}
	if e == nil {
		s.Hops.Used = h.Used
		s.Hops.Allowance = h.Allowance
		s.Hops.Exhausted = h.Used > h.Allowance
	}
	rs, e := tx.ListRoundCounters(t.ID)
	if e != nil {
		return s, e
	}
	for _, r := range rs {
		w, e := tx.GetMember(r.WriterID)
		if e != nil {
			return s, e
		}
		v, e := tx.GetMember(r.ReviewerID)
		if e != nil {
			return s, e
		}
		s.Rounds = append(s.Rounds, model.Rounds{Writer: ref(*w), Reviewer: ref(*v), Used: r.Used, Allowance: r.Allowance, Base: t.ReviewRounds, Exhausted: r.Used > r.Allowance})
	}
	if held {
		ds, e := tx.ListTeamDeliveries(t.ID, model.DeliveryStateHeld)
		if e != nil {
			return s, e
		}
		for _, d := range ds {
			if d.Delivery.Reason == nil || (*d.Delivery.Reason != model.DeliveryReasonBudget && *d.Delivery.Reason != model.DeliveryReasonRounds) {
				continue
			}
			m, e := tx.GetMessage(d.MessageID)
			if e != nil {
				return s, e
			}
			s.Held = append(s.Held, policy.HeldDelivery{Message: m.ID, Seq: d.MessageSeq, From: m.From, To: d.Delivery.To, Reason: *d.Delivery.Reason, Hop: m.Hop, Round: m.Round})
		}
	}
	return s, nil
}
func (b *Broker) grid(tx *store.Tx, team string) ([]model.GridEdge, error) {
	rows, e := tx.ListGrid(team)
	if e != nil {
		return nil, e
	}
	out := []model.GridEdge{}
	index := map[[2]string]int{}
	for _, r := range rows {
		from, e := tx.GetMember(r.FromID)
		if e != nil {
			return nil, e
		}
		to, e := tx.GetMember(r.ToID)
		if e != nil {
			return nil, e
		}
		k := [2]string{from.Name, to.Name}
		i, ok := index[k]
		if !ok {
			i = len(out)
			index[k] = i
			out = append(out, model.GridEdge{From: from.Name, To: to.Name, Can: []model.GridCapability{}})
		}
		out[i].Can = append(out[i].Can, r.Capability)
	}
	return out, nil
}
