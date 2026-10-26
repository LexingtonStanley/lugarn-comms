package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	if e = json.Unmarshal(b, &v); e != nil {
		t.Fatal(e)
	}
	return v
}

func TestFiles(t *testing.T) {
	for _, h := range (model.Harness("")).Values() {
		for _, p := range (model.Profile("")).Values() {
			t.Run(string(h)+"/"+string(p), func(t *testing.T) {
				l := testLaunch(t, h)
				l.Profile = p
				if err := WriteFiles(l); err != nil {
					t.Fatal(err)
				}
				paths, err := l.OwnedPaths(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if _, err = os.Stat(l.TokenPath()); !os.IsNotExist(err) {
					t.Fatal("wrote caller token")
				}
				switch h {
				case model.HarnessClaude:
					got := readJSON(t, l.path(".json"))["mcpServers"].(map[string]any)["lugarn"].(map[string]any)
					b, _ := json.Marshal(l.mcpArgs())
					actual, _ := json.Marshal(got["args"])
					if got["command"] != l.Binary || string(b) != string(actual) {
						t.Fatal(got)
					}
					readJSON(t, l.path(".settings.json"))
					if len(paths) != 5 {
						t.Fatal(paths)
					}
				case model.HarnessOpenCode:
					got := readJSON(t, l.path(".opencode.json"))
					reg := got["mcp"].(map[string]any)["lugarn"].(map[string]any)
					command := reg["command"].([]any)
					if command[0] != l.Binary || len(command) != 6 || reg["type"] != "local" || reg["enabled"] != true || reg["codemode"] != false {
						t.Fatal(reg)
					}
					native := got["mcp"].(map[string]any)["servers"].(map[string]any)["lugarn"].(map[string]any)
					if native["type"] != "local" || native["disabled"] != false || native["codemode"] != false || !reflect.DeepEqual(native["command"], reg["command"]) {
						t.Fatal(native)
					}
					if !reflect.DeepEqual(native["timeout"], map[string]any{"startup": float64(30000), "catalog": float64(30000)}) {
						t.Fatal(native["timeout"])
					}
					expected := map[string]any{"edit": "deny", "bash": "deny"}
					if p != model.ProfileReadOnly {
						expected = map[string]any{"edit": "allow", "bash": "allow", "external_directory": "deny"}
						if p == model.ProfileBypass {
							expected["external_directory"] = "allow"
						}
					}
					if !reflect.DeepEqual(got["permission"], expected) {
						t.Fatal(got)
					}
				default:
					if len(paths) != 2 {
						t.Fatal(paths)
					}
				}
				if runtime.GOOS != "windows" {
					for _, path := range paths {
						if st, e := os.Stat(path); e == nil && !st.IsDir() && st.Mode().Perm() != 0600 {
							t.Fatalf("%s mode %v", path, st.Mode())
						}
					}
				}
			})
		}
	}
}

func TestHookAndPointer(t *testing.T) {
	for _, platform := range []string{"linux", "windows"} {
		b, err := ClaudeHookSettings(testLaunchID, platform)
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			Hooks struct {
				SessionStart []struct {
					Hooks []struct {
						Command, Shell string
						Timeout        int
					}
				}
			}
		}
		if err = json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		hook := v.Hooks.SessionStart[0].Hooks[0]
		if hook.Timeout != 10 || strings.Contains(hook.Command, "Remove-Item") || strings.Contains(hook.Command, "find ") {
			t.Fatal(hook)
		}
		if platform == "windows" {
			if hook.Shell != "powershell" || !strings.Contains(hook.Command, "AppendAllText") {
				t.Fatal(hook)
			}
			continue
		}
		if runtime.GOOS == "windows" {
			continue
		}
		home := t.TempDir()
		t.Setenv("HOME", home)
		input := `{"session_id":"` + testSessionID + `"}` + "\n"
		c := exec.Command("sh", "-c", hook.Command)
		c.Stdin = strings.NewReader(input)
		out, err := c.CombinedOutput()
		if err != nil || len(out) != 0 {
			t.Fatalf("hook: %s %v", out, err)
		}
		path := filepath.Join(home, ".lugarn", "conversations", testLaunchID)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString(`{"session_id":"dddddddd-dddd-4ddd-8ddd-dddddddddddd"}` + "\n" + "garbage\n" + `{"session_id":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","agent_id":null}` + "\n" + `{"session_id":"cccccccc-cccc-4ccc-8ccc-cccccccccccc"}`)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		id, err := ReadPointer(home, testLaunchID)
		if err != nil || id != "dddddddd-dddd-4ddd-8ddd-dddddddddddd" {
			t.Fatalf("%s %v", id, err)
		}
	}
	if _, err := ClaudeHookSettings("../bad", "linux"); err == nil {
		t.Fatal("accepted traversal")
	}
	home := t.TempDir()
	id, err := ReadPointer(home, testLaunchID)
	if err != nil || id != "" {
		t.Fatal(id, err)
	}
}

