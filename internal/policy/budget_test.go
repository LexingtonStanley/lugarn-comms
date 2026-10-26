package policy

import (
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

const (
	msg  = model.MessageKindMessage
	rep  = model.MessageKindReply
	auto = model.MessageKindAutoReply
	task = model.MessageKindTask
)

func TestHopsCountContinueAndReset(t *testing.T) {
	s := newSim(t, 3, 100)
	// Agent messages in a row: 1..3 queued, the 4th exhausts the budget.
	for i, want := range [][]string{
		{"hops:used"}, {"hops:used"}, {"hops:used"}, {"hops:exhausted"}, {"hops:used"},
	} {
		p := s.send("writer", "reviewer", msg)
		equal(t, "events", causes(p.Events), want)
		if *p.Hop != int64(i+1) {
			t.Fatalf("hop %d = %d", i+1, *p.Hop)
		}
	}
	equal(t, "after 5", s.deliveries(), []string{
		"M001 queued h1", "M002 queued h2", "M003 queued h3", "M004 held budget h4", "M005 held budget h5"})
	equal(t, "counters", s.counters(), "hops 5/3!")

	// Continue by 1: the 4th is within the new allowance; the 5th still waits,
	// so attention is raised again.
	r := s.continueHops(ptr(int64(1)))
	equal(t, "continue events", causes(r.Events), []string{"hops:continued:human", "hops:exhausted"})
	equal(t, "released", r.Released, int64(1))
	equal(t, "after continue", s.deliveries()[3:], []string{"M004 queued h4", "M005 held budget h5"})
	equal(t, "counters", s.counters(), "hops 5/4!")

	// Continue by the default step (the base, 3): everything goes, and the
	// next agent message counts on from used.
	r = s.continueHops(nil)
	equal(t, "continue events", causes(r.Events), []string{"hops:continued:human"})
	equal(t, "counters", s.counters(), "hops 5/7")
	p := s.send("writer", "reviewer", msg)
	equal(t, "hop after continue", *p.Hop, int64(6))

	// More messages pile up held, then the person writes: the budget resets
	// and the held ones are counted again in order, from 1.
	s.send("writer", "reviewer", msg) // h7 queued
	s.send("writer", "reviewer", msg) // h8 held
	s.send("writer", "reviewer", msg) // h9 held
	s.send("writer", "reviewer", msg) // h10 held
	s.send("writer", "reviewer", msg) // h11 held
	equal(t, "before reset", s.counters(), "hops 11/7!")
	p = s.send("human", "writer", msg)
	equal(t, "human message counts nothing", [2]bool{p.Hop == nil, p.Round == nil}, [2]bool{true, true})
	equal(t, "reset events", causes(p.Events), []string{"hops:reset:human", "hops:exhausted"})
	equal(t, "renumbered", s.deliveries()[7:11], []string{
		"M008 queued h1", "M009 queued h2", "M010 queued h3", "M011 held budget h4"})
	equal(t, "counters", s.counters(), "hops 4/3!")
	var renumbered, moved int
	for _, u := range p.Budget.Held {
		if u.Renumbered {
			renumbered++
		}
		if u.Moved {
			moved++
		}
	}
	equal(t, "updates", [3]int64{int64(renumbered), int64(moved), p.Budget.Released}, [3]int64{4, 3, 3})

	// The next agent message counts on from the renumbered count.
	p = s.send("writer", "reviewer", msg)
	equal(t, "hop after reset", *p.Hop, int64(5))
	equal(t, "still held", p.Deliveries[0].State, model.DeliveryStateHeld)

	// A team start resets the same way, with by null.
	r = s.start()
	equal(t, "start events", causes(r.Events), []string{"hops:reset"})
	equal(t, "counters", s.counters(), "hops 2/3")
	equal(t, "after start", s.deliveries()[10:], []string{"M011 queued h1", "M012 queued", "M013 queued h2"})

	// Nothing to reset: no event.
	equal(t, "idle start", causes(s.start().Events), []string{"hops:reset"})
	equal(t, "counters", s.counters(), "hops 0/3")
	equal(t, "no-op start", causes(s.start().Events), []string{})
	p = s.send("human", "reviewer", msg)
	equal(t, "no-op human reset", causes(p.Events), []string{})
}

func TestHopsCountWhatCounts(t *testing.T) {
	s := newSim(t, 16, 100)
	cases := []struct {
		from, to string
		kind     model.MessageKind
		counts   bool
	}{
		{"writer", "reviewer", msg, true},
		{"reviewer", "writer", rep, true},
		{"reviewer", "writer", auto, true},
		{"writer", "reviewer", task, true}, // a spawn task: from an agent
		{"writer", HumanName, msg, false},
		{"reviewer", HumanName, auto, false},
		{HumanName, "writer", msg, false},
		{HumanName, "reviewer", task, false}, // an initial task
	}
	var used int64
	for _, c := range cases {
		p := s.send(c.from, c.to, c.kind)
		if c.counts {
			used++
			if p.Hop == nil || *p.Hop != used {
				t.Fatalf("%s → %s %s: hop %v; want %d", c.from, c.to, c.kind, p.Hop, used)
			}
		} else if p.Hop != nil {
			t.Fatalf("%s → %s %s counted", c.from, c.to, c.kind)
		}
	}
	// A message the grid denies doesn't count either.
	s.grid = NewGrid(nil)
	p := s.send("writer", "reviewer", msg)
	equal(t, "denied", [2]any{p.Outcome, p.Hop == nil}, [2]any{OutcomeDenied, true})
	p = s.send("writer", "reviewer", auto)
	equal(t, "skipped", [2]any{p.Outcome, p.Hop == nil}, [2]any{OutcomeSkipped, true})
	equal(t, "used", s.hops.Used, int64(0)) // the human messages reset it
}

// The writer–reviewer exchange from PR #3: a round is each reviewer → writer
// message, so the writer's clarifying questions don't use rounds up.
func TestReviewRoundsWriterReviewerExchange(t *testing.T) {
	s := newSim(t, 100, 2)
	s.send("human", "writer", task)         // M001 the initial task
	s.send("writer", "reviewer", msg)       // M002 please review
	p := s.send("reviewer", "writer", msg)  // M003 review 1
	equal(t, "round 1", *p.Round, int64(1)) //
	p = s.send("writer", "reviewer", msg)   // M004 a clarifying question
	equal(t, "question has no round", p.Round, (*int64)(nil))
	s.send("writer", "reviewer", msg)     // M005 another
	s.send("reviewer", "writer", auto)    // M006 its auto-reply answers: review 2
	p = s.send("reviewer", "writer", rep) // M007 review 3: over the allowance
	equal(t, "held for rounds", [2]any{p.Deliveries[0].State, *p.Deliveries[0].Reason},
		[2]any{model.DeliveryStateHeld, model.DeliveryReasonRounds})
	equal(t, "events", causes(p.Events), []string{"hops:used", "rounds:exhausted"})
	p = s.send("writer", "reviewer", msg) // M008 the writer still gets through
	equal(t, "writer not held", p.Deliveries[0].State, model.DeliveryStateQueued)
	s.send("reviewer", "writer", msg) // M009 review 4: held too
	equal(t, "exchange", s.deliveries(), []string{
		"M001 queued", "M002 queued h1", "M003 queued h2 r1", "M004 queued h3", "M005 queued h4",
		"M006 queued h5 r2", "M007 held rounds h6 r3", "M008 queued h7", "M009 held rounds h8 r4"})
	equal(t, "counters", s.counters(), "hops 8/100 rounds 4/2!")
	equal(t, "pair", [2]string{s.rounds[0].Writer.Name, s.rounds[0].Reviewer.Name}, [2]string{"writer", "reviewer"})

	// Continue by 1: review 3 goes; review 4 still waits.
	r := s.continueRounds(ptr(int64(1)))
	equal(t, "continue events", causes(r.Events), []string{"rounds:continued:human", "rounds:exhausted"})
	equal(t, "after continue", s.deliveries()[6:], []string{"M007 queued h6 r3", "M008 queued h7", "M009 held rounds h8 r4"})

	// The person writes to the writer: the pair resets, and review 4 is
	// counted again as round 1.
	p = s.send("human", "writer", msg)
	equal(t, "reset events", causes(p.Events), []string{"hops:reset:human", "rounds:reset:human"})
	equal(t, "after reset", s.deliveries()[8], "M009 queued h8 r1")
	equal(t, "counters", s.counters(), "hops 0/100 rounds 1/2")

	// A message from the person to someone outside the pair leaves it alone.
	s.members = append(s.members, agent("X", "other", model.RoleCustom, model.HarnessClaude))
	s.send("reviewer", "writer", msg) // round 2
	p = s.send("human", "other", msg)
	equal(t, "unrelated reset", causes(p.Events), []string{"hops:reset:human"})
	equal(t, "counters", s.counters(), "hops 0/100 rounds 2/2")
	// To the reviewer, it resets.
	p = s.send("human", "reviewer", msg)
	equal(t, "reviewer reset", causes(p.Events), []string{"rounds:reset:human"})
}

// A delivery held for budget whose round is also over its pair's allowance
// stays held, now for rounds, when the budget continues.
func TestBudgetReleaseChecksRounds(t *testing.T) {
	s := newSim(t, 2, 1)
	s.send("reviewer", "writer", msg) // h1 r1
	s.send("reviewer", "writer", msg) // h2 r2: held rounds
	p := s.send("reviewer", "writer", msg)
	equal(t, "budget wins", *p.Deliveries[0].Reason, model.DeliveryReasonBudget)
	equal(t, "events", causes(p.Events), []string{"hops:exhausted", "rounds:used"})
	equal(t, "before", s.deliveries(), []string{"M001 queued h1 r1", "M002 held rounds h2 r2", "M003 held budget h3 r3"})

	r := s.continueHops(nil)
	equal(t, "moved to rounds", s.deliveries()[2], "M003 held rounds h3 r3")
	equal(t, "update", r.Held, []HeldUpdate{{Message: "M003", To: model.MemberRef{ID: "W", Name: "writer"},
		From: model.DeliveryReasonBudget, State: model.DeliveryStateHeld, Reason: ptr(model.DeliveryReasonRounds),
		Hop: ptr(int64(3)), Round: ptr(int64(3)), Moved: true}})
	equal(t, "released", r.Released, int64(0))

	r = s.continueRounds(ptr(int64(5)))
	equal(t, "released", r.Released, int64(2))
	equal(t, "after", s.deliveries(), []string{"M001 queued h1 r1", "M002 queued h2 r2", "M003 queued h3 r3"})
}

// A reset by the person renumbers a budget-held delivery's round with its
// pair, so it isn't caught by its old round when the budget lets it go.
func TestHumanResetRenumbersBudgetHeldRounds(t *testing.T) {
	s := newSim(t, 1, 1)
	s.send("reviewer", "writer", msg) // h1 r1
	s.send("reviewer", "writer", msg) // h2 r2: budget (and rounds over)
	s.send("reviewer", "writer", msg) // h3 r3: budget
	p := s.send("human", "writer", msg)
	equal(t, "events", causes(p.Events), []string{"hops:reset:human", "hops:exhausted", "rounds:reset:human", "rounds:exhausted"})
	equal(t, "after", s.deliveries()[:3], []string{"M001 queued h1 r1", "M002 queued h1 r1", "M003 held budget h2 r2"})
	equal(t, "counters", s.counters(), "hops 2/1! rounds 2/1!")
}

func TestContinueRoundsChoices(t *testing.T) {
	h := model.MemberRef{ID: "H", Name: "human"}
	w := agent("W", "writer", model.RoleWriter, model.HarnessClaude)
	rv := agent("R", "reviewer", model.RoleReviewer, model.HarnessCodex)
	w2 := agent("W2", "writer2", model.RoleWriter, model.HarnessClaude)
	state := BudgetState{Hops: model.Hops{Allowance: 16, Base: 16}, ReviewRounds: 3, Rounds: []model.Rounds{
		{Writer: ref(w), Reviewer: ref(rv), Used: 4, Allowance: 3},
		{Writer: ref(w2), Reviewer: ref(rv), Used: 2, Allowance: 3},
	}}

	// Neither: every exhausted pair, by the default step.
	r, err := ContinueRounds(state, nil, nil, nil, h)
	requireNoError(t, err)
	equal(t, "allowances", [2]int64{r.Rounds[0].Allowance, r.Rounds[1].Allowance}, [2]int64{6, 3})
	equal(t, "events", causes(r.Events), []string{"rounds:continued:human"})

	// A named pair that never exchanged a review gets a counter.
	other := agent("W3", "writer3", model.RoleWriter, model.HarnessPi)
	r, err = ContinueRounds(state, &other, &rv, ptr(int64(2)), h)
	requireNoError(t, err)
	equal(t, "new pair", r.Rounds[2], model.Rounds{Writer: ref(other), Reviewer: ref(rv), Used: 0, Allowance: 5, Base: 3})

	// Roles must match, and both come together.
	_, err = ContinueRounds(state, &rv, &w, nil, h)
	requireCode(t, err, model.ErrorInvalidRequest)
	equal(t, "paths", fieldErrorPaths(t, err), []string{"/writer", "/reviewer"})
	_, err = ContinueRounds(state, &w, nil, nil, h)
	requireCode(t, err, model.ErrorInvalidRequest)

	// Amount is 1 to 1000.
	for _, a := range []int64{0, -1, 1001} {
		_, err = ContinueRounds(state, nil, nil, ptr(a), h)
		requireCode(t, err, model.ErrorInvalidRequest)
		_, err = ContinueHops(state, ptr(a), h)
		requireCode(t, err, model.ErrorInvalidRequest)
	}
	for _, a := range []int64{1, 1000} {
		r, err = ContinueHops(state, ptr(a), h)
		requireNoError(t, err)
		equal(t, "allowance", r.Hops.Allowance, 16+a)
	}

	// Continuing when nothing is exhausted is allowed and still recorded.
	r, err = ContinueHops(state, nil, h)
	requireNoError(t, err)
	equal(t, "hops", r.Hops, model.Hops{Used: 0, Allowance: 32, Base: 16})
	equal(t, "events", causes(r.Events), []string{"hops:continued:human"})
}

func TestResetForStartRounds(t *testing.T) {
	w := model.MemberRef{ID: "W", Name: "writer"}
	rv := model.MemberRef{ID: "R", Name: "reviewer"}
	r := ResetForStart(BudgetState{
		Hops:         model.Hops{Used: 3, Allowance: 16, Base: 20}, // hop_budget changed since
		ReviewRounds: 3,
		Rounds: []model.Rounds{
			{Writer: w, Reviewer: rv, Used: 5, Allowance: 4},
			{Writer: model.MemberRef{ID: "W2", Name: "w2"}, Reviewer: rv, Used: 0, Allowance: 3},
		},
		Held: []HeldDelivery{
			{Message: "B", Seq: 9, From: rv, To: w, Reason: model.DeliveryReasonRounds, Hop: ptr(int64(2)), Round: ptr(int64(5))},
			{Message: "A", Seq: 8, From: rv, To: w, Reason: model.DeliveryReasonRounds, Hop: ptr(int64(1)), Round: ptr(int64(4))},
			{Message: "X", Seq: 1, From: rv, To: w, Reason: model.DeliveryReasonPersonControl},
		},
	})
	equal(t, "hops", r.Hops, model.Hops{Used: 0, Allowance: 20, Base: 20})
	equal(t, "rounds", r.Rounds[0], model.Rounds{Writer: w, Reviewer: rv, Used: 2, Allowance: 3, Base: 3})
	equal(t, "untouched pair", r.Rounds[1].Allowance, int64(3))
	equal(t, "events", causes(r.Events), []string{"hops:reset", "rounds:reset"})
	equal(t, "order", []string{r.Held[0].Message, r.Held[1].Message}, []string{"A", "B"})
	equal(t, "renumbered", []int64{*r.Held[0].Round, *r.Held[1].Round}, []int64{1, 2})
	equal(t, "released", r.Released, int64(2))

	// Counters the store hasn't written yet (allowance 0) start fresh.
	r = ResetForStart(BudgetState{Hops: model.Hops{Base: 16}, ReviewRounds: 3,
		Rounds: []model.Rounds{{Writer: w, Reviewer: rv}}})
	equal(t, "fresh", [2]any{r.Hops, r.Rounds[0]}, [2]any{model.Hops{Allowance: 16, Base: 16},
		model.Rounds{Writer: w, Reviewer: rv, Allowance: 3, Base: 3}})
	equal(t, "fresh events", r.Events, []Event{})
}

func TestBudgetEventData(t *testing.T) {
	s := newSim(t, 1, 1)
	s.send("reviewer", "writer", msg)
	p := s.send("reviewer", "writer", msg)
	e := p.Events[0]
	equal(t, "type", [2]any{e.Type, e.Timeline}, [2]any{model.EventBudgetChanged, true})
	equal(t, "budget data", jsonOf(t, e.Data), map[string]any{
		"hops":  map[string]any{"used": 2.0, "allowance": 1.0, "base": 1.0, "exhausted": true},
		"cause": "exhausted", "by": nil,
	})
	e = p.Events[1]
	equal(t, "rounds data", jsonOf(t, e.Data), map[string]any{
		"rounds": map[string]any{"writer": map[string]any{"id": "W", "name": "writer"},
			"reviewer": map[string]any{"id": "R", "name": "reviewer"}, "used": 2.0, "allowance": 1.0, "base": 1.0, "exhausted": true},
		"cause": "exhausted", "by": nil,
	})
	if e.Member != nil || e.Message != nil {
		t.Fatal("budget events name no member or message")
	}
	first := s.send("writer", "reviewer", msg).Events[0]
	equal(t, "used is not on the timeline", [2]any{first.Data.(wire.BudgetChangedEventData).Cause, first.Timeline},
		[2]any{model.BudgetCauseUsed, false})
	reset := s.send("human", "writer", msg).Events[0]
	equal(t, "reset by human", jsonOf(t, reset.Data)["by"], map[string]any{"id": "H", "name": "human"})
}
