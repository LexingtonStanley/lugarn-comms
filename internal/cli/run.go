// Package cli implements the six agent commands for pi and scripts.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/agentops"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

const Usage = `Usage: lugarn-comms [global flags] <command> [arguments]
  send <to> [text]
  reply <id> [text]
  inbox [--direction in|out|both] [--limit N] [--before ID]
  team
  spawn <name> <harness> [task] [--profile P] [--mode headless|pane]
  stop <member>
Global flags: --member ID --token-file PATH --format json|text (default json)
Omitted text or - reads stdin.
`

// Options permits deterministic environment lookup in tests.
type Options struct{ Getenv func(string) string }

// Run parses a command, authenticates it and prints the matching tool object.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, svc service.Tools, auth service.Authenticator) int {
	return RunWithOptions(ctx, args, stdin, stdout, stderr, svc, auth, Options{})
}

// RunWithOptions is Run with an injectable environment lookup.
func RunWithOptions(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, svc service.Tools, auth service.Authenticator, opts Options) int {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	command, positionals, flags, err := parse(args)
	format := "json"
	if v, ok := flags["format"]; ok {
		format = v
	}
	fail := func(err error) int { return render(stdout, stderr, format, command, nil, err) }
	if err != nil {
		return fail(err)
	}
	if format != "json" && format != "text" {
		return fail(invalid("format must be json or text"))
	}
	if _, help := flags["help"]; help {
		if _, err := io.WriteString(stdout, Usage); err != nil {
			return 1
		}
		return 0
	}
	input, err := makeInput(command, positionals, flags, stdin)
	if err != nil {
		return fail(err)
	}
	b, err := json.Marshal(input)
	if err != nil {
		return fail(err)
	}
	typed, err := wire.DecodeToolInput(command, b)
	if err != nil {
		return fail(err)
	}
	identity := agentops.Identity{Member: getenv("LUGARN_COMMS_MEMBER"), TokenFile: getenv("LUGARN_COMMS_TOKEN_FILE")}
	if v, ok := flags["member"]; ok {
		identity.Member = v
	}
	if v, ok := flags["token-file"]; ok {
		identity.TokenFile = v
	}
	caller, authErr := identity.Authenticate(ctx, auth, model.FaceCLI)
	if authErr != nil {
		return fail(authErr)
	}
	output, err := agentops.CallTool(ctx, svc, caller, typed)
	return render(stdout, stderr, format, command, output, err)
}

