package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/agentops"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func session(t *testing.T, identity agentops.Identity, fake *service.Fake) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	srv, err := New(identity, fake, fake)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
func TestToolsThroughSDK(t *testing.T) {
	var fail atomic.Bool
	fake := exampleFake(t, &fail)
	var calls atomic.Int64
	fake.AuthenticateFunc = func(_ context.Context, member string, token *service.Token, face model.Face) (service.Caller, *model.Error) {
		calls.Add(1)
		if face != model.FaceMCP {
			t.Error(face)
		}
		return service.Caller{MemberID: "member"}, nil
	}
	cs := session(t, agentops.Identity{}, fake)
	if cs.InitializeResult().ServerInfo.Name != "lugarn" {
		t.Fatal(cs.InitializeResult())
	}
	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 6 {
		t.Fatal(listed)
	}
	for _, tool := range listed.Tools {
		wantInput, err := wire.ToolSchema(tool.Name, false)
		if err != nil {
			t.Fatal(err)
		}
		wantOutput, err := wire.ToolSchema(tool.Name, true)
		if err != nil {
			t.Fatal(err)
		}
		assertJSONEqual(t, tool.InputSchema, wantInput)
		assertJSONEqual(t, tool.OutputSchema, wantOutput)
		t.Run(tool.Name, func(t *testing.T) {
			var args any
			example(t, tool.Name+".input.json", &args)
			fail.Store(false)
			got, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool.Name, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if got.IsError {
				t.Fatal(got)
			}
			expected := wire.NewToolOutput(tool.Name)
			example(t, tool.Name+".output.json", expected)
			assertJSONEqual(t, got.StructuredContent, expected)
			// Compile the actual advertised schemas independently and validate
			// both fixture objects. This also proves all refs are self-contained.
			validateSchema(t, tool.InputSchema, args)
			validateSchema(t, tool.OutputSchema, got.StructuredContent)
			b, err := json.Marshal(expected)
			if err != nil {
				t.Fatal(err)
			}
			checkText(t, got, string(b))
			fail.Store(true)
			got, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool.Name, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if !got.IsError {
				t.Fatal("expected tool failure")
			}
			assertJSONEqual(t, got.StructuredContent, wire.ToolError{Error: *model.NewError(model.ErrorDenied, "test failure")})
			checkText(t, got, "denied: test failure")
		})
	}
	if calls.Load() != 12 {
		t.Fatalf("auth calls %d", calls.Load())
	}
}
func TestBadArgumentsAreToolErrors(t *testing.T) {
	fake := &service.Fake{AuthenticateFunc: func(context.Context, string, *service.Token, model.Face) (service.Caller, *model.Error) {
		return service.Caller{}, nil
	}}
	cs := session(t, agentops.Identity{}, fake)
	for _, tt := range []struct {
		name string
		args any
	}{
		{"send", map[string]any{}}, {"send", map[string]any{"to": "all", "text": "hi"}}, {"send", map[string]any{"to": "human", "text": strings.Repeat("界", 8001)}},
		{"reply", map[string]any{"id": "bad", "text": "hello"}}, {"team", map[string]any{"extra": 1}}, {"inbox", map[string]any{"limit": 101}}, {"spawn", map[string]any{"name": "human", "harness": "claude", "task": "hi"}}, {"stop", map[string]any{"member": "all"}},
	} {
		got, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tt.name, Arguments: tt.args})
		if err != nil {
			t.Fatalf("RPC error for bad arguments: %v", err)
		}
		if !got.IsError {
			t.Fatal(got)
		}
		var object struct {
			Error model.Error `json:"error"`
		}
		b, _ := json.Marshal(got.StructuredContent)
		if err = json.Unmarshal(b, &object); err != nil {
			t.Fatal(err)
		}
		if object.Error.Code != model.ErrorInvalidRequest {
			t.Fatal(string(b))
		}
		checkText(t, got, object.Error.Error())
	}
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "unknown", Arguments: map[string]any{}}); err == nil {
		t.Fatal("unknown tool should be an RPC error")
	}
}
func TestTokenRereadAndAuthFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launch.token")
	token1, _ := model.NewToken()
	token2, _ := model.NewToken()
	var mu sync.Mutex
	var hashes []string
	fake := &service.Fake{AuthenticateFunc: func(_ context.Context, member string, token *service.Token, face model.Face) (service.Caller, *model.Error) {
		if member != "m" || face != model.FaceMCP {
			t.Errorf("identity %s %s", member, face)
		}
		if token == nil {
			return service.Caller{}, model.NewError(model.ErrorUnauthenticated, "no token")
		}
		if token.Hash != model.HashToken(string(token.Bytes)) {
			t.Error("incorrect hash")
		}
		mu.Lock()
		hashes = append(hashes, token.Hash)
		mu.Unlock()
		return service.Caller{}, nil
	}, ToolTeamFunc: func(context.Context, service.Caller, wire.TeamInput) (wire.TeamOutput, error) {
		return wire.TeamOutput{}, nil
	}}
	cs := session(t, agentops.Identity{Member: "m", TokenFile: path}, fake)
	for _, token := range []string{token1 + "\n", token2 + "\r\n"} {
		if err := os.WriteFile(path, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "team", Arguments: map[string]any{}})
		if err != nil || got.IsError {
			t.Fatalf("%v %v", got, err)
		}
	}
	mu.Lock()
	actual := append([]string(nil), hashes...)
	mu.Unlock()
	if !reflect.DeepEqual(actual, []string{model.HashToken(token1), model.HashToken(token2)}) {
		t.Fatal(actual)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "team", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsError {
		t.Fatal(got)
	}
	checkText(t, got, "unauthenticated: no token")
	assertJSONEqual(t, got.StructuredContent, wire.ToolError{Error: *model.NewError(model.ErrorUnauthenticated, "no token")})
}
func assertJSONEqual(t *testing.T, a, b any) {
	t.Helper()
	ba, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var va, vb any
	_ = json.Unmarshal(ba, &va)
	_ = json.Unmarshal(bb, &vb)
	if !reflect.DeepEqual(va, vb) {
		t.Fatalf("JSON differs\ngot %s\nwant %s", ba, bb)
	}
}
func checkText(t *testing.T, result *mcp.CallToolResult, want string) {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatal(result.Content)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || text.Text != want {
		t.Fatalf("content = %#v want %s", result.Content, want)
	}
}

