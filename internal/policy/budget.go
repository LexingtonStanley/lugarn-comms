package policy

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Pair names a (writer, reviewer) pair whose review rounds are counted.
type Pair struct {
	Writer   model.MemberRef
	Reviewer model.MemberRef
}

// HeldDelivery is a delivery held for budget or rounds, as the store has it.
type HeldDelivery struct {
	Message string
	// Seq is the message's store sequence: deliveries are counted and released
	// in this order.
	Seq    int64
	From   model.MemberRef // the message's sender
	To     model.MemberRef // the recipient
	Reason model.DeliveryReason
	Hop    *int64
	// Round is set when the message counted as a review round. Its pair is
	// (writer = To, reviewer = From): only reviewer → writer messages count.
	Round *int64
}

// BudgetState is a team's hop and round counters and its held deliveries.
type BudgetState struct {
	// Hops is the team's hop counter, with Base the team's current hop_budget.
	// A counter with Allowance 0 is taken as fresh: used 0, allowance Base.
	Hops model.Hops
	// Rounds are the team's round counters this run, one per pair. Their Base
	// is taken from ReviewRounds.
	Rounds []model.Rounds
	// ReviewRounds is the team's current review_rounds: every pair's base and
	// a new pair's allowance.
	ReviewRounds int64
	// Held are the team's deliveries held for budget or rounds. Deliveries
	// held for other reasons are ignored. Needed by resets and continues; an
	// agent's message ignores it.
	Held []HeldDelivery
}

// HeldUpdate is a held delivery that a reset or a continue changed.
type HeldUpdate struct {
	Message string
	To      model.MemberRef
	// From is the reason it was held for before.
	From model.DeliveryReason
	// State is queued, or held when it still waits.
	State model.DeliveryState
	// Reason is budget or rounds while held; nil once queued.
	Reason *model.DeliveryReason
	// Hop and Round are the message's numbers now.
	Hop   *int64
	Round *int64
	// Moved: the delivery's state or reason changed. Write it, and write
	// delivery.changed (from held) with its snapshot.
	Moved bool
	// Renumbered: the message's hop or round changed. Write the message.
	Renumbered bool
}

// BudgetResult is the counters after a decision and what it did to held
// deliveries.
type BudgetResult struct {
	Hops model.Hops
	// Rounds is every pair's counter after the decision: the input's pairs in
	// their order, then any new pair. A pair at used 0 with its base
	// allowance is the same as no row.
	Rounds []model.Rounds
	// Held are the held deliveries that changed, in message order.
	Held []HeldUpdate
	// Released counts deliveries moved back to queued.
	Released int64
	// Events are budget.changed and rounds.changed, in order.
	Events []Event
}

// MaxContinueAmount bounds continue-budget's amount.
const MaxContinueAmount = 1000

// ContinueHops is continue-budget with kind hops: the allowance grows by
// amount (nil: the team's hop_budget), and deliveries held for budget whose
// hop is now within it go back to queued in order, unless their review round
// is over its pair's allowance, which holds them for rounds instead. Events:
// budget.changed (continued, by the person), then budget.changed (exhausted)
// if deliveries are still held over the new allowance.
func ContinueHops(s BudgetState, amount *int64, by model.MemberRef) (BudgetResult, *model.Error) {
	n, err := continueAmount(amount, s.Hops.Base)
	if err != nil {
		return BudgetResult{}, err
	}
	l := newLedger(s)
	l.continueHops(n, &by)
	return l.result(), nil
}

