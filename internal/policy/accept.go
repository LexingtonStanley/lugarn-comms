package policy

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Limits on messages to human.
const (
	// MaxHumanText caps a message to human, in code points.
	MaxHumanText = 8000
	// HumanNotifyLimit is how many of one member's messages to human in any
	// 10 minutes raise attention.
	HumanNotifyLimit = 5
)

// ResolveRecipient finds the live member an agent or the CLI addresses by
// name. The name all resolves to all = true (only human may use it; Accept
// refuses an agent). Any other name that isn't a live member's is
// unknown_member, and nothing is stored.
func ResolveRecipient(live []model.Member, name string) (to *model.Member, all bool, err *model.Error) {
	if name == AllName {
		return nil, true, nil
	}
	for i := range live {
		if live[i].Name == name && live[i].RemovedAt == nil {
			return &live[i], false, nil
		}
	}
	return nil, false, model.NewError(model.ErrorUnknownMember, fmt.Sprintf("no live member is called %s", name)).
		WithDetail("name", name)
}

// MessageInput is one message to accept, with the store facts it needs.
type MessageInput struct {
	// ID is the new message's id, minted by the caller.
	ID string
	// Kind is message, reply, auto_reply or task. A message with ReplyTo is a
	// reply.
	Kind model.MessageKind
	// From is the resolved sender: an agent, or the team's human.
	From model.Member
	// To is the resolved recipient; nil with All.
	To *model.Member
	// All is the person's send to every live agent member that is starting or
	// running.
	All bool
	// Live is the team's live roster, human included. Only All needs it.
	Live []model.Member
	Text string
	// ReplyTo is the message this answers.
	ReplyTo *string
	// ReplyFound is the caller's check of ReplyTo against the store: for a
	// reply, a message the sender received; for a send, a message of this
	// team. Accept refuses with not_found when it is false.
	ReplyFound bool
	Grid       Grid
	// Budget is the team's counters. Held deliveries matter only to a message
	// from human, whose resets count them again.
	Budget BudgetState
	// ToHumanRecent is how many messages the sender sent human in the 10
	// minutes before this one.
	ToHumanRecent int
}

// Outcome is what becomes of an accepted message.
type Outcome string

const (
	// OutcomeAccepted: store the message and its deliveries.
	OutcomeAccepted Outcome = "accepted"
	// OutcomeDenied: the grid refuses. Store the message with its delivery
	// denied (grid), write policy.denied, and answer the sender with Err.
	OutcomeDenied Outcome = "denied"
	// OutcomeSkipped: an auto-reply the grid refuses. Store nothing.
	OutcomeSkipped Outcome = "skipped"
)

// DeliveryPlan is one recipient's new delivery.
type DeliveryPlan struct {
	To     model.MemberRef
	State  model.DeliveryState
	Reason *model.DeliveryReason
	// Via is human for a delivery to human, else nil until delivered.
	Via *model.DeliveryVia
	// Wake: this delivery may relaunch a stopped recipient.
	Wake bool
	// Notify is set only on deliveries to human: false past the rate limit.
	Notify *bool
	// WillWake is send's will_wake: the recipient is stopped and this queued
	// delivery will start it.
	WillWake bool
}

// MessagePlan is the decision on one message.
//
// For OutcomeAccepted and OutcomeDenied the caller stores the message (with
// Kind, Broadcast, ReplyTo, Text, Hop and Round) and its Deliveries, applies
// Budget (counters, then each held update), and writes message.created, then
// Events, then delivery.changed for each moved held delivery.
type MessagePlan struct {
	Outcome Outcome
	// Err is the sender's answer for OutcomeDenied: denied, with
	// details.action and details.message_id.
	Err       *model.Error
	Kind      model.MessageKind
	Broadcast bool
	ReplyTo   *string
	// Text is the text to store: an auto-reply over its cap is cut, ending
	// with the marker line.
	Text string
	// Hop and Round are the message's numbers if it counts.
	Hop        *int64
	Round      *int64
	Deliveries []DeliveryPlan
	// Budget is the counters after this message, and the held deliveries a
	// message from human released or renumbered.
	Budget BudgetResult
	// Events are policy.denied, budget.changed and rounds.changed, in order.
	Events []Event
}

