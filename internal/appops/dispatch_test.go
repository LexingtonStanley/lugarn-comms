package appops

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"os"
	"path/filepath"
	"testing"
)

func TestEveryOpDispatch(t *testing.T) {
	fake := &service.Fake{}
	called := ""
	fake.HelloFunc = func(context.Context, wire.HelloRequest) (wire.HelloResult, error) {
		called = "hello"
		return wire.HelloResult{}, nil
	}
	fake.ApplyTeamFunc = func(context.Context, wire.ApplyTeamRequest) (wire.ApplyTeamResult, error) {
		called = "apply-team"
		return wire.ApplyTeamResult{}, nil
	}
	fake.GetTeamFunc = func(context.Context, wire.GetTeamRequest) (wire.GetTeamResult, error) {
		called = "get-team"
		return wire.GetTeamResult{}, nil
	}
	fake.StartTeamFunc = func(context.Context, wire.StartTeamRequest) (wire.StartTeamResult, error) {
		called = "start-team"
		return wire.StartTeamResult{}, nil
	}
	fake.StopTeamFunc = func(context.Context, wire.StopTeamRequest) (wire.StopTeamResult, error) {
		called = "stop-team"
		return wire.StopTeamResult{}, nil
	}
	fake.StatusFunc = func(context.Context, wire.StatusRequest) (wire.StatusResult, error) {
		called = "status"
		return wire.StatusResult{}, nil
	}
	fake.MembersFunc = func(context.Context, wire.MembersRequest) (wire.MembersResult, error) {
		called = "members"
		return wire.MembersResult{}, nil
	}
	fake.TimelineFunc = func(context.Context, wire.TimelineRequest) (wire.TimelineResult, error) {
		called = "timeline"
		return wire.TimelineResult{}, nil
	}
	fake.SendFunc = func(context.Context, wire.SendRequest) (wire.SendResult, error) {
		called = "send"
		return wire.SendResult{}, nil
	}
	fake.ContinueBudgetFunc = func(context.Context, wire.ContinueBudgetRequest) (wire.ContinueBudgetResult, error) {
		called = "continue-budget"
		return wire.ContinueBudgetResult{}, nil
	}
	fake.ResolveDeliveryFunc = func(context.Context, wire.ResolveDeliveryRequest) (wire.ResolveDeliveryResult, error) {
		called = "resolve-delivery"
		return wire.ResolveDeliveryResult{}, nil
	}
	fake.HoldFunc = func(context.Context, wire.HoldRequest) (wire.HoldResult, error) {
		called = "hold"
		return wire.HoldResult{}, nil
	}
	fake.OpenTerminalFunc = func(context.Context, wire.OpenTerminalRequest) (wire.RequestResult, error) {
		called = "open-terminal"
		return wire.RequestResult{}, nil
	}
	fake.ReturnHeadlessFunc = func(context.Context, wire.ReturnHeadlessRequest) (wire.RequestResult, error) {
		called = "return-headless"
		return wire.RequestResult{}, nil
	}
	fake.StopMemberFunc = func(context.Context, wire.StopMemberRequest) (wire.RequestResult, error) {
		called = "stop-member"
		return wire.RequestResult{}, nil
	}
	fake.WorktreesFunc = func(context.Context, wire.WorktreesRequest) (wire.WorktreesResult, error) {
		called = "worktrees"
		return wire.WorktreesResult{}, nil
	}
	fake.RemoveWorktreesFunc = func(context.Context, wire.RemoveWorktreesRequest) (wire.RemoveWorktreesResult, error) {
		called = "remove-worktrees"
		return wire.RemoveWorktreesResult{}, nil
	}
	fake.StartHerdrFunc = func(context.Context, wire.StartHerdrRequest) (wire.StartHerdrResult, error) {
		called = "start-herdr"
		return wire.StartHerdrResult{}, nil
	}
	for _, op := range (wire.OpName("")).Values() {
		if op == "watch" {
			continue
		}
		t.Run(string(op), func(t *testing.T) {
			var request []byte
			var err error
			switch op {
			case "get-team", "stop-team":
				request = []byte(`{"protocol":1,"op":"` + string(op) + `","team":"t"}`)
			case "stop-member", "return-headless":
				request = []byte(`{"protocol":1,"op":"` + string(op) + `","team":"t","member":"m"}`)
			default:
				request, err = os.ReadFile(filepath.Join("..", "..", "schema", "examples", "app-ops", string(op)+".request.json"))
				if err != nil {
					t.Fatal(err)
				}
			}
			called = ""
			var out bytes.Buffer
			if got := Run(context.Background(), string(op), bytes.NewReader(request), &out, fake); got != 0 {
				t.Fatalf("exit %d: %s", got, &out)
			}
			if called != string(op) {
				t.Fatalf("called %s want %s", called, op)
			}
			var response wire.Response
			if err = json.Unmarshal(out.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if !response.OK || response.Op != op {
				t.Fatal(out.String())
			}
		})
	}
}
