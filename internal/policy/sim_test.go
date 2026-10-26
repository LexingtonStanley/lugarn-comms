package policy

import (
	"fmt"
	"slices"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// sim plays the store: it keeps the team's members, grid, counters and every
// delivery, and writes back each decision the way the service layer would.
type sim struct {
	t       *testing.T
	members []model.Member
	grid    Grid
	hops    model.Hops
	rounds  []model.Rounds
	review  int64
	seq     int64
	dels    []*simDelivery
}

type simDelivery struct {
	msg      string
	seq      int64
	from, to model.MemberRef
	state    model.DeliveryState
	reason   model.DeliveryReason
	hop      *int64
	round    *int64
}

// newSim is the writer–reviewer team: human, writer (claude, pane), reviewer
// (codex, headless), with writer → reviewer message+wake and reviewer →
// writer message, a hop budget of hops and review rounds of rounds.
func newSim(t *testing.T, hops, rounds int64) *sim {
	return &sim{
		t: t,
		members: []model.Member{
			humanMember("H"),
			agent("W", "writer", model.RoleWriter, model.HarnessClaude, mode(model.ModePane)),
			agent("R", "reviewer", model.RoleReviewer, model.HarnessCodex),
		},
		grid: NewGrid([]model.GridEdge{
			{From: "writer", To: "reviewer", Can: []model.GridCapability{model.GridCapabilityMessage, model.GridCapabilityWake}},
			{From: "reviewer", To: "writer", Can: []model.GridCapability{model.GridCapabilityMessage}},
		}),
		hops:   model.Hops{Allowance: hops, Base: hops},
		review: rounds,
	}
}

func (s *sim) member(name string) model.Member {
	for _, m := range s.members {
		if m.Name == name {
			return m
		}
	}
	s.t.Fatalf("no member %s", name)
	return model.Member{}
}

func (s *sim) budget() BudgetState {
	b := BudgetState{Hops: s.hops, Rounds: slices.Clone(s.rounds), ReviewRounds: s.review}
	for _, d := range s.dels {
		if d.state == model.DeliveryStateHeld {
			b.Held = append(b.Held, HeldDelivery{Message: d.msg, Seq: d.seq, From: d.from, To: d.to,
				Reason: d.reason, Hop: d.hop, Round: d.round})
		}
	}
	return b
}

// send accepts one message and writes it back. kind auto_reply and reply get
// a reply_to the caller is taken to have received.
func (s *sim) send(from, to string, kind model.MessageKind) MessagePlan {
	s.t.Helper()
	s.seq++
	id := fmt.Sprintf("M%03d", s.seq)
	in := MessageInput{ID: id, Kind: kind, From: s.member(from), Live: s.members, Text: "text " + id,
		Grid: s.grid, Budget: s.budget()}
	if to == AllName {
		in.All = true
	} else {
		m := s.member(to)
		in.To = &m
	}
	if kind == model.MessageKindReply || kind == model.MessageKindAutoReply {
		in.ReplyTo, in.ReplyFound = ptr("M000"), true
	}
	plan, err := Accept(in)
	requireNoError(s.t, err)
	s.write(plan.Budget)
	if plan.Outcome == OutcomeSkipped {
		return plan
	}
	for _, d := range plan.Deliveries {
		sd := &simDelivery{msg: id, seq: s.seq, from: ref(in.From), to: d.To, state: d.State, hop: plan.Hop, round: plan.Round}
		if d.Reason != nil {
			sd.reason = *d.Reason
		}
		s.dels = append(s.dels, sd)
	}
	return plan
}

func (s *sim) write(r BudgetResult) {
	s.hops, s.rounds = r.Hops, r.Rounds
	for _, u := range r.Held {
		for _, d := range s.dels {
			if d.msg != u.Message || d.to.ID != u.To.ID {
				continue
			}
			if d.state != model.DeliveryStateHeld || d.reason != u.From {
				s.t.Fatalf("update for %s from %s, but it is %s %s", u.Message, u.From, d.state, d.reason)
			}
			d.state, d.hop, d.round, d.reason = u.State, u.Hop, u.Round, ""
			if u.Reason != nil {
				d.reason = *u.Reason
			}
		}
	}
}

func (s *sim) continueHops(amount *int64) BudgetResult {
	s.t.Helper()
	r, err := ContinueHops(s.budget(), amount, ref(s.member(HumanName)))
	requireNoError(s.t, err)
	s.write(r)
	return r
}

func (s *sim) continueRounds(amount *int64) BudgetResult {
	s.t.Helper()
	w, rv := s.member("writer"), s.member("reviewer")
	r, err := ContinueRounds(s.budget(), &w, &rv, amount, ref(s.member(HumanName)))
	requireNoError(s.t, err)
	s.write(r)
	return r
}

func (s *sim) start() BudgetResult {
	r := ResetForStart(s.budget())
	s.write(r)
	return r
}

// deliveries summarises every delivery as "<msg> <state>[ <reason>] h<hop> r<round>".
func (s *sim) deliveries() []string {
	out := []string{}
	for _, d := range s.dels {
		line := d.msg + " " + string(d.state)
		if d.reason != "" {
			line += " " + string(d.reason)
		}
		if d.hop != nil {
			line += fmt.Sprintf(" h%d", *d.hop)
		}
		if d.round != nil {
			line += fmt.Sprintf(" r%d", *d.round)
		}
		out = append(out, line)
	}
	return out
}

func (s *sim) counters() string {
	out := fmt.Sprintf("hops %d/%d", s.hops.Used, s.hops.Allowance)
	if s.hops.Exhausted {
		out += "!"
	}
	for _, r := range s.rounds {
		out += fmt.Sprintf(" rounds %d/%d", r.Used, r.Allowance)
		if r.Exhausted {
			out += "!"
		}
	}
	return out
}

// causes lists budget and rounds events as "hops:<cause>[:by]" and
// "rounds:<cause>[:by]".
func causes(events []Event) []string {
	out := []string{}
	for _, e := range events {
		switch d := e.Data.(type) {
		case wire.BudgetChangedEventData:
			s := "hops:" + string(d.Cause)
			if d.By != nil {
				s += ":" + d.By.Name
			}
			out = append(out, s)
		case wire.RoundsChangedEventData:
			s := "rounds:" + string(d.Cause)
			if d.By != nil {
				s += ":" + d.By.Name
			}
			out = append(out, s)
		case wire.PolicyDeniedEventData:
			out = append(out, "denied:"+string(d.Action))
		}
	}
	return out
}
