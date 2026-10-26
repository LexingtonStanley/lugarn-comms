package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func exampleFake(t *testing.T, fail *atomic.Bool) *service.Fake {
	t.Helper()
	f := &service.Fake{}
	var send wire.SendOutput
	example(t, "send.output.json", &send)
	f.ToolSendFunc = func(_ context.Context, caller service.Caller, req wire.SendInput) (wire.SendOutput, error) {
		if caller.MemberID != "member" {
			t.Errorf("lost caller: %v", caller)
		}
		if fail.Load() {
			return wire.SendOutput{}, model.NewError(model.ErrorDenied, "test failure")
		}
		return send, nil
	}
	var reply wire.ReplyOutput
	example(t, "reply.output.json", &reply)
	f.ToolReplyFunc = func(_ context.Context, caller service.Caller, req wire.ReplyInput) (wire.ReplyOutput, error) {
		if caller.MemberID != "member" {
			t.Errorf("lost caller: %v", caller)
		}
		if fail.Load() {
			return wire.ReplyOutput{}, model.NewError(model.ErrorDenied, "test failure")
		}
		return reply, nil
	}
	var inbox wire.InboxOutput
	example(t, "inbox.output.json", &inbox)
	f.ToolInboxFunc = func(_ context.Context, caller service.Caller, req wire.InboxInput) (wire.InboxOutput, error) {
		if caller.MemberID != "member" {
			t.Errorf("lost caller: %v", caller)
		}
		if fail.Load() {
			return wire.InboxOutput{}, model.NewError(model.ErrorDenied, "test failure")
		}
		return inbox, nil
	}
	var team wire.TeamOutput
	example(t, "team.output.json", &team)
	f.ToolTeamFunc = func(_ context.Context, caller service.Caller, req wire.TeamInput) (wire.TeamOutput, error) {
		if caller.MemberID != "member" {
			t.Errorf("lost caller: %v", caller)
		}
		if fail.Load() {
			return wire.TeamOutput{}, model.NewError(model.ErrorDenied, "test failure")
		}
		return team, nil
	}
	var spawn wire.SpawnOutput
	example(t, "spawn.output.json", &spawn)
	f.ToolSpawnFunc = func(_ context.Context, caller service.Caller, req wire.SpawnInput) (wire.SpawnOutput, error) {
		if caller.MemberID != "member" {
			t.Errorf("lost caller: %v", caller)
		}
		if fail.Load() {
			return wire.SpawnOutput{}, model.NewError(model.ErrorDenied, "test failure")
		}
		return spawn, nil
	}
	var stop wire.StopOutput
	example(t, "stop.output.json", &stop)
	f.ToolStopFunc = func(_ context.Context, caller service.Caller, req wire.StopInput) (wire.StopOutput, error) {
		if caller.MemberID != "member" {
			t.Errorf("lost caller: %v", caller)
		}
		if fail.Load() {
			return wire.StopOutput{}, model.NewError(model.ErrorDenied, "test failure")
		}
		return stop, nil
	}
	return f
}
func example(t *testing.T, name string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "schema", "examples", "mcp", name))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}