// ContinueRounds is continue-budget with kind rounds. With writer and
// reviewer it adds amount (nil: review_rounds) to that pair, creating its
// counter if the pair hasn't exchanged a review yet; with neither, to every
// exhausted pair. Deliveries held for rounds whose round is now within their
// pair's allowance go back to queued in order. Events per pair:
// rounds.changed (continued, by the person), then rounds.changed (exhausted)
// if the pair is still over.
//
// writer and reviewer come together; writer must have role writer and
// reviewer role reviewer, else invalid_request.
func ContinueRounds(s BudgetState, writer, reviewer *model.Member, amount *int64, by model.MemberRef) (BudgetResult, *model.Error) {
	n, err := continueAmount(amount, s.ReviewRounds)
	if err != nil {
		return BudgetResult{}, err
	}
	l := newLedger(s)
	switch {
	case writer == nil && reviewer == nil:
		var keys []pairKey
		for _, r := range l.rounds {
			if r.Used > r.Allowance {
				keys = append(keys, keyOf(r.Writer, r.Reviewer))
			}
		}
		l.continueRounds(keys, n, &by)
	case writer == nil || reviewer == nil:
		return BudgetResult{}, model.NewError(model.ErrorInvalidRequest, "continue-budget takes both writer and reviewer, or neither").
			WithDetail("errors", []FieldError{{Path: "/writer", Message: "writer and reviewer come together"}})
	default:
		var errs fieldErrors
		if writer.Role == nil || *writer.Role != model.RoleWriter {
			errs.add("/writer", "%s isn't a writer", writer.Name)
		}
		if reviewer.Role == nil || *reviewer.Role != model.RoleReviewer {
			errs.add("/reviewer", "%s isn't a reviewer", reviewer.Name)
		}
		if len(errs) > 0 {
			return BudgetResult{}, model.NewError(model.ErrorInvalidRequest, errs[0].Path[1:]+": "+errs[0].Message).
				WithDetail("errors", []FieldError(errs))
		}
		p := Pair{Writer: ref(*writer), Reviewer: ref(*reviewer)}
		l.pair(p)
		l.continueRounds([]pairKey{keyOf(p.Writer, p.Reviewer)}, n, &by)
	}
	return l.result(), nil
}

// ResetForStart is the reset every team start makes: hops to used 0 and
// allowance hop_budget, every pair to used 0 and allowance review_rounds,
// with held deliveries counted again in order and renumbered, and those
// within the new allowances queued. Events have by null and appear only for
// counters that change.
func ResetForStart(s BudgetState) BudgetResult {
	l := newLedger(s)
	l.resetHops(nil)
	keys := make([]pairKey, 0, len(l.rounds))
	for _, r := range l.rounds {
		keys = append(keys, keyOf(r.Writer, r.Reviewer))
	}
	l.resetRounds(keys, nil)
	return l.result()
}

func continueAmount(amount *int64, base int64) (int64, *model.Error) {
	if amount == nil {
		return base, nil
	}
	if *amount < 1 || *amount > MaxContinueAmount {
		return 0, model.NewError(model.ErrorInvalidRequest, fmt.Sprintf("amount must be from 1 to %d", MaxContinueAmount)).
			WithDetail("errors", []FieldError{{Path: "/amount", Message: fmt.Sprintf("must be from 1 to %d", MaxContinueAmount)}})
	}
	return *amount, nil
}

type pairKey struct{ writer, reviewer string }

func keyOf(writer, reviewer model.MemberRef) pairKey { return pairKey{writer.ID, reviewer.ID} }

type heldItem struct {
	orig   HeldDelivery
	state  model.DeliveryState
	reason model.DeliveryReason
	hop    *int64
	round  *int64
}

func (h *heldItem) pair() pairKey { return pairKey{h.orig.To.ID, h.orig.From.ID} }

func (h *heldItem) heldFor(r model.DeliveryReason) bool {
	return h.state == model.DeliveryStateHeld && h.reason == r
}

// ledger applies the counting rules to one snapshot of the counters.
type ledger struct {
	hops   model.Hops
	rounds []model.Rounds
	index  map[pairKey]int
	base   int64
	held   []*heldItem
	events []Event
}

