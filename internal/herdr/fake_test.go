package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Copies of this test binary stand in for herdr (and, in the live test, for
// an interactive agent). A copy decides what it is from its own file name and
// keeps its state beside itself, not in the environment, because a server
// started through WMI gets the user's environment rather than the test's.
var personas = map[string]func(dir string) int{"herdr": fakeHerdr}

// helpers are modes a copy runs when LUGARN_HERDR_HELPER names one: the live
// test starts them in panes and sessions.
var helpers = map[string]func() int{"env": envHelper}

func TestMain(m *testing.M) {
	if exe, err := os.Executable(); err == nil {
		name := strings.TrimSuffix(filepath.Base(exe), ".exe")
		if run, ok := personas[name]; ok {
			os.Exit(run(filepath.Dir(exe)))
		}
	}
	if mode := os.Getenv("LUGARN_HERDR_HELPER"); mode != "" {
		run, ok := helpers[mode]
		if !ok {
			os.Exit(2)
		}
		os.Exit(run())
	}
	os.Exit(m.Run())
}

// envHelper writes the LUGARN_* variables it sees, as JSON, to
// LUGARN_HERDR_HELPER_OUT.
func envHelper() int {
	seen := map[string]string{}
	for _, kv := range os.Environ() {
		if name, value, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "LUGARN_") {
			seen[name] = value
		}
	}
	b, _ := json.Marshal(seen)
	if err := os.WriteFile(os.Getenv("LUGARN_HERDR_HELPER_OUT"), b, 0o600); err != nil {
		return 1
	}
	return 0
}

// response is one scripted answer.
type response struct {
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
	Exit    int    `json:"exit"`
	SleepMS int    `json:"sleep_ms"`
}

// call is one logged invocation of the fake.
type call struct {
	Args     []string `json:"args"`
	HerdrEnv []string `json:"herdr_env"`
	Dir      string   `json:"dir"`
	Path     string   `json:"path"`
}

// fakeHerdr answers from <dir>/script.json, keyed by verb ("agent prompt",
// "status", "server"). Answers for a verb are used in order; the last repeats.
// "status" without a script answers from the fake server's pid file; "server"
// writes that file and runs until <dir>/server.stop appears, or exits at once
// (<dir>/server.mode "exit"), or never answers ("silent").
func fakeHerdr(dir string) int {
	args := os.Args[1:]
	var herdrEnv []string
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(strings.ToUpper(name), "HERDR_") {
			herdrEnv = append(herdrEnv, name)
		}
	}
	wd, _ := os.Getwd()
	appendJSON(filepath.Join(dir, "calls.jsonl"), call{Args: args, HerdrEnv: herdrEnv, Dir: wd, Path: os.Getenv("PATH")})
	if len(args) < 3 || args[0] != "--session" || args[1] == "" {
		fmt.Fprintln(os.Stderr, "fake herdr: --session must come first")
		return 91
	}
	verb := args[2]
	if verb != "status" && verb != "server" && len(args) > 3 {
		verb += " " + args[3]
	}
	var script map[string][]response
	if b, err := os.ReadFile(filepath.Join(dir, "script.json")); err == nil {
		_ = json.Unmarshal(b, &script)
	}
	if answers := script[verb]; len(answers) > 0 {
		r := answers[min(nextCount(dir, verb), len(answers)-1)]
		time.Sleep(time.Duration(r.SleepMS) * time.Millisecond)
		fmt.Fprint(os.Stdout, r.Stdout)
		fmt.Fprint(os.Stderr, r.Stderr)
		return r.Exit
	}
	switch verb {
	case "status":
		running := false
		if b, err := os.ReadFile(filepath.Join(dir, "server.pid")); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				running = processAlive(pid)
			}
		}
		fmt.Println(statusJSON(args[1], running))
		return 0
	case "server":
		mode, _ := os.ReadFile(filepath.Join(dir, "server.mode"))
		switch strings.TrimSpace(string(mode)) {
		case "exit":
			fmt.Fprintln(os.Stderr, "error: fake server failed")
			return 1
		case "silent":
		default:
			_ = os.WriteFile(filepath.Join(dir, "server.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600)
		}
		for deadline := time.Now().Add(5 * time.Minute); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			if _, err := os.Stat(filepath.Join(dir, "server.stop")); err == nil {
				return 0
			}
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "unknown command: %s\n", strings.Join(args[2:], " "))
	return 2
}

func statusJSON(session string, running bool) string {
	if running {
		return `{"client":{"version":"0.9.5","channel":"stable","protocol":22,"session":"` + session + `"},` +
			`"server":{"status":"running","running":true,"version":"0.9.5","protocol":22,"compatible":true,` +
			`"socket":"/fake/` + session + `/herdr.sock","session":"` + session + `"}}`
	}
	return `{"client":{"version":"0.9.5","channel":"stable","protocol":22,"session":"` + session + `"},` +
		`"server":{"status":"not_running","running":false,"version":null,"protocol":null,"compatible":null,` +
		`"socket":"/fake/` + session + `/herdr.sock","session":"` + session + `"}}`
}

func nextCount(dir, verb string) int {
	path := filepath.Join(dir, "count-"+strings.ReplaceAll(verb, " ", "_"))
	b, _ := os.ReadFile(path)
	n, _ := strconv.Atoi(string(b))
	_ = os.WriteFile(path, []byte(strconv.Itoa(n+1)), 0o600)
	return n
}

func appendJSON(path string, v any) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(v)
}

// fake is one fake herdr install in its own directory.
type fake struct {
	t      *testing.T
	dir    string
	bin    string
	script map[string][]response
}

func newFake(t *testing.T) *fake {
	t.Helper()
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	dir := t.TempDir()
	name := "herdr"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	f := &fake{t: t, dir: dir, bin: filepath.Join(dir, name), script: map[string][]response{}}
	copyExecutable(t, f.bin)
	return f
}

func copyExecutable(t *testing.T, to string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// on scripts answers for a verb, replacing earlier ones.
func (f *fake) on(verb string, answers ...response) {
	f.t.Helper()
	f.script[verb] = answers
	b, err := json.Marshal(f.script)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "script.json"), b, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fake) calls() []call {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "calls.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var out []call
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var c call
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			f.t.Fatalf("call log: %v", err)
		}
		out = append(out, c)
	}
	return out
}

func (f *fake) client() *Client { return New(f.bin, "lugarn-fake") }

// ok is a successful answer carrying result.
func ok(result string) response {
	return response{Stdout: `{"id":"cli:fake","result":` + result + "}\n"}
}

// fails is Herdr's error answer: JSON on stderr, exit status 1.
func fails(code, message string) response {
	return response{Stderr: `{"error":{"code":"` + code + `","message":"` + message + `"},"id":"cli:fake"}` + "\n", Exit: 1}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}
