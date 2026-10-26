package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/agentops"
	"github.com/LexingtonStanley/lugarn-comms/internal/appops"
	"github.com/LexingtonStanley/lugarn-comms/internal/broker"
	"github.com/LexingtonStanley/lugarn-comms/internal/cli"
	"github.com/LexingtonStanley/lugarn-comms/internal/mcpserver"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/service"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
	"github.com/LexingtonStanley/lugarn-comms/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const usage = `Usage: lugarn-comms <command> [arguments]

Commands:
  mcp       Serve an agent's comms tools over MCP on stdio
  send      Send a message to a team member
  reply     Reply to a message
  inbox     Read your messages
  team      Show your team and what you may do
  spawn     Spawn a worker
  stop      Stop a worker you spawned
  app       Answer requests from the Lugarn app
  run       Run a team's runner (not implemented yet)
  version   Print build information (use --json for JSON)
  help      Print this help (also -h or --help)

Exit codes:
  0   Success (including help)
  1   Output error
  2   Invalid request (or run is not implemented yet)
  64  Invalid command or arguments
`

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	os.Exit(runWithInput(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithInput(context.Background(), args, os.Stdin, stdout, stderr)
}

func runWithInput(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return writeOutput(stdout, stderr, usage)
	}

	switch args[0] {
	case "help", "-h", "--help":
		if len(args) != 1 {
			return usageError(stderr, fmt.Sprintf("unexpected arguments for %s", args[0]))
		}
		return writeOutput(stdout, stderr, usage)
	case "version":
		if len(args) == 1 {
			return writeOutput(stdout, stderr, fmt.Sprintf("lugarn-comms %s (%s, %s/%s)\n",
				version.Version, version.Commit, runtime.GOOS, runtime.GOARCH))
		}
		if len(args) != 2 || args[1] != "--json" {
			return usageError(stderr, "usage: lugarn-comms version [--json]")
		}
		info := struct {
			Name     string `json:"name"`
			Version  string `json:"version"`
			Commit   string `json:"commit"`
			OS       string `json:"os"`
			Arch     string `json:"arch"`
			Protocol int    `json:"protocol"`
		}{"lugarn-comms", version.Version, version.Commit, runtime.GOOS, runtime.GOARCH, version.Protocol}
		if err := json.NewEncoder(stdout).Encode(info); err != nil {
			fmt.Fprintf(stderr, "lugarn-comms: write output: %v\n", err)
			return 1
		}
		return 0
	case "app":
		if len(args) != 2 {
			return usageError(stderr, "usage: lugarn-comms app <op>")
		}
		svc, close := openService(ctx)
		defer close()
		return appops.Run(ctx, args[1], stdin, stdout, svc)
	case "mcp":
		flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
		flags.SetOutput(stderr)
		member := flags.String("member", "", "member id")
		token := flags.String("token-file", "", "launch token path")
		if err := flags.Parse(args[1:]); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 2
		}
		if flags.NArg() != 0 {
			return usageError(stderr, "unexpected MCP arguments")
		}
		if *member == "" || *token == "" {
			fmt.Fprintln(stderr, "unauthenticated: --member and --token-file are required")
			return 3
		}
		svc, close := openService(ctx)
		defer close()
		server, err := mcpserver.New(agentops.Identity{Member: *member, TokenFile: *token}, svc, svc)
		if err == nil {
			reader, ok := stdin.(io.ReadCloser)
			if !ok {
				reader = io.NopCloser(stdin)
			}
			err = server.Run(ctx, &mcp.IOTransport{Reader: reader, Writer: writerCloser{stdout}})
		}
		if err != nil {
			fmt.Fprintln(stderr, model.AsError(err))
			return 1
		}
		return 0
	case "send", "reply", "inbox", "team", "spawn", "stop":
		svc, close := openService(ctx)
		defer close()
		return cli.Run(ctx, args, stdin, stdout, stderr, svc, svc)
	case "run":
		fmt.Fprintf(stderr, "lugarn-comms: %s is not implemented yet (C2)\n", args[0])
		return 2
	default:
		if len(args) > 0 && (args[0] == "--member" || args[0] == "--token-file" || args[0] == "--format" || strings.HasPrefix(args[0], "--member=") || strings.HasPrefix(args[0], "--token-file=") || strings.HasPrefix(args[0], "--format=")) {
			svc, close := openService(ctx)
			defer close()
			return cli.Run(ctx, args, stdin, stdout, stderr, svc, svc)
		}
		return usageError(stderr, fmt.Sprintf("unknown subcommand %q", args[0]))
	}
}

func usageError(stderr io.Writer, message string) int {
	fmt.Fprintf(stderr, "lugarn-comms: %s\nTry 'lugarn-comms help' for usage.\n", message)
	return 64
}

func writeOutput(stdout, stderr io.Writer, output string) int {
	if _, err := io.WriteString(stdout, output); err != nil {
		fmt.Fprintf(stderr, "lugarn-comms: write output: %v\n", err)
		return 1
	}
	return 0
}

type writerCloser struct{ io.Writer }

func (writerCloser) Close() error { return nil }

// A startup failure is served through the existing front doors so app envelopes,
// CLI JSON/text and MCP tool failures retain their normal formats.
type services interface {
	service.App
	service.Tools
	service.Authenticator
}

func openService(ctx context.Context) (services, func()) {
	dir, err := statedir.Resolve()
	if err == nil {
		err = statedir.Ensure(dir)
	}
	if err != nil {
		return errorService{model.AsError(err)}, func() {}
	}
	db, err := store.Open(ctx, filepath.Join(dir, "comms.db"))
	if err != nil {
		return errorService{model.AsError(err)}, func() {}
	}
	return broker.New(db, broker.Options{StateDir: dir}), func() { _ = db.Close() }
}
