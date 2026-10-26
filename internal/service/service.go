// Package service defines the broker contracts shared by its front doors.
package service

import (
	"context"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// App is the typed interface used by app ops. Implementations return *model.Error errors.
type App interface {
	Hello(context.Context, wire.HelloRequest) (wire.HelloResult, error)
	ApplyTeam(context.Context, wire.ApplyTeamRequest) (wire.ApplyTeamResult, error)
	GetTeam(context.Context, wire.GetTeamRequest) (wire.GetTeamResult, error)
	StartTeam(context.Context, wire.StartTeamRequest) (wire.StartTeamResult, error)
	StopTeam(context.Context, wire.StopTeamRequest) (wire.StopTeamResult, error)
	Status(context.Context, wire.StatusRequest) (wire.StatusResult, error)
	Members(context.Context, wire.MembersRequest) (wire.MembersResult, error)
	Timeline(context.Context, wire.TimelineRequest) (wire.TimelineResult, error)
	Send(context.Context, wire.SendRequest) (wire.SendResult, error)
	ContinueBudget(context.Context, wire.ContinueBudgetRequest) (wire.ContinueBudgetResult, error)
	ResolveDelivery(context.Context, wire.ResolveDeliveryRequest) (wire.ResolveDeliveryResult, error)
	Hold(context.Context, wire.HoldRequest) (wire.HoldResult, error)
	OpenTerminal(context.Context, wire.OpenTerminalRequest) (wire.RequestResult, error)
	ReturnHeadless(context.Context, wire.ReturnHeadlessRequest) (wire.RequestResult, error)
	StopMember(context.Context, wire.StopMemberRequest) (wire.RequestResult, error)
	Worktrees(context.Context, wire.WorktreesRequest) (wire.WorktreesResult, error)
	RemoveWorktrees(context.Context, wire.RemoveWorktreesRequest) (wire.RemoveWorktreesResult, error)
	StartHerdr(context.Context, wire.StartHerdrRequest) (wire.StartHerdrResult, error)
	Watch(context.Context, wire.WatchRequest, func(wire.WatchLine) error) error
}

// Tools exposes authenticated agent operations.
type Tools interface {
	ToolSend(context.Context, Caller, wire.SendInput) (wire.SendOutput, error)
	ToolReply(context.Context, Caller, wire.ReplyInput) (wire.ReplyOutput, error)
	ToolInbox(context.Context, Caller, wire.InboxInput) (wire.InboxOutput, error)
	ToolTeam(context.Context, Caller, wire.TeamInput) (wire.TeamOutput, error)
	ToolSpawn(context.Context, Caller, wire.SpawnInput) (wire.SpawnOutput, error)
	ToolStop(context.Context, Caller, wire.StopInput) (wire.StopOutput, error)
}

// Caller identifies an authenticated launch; only the authenticator constructs it.
type Caller struct {
	MemberID string
	TeamID   string
	LaunchID string
}

// Token carries the exact encoded token bytes (without the file line ending) and
// their SHA-256 hex digest, computed afresh for each call.
type Token struct {
	Bytes []byte
	Hash  string
}

// Authenticator owns token lookup, revocation checks and auth.rejected events.
// A nil token denotes an absent, empty or unreadable token file.
type Authenticator interface {
	Authenticate(context.Context, string, *Token, model.Face) (Caller, error)
}
