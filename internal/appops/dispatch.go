package appops

import (
	"context"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func dispatch(ctx context.Context, req any, svc service.App) (any, error) {
	switch v := req.(type) {
	case *wire.HelloRequest:
		return svc.Hello(ctx, *v)
	case *wire.ApplyTeamRequest:
		return svc.ApplyTeam(ctx, *v)
	case *wire.GetTeamRequest:
		return svc.GetTeam(ctx, *v)
	case *wire.StartTeamRequest:
		return svc.StartTeam(ctx, *v)
	case *wire.StopTeamRequest:
		return svc.StopTeam(ctx, *v)
	case *wire.StatusRequest:
		return svc.Status(ctx, *v)
	case *wire.MembersRequest:
		return svc.Members(ctx, *v)
	case *wire.TimelineRequest:
		return svc.Timeline(ctx, *v)
	case *wire.SendRequest:
		return svc.Send(ctx, *v)
	case *wire.ContinueBudgetRequest:
		return svc.ContinueBudget(ctx, *v)
	case *wire.ResolveDeliveryRequest:
		return svc.ResolveDelivery(ctx, *v)
	case *wire.HoldRequest:
		return svc.Hold(ctx, *v)
	case *wire.OpenTerminalRequest:
		return svc.OpenTerminal(ctx, *v)
	case *wire.ReturnHeadlessRequest:
		return svc.ReturnHeadless(ctx, *v)
	case *wire.StopMemberRequest:
		return svc.StopMember(ctx, *v)
	case *wire.WorktreesRequest:
		return svc.Worktrees(ctx, *v)
	case *wire.RemoveWorktreesRequest:
		return svc.RemoveWorktrees(ctx, *v)
	case *wire.StartHerdrRequest:
		return svc.StartHerdr(ctx, *v)
	}
	return nil, model.NewError(model.ErrorInvalidRequest, "unknown app op")
}
