// Package mcpserver serves the six lugarn tools over the official MCP SDK.
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/LexingtonStanley/lugarn-comms/internal/agentops"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/version"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// New constructs a server named lugarn. Each invocation re-reads identity's file.
func New(identity agentops.Identity, svc service.Tools, auth service.Authenticator) (*mcp.Server, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "lugarn", Version: version.Version}, nil)
	for _, name := range (wire.ToolName("")).Values() {
		name := string(name)
		input, err := wire.ToolSchema(name, false)
		if err != nil {
			return nil, err
		}
		output, err := wire.ToolSchema(name, true)
		if err != nil {
			return nil, err
		}
		server.AddTool(&mcp.Tool{Name: name, Description: descriptions[name], InputSchema: input, OutputSchema: output}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			arguments := req.Params.Arguments
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			input, inputErr := wire.DecodeToolInput(name, arguments)
			caller, authErr := identity.Authenticate(ctx, auth, model.FaceMCP)
			if inputErr != nil {
				return failure(model.AsError(inputErr)), nil
			}
			if authErr != nil {
				return failure(model.AsError(authErr)), nil
			}
			output, err := agentops.CallTool(ctx, svc, caller, input)
			if err != nil {
				return failure(model.AsError(err)), nil
			}
			b, err := json.Marshal(output)
			if err != nil {
				return failure(model.AsError(err)), nil
			}
			return &mcp.CallToolResult{StructuredContent: output, Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil
		})
	}
	return server, nil
}
func failure(err *model.Error) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, StructuredContent: wire.ToolError{Error: *err}, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}
}

var descriptions = map[string]string{"send": "Send a message to a team member or human.", "reply": "Reply to a received message.", "inbox": "Read delivered or uncertain incoming messages and outgoing history.", "team": "Read the current roster, capabilities and spawn limits.", "spawn": "Spawn a worker when permitted by the team.", "stop": "Stop a worker you spawned."}
