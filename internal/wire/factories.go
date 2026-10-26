package wire

// NewRequest returns a pointer to the typed request for an app op, or nil.
func NewRequest(name string) any {
	switch name {
	case "hello":
		return new(HelloRequest)
	case "apply-team":
		return new(ApplyTeamRequest)
	case "get-team":
		return new(GetTeamRequest)
	case "start-team":
		return new(StartTeamRequest)
	case "stop-team":
		return new(StopTeamRequest)
	case "status":
		return new(StatusRequest)
	case "members":
		return new(MembersRequest)
	case "timeline":
		return new(TimelineRequest)
	case "send":
		return new(SendRequest)
	case "continue-budget":
		return new(ContinueBudgetRequest)
	case "resolve-delivery":
		return new(ResolveDeliveryRequest)
	case "hold":
		return new(HoldRequest)
	case "open-terminal":
		return new(OpenTerminalRequest)
	case "return-headless":
		return new(ReturnHeadlessRequest)
	case "stop-member":
		return new(StopMemberRequest)
	case "worktrees":
		return new(WorktreesRequest)
	case "remove-worktrees":
		return new(RemoveWorktreesRequest)
	case "start-herdr":
		return new(StartHerdrRequest)
	case "watch":
		return new(WatchRequest)
	}
	return nil
}

// NewResult returns a pointer to the typed result for an app op, or nil.
func NewResult(name string) any {
	switch name {
	case "hello":
		return new(HelloResult)
	case "apply-team":
		return new(ApplyTeamResult)
	case "get-team":
		return new(GetTeamResult)
	case "start-team":
		return new(StartTeamResult)
	case "stop-team":
		return new(StopTeamResult)
	case "status":
		return new(StatusResult)
	case "members":
		return new(MembersResult)
	case "timeline":
		return new(TimelineResult)
	case "send":
		return new(SendResult)
	case "continue-budget":
		return new(ContinueBudgetResult)
	case "resolve-delivery":
		return new(ResolveDeliveryResult)
	case "hold":
		return new(HoldResult)
	case "open-terminal":
		return new(RequestResult)
	case "return-headless":
		return new(RequestResult)
	case "stop-member":
		return new(RequestResult)
	case "worktrees":
		return new(WorktreesResult)
	case "remove-worktrees":
		return new(RemoveWorktreesResult)
	case "start-herdr":
		return new(StartHerdrResult)
	}
	return nil
}

// NewToolInput returns a pointer to a tool input object, or nil.
func NewToolInput(name string) any {
	switch name {
	case "send":
		return new(SendInput)
	case "reply":
		return new(ReplyInput)
	case "inbox":
		return new(InboxInput)
	case "team":
		return new(TeamInput)
	case "spawn":
		return new(SpawnInput)
	case "stop":
		return new(StopInput)
	}
	return nil
}

// NewToolOutput returns a pointer to a tool output object, or nil.
func NewToolOutput(name string) any {
	switch name {
	case "send":
		return new(SendOutput)
	case "reply":
		return new(ReplyOutput)
	case "inbox":
		return new(InboxOutput)
	case "team":
		return new(TeamOutput)
	case "spawn":
		return new(SpawnOutput)
	case "stop":
		return new(StopOutput)
	}
	return nil
}
func newEventData(name string) EventData {
	switch name {
	case "team.applied":
		return new(TeamAppliedEventData)
	case "team.state_changed":
		return new(TeamStateChangedEventData)
	case "runner.state_changed":
		return new(RunnerStateChangedEventData)
	case "member.added":
		return new(MemberAddedEventData)
	case "member.state_changed":
		return new(MemberStateChangedEventData)
	case "member.activity_changed":
		return new(MemberActivityChangedEventData)
	case "member.session_added":
		return new(MemberSessionAddedEventData)
	case "member.removed":
		return new(MemberRemovedEventData)
	case "lease.changed":
		return new(LeaseChangedEventData)
	case "message.created":
		return new(MessageCreatedEventData)
	case "delivery.changed":
		return new(DeliveryChangedEventData)
	case "turn.started":
		return new(TurnStartedEventData)
	case "turn.finished":
		return new(TurnFinishedEventData)
	case "policy.denied":
		return new(PolicyDeniedEventData)
	case "budget.changed":
		return new(BudgetChangedEventData)
	case "rounds.changed":
		return new(RoundsChangedEventData)
	case "request.changed":
		return new(RequestChangedEventData)
	case "auth.rejected":
		return new(AuthRejectedEventData)
	case "worktree.removed":
		return new(WorktreeRemovedEventData)
	}
	return nil
}
