package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/version"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
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

func TestFrontDoorDispatch(t *testing.T) {
	t.Setenv("LUGARN_COMMS_HOME", t.TempDir())
	t.Setenv("LUGARN_COMMS_MEMBER", "")
	t.Setenv("LUGARN_COMMS_TOKEN_FILE", "")
	for _, tt := range []struct {
		args  []string
		input string
		code  int
		out   string
	}{
		{[]string{"app", "status"}, `{"protocol":1,"op":"status","request_id":"x"}`, 0, `"teams":[]`},
		{[]string{"team"}, "", 3, `"error"`},
		{[]string{"--format", "json", "team"}, "", 3, `"error"`},
		{[]string{"spawn"}, "", 2, `"invalid_request"`},
		{[]string{"stop"}, "", 2, `"invalid_request"`},
		{[]string{"mcp"}, "", 3, ""},
		{[]string{"app"}, "", 64, ""},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var out, diag bytes.Buffer
			if got := runWithInput(context.Background(), tt.args, strings.NewReader(tt.input), &out, &diag); got != tt.code {
				t.Fatalf("exit %d want %d: %s %s", got, tt.code, &out, &diag)
			}
			if !strings.Contains(out.String(), tt.out) {
				t.Fatal(out.String())
			}
		})
	}
}

func TestStartupErrorsUseFaceFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUGARN_COMMS_HOME", path)
	for _, tt := range []struct {
		args  []string
		input string
		op    string
	}{
		{[]string{"app", "status"}, `{"protocol":1,"op":"status","request_id":"startup"}`, "status"},
		{[]string{"team"}, "", ""},
	} {
		var out, diag bytes.Buffer
		if got := runWithInput(context.Background(), tt.args, strings.NewReader(tt.input), &out, &diag); got != 1 {
			t.Fatalf("exit %d: %s %s", got, &out, &diag)
		}
		var v map[string]any
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		failure := v["error"].(map[string]any)
		if failure["code"] != "internal" {
			t.Fatal(v)
		}
		if tt.op != "" && (v["op"] != tt.op || v["request_id"] != "startup") {
			t.Fatal(v)
		}
	}
}

func TestRunnerCommand(t *testing.T) {
	t.Setenv("LUGARN_COMMS_HOME", t.TempDir())
	for _, tt := range []struct {
		args []string
		want int
	}{{[]string{"run"}, 2}, {[]string{"run", "--help"}, 0}, {[]string{"run", "--team", "unknown"}, 10}, {[]string{"run", "--team", "x", "extra"}, 2}} {
		var out, diag bytes.Buffer
		if got := runWithInput(context.Background(), tt.args, strings.NewReader(""), &out, &diag); got != tt.want {
			t.Fatalf("%v: exit %d want %d: %s", tt.args, got, tt.want, &diag)
		}
	}
}

func TestMCPStateRootSurvivesStrippedEnvironment(t *testing.T) {
	state := t.TempDir()
	t.Setenv("LUGARN_COMMS_HOME", "")
	if e := os.MkdirAll(filepath.Join(state, "launch"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(state, "comms.db"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	token := filepath.Join(state, "launch", "00000000000000000000000001.token")
	if e := os.WriteFile(token, []byte("test"), 0600); e != nil {
		t.Fatal(e)
	}
	svc, closeStore := openServiceForToken(context.Background(), token)
	defer closeStore()
	result, err := svc.Status(context.Background(), wire.StatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Teams) != 0 {
		t.Fatal(result)
	}
	if _, err := os.Stat(filepath.Join(state, "comms.db")); err != nil {
		t.Fatal(err)
	}
}

func TestTokenPathCannotCreateOrChmodAStore(t *testing.T) {
	for _, kind := range []string{"missing-token", "missing-db", "symlink-token"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			token := filepath.Join(dir, "launch", "00000000000000000000000001.token")
			if e := os.MkdirAll(filepath.Dir(token), 0755); e != nil {
				t.Fatal(e)
			}
			if kind != "missing-token" {
				if e := os.WriteFile(token, []byte("test"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if kind != "missing-db" {
				if e := os.WriteFile(filepath.Join(dir, "comms.db"), nil, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "symlink-token" {
				if e := os.Remove(token); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(filepath.Join(dir, "comms.db"), token); e != nil {
					t.Skip(e)
				}
			}
			if e := os.Chmod(dir, 0755); e != nil {
				t.Fatal(e)
			}
			before, e := os.Stat(dir)
			if e != nil {
				t.Fatal(e)
			}
			svc, closeStore := openServiceForToken(context.Background(), token)
			defer closeStore()
			if _, e = svc.Status(context.Background(), wire.StatusRequest{}); e == nil {
				t.Fatal("untrusted token path opened a store")
			}
			after, e := os.Stat(dir)
			if e != nil {
				t.Fatal(e)
			}
			if before.Mode() != after.Mode() {
				t.Fatal("changed unrelated directory permissions", before.Mode(), after.Mode())
			}
			if kind == "missing-db" {
				if _, e = os.Lstat(filepath.Join(dir, "comms.db")); !os.IsNotExist(e) {
					t.Fatal("created a database", e)
				}
			}
		})
	}
}
