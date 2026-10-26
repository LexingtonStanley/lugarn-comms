package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/LexingtonStanley/lugarn-comms/internal/version"
)

const usage = `Usage: lugarn-comms <command> [arguments]

Commands:
  mcp       Serve an agent's comms tools over MCP on stdio (not implemented yet)
  send      Send a message to a team member (not implemented yet)
  reply     Reply to a message (not implemented yet)
  inbox     Read your messages (not implemented yet)
  team      Show your team and what you may do (not implemented yet)
  app       Answer requests from the Lugarn app (not implemented yet)
  run       Run a team's runner (not implemented yet)
  version   Print build information (use --json for JSON)
  help      Print this help (also -h or --help)

Exit codes:
  0   Success (including help)
  1   Output error
  2   Command is not implemented yet
  64  Invalid command or arguments
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
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
	case "mcp", "send", "reply", "inbox", "team", "app", "run":
		fmt.Fprintf(stderr, "lugarn-comms: %s is not implemented yet\n", args[0])
		return 2
	default:
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