// Accept runs steps 1–6 of Accepting a message. A refusal that stores
// nothing returns an error: not_found (reply_to), invalid_request (to self,
// an agent to all, a broadcast with nobody to receive it, text out of
// bounds, an agent's send or reply to human over 8,000 code points).
func Accept(in MessageInput) (MessagePlan, *model.Error) {
	kind := in.Kind
	if kind == model.MessageKindMessage && in.ReplyTo != nil {
		kind = model.MessageKindReply
	}
	switch {
	case !kind.Valid():
		return MessagePlan{}, invalid("/kind", "must be one of %v", model.MessageKind("").Values())
	case (kind == model.MessageKindReply || kind == model.MessageKindAutoReply) && in.ReplyTo == nil:
		return MessagePlan{}, invalid("/reply_to", "a %s needs the message it answers", kind)
	case kind == model.MessageKindTask && in.ReplyTo != nil:
		return MessagePlan{}, invalid("/reply_to", "a task answers no message")
	}
	if in.ReplyTo != nil && !in.ReplyFound {
		msg := fmt.Sprintf("no message %s in this team", *in.ReplyTo)
		if kind == model.MessageKindReply && in.From.Kind != model.MemberKindHuman {
			msg = fmt.Sprintf("%s isn't a message you received", *in.ReplyTo)
		}
		return MessagePlan{}, model.NewError(model.ErrorNotFound, msg).WithDetail("reply_to", *in.ReplyTo)
	}

	plan := MessagePlan{Outcome: OutcomeAccepted, Kind: kind, ReplyTo: in.ReplyTo, Events: []Event{}}
	fromHuman := in.From.Kind == model.MemberKindHuman

	// Step 1: resolve.
	var recipients []model.Member
	switch {
	case in.All:
		if !fromHuman {
			return MessagePlan{}, invalid("/to", "only the person can send to all")
		}
		for _, m := range in.Live {
			if m.Kind == model.MemberKindAgent && m.RemovedAt == nil &&
				(m.State == model.MemberStateStarting || m.State == model.MemberStateRunning) {
				recipients = append(recipients, m)
			}
		}
		if len(recipients) == 0 {
			return MessagePlan{}, invalid("/all", "no member is starting or running to receive it")
		}
		plan.Broadcast = true
	case in.To == nil:
		return MessagePlan{}, invalid("/to", "is required")
	case in.To.ID == in.From.ID:
		return MessagePlan{}, invalid("/to", "you can't message yourself")
	default:
		recipients = []model.Member{*in.To}
	}
	toHuman := !plan.Broadcast && recipients[0].Kind == model.MemberKindHuman
	text, err := acceptText(in.Text, kind, toHuman)
	if err != nil {
		return MessagePlan{}, err
	}
	plan.Text = text

	ledger := newLedger(in.Budget)
	if plan.Broadcast {
		// The person's broadcast: no grid, no counting, wakes no one.
		for _, m := range recipients {
			plan.Deliveries = append(plan.Deliveries, DeliveryPlan{To: ref(m), State: model.DeliveryStateQueued})
		}
		humanResets(ledger, recipients, in.From)
		plan.Budget = ledger.result()
		plan.Events = append(plan.Events, plan.Budget.Events...)
		return plan, nil
	}

	to := recipients[0]
	// Step 2: policy.
	if !fromHuman && !toHuman && !in.Grid.Can(in.From.Name, to.Name, model.GridCapabilityMessage) {
		if kind == model.MessageKindAutoReply {
			return MessagePlan{Outcome: OutcomeSkipped, Kind: kind, ReplyTo: in.ReplyTo, Text: text,
				Deliveries: []DeliveryPlan{}, Budget: ledger.result(), Events: []Event{}}, nil
		}
		plan.Outcome = OutcomeDenied
		plan.Err = model.NewError(model.ErrorDenied, fmt.Sprintf("%s may not message %s", in.From.Name, to.Name)).
			WithDetails(map[string]any{"action": model.PolicyActionMessage, "message_id": in.ID})
		plan.Deliveries = []DeliveryPlan{{To: ref(to), State: model.DeliveryStateDenied, Reason: ptr(model.DeliveryReasonGrid)}}
		target := ref(to)
		plan.Events = append(plan.Events, policyDenied(model.PolicyActionMessage, ref(in.From), &target, ptr(in.ID), plan.Err))
		plan.Budget = ledger.result()
		return plan, nil
	}

	// Step 3: to human.
	if toHuman {
		plan.Deliveries = []DeliveryPlan{{To: ref(to), State: model.DeliveryStateDelivered,
			Via: ptr(model.DeliveryViaHuman), Notify: ptr(in.ToHumanRecent < HumanNotifyLimit)}}
		plan.Budget = ledger.result()
		return plan, nil
	}

	// Step 4: count.
	d := DeliveryPlan{To: ref(to), State: model.DeliveryStateQueued}
	if !fromHuman {
		hop, overHops := ledger.countHop()
		plan.Hop = ptr(hop)
		overRounds := false
		if roleIs(in.From, model.RoleReviewer) && roleIs(to, model.RoleWriter) {
			var round int64
			round, overRounds = ledger.countRound(Pair{Writer: ref(to), Reviewer: ref(in.From)})
			plan.Round = ptr(round)
		}
		switch {
		case overHops:
			d.State, d.Reason = model.DeliveryStateHeld, ptr(model.DeliveryReasonBudget)
		case overRounds:
			d.State, d.Reason = model.DeliveryStateHeld, ptr(model.DeliveryReasonRounds)
		}
	}

	// Step 5: wake.
	d.Wake = fromHuman || in.Grid.Can(in.From.Name, to.Name, model.GridCapabilityWake)
	d.WillWake = d.Wake && d.State == model.DeliveryStateQueued && to.State == model.MemberStateStopped
	plan.Deliveries = []DeliveryPlan{d}

	// Step 6: a message from human resets.
	if fromHuman {
		humanResets(ledger, recipients, in.From)
	}
	plan.Budget = ledger.result()
	plan.Events = append(plan.Events, plan.Budget.Events...)
	return plan, nil
}

