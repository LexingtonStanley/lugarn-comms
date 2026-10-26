package main

import (
	"context"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

type errorService struct{ err *model.Error }

func (s errorService) Hello(context.Context, wire.HelloRequest) (wire.HelloResult, error) {
	return wire.HelloResult{}, s.err
}
func (s errorService) ApplyTeam(context.Context, wire.ApplyTeamRequest) (wire.ApplyTeamResult, error) {
	return wire.ApplyTeamResult{}, s.err
}
func (s errorService) GetTeam(context.Context, wire.GetTeamRequest) (wire.GetTeamResult, error) {
	return wire.GetTeamResult{}, s.err
}
func (s errorService) StartTeam(context.Context, wire.StartTeamRequest) (wire.StartTeamResult, error) {
	return wire.StartTeamResult{}, s.err
}
func (s errorService) StopTeam(context.Context, wire.StopTeamRequest) (wire.StopTeamResult, error) {
	return wire.StopTeamResult{}, s.err
}
func (s errorService) Status(context.Context, wire.StatusRequest) (wire.StatusResult, error) {
	return wire.StatusResult{}, s.err
}
func (s errorService) Members(context.Context, wire.MembersRequest) (wire.MembersResult, error) {
	return wire.MembersResult{}, s.err
}
func (s errorService) Timeline(context.Context, wire.TimelineRequest) (wire.TimelineResult, error) {
	return wire.TimelineResult{}, s.err
}
func (s errorService) Send(context.Context, wire.SendRequest) (wire.SendResult, error) {
	return wire.SendResult{}, s.err
}
func (s errorService) ContinueBudget(context.Context, wire.ContinueBudgetRequest) (wire.ContinueBudgetResult, error) {
	return wire.ContinueBudgetResult{}, s.err
}
func (s errorService) ResolveDelivery(context.Context, wire.ResolveDeliveryRequest) (wire.ResolveDeliveryResult, error) {
	return wire.ResolveDeliveryResult{}, s.err
}
func (s errorService) Hold(context.Context, wire.HoldRequest) (wire.HoldResult, error) {
	return wire.HoldResult{}, s.err
}
func (s errorService) OpenTerminal(context.Context, wire.OpenTerminalRequest) (wire.RequestResult, error) {
	return wire.RequestResult{}, s.err
}
func (s errorService) ReturnHeadless(context.Context, wire.ReturnHeadlessRequest) (wire.RequestResult, error) {
	return wire.RequestResult{}, s.err
}
func (s errorService) StopMember(context.Context, wire.StopMemberRequest) (wire.RequestResult, error) {
	return wire.RequestResult{}, s.err
}
func (s errorService) Worktrees(context.Context, wire.WorktreesRequest) (wire.WorktreesResult, error) {
	return wire.WorktreesResult{}, s.err
}
func (s errorService) RemoveWorktrees(context.Context, wire.RemoveWorktreesRequest) (wire.RemoveWorktreesResult, error) {
	return wire.RemoveWorktreesResult{}, s.err
}
func (s errorService) StartHerdr(context.Context, wire.StartHerdrRequest) (wire.StartHerdrResult, error) {
	return wire.StartHerdrResult{}, s.err
}
func (s errorService) Watch(context.Context, wire.WatchRequest, func(wire.WatchLine) error) error {
	return s.err
}
func (s errorService) ToolSend(context.Context, service.Caller, wire.SendInput) (wire.SendOutput, error) {
	return wire.SendOutput{}, s.err
}
func (s errorService) ToolReply(context.Context, service.Caller, wire.ReplyInput) (wire.ReplyOutput, error) {
	return wire.ReplyOutput{}, s.err
}
func (s errorService) ToolInbox(context.Context, service.Caller, wire.InboxInput) (wire.InboxOutput, error) {
	return wire.InboxOutput{}, s.err
}
func (s errorService) ToolTeam(context.Context, service.Caller, wire.TeamInput) (wire.TeamOutput, error) {
	return wire.TeamOutput{}, s.err
}
func (s errorService) ToolSpawn(context.Context, service.Caller, wire.SpawnInput) (wire.SpawnOutput, error) {
	return wire.SpawnOutput{}, s.err
}
func (s errorService) ToolStop(context.Context, service.Caller, wire.StopInput) (wire.StopOutput, error) {
	return wire.StopOutput{}, s.err
}
func (s errorService) Authenticate(context.Context, string, *service.Token, model.Face) (service.Caller, error) {
	return service.Caller{}, s.err
}
