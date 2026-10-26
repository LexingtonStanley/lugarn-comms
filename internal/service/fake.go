package service

import (
	"context"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// Fake is an in-memory service with typed callbacks. Unset callbacks return the C1d stub error.
// Configure callbacks before use; callbacks must synchronize any state they mutate.
type Fake struct {
	HelloFunc           func(context.Context, wire.HelloRequest) (wire.HelloResult, error)
	ApplyTeamFunc       func(context.Context, wire.ApplyTeamRequest) (wire.ApplyTeamResult, error)
	GetTeamFunc         func(context.Context, wire.GetTeamRequest) (wire.GetTeamResult, error)
	StartTeamFunc       func(context.Context, wire.StartTeamRequest) (wire.StartTeamResult, error)
	StopTeamFunc        func(context.Context, wire.StopTeamRequest) (wire.StopTeamResult, error)
	StatusFunc          func(context.Context, wire.StatusRequest) (wire.StatusResult, error)
	MembersFunc         func(context.Context, wire.MembersRequest) (wire.MembersResult, error)
	TimelineFunc        func(context.Context, wire.TimelineRequest) (wire.TimelineResult, error)
	SendFunc            func(context.Context, wire.SendRequest) (wire.SendResult, error)
	ContinueBudgetFunc  func(context.Context, wire.ContinueBudgetRequest) (wire.ContinueBudgetResult, error)
	ResolveDeliveryFunc func(context.Context, wire.ResolveDeliveryRequest) (wire.ResolveDeliveryResult, error)
	HoldFunc            func(context.Context, wire.HoldRequest) (wire.HoldResult, error)
	OpenTerminalFunc    func(context.Context, wire.OpenTerminalRequest) (wire.RequestResult, error)
	ReturnHeadlessFunc  func(context.Context, wire.ReturnHeadlessRequest) (wire.RequestResult, error)
	StopMemberFunc      func(context.Context, wire.StopMemberRequest) (wire.RequestResult, error)
	WorktreesFunc       func(context.Context, wire.WorktreesRequest) (wire.WorktreesResult, error)
	RemoveWorktreesFunc func(context.Context, wire.RemoveWorktreesRequest) (wire.RemoveWorktreesResult, error)
	StartHerdrFunc      func(context.Context, wire.StartHerdrRequest) (wire.StartHerdrResult, error)
	WatchFunc           func(context.Context, wire.WatchRequest, func(wire.WatchLine) error) error
	AuthenticateFunc    func(context.Context, string, *Token, model.Face) (Caller, *model.Error)
	ToolSendFunc        func(context.Context, Caller, wire.SendInput) (wire.SendOutput, error)
	ToolReplyFunc       func(context.Context, Caller, wire.ReplyInput) (wire.ReplyOutput, error)
	ToolInboxFunc       func(context.Context, Caller, wire.InboxInput) (wire.InboxOutput, error)
	ToolTeamFunc        func(context.Context, Caller, wire.TeamInput) (wire.TeamOutput, error)
	ToolSpawnFunc       func(context.Context, Caller, wire.SpawnInput) (wire.SpawnOutput, error)
	ToolStopFunc        func(context.Context, Caller, wire.StopInput) (wire.StopOutput, error)
}

// NotImplemented is the placeholder used until C1d supplies the broker service.
func NotImplemented() *model.Error {
	return model.NewError(model.ErrorInternal, "not implemented yet (C1d)")
}
func (f *Fake) Hello(ctx context.Context, req wire.HelloRequest) (wire.HelloResult, error) {
	if f.HelloFunc != nil {
		return f.HelloFunc(ctx, req)
	}
	return wire.HelloResult{}, NotImplemented()
}
func (f *Fake) ApplyTeam(ctx context.Context, req wire.ApplyTeamRequest) (wire.ApplyTeamResult, error) {
	if f.ApplyTeamFunc != nil {
		return f.ApplyTeamFunc(ctx, req)
	}
	return wire.ApplyTeamResult{}, NotImplemented()
}
func (f *Fake) GetTeam(ctx context.Context, req wire.GetTeamRequest) (wire.GetTeamResult, error) {
	if f.GetTeamFunc != nil {
		return f.GetTeamFunc(ctx, req)
	}
	return wire.GetTeamResult{}, NotImplemented()
}
func (f *Fake) StartTeam(ctx context.Context, req wire.StartTeamRequest) (wire.StartTeamResult, error) {
	if f.StartTeamFunc != nil {
		return f.StartTeamFunc(ctx, req)
	}
	return wire.StartTeamResult{}, NotImplemented()
}
func (f *Fake) StopTeam(ctx context.Context, req wire.StopTeamRequest) (wire.StopTeamResult, error) {
	if f.StopTeamFunc != nil {
		return f.StopTeamFunc(ctx, req)
	}
	return wire.StopTeamResult{}, NotImplemented()
}
func (f *Fake) Status(ctx context.Context, req wire.StatusRequest) (wire.StatusResult, error) {
	if f.StatusFunc != nil {
		return f.StatusFunc(ctx, req)
	}
	return wire.StatusResult{}, NotImplemented()
}
func (f *Fake) Members(ctx context.Context, req wire.MembersRequest) (wire.MembersResult, error) {
	if f.MembersFunc != nil {
		return f.MembersFunc(ctx, req)
	}
	return wire.MembersResult{}, NotImplemented()
}
func (f *Fake) Timeline(ctx context.Context, req wire.TimelineRequest) (wire.TimelineResult, error) {
	if f.TimelineFunc != nil {
		return f.TimelineFunc(ctx, req)
	}
	return wire.TimelineResult{}, NotImplemented()
}
func (f *Fake) Send(ctx context.Context, req wire.SendRequest) (wire.SendResult, error) {
	if f.SendFunc != nil {
		return f.SendFunc(ctx, req)
	}
	return wire.SendResult{}, NotImplemented()
}
func (f *Fake) ContinueBudget(ctx context.Context, req wire.ContinueBudgetRequest) (wire.ContinueBudgetResult, error) {
	if f.ContinueBudgetFunc != nil {
		return f.ContinueBudgetFunc(ctx, req)
	}
	return wire.ContinueBudgetResult{}, NotImplemented()
}
func (f *Fake) ResolveDelivery(ctx context.Context, req wire.ResolveDeliveryRequest) (wire.ResolveDeliveryResult, error) {
	if f.ResolveDeliveryFunc != nil {
		return f.ResolveDeliveryFunc(ctx, req)
	}
	return wire.ResolveDeliveryResult{}, NotImplemented()
}
func (f *Fake) Hold(ctx context.Context, req wire.HoldRequest) (wire.HoldResult, error) {
	if f.HoldFunc != nil {
		return f.HoldFunc(ctx, req)
	}
	return wire.HoldResult{}, NotImplemented()
}
func (f *Fake) OpenTerminal(ctx context.Context, req wire.OpenTerminalRequest) (wire.RequestResult, error) {
	if f.OpenTerminalFunc != nil {
		return f.OpenTerminalFunc(ctx, req)
	}
	return wire.RequestResult{}, NotImplemented()
}
func (f *Fake) ReturnHeadless(ctx context.Context, req wire.ReturnHeadlessRequest) (wire.RequestResult, error) {
	if f.ReturnHeadlessFunc != nil {
		return f.ReturnHeadlessFunc(ctx, req)
	}
	return wire.RequestResult{}, NotImplemented()
}
func (f *Fake) StopMember(ctx context.Context, req wire.StopMemberRequest) (wire.RequestResult, error) {
	if f.StopMemberFunc != nil {
		return f.StopMemberFunc(ctx, req)
	}
	return wire.RequestResult{}, NotImplemented()
}
func (f *Fake) Worktrees(ctx context.Context, req wire.WorktreesRequest) (wire.WorktreesResult, error) {
	if f.WorktreesFunc != nil {
		return f.WorktreesFunc(ctx, req)
	}
	return wire.WorktreesResult{}, NotImplemented()
}
func (f *Fake) RemoveWorktrees(ctx context.Context, req wire.RemoveWorktreesRequest) (wire.RemoveWorktreesResult, error) {
	if f.RemoveWorktreesFunc != nil {
		return f.RemoveWorktreesFunc(ctx, req)
	}
	return wire.RemoveWorktreesResult{}, NotImplemented()
}
func (f *Fake) StartHerdr(ctx context.Context, req wire.StartHerdrRequest) (wire.StartHerdrResult, error) {
	if f.StartHerdrFunc != nil {
		return f.StartHerdrFunc(ctx, req)
	}
	return wire.StartHerdrResult{}, NotImplemented()
}
func (f *Fake) ToolSend(ctx context.Context, caller Caller, req wire.SendInput) (wire.SendOutput, error) {
	if f.ToolSendFunc != nil {
		return f.ToolSendFunc(ctx, caller, req)
	}
	return wire.SendOutput{}, NotImplemented()
}
func (f *Fake) ToolReply(ctx context.Context, caller Caller, req wire.ReplyInput) (wire.ReplyOutput, error) {
	if f.ToolReplyFunc != nil {
		return f.ToolReplyFunc(ctx, caller, req)
	}
	return wire.ReplyOutput{}, NotImplemented()
}
func (f *Fake) ToolInbox(ctx context.Context, caller Caller, req wire.InboxInput) (wire.InboxOutput, error) {
	if f.ToolInboxFunc != nil {
		return f.ToolInboxFunc(ctx, caller, req)
	}
	return wire.InboxOutput{}, NotImplemented()
}
func (f *Fake) ToolTeam(ctx context.Context, caller Caller, req wire.TeamInput) (wire.TeamOutput, error) {
	if f.ToolTeamFunc != nil {
		return f.ToolTeamFunc(ctx, caller, req)
	}
	return wire.TeamOutput{}, NotImplemented()
}
func (f *Fake) ToolSpawn(ctx context.Context, caller Caller, req wire.SpawnInput) (wire.SpawnOutput, error) {
	if f.ToolSpawnFunc != nil {
		return f.ToolSpawnFunc(ctx, caller, req)
	}
	return wire.SpawnOutput{}, NotImplemented()
}
func (f *Fake) ToolStop(ctx context.Context, caller Caller, req wire.StopInput) (wire.StopOutput, error) {
	if f.ToolStopFunc != nil {
		return f.ToolStopFunc(ctx, caller, req)
	}
	return wire.StopOutput{}, NotImplemented()
}
func (f *Fake) Watch(ctx context.Context, req wire.WatchRequest, emit func(wire.WatchLine) error) error {
	if f.WatchFunc != nil {
		return f.WatchFunc(ctx, req, emit)
	}
	return NotImplemented()
}
func (f *Fake) Authenticate(ctx context.Context, member string, token *Token, face model.Face) (Caller, *model.Error) {
	if f.AuthenticateFunc != nil {
		return f.AuthenticateFunc(ctx, member, token, face)
	}
	return Caller{}, NotImplemented()
}

var _ App = (*Fake)(nil)
var _ Tools = (*Fake)(nil)
var _ Authenticator = (*Fake)(nil)