func TestPointerRecordsEveryPlatform(t *testing.T) {
	next := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	line := func(id string) string { return `{"session_id":"` + id + `"}` + "\n" }
	for _, tc := range []struct{ name, body, want string }{
		{"empty", "", ""},
		{"last main", line(testSessionID) + line(next), next},
		{"skip subagent", line(testSessionID) + `{"session_id":"` + next + `","agent_id":"worker"}` + "\n", testSessionID},
		{"null agent", line(testSessionID) + `{"session_id":"` + next + `","agent_id":null}` + "\n", testSessionID},
		{"partial tail", line(testSessionID) + strings.TrimSuffix(line(next), "\n"), testSessionID},
		{"bad record", line(testSessionID) + "bad JSON\n" + line("--unsafe"), testSessionID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, ".lugarn", "conversations")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, testLaunchID), []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := ReadPointer(home, testLaunchID)
			if err != nil || got != tc.want {
				t.Fatal(got, err)
			}
		})
	}
}

// OpenCode's config follows its major version: 2.x gets the flat entry and
// the native mcp.servers entry, 1.x the flat entry alone, without codemode,
// since 1.0's entry schema is strict and has no Code Mode.
func TestOpenCodeConfigByVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		native  bool
	}{
		{"2.1.4", true}, {"v2.0.0", true}, {"2.3.0-beta.1", true},
		{"1.19.4", false}, {"v1.0.0", false}, {"1", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			l := testLaunch(t, model.HarnessOpenCode)
			l.Version = tc.version
			if err := WriteFiles(l); err != nil {
				t.Fatal(err)
			}
			mcp := readJSON(t, l.path(".opencode.json"))["mcp"].(map[string]any)
			flat := mcp["lugarn"].(map[string]any)
			command, _ := json.Marshal(append([]string{l.Binary}, l.mcpArgs()...))
			gotCommand, _ := json.Marshal(flat["command"])
			if flat["type"] != "local" || flat["enabled"] != true || string(gotCommand) != string(command) {
				t.Fatalf("flat entry %v", flat)
			}
			_, hasServers := mcp["servers"]
			codemode, hasCodemode := flat["codemode"]
			if tc.native {
				native := mcp["servers"].(map[string]any)["lugarn"].(map[string]any)
				if len(mcp) != 2 || codemode != false || native["codemode"] != false || native["disabled"] != false || !reflect.DeepEqual(native["command"], flat["command"]) {
					t.Fatalf("2.x mcp = %v", mcp)
				}
			} else if len(mcp) != 1 || hasServers || hasCodemode || len(flat) != 3 {
				t.Fatalf("1.x mcp = %v", mcp)
			}
		})
	}
	for version, code := range map[string]model.ErrorCode{
		"":      model.ErrorInvalidRequest,
		"next":  model.ErrorInvalidRequest,
		"3.0.0": model.ErrorHarnessUnavailable,
		"0.9.1": model.ErrorHarnessUnavailable,
		"12.1":  model.ErrorHarnessUnavailable,
	} {
		l := testLaunch(t, model.HarnessOpenCode)
		l.Version = version
		err := WriteFiles(l)
		if p := model.AsError(err); p == nil || p.Code != code {
			t.Fatalf("version %q: %v, want %s", version, err, code)
		}
		if _, statErr := os.Stat(l.path(".opencode.json")); !os.IsNotExist(statErr) {
			t.Fatalf("version %q wrote a config", version)
		}
	}
	// Other harnesses don't need a version.
	for _, h := range []model.Harness{model.HarnessClaude, model.HarnessCodex, model.HarnessPi} {
		if err := WriteFiles(testLaunch(t, h)); err != nil {
			t.Fatalf("%s without a version: %v", h, err)
		}
	}
	l := testLaunch(t, model.HarnessCodex)
	l.Version = "1.0\n--evil"
	if l.Validate() == nil {
		t.Fatal("accepted a version with a newline")
	}
}