// humanResets applies a message from human: the hop budget resets, and so do
// the rounds of every pair one of its recipients belongs to.
func humanResets(l *ledger, recipients []model.Member, human model.Member) {
	by := ref(human)
	l.resetHops(&by)
	var keys []pairKey
	for _, r := range l.rounds {
		for _, m := range recipients {
			if r.Writer.ID == m.ID || r.Reviewer.ID == m.ID {
				keys = append(keys, keyOf(r.Writer, r.Reviewer))
				break
			}
		}
	}
	l.resetRounds(keys, &by)
}

func roleIs(m model.Member, r model.Role) bool { return m.Role != nil && *m.Role == r }

// acceptText checks a message's text against its caps: 65,536 code points
// for anyone, 8,000 for human. An auto-reply over its cap is cut instead of
// refused, since nobody is there to retry it.
func acceptText(text string, kind model.MessageKind, toHuman bool) (string, *model.Error) {
	if !utf8.ValidString(text) {
		return "", invalid("/text", "must be valid UTF-8")
	}
	n := utf8.RuneCountInString(text)
	if n < model.MinTextCodePoints {
		return "", invalid("/text", "must not be empty")
	}
	limit := model.MaxTextCodePoints
	if toHuman {
		limit = MaxHumanText
	}
	if n <= limit {
		return text, nil
	}
	if kind == model.MessageKindAutoReply {
		return CutText(text, limit), nil
	}
	msg := fmt.Sprintf("text is capped at %d characters", limit)
	if toHuman {
		msg = fmt.Sprintf("messages to human are capped at %d characters: shorten it and send again", limit)
	}
	return "", model.NewError(model.ErrorInvalidRequest, msg).
		WithDetails(map[string]any{"field": "text", "limit": limit, "length": n})
}

// CutText returns text cut to at most limit code points, ending with the
// line "[… cut by Lugarn: <n> more characters in the transcript]", where n
// counts the code points left out. Text within the limit comes back as is.
func CutText(text string, limit int) string {
	total := utf8.RuneCountInString(text)
	if total <= limit {
		return text
	}
	// The marker's length depends on n's digits, so settle on the largest
	// kept prefix whose marker still fits.
	kept := limit
	for kept > 0 {
		marker := cutMarker(total - kept)
		if kept+1+utf8.RuneCountInString(marker) <= limit {
			break
		}
		kept--
	}
	return prefixRunes(text, kept) + "\n" + cutMarker(total-kept)
}

func cutMarker(n int) string {
	return "[… cut by Lugarn: " + strconv.Itoa(n) + " more characters in the transcript]"
}

func prefixRunes(s string, n int) string {
	i := 0
	for ; n > 0 && i < len(s); n-- {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}

func invalid(path, format string, args ...any) *model.Error {
	msg := fmt.Sprintf(format, args...)
	return model.NewError(model.ErrorInvalidRequest, path[1:]+": "+msg).
		WithDetail("errors", []FieldError{{Path: path, Message: msg}})
}