func TestOmittedArgumentsForNoRequiredInputs(t *testing.T) {
	fake := &service.Fake{
		AuthenticateFunc: func(context.Context, string, *service.Token, model.Face) (service.Caller, *model.Error) {
			return service.Caller{}, nil
		},
		ToolTeamFunc: func(context.Context, service.Caller, wire.TeamInput) (wire.TeamOutput, error) {
			return wire.TeamOutput{}, nil
		},
		ToolInboxFunc: func(context.Context, service.Caller, wire.InboxInput) (wire.InboxOutput, error) {
			return wire.InboxOutput{}, nil
		},
	}
	cs := session(t, agentops.Identity{}, fake)
	for _, name := range []string{"team", "inbox"} {
		got, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name})
		if err != nil || got.IsError {
			t.Fatalf("%s: %v %v", name, got, err)
		}
	}
}

func validateSchema(t *testing.T, sch, value any) {
	t.Helper()
	compiler := jsonschema.NewCompiler()
	b, err := json.Marshal(sch)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err = json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if err = compiler.AddResource("https://example.invalid/tool.json", doc); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile("https://example.invalid/tool.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object any
	if err = json.Unmarshal(b, &object); err != nil {
		t.Fatal(err)
	}
	if err = compiled.Validate(object); err != nil {
		t.Fatal(err)
	}
}