func newLedger(s BudgetState) *ledger {
	l := &ledger{hops: s.Hops, index: map[pairKey]int{}, base: s.ReviewRounds}
	if l.hops.Allowance < 1 {
		l.hops.Used, l.hops.Allowance = 0, l.hops.Base
	}
	l.hops.Exhausted = l.hops.Used > l.hops.Allowance
	for _, r := range s.Rounds {
		r.Base = s.ReviewRounds
		if r.Allowance < 1 {
			r.Used, r.Allowance = 0, r.Base
		}
		r.Exhausted = r.Used > r.Allowance
		l.index[keyOf(r.Writer, r.Reviewer)] = len(l.rounds)
		l.rounds = append(l.rounds, r)
	}
	for _, d := range s.Held {
		if d.Reason != model.DeliveryReasonBudget && d.Reason != model.DeliveryReasonRounds {
			continue
		}
		l.held = append(l.held, &heldItem{orig: d, state: model.DeliveryStateHeld, reason: d.Reason, hop: d.Hop, round: d.Round})
	}
	slices.SortStableFunc(l.held, func(a, b *heldItem) int { return cmp.Compare(a.orig.Seq, b.orig.Seq) })
	return l
}

// pair returns the index of p's counter, creating it at used 0 and the base
// allowance.
func (l *ledger) pair(p Pair) int {
	k := keyOf(p.Writer, p.Reviewer)
	if i, ok := l.index[k]; ok {
		return i
	}
	l.index[k] = len(l.rounds)
	l.rounds = append(l.rounds, model.Rounds{Writer: p.Writer, Reviewer: p.Reviewer, Allowance: l.base, Base: l.base})
	return len(l.rounds) - 1
}

// countHop counts one agent message: it gets hop = used + 1. over reports a
// hop past the allowance. The message that makes the budget exhausted emits
// budget.changed (exhausted); every other one (used).
func (l *ledger) countHop() (hop int64, over bool) {
	was := l.hops.Exhausted
	l.hops.Used++
	l.hops.Exhausted = l.hops.Used > l.hops.Allowance
	cause := model.BudgetCauseUsed
	if !was && l.hops.Exhausted {
		cause = model.BudgetCauseExhausted
	}
	l.events = append(l.events, budgetChanged(l.hops, cause, nil))
	return l.hops.Used, l.hops.Used > l.hops.Allowance
}

// countRound counts one review for p, likewise.
func (l *ledger) countRound(p Pair) (round int64, over bool) {
	i := l.pair(p)
	r := &l.rounds[i]
	was := r.Exhausted
	r.Used++
	r.Exhausted = r.Used > r.Allowance
	cause := model.BudgetCauseUsed
	if !was && r.Exhausted {
		cause = model.BudgetCauseExhausted
	}
	l.events = append(l.events, roundsChanged(*r, cause, nil))
	return r.Used, r.Used > r.Allowance
}

// release takes a delivery off its budget hold: queued, unless its review
// round is over its pair's allowance, which holds it for rounds.
func (l *ledger) release(it *heldItem) {
	if it.round != nil {
		allowance := l.base
		if i, ok := l.index[it.pair()]; ok {
			allowance = l.rounds[i].Allowance
		}
		if *it.round > allowance {
			it.reason = model.DeliveryReasonRounds
			return
		}
	}
	it.state, it.reason = model.DeliveryStateQueued, ""
}

func (l *ledger) resetHops(by *model.MemberRef) {
	before := l.hops
	l.hops.Used, l.hops.Allowance = 0, l.hops.Base
	changed := false
	for _, it := range l.held {
		if !it.heldFor(model.DeliveryReasonBudget) {
			continue
		}
		l.hops.Used++
		if it.hop == nil || *it.hop != l.hops.Used {
			it.hop = ptr(l.hops.Used)
			changed = true
		}
		if l.hops.Used <= l.hops.Allowance {
			l.release(it)
			changed = true
		}
	}
	l.hops.Exhausted = l.hops.Used > l.hops.Allowance
	if changed || l.hops != before {
		l.events = append(l.events, budgetChanged(l.hops, model.BudgetCauseReset, by))
		if l.hops.Exhausted {
			l.events = append(l.events, budgetChanged(l.hops, model.BudgetCauseExhausted, nil))
		}
	}
}

