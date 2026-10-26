package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func authFake() *service.Fake {
	return &service.Fake{AuthenticateFunc: func(context.Context, string, *service.Token, model.Face) (service.Caller, *model.Error) {
		return service.Caller{MemberID: "caller"}, nil
	}}
}
func TestAllCommands(t *testing.T) {
	id := "01M5W861CRYYK596NGYA1DQ91K"
	fake := authFake()
	var input any
	var caller service.Caller
	fake.ToolSendFunc = func(_ context.Context, c service.Caller, r wire.SendInput) (wire.SendOutput, error) {
		input = r
		caller = c
		return wire.SendOutput{ID: id, To: model.MemberRef{Name: r.To}, State: model.DeliveryStateQueued}, nil
	}
	fake.ToolReplyFunc = func(_ context.Context, c service.Caller, r wire.ReplyInput) (wire.ReplyOutput, error) {
		input = r
		caller = c
		return wire.ReplyOutput{ID: id, To: model.MemberRef{Name: "writer"}, State: model.DeliveryStateQueued}, nil
	}
	fake.ToolInboxFunc = func(_ context.Context, c service.Caller, r wire.InboxInput) (wire.InboxOutput, error) {
		input = r
		caller = c
		return wire.InboxOutput{}, nil
	}
	fake.ToolTeamFunc = func(_ context.Context, c service.Caller, r wire.TeamInput) (wire.TeamOutput, error) {
		input = r
		caller = c
		return wire.TeamOutput{Team: wire.TeamOutputTeam{Name: "test", State: model.TeamStateRunning}}, nil
	}
	fake.ToolSpawnFunc = func(_ context.Context, c service.Caller, r wire.SpawnInput) (wire.SpawnOutput, error) {
		input = r
		caller = c
		return wire.SpawnOutput{Member: wire.SpawnOutputMember{Name: r.Name, State: model.MemberStateStarting}}, nil
	}
	fake.ToolStopFunc = func(_ context.Context, c service.Caller, r wire.StopInput) (wire.StopOutput, error) {
		input = r
		caller = c
		return wire.StopOutput{Member: wire.StopOutputMember{Name: r.Member, State: model.MemberStateStopped}}, nil
	}
	for _, tt := range []struct {
		args    []string
		stdin   string
		want    map[string]any
		summary string
	}{
		{[]string{"send", "writer", "explicit"}, "ignored", map[string]any{"to": "writer", "text": "explicit"}, "queued"},
		{[]string{"send", "writer"}, "from stdin\n", map[string]any{"to": "writer", "text": "from stdin\n"}, "queued"},
		{[]string{"send", "writer", "-"}, "dash stdin", map[string]any{"to": "writer", "text": "dash stdin"}, "queued"},
		{[]string{"reply", id}, "reply stdin", map[string]any{"id": id, "text": "reply stdin"}, "queued"},
		{[]string{"reply", id, "-"}, "reply dash", map[string]any{"id": id, "text": "reply dash"}, "queued"},
		{[]string{"inbox", "--direction", "both", "--limit=5", "--before", id}, "", map[string]any{"direction": "both", "limit": float64(5), "before": id}, "inbox: 0 messages"},
		{[]string{"team"}, "", map[string]any{}, "team: test"},
		{[]string{"spawn", "worker", "codex", "task", "--profile", "read-only", "--mode=pane"}, "ignored", map[string]any{"name": "worker", "harness": "codex", "task": "task", "profile": "read-only", "mode": "pane"}, "worker (starting)"},
		{[]string{"spawn", "worker", "claude"}, "spawn stdin", map[string]any{"name": "worker", "harness": "claude", "task": "spawn stdin"}, "starting"},
		{[]string{"spawn", "worker", "claude", "-"}, "spawn dash", map[string]any{"name": "worker", "harness": "claude", "task": "spawn dash"}, "starting"},
		{[]string{"stop", "worker"}, "", map[string]any{"member": "worker"}, "worker (stopped)"},
		{[]string{"send", "writer", "--", "--literal"}, "", map[string]any{"to": "writer", "text": "--literal"}, "queued"},
	} {
		for _, format := range []string{"json", "text"} {
			t.Run(strings.Join(tt.args, " ")+"/"+format, func(t *testing.T) {
				var out, diag bytes.Buffer
				args := append([]string{"--format", format}, tt.args...)
				if got := RunWithOptions(context.Background(), args, strings.NewReader(tt.stdin), &out, &diag, fake, fake, Options{Getenv: func(string) string { return "" }}); got != 0 {
					t.Fatalf("exit %d: %s %s", got, &out, &diag)
				}
				if caller.MemberID != "caller" {
					t.Fatal(caller)
				}
				b, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				var actual map[string]any
				if err = json.Unmarshal(b, &actual); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, tt.want) {
					t.Fatalf("input %s want %v", b, tt.want)
				}
				if format == "json" {
					var object map[string]any
					if err = json.Unmarshal(out.Bytes(), &object); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(diag.String(), tt.summary) || strings.Count(diag.String(), "\n") != 1 {
						t.Fatal(diag.String())
					}
				} else {
					if !strings.Contains(out.String(), tt.summary) || diag.Len() != 0 {
						t.Fatalf("%s %s", &out, &diag)
					}
				}
			})
		}
	}
}
func TestIdentityFlagsAndEnvironment(t *testing.T) {
	token, _ := model.NewToken()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		args     []string
		env      map[string]string
		member   string
		hasToken bool
	}{
		{[]string{"team"}, map[string]string{"LUGARN_COMMS_MEMBER": "env", "LUGARN_COMMS_TOKEN_FILE": path}, "env", true},
		{[]string{"--member", "flag", "team", "--token-file", path}, map[string]string{"LUGARN_COMMS_MEMBER": "env", "LUGARN_COMMS_TOKEN_FILE": "missing"}, "flag", true},
		{[]string{"team", "--member=", "--token-file="}, map[string]string{"LUGARN_COMMS_MEMBER": "env", "LUGARN_COMMS_TOKEN_FILE": path}, "", false},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			fake := authFake()
			fake.AuthenticateFunc = func(_ context.Context, member string, got *service.Token, face model.Face) (service.Caller, *model.Error) {
				if member != tt.member || face != model.FaceCLI {
					t.Errorf("member %q face %s", member, face)
				}
				if (got != nil) != tt.hasToken {
					t.Errorf("token %v", got)
				}
				if got != nil && (got.Hash != model.HashToken(token) || string(got.Bytes) != token) {
					t.Error("bad token or hash")
				}
				return service.Caller{}, nil
			}
			fake.ToolTeamFunc = func(context.Context, service.Caller, wire.TeamInput) (wire.TeamOutput, error) {
				return wire.TeamOutput{}, nil
			}
			var out, diag bytes.Buffer
			if code := RunWithOptions(context.Background(), tt.args, strings.NewReader(""), &out, &diag, fake, fake, Options{Getenv: func(k string) string { return tt.env[k] }}); code != 0 {
				t.Fatalf("%d %s", code, &out)
			}
		})
	}
}
func TestArgumentFailures(t *testing.T) {
	id := "01M5W861CRYYK596NGYA1DQ91K"
	for _, args := range [][]string{
		{}, {"unknown"}, {"send"}, {"send", "writer", "a", "b"}, {"send", "all", "hello"}, {"send", "human", strings.Repeat("界", 8001)},
		{"send", "writer", ""}, {"reply", "bad", "hello"}, {"reply", id, "a", "b"}, {"team", "extra"}, {"team", "--limit", "3"},
		{"team", "--format", "xml"}, {"team", "--bogus"}, {"team", "--member"}, {"inbox", "--limit", "no"}, {"inbox", "--limit", "0"}, {"inbox", "--limit", "101"}, {"inbox", "--direction", "sideways"},
		{"spawn", "worker"}, {"spawn", "human", "claude", "task"}, {"spawn", "worker", "unknown", "task"}, {"spawn", "worker", "claude", "task", "--profile", "root"}, {"spawn", "worker", "claude", "task", "--mode", "other"},
		{"stop"}, {"stop", "worker", "extra"}, {"stop", "all"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			fake := authFake()
			fake.AuthenticateFunc = func(context.Context, string, *service.Token, model.Face) (service.Caller, *model.Error) {
				t.Error("authenticated invalid args")
				return service.Caller{}, nil
			}
			var out, diag bytes.Buffer
			if code := Run(context.Background(), args, strings.NewReader(""), &out, &diag, fake, fake); code != 2 {
				t.Fatalf("exit %d: %s %s", code, &out, &diag)
			}
			if !strings.Contains(diag.String(), "invalid_request:") {
				t.Fatal(diag.String())
			}
		})
	}
}
func TestErrorExitCodesAndFormats(t *testing.T) {
	for _, code := range (model.ErrorCode("")).Values() {
		for _, format := range []string{"json", "text"} {
			t.Run(string(code)+"/"+format, func(t *testing.T) {
				fake := authFake()
				fake.ToolTeamFunc = func(context.Context, service.Caller, wire.TeamInput) (wire.TeamOutput, error) {
					return wire.TeamOutput{}, model.NewError(code, "test\nmessage")
				}
				var out, diag bytes.Buffer
				if got := Run(context.Background(), []string{"team", "--format", format}, strings.NewReader(""), &out, &diag, fake, fake); got != model.ExitCode(code) {
					t.Fatalf("exit %d", got)
				}
				if strings.Count(diag.String(), "\n") != 1 || diag.String() != string(code)+": test message\n" {
					t.Fatal(diag.String())
				}
				if format == "json" {
					var object wire.ToolError
					if err := model.DecodeStrict(out.Bytes(), &object); err != nil {
						t.Fatal(err)
					}
					if object.Error.Code != code || object.Error.Message != "test\nmessage" {
						t.Fatal(out.String())
					}
				} else if out.Len() != 0 {
					t.Fatal(out.String())
				}
			})
		}
	}
}
func TestAuthenticationFailure(t *testing.T) {
	fake := authFake()
	fake.AuthenticateFunc = func(context.Context, string, *service.Token, model.Face) (service.Caller, *model.Error) {
		return service.Caller{}, model.NewError(model.ErrorUnauthenticated, "no token")
	}
	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"team"}, strings.NewReader(""), &out, &diag, fake, fake); code != 3 || !strings.Contains(out.String(), `"code":"unauthenticated"`) {
		t.Fatalf("%d %s", code, &out)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("write failure") }
func TestIOFailuresAndHelp(t *testing.T) {
	fake := authFake()
	fake.ToolTeamFunc = func(context.Context, service.Caller, wire.TeamInput) (wire.TeamOutput, error) {
		return wire.TeamOutput{}, nil
	}
	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"send", "writer"}, failReader{}, &out, &diag, fake, fake); code != 2 {
		t.Fatal(code)
	}
	if code := Run(context.Background(), []string{"team"}, strings.NewReader(""), failWriter{}, io.Discard, fake, fake); code != 1 {
		t.Fatal(code)
	}
	out.Reset()
	if code := Run(context.Background(), []string{"team", "--help"}, strings.NewReader(""), &out, io.Discard, fake, fake); code != 0 || out.String() != Usage {
		t.Fatalf("%d %s", code, &out)
	}
}
