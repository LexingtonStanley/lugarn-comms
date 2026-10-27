package broker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func init() {
	integrationHelpers["pane-launch-wrapper"] = func() int {
		code, e := RunPaneLaunch(os.Getenv("LUGARN_PANE_TEST_LAUNCH"), os.Stdin, os.Stdout, os.Stderr)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 91
		}
		return code
	}
	integrationHelpers["pane-launch-child"] = func() int {
		input, _ := io.ReadAll(os.Stdin)
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(os.Stdout).Encode(paneLaunchReport{Args: os.Args[1:], Path: os.Getenv("PATH"), Marker: os.Getenv("LUGARN_PANE_TEST_MARKER"), Cwd: cwd, Input: string(input), ConfigContent: os.Getenv("OPENCODE_CONFIG_CONTENT")})
		fmt.Fprint(os.Stderr, "child stderr marker")
		code, _ := strconv.Atoi(os.Getenv("LUGARN_PANE_TEST_EXIT"))
		return code
	}
}

type paneLaunchReport struct {
	Args                                    []string
	Path, Marker, Cwd, Input, ConfigContent string
}

func writeTestPaneCapsule(t *testing.T, state, id string, config paneLaunchCapsule) string {
	t.Helper()
	root := filepath.Join(state, "launch", id)
	must(t, os.MkdirAll(root, 0700))
	raw, e := json.Marshal(config)
	must(t, e)
	path := filepath.Join(root, "pane-launch.json")
	must(t, os.WriteFile(path, raw, 0600))
	return path
}
func TestPaneLaunchExactArgvAndExitCode(t *testing.T) {
	for _, kind := range []model.Harness{model.HarnessClaude, model.HarnessCodex, model.HarnessOpenCode} {
		for _, exitCode := range []int{0, 7} {
			t.Run(fmt.Sprintf("%s/%d", kind, exitCode), func(t *testing.T) {
				state := t.TempDir()
				id := cleanupID(t)
				binary, e := os.Executable()
				must(t, e)
				work := filepath.Join(t.TempDir(), "work with spaces")
				must(t, os.Mkdir(work, 0700))
				launch := harness.Launch{Harness: kind, Profile: model.ProfileReadOnly, StateDir: state, WorkDir: work, LaunchID: id, MemberID: "worker", Binary: binary, SessionID: "00000000-0000-4000-8000-000000000001", Resume: true, Version: "2.1.4"}
				if kind == model.HarnessOpenCode {
					launch.SessionID = "ses_fixture"
				}
				args, e := harness.Args(launch, model.ModePane)
				must(t, e)
				args = append(args, "", `literal "quotes" and \backslash\`, "space and 🦊", `%PATH% $env:X & (literal)`)
				pathEnv := filepath.Join(state, "trusted bin") + string(os.PathListSeparator) + os.Getenv("PATH")
				capsule := paneLaunchCapsule{Harness: kind, Binary: binary, Args: args, Workdir: work, Env: map[string]string{"PATH": pathEnv, "LUGARN_INTEGRATION_HELPER": "pane-launch-child", "LUGARN_PANE_TEST_MARKER": "from capsule", "LUGARN_PANE_TEST_EXIT": strconv.Itoa(exitCode)}}
				path := writeTestPaneCapsule(t, state, id, capsule)
				cmd := exec.Command(binary)
				cmd.Env = append(os.Environ(), "LUGARN_INTEGRATION_HELPER=pane-launch-wrapper", "LUGARN_PANE_TEST_LAUNCH="+id, "LUGARN_COMMS_HOME="+state, "LUGARN_COMMS_PANE_CONFIG="+path, "LUGARN_PANE_TEST_MARKER=hostile parent", "OPENCODE_CONFIG_CONTENT=hostile inherited config")
				cmd.Stdin = strings.NewReader("console input\n")
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				raw, e := cmd.Output()
				if exitCode == 0 {
					must(t, e)
				} else if status, ok := e.(*exec.ExitError); !ok || status.ExitCode() != exitCode {
					t.Fatalf("exit was not forwarded: %v stderr=%s", e, &stderr)
				}
				var report paneLaunchReport
				must(t, json.Unmarshal(raw, &report))
				gotDir, e := os.Stat(report.Cwd)
				must(t, e)
				wantDir, e := os.Stat(work)
				must(t, e)
				if !reflect.DeepEqual(report.Args, args) || report.Path != pathEnv || report.Marker != "from capsule" || !os.SameFile(gotDir, wantDir) || report.Input != "console input\n" || stderr.String() != "child stderr marker" {
					t.Fatal(report, &stderr)
				}
				if kind == model.HarnessOpenCode && report.ConfigContent != "" {
					t.Fatal("inherited config bypassed per-launch config")
				}
			})
		}
	}
}
func TestPaneLaunchCapsuleConfinement(t *testing.T) {
	for _, kind := range []string{"different-id", "relative", "wrong-name", "file-link", "directory-link", "no-explicit-state"} {
		t.Run(kind, func(t *testing.T) {
			state := t.TempDir()
			id := cleanupID(t)
			path := writeTestPaneCapsule(t, state, id, paneLaunchCapsule{})
			switch kind {
			case "different-id":
				id = cleanupID(t)
			case "relative":
				path = "pane-launch.json"
			case "wrong-name":
				path = filepath.Join(filepath.Dir(path), "other.json")
			case "file-link":
				target := filepath.Join(t.TempDir(), "outside.json")
				must(t, os.WriteFile(target, []byte("{}"), 0600))
				must(t, os.Remove(path))
				if e := os.Symlink(target, path); e != nil {
					t.Skip(e)
				}
			case "directory-link":
				root := filepath.Dir(path)
				target := filepath.Join(t.TempDir(), "outside")
				must(t, os.Rename(root, target))
				if e := os.Symlink(target, root); e != nil {
					t.Skip(e)
				}
			case "no-explicit-state":
				state = ""
			}
			t.Setenv("LUGARN_COMMS_HOME", state)
			t.Setenv("LUGARN_COMMS_PANE_CONFIG", path)
			if _, e := RunPaneLaunch(id, strings.NewReader(""), io.Discard, io.Discard); e == nil {
				t.Fatal("accepted capsule outside the launch")
			}
		})
	}
}

func TestWindowsPaneCapsuleContainsOnlyLaunchEnvironment(t *testing.T) {
	const sentinel = "parent-environment-must-not-be-copied"
	t.Setenv("LUGARN_PARENT_SECRET", sentinel)
	for _, kind := range []model.Harness{model.HarnessClaude, model.HarnessCodex, model.HarnessOpenCode} {
		t.Run(string(kind), func(t *testing.T) {
			b, _ := setup(t)
			binary, e := os.Executable()
			must(t, e)
			i, e := NewIntegration(b, IntegrationOptions{Binary: binary, HomeDir: t.TempDir()})
			must(t, e)
			l := harness.Launch{Harness: kind, Profile: model.ProfileReadOnly, StateDir: b.opts.StateDir, WorkDir: t.TempDir(), LaunchID: cleanupID(t), MemberID: "writer", Binary: binary, SessionID: "00000000-0000-4000-8000-000000000001", Version: "2.1.4", ExtraEnv: map[string]string{"LAUNCH_ONLY": "explicit"}}
			if kind != model.HarnessClaude {
				l.SessionID = ""
			}
			_, _, e = i.prepareWindowsPane(l, binary, []string{"exact arg", ""})
			must(t, e)
			path := filepath.Join(l.StateDir, "launch", l.LaunchID, "pane-launch.json")
			raw, e := os.ReadFile(path)
			must(t, e)
			if strings.Contains(string(raw), sentinel) || strings.Contains(string(raw), "LUGARN_PARENT_SECRET") {
				t.Fatal("copied parent environment")
			}
			var capsule paneLaunchCapsule
			must(t, json.Unmarshal(raw, &capsule))
			expected := []string{"LAUNCH_ONLY", "LUGARN_COMMS_HOME", "LUGARN_COMMS_MEMBER", "LUGARN_COMMS_PANE_CONFIG", "LUGARN_COMMS_POINTER_HOME", "LUGARN_COMMS_TOKEN_FILE", "NO_COLOR", "PATH", "PWD"}
			if kind == model.HarnessOpenCode {
				expected = append(expected, "OPENCODE_CONFIG", "OPENCODE_DISABLE_AUTOUPDATE")
			}
			got := make([]string, 0, len(capsule.Env))
			for key := range capsule.Env {
				got = append(got, key)
			}
			sort.Strings(got)
			sort.Strings(expected)
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("environment keys = %v, want %v", got, expected)
			}
			if !reflect.DeepEqual(capsule.Args, []string{"exact arg", ""}) {
				t.Fatal("changed argv")
			}
			if runtime.GOOS != "windows" {
				info, e := os.Stat(path)
				must(t, e)
				if info.Mode().Perm() != 0600 {
					t.Fatal(info.Mode())
				}
			}
		})
	}
}
