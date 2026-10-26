package agentops

import (
	"context"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

// CallTool dispatches an already validated typed tool input.
func CallTool(ctx context.Context, svc service.Tools, caller service.Caller, input any) (any, error) {
	switch v := input.(type) {
	case *wire.SendInput:
		return svc.ToolSend(ctx, caller, *v)
	case *wire.ReplyInput:
		return svc.ToolReply(ctx, caller, *v)
	case *wire.InboxInput:
		return svc.ToolInbox(ctx, caller, *v)
	case *wire.TeamInput:
		return svc.ToolTeam(ctx, caller, *v)
	case *wire.SpawnInput:
		return svc.ToolSpawn(ctx, caller, *v)
	case *wire.StopInput:
		return svc.ToolStop(ctx, caller, *v)
	}
	return nil, model.NewError(model.ErrorInvalidRequest, "unknown tool input")
}