func invalid(s string) *model.Error { return model.NewError(model.ErrorInvalidRequest, s) }
func parse(args []string) (string, []string, map[string]string, error) {
	flags := map[string]string{}
	var pos []string
	command := ""
	literal := false
	allowed := map[string]bool{"member": true, "token-file": true, "format": true, "direction": true, "limit": true, "before": true, "profile": true, "mode": true}
	for n := 0; n < len(args); n++ {
		arg := args[n]
		if arg == "--" && !literal {
			literal = true
			continue
		}
		if !literal && (arg == "--help" || arg == "-h") {
			flags["help"] = "true"
			continue
		}
		if !literal && strings.HasPrefix(arg, "--") {
			key, value, equals := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			if !allowed[key] {
				return command, pos, flags, invalid("unknown flag --" + key)
			}
			if !equals {
				n++
				if n >= len(args) {
					return command, pos, flags, invalid("missing value for --" + key)
				}
				value = args[n]
			}
			flags[key] = value
			continue
		}
		if !literal && strings.HasPrefix(arg, "-") && arg != "-" {
			return command, pos, flags, invalid("unknown flag " + arg)
		}
		if command == "" {
			command = arg
		} else {
			pos = append(pos, arg)
		}
	}
	if !wire.ToolName(command).Valid() {
		return command, pos, flags, invalid("expected send, reply, inbox, team, spawn or stop")
	}
	for key := range flags {
		if key == "member" || key == "token-file" || key == "format" || key == "help" {
			continue
		}
		if command == "inbox" && (key == "direction" || key == "limit" || key == "before") {
			continue
		}
		if command == "spawn" && (key == "profile" || key == "mode") {
			continue
		}
		return command, pos, flags, invalid("flag --" + key + " is not valid for " + command)
	}
	return command, pos, flags, nil
}
func makeInput(command string, pos []string, flags map[string]string, stdin io.Reader) (map[string]any, error) {
	input := map[string]any{}
	text := func(index int, key string) error {
		value := ""
		if len(pos) > index {
			value = pos[index]
		}
		if len(pos) <= index || value == "-" {
			b, err := io.ReadAll(io.LimitReader(stdin, 4*model.MaxTextCodePoints+1))
			if err != nil {
				return invalid("cannot read text from stdin")
			}
			value = string(b)
		}
		input[key] = value
		return nil
	}
	switch command {
	case "send", "reply":
		if len(pos) < 1 || len(pos) > 2 {
			return nil, invalid("usage: " + command + " <recipient or id> [text]")
		}
		key := "to"
		if command == "reply" {
			key = "id"
		}
		input[key] = pos[0]
		if err := text(1, "text"); err != nil {
			return nil, err
		}
	case "spawn":
		if len(pos) < 2 || len(pos) > 3 {
			return nil, invalid("usage: spawn <name> <harness> [task]")
		}
		input["name"] = pos[0]
		input["harness"] = pos[1]
		if err := text(2, "task"); err != nil {
			return nil, err
		}
		for _, key := range []string{"profile", "mode"} {
			if v, ok := flags[key]; ok {
				input[key] = v
			}
		}
	case "stop":
		if len(pos) != 1 {
			return nil, invalid("usage: stop <member>")
		}
		input["member"] = pos[0]
	case "team", "inbox":
		if len(pos) != 0 {
			return nil, invalid(command + " accepts no positional arguments")
		}
		if command == "inbox" {
			for _, key := range []string{"direction", "before", "limit"} {
				if v, ok := flags[key]; ok {
					if key == "limit" {
						n, err := strconv.ParseInt(v, 10, 64)
						if err != nil {
							return nil, invalid("limit must be an integer")
						}
						input[key] = n
					} else {
						input[key] = v
					}
				}
			}
		}
	}
	return input, nil
}
func render(stdout, stderr io.Writer, format, command string, output any, err error) int {
	code := 0
	summary := ""
	value := output
	if err != nil {
		e := model.AsError(err)
		code = model.ExitCode(e.Code)
		value = wire.ToolError{Error: *e}
		summary = e.Error()
	} else {
		summary = summarize(command, output)
	}
	// Service messages may contain line breaks; diagnostics stay exactly one line.
	summary = strings.Join(strings.Fields(summary), " ")
	if format != "text" {
		if json.NewEncoder(stdout).Encode(value) != nil {
			return 1
		}
		if _, err := fmt.Fprintln(stderr, summary); err != nil {
			return 1
		}
	} else {
		dest := stdout
		if err != nil {
			dest = stderr
		}
		if _, err := fmt.Fprintln(dest, summary); err != nil {
			return 1
		}
	}
	return code
}
func summarize(command string, value any) string {
	switch v := value.(type) {
	case wire.SendOutput:
		return fmt.Sprintf("%s: %s to %s (%s)", command, v.ID, v.To.Name, v.State)
	case wire.InboxOutput:
		return fmt.Sprintf("inbox: %d messages", len(v.Messages))
	case wire.TeamOutput:
		return fmt.Sprintf("team: %s (%s), %d other members", v.Team.Name, v.Team.State, len(v.Members))
	case wire.SpawnOutput:
		return fmt.Sprintf("spawn: %s (%s)", v.Member.Name, v.Member.State)
	case wire.StopOutput:
		return fmt.Sprintf("stop: %s (%s)", v.Member.Name, v.Member.State)
	default:
		return command + ": ok"
	}
}
