package policy

import (
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

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
	// Data is wire.PolicyDeniedEventData, wire.BudgetChangedEventData or
	// wire.RoundsChangedEventData.
	Data wire.EventData
}

func policyDenied(action model.PolicyAction, actor model.MemberRef, target *model.MemberRef, messageID *string, err *model.Error) Event {
	id := actor.ID
	return Event{
		Type:     model.EventPolicyDenied,
		Timeline: true,
		Member:   &id,
		Message:  messageID,
		Data:     wire.PolicyDeniedEventData{Action: action, Actor: actor, Target: target, MessageID: messageID, Error: *err},
	}
}

func budgetChanged(h model.Hops, cause model.BudgetCause, by *model.MemberRef) Event {
	return Event{
		Type:     model.EventBudgetChanged,
		Timeline: cause != model.BudgetCauseUsed,
		Data:     wire.BudgetChangedEventData{Hops: h, Cause: cause, By: by},
	}
}

func roundsChanged(r model.Rounds, cause model.BudgetCause, by *model.MemberRef) Event {
	return Event{
		Type:     model.EventRoundsChanged,
		Timeline: cause != model.BudgetCauseUsed,
		Data:     wire.RoundsChangedEventData{Rounds: r, Cause: cause, By: by},
	}
}

func ref(m model.Member) model.MemberRef { return model.MemberRef{ID: m.ID, Name: m.Name} }

func ptr[T any](v T) *T { return &v }
