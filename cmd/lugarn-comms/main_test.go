package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/version"
)

func TestRun(t *testing.T) {
	type testCase struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
		json   bool
	}
	tests := []testCase{
		{name: "no arguments", stdout: usage},
		{name: "help", args: []string{"help"}, stdout: usage},
		{name: "short help", args: []string{"-h"}, stdout: usage},
		{name: "long help", args: []string{"--help"}, stdout: usage},
		{name: "version", args: []string{"version"}, stdout: fmt.Sprintf("lugarn-comms %s (%s, %s/%s)\n", version.Version, version.Commit, runtime.GOOS, runtime.GOARCH)},
		{name: "JSON version", args: []string{"version", "--json"}, json: true},
		{name: "unknown command", args: []string{"unknown"}, code: 64, stderr: "lugarn-comms: unknown subcommand \"unknown\"\nTry 'lugarn-comms help' for usage.\n"},
		{name: "unknown flag", args: []string{"--unknown"}, code: 64, stderr: "lugarn-comms: unknown subcommand \"--unknown\"\nTry 'lugarn-comms help' for usage.\n"},
		{name: "invalid version flag", args: []string{"version", "--unknown"}, code: 64, stderr: "lugarn-comms: usage: lugarn-comms version [--json]\nTry 'lugarn-comms help' for usage.\n"},
		{name: "extra version argument", args: []string{"version", "--json", "extra"}, code: 64, stderr: "lugarn-comms: usage: lugarn-comms version [--json]\nTry 'lugarn-comms help' for usage.\n"},
	}
	for _, command := range []string{"help", "-h", "--help"} {
		tests = append(tests, testCase{name: command + " extra argument", args: []string{command, "extra"}, code: 64, stderr: fmt.Sprintf("lugarn-comms: unexpected arguments for %s\nTry 'lugarn-comms help' for usage.\n", command)})
	}
	for _, command := range []string{"mcp", "send", "reply", "inbox", "team", "app", "run"} {
		for _, args := range [][]string{{command}, {command, "--help"}} {
			tests = append(tests, testCase{name: strings.Join(args, " "), args: args, code: 2, stderr: fmt.Sprintf("lugarn-comms: %s is not implemented yet\n", command)})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.code {
				t.Errorf("exit code = %d, want %d", code, tt.code)
			}
			if stderr.String() != tt.stderr {
				t.Errorf("stderr = %q, want %q", stderr.String(), tt.stderr)
			}
			if !tt.json {
				if stdout.String() != tt.stdout {
					t.Errorf("stdout = %q, want %q", stdout.String(), tt.stdout)
				}
				return
			}
			decoder := json.NewDecoder(&stdout)
			var got map[string]any
			if err := decoder.Decode(&got); err != nil {
				t.Fatalf("decode JSON: %v", err)
			}
			want := map[string]any{
				"name": "lugarn-comms", "version": version.Version, "commit": version.Commit,
				"os": runtime.GOOS, "arch": runtime.GOARCH, "protocol": float64(1),
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("JSON = %#v, want %#v", got, want)
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				t.Errorf("expected exactly one JSON object; second decode: %v", err)
			}
		})
	}
}

func TestRunOutputError(t *testing.T) {
	for _, args := range [][]string{nil, {"version"}, {"version", "--json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stderr bytes.Buffer
			if code := run(args, failingWriter{}, &stderr); code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if got, want := stderr.String(), "lugarn-comms: write output: broken writer\n"; got != want {
				t.Errorf("stderr = %q, want %q", got, want)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("broken writer")
}
