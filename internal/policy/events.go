package policy

import "github.com/LexingtonStanley/lugarn-comms/internal/model"

// Event is one row for the store's events table whose data policy builds in
// full: policy.denied, budget.changed and rounds.changed. The store adds seq,
// at and team when it writes it.
type Event struct {
	Type model.EventType
	// Timeline is the events.timeline flag: policy.denied always; budget and
	// rounds changes unless their cause is used.
	Timeline bool
	// Member is events.member_id: the actor of a policy.denied, else nil.
	Member *string
	// Message is events.message_id: the refused message, else nil.
	Message *string
	// Data is PolicyDeniedData, BudgetChangedData or RoundsChangedData.
	Data any
}

// PolicyDeniedData is policy.denied's data.
type PolicyDeniedData struct {
	Action    model.PolicyAction `json:"action"`
	Actor     model.MemberRef    `json:"actor"`
	Target    *model.MemberRef   `json:"target"`
	MessageID *string            `json:"message_id"`
	Error     *model.Error       `json:"error"`
}

// BudgetChangedData is budget.changed's data.
type BudgetChangedData struct {
	Hops  model.Hops        `json:"hops"`
	Cause model.BudgetCause `json:"cause"`
	By    *model.MemberRef  `json:"by"`
}

// RoundsChangedData is rounds.changed's data.
type RoundsChangedData struct {
	Rounds model.Rounds      `json:"rounds"`
	Cause  model.BudgetCause `json:"cause"`
	By     *model.MemberRef  `json:"by"`
}

func policyDenied(action model.PolicyAction, actor model.MemberRef, target *model.MemberRef, messageID *string, err *model.Error) Event {
	id := actor.ID
	return Event{
		Type:     model.EventPolicyDenied,
		Timeline: true,
		Member:   &id,
		Message:  messageID,
		Data:     PolicyDeniedData{Action: action, Actor: actor, Target: target, MessageID: messageID, Error: err},
	}
}

func budgetChanged(h model.Hops, cause model.BudgetCause, by *model.MemberRef) Event {
	return Event{
		Type:     model.EventBudgetChanged,
		Timeline: cause != model.BudgetCauseUsed,
		Data:     BudgetChangedData{Hops: h, Cause: cause, By: by},
	}
}

func roundsChanged(r model.Rounds, cause model.BudgetCause, by *model.MemberRef) Event {
	return Event{
		Type:     model.EventRoundsChanged,
		Timeline: cause != model.BudgetCauseUsed,
		Data:     RoundsChangedData{Rounds: r, Cause: cause, By: by},
	}
}

func ref(m model.Member) model.MemberRef { return model.MemberRef{ID: m.ID, Name: m.Name} }

func ptr[T any](v T) *T { return &v }