func (l *ledger) continueHops(amount int64, by *model.MemberRef) {
	l.hops.Allowance += amount
	for _, it := range l.held {
		if it.heldFor(model.DeliveryReasonBudget) && (it.hop == nil || *it.hop <= l.hops.Allowance) {
			l.release(it)
		}
	}
	l.hops.Exhausted = l.hops.Used > l.hops.Allowance
	l.events = append(l.events, budgetChanged(l.hops, model.BudgetCauseContinued, by))
	if l.hops.Exhausted {
		l.events = append(l.events, budgetChanged(l.hops, model.BudgetCauseExhausted, nil))
	}
}

// resetRounds resets each listed pair, in counter order, and counts its held
// deliveries again in order: those held for budget get new rounds and stay
// held; those held for rounds within the new allowance are queued.
func (l *ledger) resetRounds(keys []pairKey, by *model.MemberRef) {
	for i := range l.rounds {
		r := &l.rounds[i]
		k := keyOf(r.Writer, r.Reviewer)
		if !slices.Contains(keys, k) {
			continue
		}
		before := *r
		r.Used, r.Allowance = 0, r.Base
		changed := false
		for _, it := range l.held {
			if it.state != model.DeliveryStateHeld || it.round == nil || it.pair() != k {
				continue
			}
			r.Used++
			if *it.round != r.Used {
				it.round = ptr(r.Used)
				changed = true
			}
			if it.reason == model.DeliveryReasonRounds && r.Used <= r.Allowance {
				it.state, it.reason = model.DeliveryStateQueued, ""
				changed = true
			}
		}
		r.Exhausted = r.Used > r.Allowance
		if changed || *r != before {
			l.events = append(l.events, roundsChanged(*r, model.BudgetCauseReset, by))
			if r.Exhausted {
				l.events = append(l.events, roundsChanged(*r, model.BudgetCauseExhausted, nil))
			}
		}
	}
}

func (l *ledger) continueRounds(keys []pairKey, amount int64, by *model.MemberRef) {
	for i := range l.rounds {
		r := &l.rounds[i]
		k := keyOf(r.Writer, r.Reviewer)
		if !slices.Contains(keys, k) {
			continue
		}
		r.Allowance += amount
		for _, it := range l.held {
			if it.heldFor(model.DeliveryReasonRounds) && it.pair() == k && (it.round == nil || *it.round <= r.Allowance) {
				it.state, it.reason = model.DeliveryStateQueued, ""
			}
		}
		r.Exhausted = r.Used > r.Allowance
		l.events = append(l.events, roundsChanged(*r, model.BudgetCauseContinued, by))
		if r.Exhausted {
			l.events = append(l.events, roundsChanged(*r, model.BudgetCauseExhausted, nil))
		}
	}
}

func (l *ledger) result() BudgetResult {
	out := BudgetResult{Hops: l.hops, Rounds: slices.Clone(l.rounds), Held: []HeldUpdate{}, Events: l.events}
	if out.Rounds == nil {
		out.Rounds = []model.Rounds{}
	}
	if out.Events == nil {
		out.Events = []Event{}
	}
	for _, it := range l.held {
		moved := it.state != model.DeliveryStateHeld || it.reason != it.orig.Reason
		renumbered := !samePtr(it.hop, it.orig.Hop) || !samePtr(it.round, it.orig.Round)
		if !moved && !renumbered {
			continue
		}
		u := HeldUpdate{Message: it.orig.Message, To: it.orig.To, From: it.orig.Reason, State: it.state,
			Hop: it.hop, Round: it.round, Moved: moved, Renumbered: renumbered}
		if it.state == model.DeliveryStateHeld {
			u.Reason = ptr(it.reason)
		} else {
			out.Released++
		}
		out.Held = append(out.Held, u)
	}
	return out
}

func samePtr(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
