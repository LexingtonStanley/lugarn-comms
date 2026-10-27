package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Real Herdr 0.9.5 output, trimmed, from the live session.
const (
	agentIdle = `{"agent":"pi","agent_status":"idle","cwd":"/w","focused":false,"interactive_ready":true,` +
		`"name":"alpha-writer","pane_id":"w1:p5","revision":0,"state_change_seq":3,"tab_id":"w1:t1",` +
		`"terminal_id":"term_1","workspace_id":"w1"}`
	agentCodexSession = `{"agent":"codex","agent_session":{"agent":"codex","kind":"id","source":"herdr:codex",` +
		`"value":"01a17955-5edd-75b3-a969-744e2caeb228"},"agent_status":"working","name":"alpha-cx",` +
		`"pane_id":"w1:p7","revision":0,"tab_id":"w1:t1","terminal_id":"term_2","workspace_id":"w1"}`
	paneShell = `{"agent_status":"unknown","cwd":"/w2","focused":false,"pane_id":"w1:p2","revision":0,` +
		`"tab_id":"w1:t1","terminal_id":"term_3","workspace_id":"w1"}`
)

func onlyCall(t *testing.T, f *fake) call {
	t.Helper()
	calls := f.calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %+v, want one", calls)
	}
	return calls[0]
}

func wantArgs(t *testing.T, c call, want ...string) {
	t.Helper()
	want = append([]string{"--session", "lugarn-fake"}, want...)
	if !reflect.DeepEqual(c.Args, want) {
		t.Fatalf("argv\n got %q\nwant %q", c.Args, want)
	}
}

func wantProtocol(t *testing.T, err error, code model.ErrorCode, reason string) *model.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want %s", code)
	}
	p := model.AsError(err)
	if p.Code != code {
		t.Fatalf("code = %s (%v), want %s", p.Code, err, code)
	}
	if reason != "" && p.Details["reason"] != reason {
		t.Fatalf("details = %v, want reason %s", p.Details, reason)
	}
	return p
}

func TestEveryCallNamesItsSessionAndStripsHerdrEnv(t *testing.T) {
	f := newFake(t)
	t.Setenv("HERDR_SESSION", "default")
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(t.TempDir(), "herdr.sock"))
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	f.on("agent list", ok(`{"agents":[],"type":"agent_list"}`))
	if _, err := f.client().Agents(testContext(t)); err != nil {
		t.Fatal(err)
	}
	c := onlyCall(t, f)
	wantArgs(t, c, "agent", "list")
	if len(c.HerdrEnv) != 0 {
		t.Fatalf("child saw %v", c.HerdrEnv)
	}
}

func TestCleanEnv(t *testing.T) {
	got := cleanEnv([]string{"PATH=/bin", "HERDR_SESSION=default", "herdr_x=1", "HERDRX=keep", "A=HERDR_=b"})
	want := []string{"PATH=/bin", "HERDRX=keep", "A=HERDR_=b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanEnv = %q, want %q", got, want)
	}
}

func TestStatus(t *testing.T) {
	ctx := testContext(t)
	st, err := New(filepath.Join(t.TempDir(), "no-herdr"), "lugarn-fake").Status(ctx)
	if err != nil || st.Installed || st.Running {
		t.Fatalf("missing binary: %+v, %v", st, err)
	}
	f := newFake(t)
	st, err = f.client().Status(ctx)
	if err != nil || !st.Installed || st.Running || st.Version != "0.9.5" || st.Session != "lugarn-fake" {
		t.Fatalf("not running: %+v, %v", st, err)
	}
	wantArgs(t, onlyCall(t, f), "status", "--json")
	f.on("status", response{Stdout: statusJSON("lugarn-fake", true)})
	st, err = f.client().Status(ctx)
	if err != nil || !st.Running || !st.Compatible || st.ServerVersion != "0.9.5" {
		t.Fatalf("running: %+v, %v", st, err)
	}
	info := st.Info()
	if !info.Installed || !info.Running || info.Version == nil || *info.Version != "0.9.5" || info.Server != "lugarn-fake" {
		t.Fatalf("info = %+v", info)
	}
}

func TestUnavailable(t *testing.T) {
	ctx := testContext(t)
	_, err := New(filepath.Join(t.TempDir(), "no-herdr"), "lugarn-fake").Agents(ctx)
	wantProtocol(t, err, model.ErrorHerdrUnavailable, "not_installed")
	if !IsUnavailable(err) {
		t.Fatal("not IsUnavailable")
	}

	f := newFake(t)
	f.on("agent list", fails("server_not_running", "no herdr server is running at /x/herdr.sock"))
	_, err = f.client().Agents(ctx)
	p := wantProtocol(t, err, model.ErrorHerdrUnavailable, "not_running")
	if p.Details["herdr_code"] != "server_not_running" || p.Details["herdr_message"] != nil {
		t.Fatalf("details = %v", p.Details)
	}
	if !IsUnavailable(err) || IsGone(err) {
		t.Fatal("misclassified")
	}
}

func TestNoAnswerIsTimeout(t *testing.T) {
	f := newFake(t)
	f.on("agent list", response{SleepMS: 5000, Stdout: `{"result":{"agents":[]}}`})
	c := f.client()
	c.Timeout = 300 * time.Millisecond
	start := time.Now()
	_, err := c.Agents(testContext(t))
	if !IsTimeout(err) || IsUnavailable(err) || IsGone(err) {
		t.Fatalf("misclassified timeout: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %s", d)
	}
}

func TestCallerCancellationIsReturned(t *testing.T) {
	f := newFake(t)
	f.on("agent list", response{SleepMS: 5000})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	_, err := f.client().Agents(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestSyntaxErrorDiscardsHerdrText(t *testing.T) {
	f := newFake(t)
	f.on("agent list", response{Stderr: "error: argument 6 is not valid UTF-8\nrun 'herdr --help' for usage\n", Exit: 2})
	_, err := f.client().Agents(testContext(t))
	p := wantProtocol(t, err, model.ErrorInternal, "")
	if p.Details["cause"] != "herdr_failed" || p.Details["herdr_message"] != nil {
		t.Fatalf("details = %v", p.Details)
	}
}

func TestBadArgumentsNeverRun(t *testing.T) {
	f := newFake(t)
	c := f.client()
	ctx := testContext(t)
	_, err := c.Prompt(ctx, "a", "bad\xff")
	wantProtocol(t, err, model.ErrorInvalidRequest, "not_utf8")
	_, err = c.Prompt(ctx, "a", "nul\x00byte")
	wantProtocol(t, err, model.ErrorInvalidRequest, "nul_byte")
	_, err = New(f.bin, "bad/session").Agents(ctx)
	wantProtocol(t, err, model.ErrorInvalidRequest, "herdr_session_name")
	_, err = New(f.bin, "").Agents(ctx)
	wantProtocol(t, err, model.ErrorInvalidRequest, "herdr_session_name")
	_, err = c.CreateWorkspace(ctx, "lugarn-a", "", map[string]string{"HERDR_SESSION": "default"})
	wantProtocol(t, err, model.ErrorInvalidRequest, "env_name")
	_, err = c.OpenTab(ctx, "w1", "writer", "", map[string]string{"A=B": "c"})
	wantProtocol(t, err, model.ErrorInvalidRequest, "env_name")
	_, err = c.OpenTab(ctx, "w1", "writer", "", map[string]string{"": "c"})
	wantProtocol(t, err, model.ErrorInvalidRequest, "env_name")
	if calls := f.calls(); len(calls) != 0 {
		t.Fatalf("invalid calls ran: %+v", calls)
	}
}

// Real tab create output, Herdr 0.9.5.
const tabCreated = `{"root_pane":{"agent_status":"unknown","cwd":"/t","focused":false,"pane_id":"w1:p2","revision":0,` +
	`"tab_id":"w1:t2","terminal_id":"term_4","workspace_id":"w1"},"tab":{"agent_status":"unknown","focused":false,` +
	`"label":"writer","number":2,"pane_count":1,"tab_id":"w1:t2","workspace_id":"w1"},"type":"tab_created"}`

func TestCreateWorkspaceAndFindItByID(t *testing.T) {
	ctx := testContext(t)
	f := newFake(t)
	f.on("workspace create", ok(`{"root_pane":`+paneShell+`,"tab":{"tab_id":"w1:t1","workspace_id":"w1","label":"1"},`+
		`"type":"workspace_created","workspace":{"active_tab_id":"w1:t1","agent_status":"unknown","focused":false,`+
		`"label":"lugarn-alpha","number":1,"pane_count":1,"tab_count":1,"workspace_id":"w1"}}`))
	c := f.client()
	cwd := filepath.Join(t.TempDir(), "with space")
	w, err := c.CreateWorkspace(ctx, "lugarn-alpha", cwd, map[string]string{"LUGARN_COMMS_MEMBER": "runner", "B": `x "$(y)" \n`})
	if err != nil || w.ID != "w1" || w.Label != "lugarn-alpha" || w.RootTab != "w1:t1" || w.RootPane != "w1:p2" {
		t.Fatalf("created: %+v %v", w, err)
	}
	// It always creates: there's no lookup by label.
	wantArgs(t, onlyCall(t, f), "workspace", "create", "--label", "lugarn-alpha", "--cwd", cwd,
		"--env", `B=x "$(y)" \n`, "--env", "LUGARN_COMMS_MEMBER=runner", "--no-focus")

	f.on("workspace get", ok(`{"type":"workspace_info","workspace":{"active_tab_id":"w1:t1","agent_status":"unknown",`+
		`"focused":true,"label":"lugarn-alpha","number":1,"pane_count":2,"tab_count":2,"workspace_id":"w1"}}`))
	got, err := c.Workspace(ctx, "w1")
	if err != nil || got.ID != "w1" || got.Label != "lugarn-alpha" {
		t.Fatalf("Workspace = %+v, %v", got, err)
	}
	calls := f.calls()
	wantArgs(t, calls[len(calls)-1], "workspace", "get", "w1")

	f.on("workspace get", fails("workspace_not_found", "workspace w9 not found"))
	_, err = c.Workspace(ctx, "w9")
	wantProtocol(t, err, model.ErrorNotFound, "gone")
	if !IsGone(err) {
		t.Fatal("a gone workspace isn't IsGone")
	}
}

func TestOpenTabAndClose(t *testing.T) {
	ctx := testContext(t)
	f := newFake(t)
	f.on("tab create", ok(tabCreated))
	c := f.client()
	tab, err := c.OpenTab(ctx, "w1", "writer", "/t", map[string]string{
		"LUGARN_COMMS_MEMBER": "m1", "LUGARN_COMMS_TOKEN_FILE": "/s/launch dir/01J.token"})
	if err != nil || tab != (Tab{ID: "w1:t2", Workspace: "w1", Label: "writer", RootPane: "w1:p2"}) {
		t.Fatalf("tab = %+v, %v", tab, err)
	}
	wantArgs(t, onlyCall(t, f), "tab", "create", "--workspace", "w1", "--cwd", "/t", "--label", "writer",
		"--env", "LUGARN_COMMS_MEMBER=m1", "--env", "LUGARN_COMMS_TOKEN_FILE=/s/launch dir/01J.token", "--no-focus")

	// No label or cwd: Herdr's defaults.
	if _, err := c.OpenTab(ctx, "w1", "", "", nil); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	wantArgs(t, calls[len(calls)-1], "tab", "create", "--workspace", "w1", "--no-focus")

	f.on("tab create", fails("workspace_not_found", "workspace w9 not found"))
	_, err = c.OpenTab(ctx, "w9", "writer", "", nil)
	if !IsGone(err) {
		t.Fatalf("tab in a gone workspace: %v", err)
	}
	f.on("tab create", fails("internal_error", "boom"))
	_, err = c.OpenTab(ctx, "w1", "writer", "", nil)
	wantProtocol(t, err, model.ErrorLaunchFailed, "")

	f.on("tab close", ok(`{"type":"ok"}`))
	if err := c.CloseTab(ctx, "w1:t2"); err != nil {
		t.Fatal(err)
	}
	calls = f.calls()
	wantArgs(t, calls[len(calls)-1], "tab", "close", "w1:t2")
	f.on("tab close", fails("tab_not_found", "tab w1:t2 not found"))
	if err := c.CloseTab(ctx, "w1:t2"); err != nil {
		t.Fatalf("closing a gone tab: %v", err)
	}
}

func TestPaneGoneAndClose(t *testing.T) {
	ctx := testContext(t)
	f := newFake(t)
	f.on("pane get", fails("pane_not_found", "pane w1:p2 not found"))
	f.on("pane close", fails("pane_not_found", "pane w1:p2 not found"))
	c := f.client()
	_, err := c.Pane(ctx, "w1:p2")
	wantProtocol(t, err, model.ErrorNotFound, "gone")
	if !IsGone(err) {
		t.Fatal("not IsGone")
	}
	exists, err := c.PaneExists(ctx, "w1:p2")
	if exists || err != nil {
		t.Fatalf("PaneExists = %v, %v", exists, err)
	}
	if err := c.ClosePane(ctx, "w1:p2"); err != nil {
		t.Fatalf("closing a gone pane: %v", err)
	}
	f.on("pane get", ok(`{"pane":`+paneShell+`,"type":"pane_info"}`))
	f.on("pane close", ok(`{"type":"ok"}`))
	if exists, err := c.PaneExists(ctx, "w1:p2"); !exists || err != nil {
		t.Fatalf("PaneExists = %v, %v", exists, err)
	}
	if err := c.ClosePane(ctx, "w1:p2"); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	wantArgs(t, calls[len(calls)-1], "pane", "close", "w1:p2")
}

func TestStartAgent(t *testing.T) {
	ctx := testContext(t)
	f := newFake(t)
	f.on("agent start", ok(`{"agent":`+agentIdle+`,"argv":["pi"],"type":"agent_started"}`))
	c := f.client()
	args := []string{"--plain", "arg with space", `q"uote`, "$(x)", `back\slash`, "%PATH%"}
	a, err := c.StartAgent(ctx, StartOptions{Name: "alpha-writer", Harness: model.HarnessPi, Pane: "w1:p5", Args: args})
	if err != nil || a.Name != "alpha-writer" || a.Kind != "pi" || a.Activity != model.ActivityIdle || !a.Ready {
		t.Fatalf("agent = %+v, %v", a, err)
	}
	wantArgs(t, onlyCall(t, f), append([]string{"agent", "start", "alpha-writer", "--kind", "pi", "--pane", "w1:p5",
		"--timeout", "30000", "--"}, args...)...)

	// No args: no "--".
	f = newFake(t)
	f.on("agent start", ok(`{"agent":`+agentIdle+`,"type":"agent_started"}`))
	if _, err := f.client().StartAgent(ctx, StartOptions{Name: "a", Harness: model.HarnessCodex, Pane: "w1:p5", Timeout: 45 * time.Second}); err != nil {
		t.Fatal(err)
	}
	wantArgs(t, onlyCall(t, f), "agent", "start", "a", "--kind", "codex", "--pane", "w1:p5", "--timeout", "45000")
}

func TestStartAgentBlockedAtTrustIsStarted(t *testing.T) {
	f := newFake(t)
	f.on("agent start", fails("agent_not_ready", "agent alpha-cl is blocked during startup and is not ready for prompts"))
	f.on("agent get", ok(`{"agent":{"agent":"claude","agent_status":"blocked","name":"alpha-cl","pane_id":"w1:p8",`+
		`"tab_id":"w1:t1","terminal_id":"t","workspace_id":"w1","revision":0},"type":"agent_info"}`))
	a, err := f.client().StartAgent(testContext(t), StartOptions{Name: "alpha-cl", Harness: model.HarnessClaude, Pane: "w1:p8"})
	if err != nil || a.Activity != model.ActivityBlocked || a.Kind != "claude" {
		t.Fatalf("agent = %+v, %v", a, err)
	}
}

func TestStartAgentFailures(t *testing.T) {
	ctx := testContext(t)
	for _, tc := range []struct {
		answer  response
		code    model.ErrorCode
		timeout bool
	}{
		{fails("agent_pane_busy", "agent target pane w1:p7 is not an available shell"), model.ErrorLaunchFailed, false},
		{fails("agent_name_taken", "agent name alpha-cx is already used"), model.ErrorLaunchFailed, false},
		{fails("timeout", "timed out waiting for agent startup"), model.ErrorLaunchFailed, true},
		{fails("agent_pane_not_found", "agent target pane w1:p99 not found"), model.ErrorNotFound, false},
		{fails("server_not_running", "no server"), model.ErrorHerdrUnavailable, false},
	} {
		f := newFake(t)
		f.on("agent start", tc.answer)
		// agent_not_ready with no agent behind it stays an error too.
		_, err := f.client().StartAgent(ctx, StartOptions{Name: "beta", Harness: model.HarnessClaude, Pane: "w1:p1"})
		p := wantProtocol(t, err, tc.code, "")
		if IsTimeout(err) != tc.timeout {
			t.Fatalf("%v: IsTimeout = %v", err, IsTimeout(err))
		}
		var he *Error
		if !errors.As(err, &he) || he.Code == "" || p.Details["herdr_code"] != he.Code {
			t.Fatalf("%v: herdr code lost: %v", err, p.Details)
		}
	}

	f := newFake(t)
	c := f.client()
	for _, o := range []StartOptions{
		{Name: "0team-writer", Harness: model.HarnessPi, Pane: "w1:p1"},
		{Name: strings.Repeat("a", 33), Harness: model.HarnessPi, Pane: "w1:p1"},
		{Name: "ok", Harness: "gemini", Pane: "w1:p1"},
		{Name: "ok", Harness: model.HarnessPi, Pane: "w1:p1", Timeout: 3 * time.Second},
		{Name: "ok", Harness: model.HarnessPi, Pane: "w1:p1", Timeout: 301 * time.Second},
	} {
		_, err := c.StartAgent(ctx, o)
		wantProtocol(t, err, model.ErrorInvalidRequest, "")
	}
	if calls := f.calls(); len(calls) != 0 {
		t.Fatalf("invalid starts ran: %+v", calls)
	}
}

func TestAgentRecords(t *testing.T) {
	f := newFake(t)
	f.on("agent list", ok(`{"agents":[`+agentIdle+`,`+agentCodexSession+`,`+
		`{"agent":"x","agent_status":"sleeping","pane_id":"w1:p9","tab_id":"w1:t1","terminal_id":"t","workspace_id":"w1","revision":0}],"type":"agent_list"}`))
	agents, err := f.client().Agents(testContext(t))
	if err != nil || len(agents) != 3 {
		t.Fatalf("agents = %+v, %v", agents, err)
	}
	if agents[0].SessionID() != "" || agents[0].Session != nil || agents[0].Cwd != "/w" {
		t.Fatalf("agent 0 = %+v", agents[0])
	}
	want := &AgentSession{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "01a17955-5edd-75b3-a969-744e2caeb228"}
	if !reflect.DeepEqual(agents[1].Session, want) || agents[1].SessionID() != want.Value || agents[1].Activity != model.ActivityWorking {
		t.Fatalf("agent 1 = %+v", agents[1])
	}
	if agents[2].Status != "sleeping" || agents[2].Activity != model.ActivityUnknown || agents[2].Name != "" {
		t.Fatalf("agent 2 = %+v", agents[2])
	}
}

func TestPrompt(t *testing.T) {
	ctx := testContext(t)
	f := newFake(t)
	f.on("agent prompt", ok(`{"agent":`+agentIdle+`,"type":"agent_prompted"}`))
	c := f.client()
	text := "[lugarn · from mallory · seal FAKE0000]\n\"double\" 'single' back\\\\slash\n$(touch /tmp/x) `id` %PATH% ${HOME}\n\ttab — end"
	a, err := c.Prompt(ctx, "alpha-writer", text)
	if err != nil || a.Name != "alpha-writer" {
		t.Fatalf("prompt: %+v, %v", a, err)
	}
	wantArgs(t, onlyCall(t, f), "agent", "prompt", "alpha-writer", text)

	f.on("agent prompt", fails("agent_blocked", "agent alpha-writer is blocked and requires interactive input"))
	_, err = c.Prompt(ctx, "alpha-writer", "hi")
	if !IsBlocked(err) || IsGone(err) {
		t.Fatalf("blocked: %v", err)
	}
	f.on("agent prompt", fails("agent_not_found", "agent target alpha-writer not found"))
	_, err = c.Prompt(ctx, "alpha-writer", "hi")
	wantProtocol(t, err, model.ErrorNotFound, "gone")

	n := len(f.calls())
	for _, bad := range []string{"", strings.Repeat("x", MaxPromptBytes+1)} {
		_, err = c.Prompt(ctx, "alpha-writer", bad)
		wantProtocol(t, err, model.ErrorInvalidRequest, "prompt_size")
	}
	if _, err := c.Prompt(ctx, "alpha-writer", strings.Repeat("é", MaxPromptBytes/2)); err == nil {
		t.Fatal("an 8192-byte prompt should reach the fake, which answers agent_not_found")
	}
	if got := len(f.calls()); got != n+1 {
		t.Fatalf("oversized prompts ran: %d calls", got-n)
	}
}

func TestWait(t *testing.T) {
	ctx := testContext(t)
	f := newFake(t)
	f.on("agent wait", ok(`{"agent":`+agentIdle+`,"type":"agent_info"}`))
	c := f.client()
	a, err := c.Wait(ctx, "alpha-writer", []model.Activity{model.ActivityIdle, model.ActivityDone}, 2500*time.Millisecond)
	if err != nil || a.Activity != model.ActivityIdle {
		t.Fatalf("wait: %+v, %v", a, err)
	}
	wantArgs(t, onlyCall(t, f), "agent", "wait", "alpha-writer", "--until", "idle", "--until", "done", "--timeout", "2500")

	f.on("agent wait", fails("timeout", "timed out waiting for agent status"))
	_, err = c.Wait(ctx, "alpha-writer", nil, time.Second)
	wantProtocol(t, err, model.ErrorTimeout, "")
	if !IsTimeout(err) {
		t.Fatal("not IsTimeout")
	}
	calls := f.calls()
	wantArgs(t, calls[len(calls)-1], "agent", "wait", "alpha-writer", "--timeout", "1000")

	f.on("agent wait", fails("agent_not_running", "agent is no longer running in the target pane"))
	_, err = c.Wait(ctx, "alpha-writer", nil, time.Second)
	if !IsGone(err) {
		t.Fatalf("exited mid-wait: %v", err)
	}
	_, err = c.Wait(ctx, "alpha-writer", nil, 0)
	wantProtocol(t, err, model.ErrorInvalidRequest, "")
	_, err = c.Wait(ctx, "alpha-writer", []model.Activity{"asleep"}, time.Second)
	wantProtocol(t, err, model.ErrorInvalidRequest, "")
}

func TestReadAgent(t *testing.T) {
	f := newFake(t)
	f.on("agent read", response{Stdout: "line one\nline two\n"})
	got, err := f.client().ReadAgent(testContext(t), "alpha-writer", 50)
	if err != nil || got != "line one\nline two\n" {
		t.Fatalf("read = %q, %v", got, err)
	}
	wantArgs(t, onlyCall(t, f), "agent", "read", "alpha-writer", "--source", "recent-unwrapped", "--lines", "50")
}

func TestRunCommand(t *testing.T) {
	ctx := testContext(t)
	f := newFake(t)
	f.on("pane run", response{})
	c := f.client()
	if err := c.RunCommand(ctx, "w1:p1", "/home/u/.local/bin/lugarn-comms", "run", "--team", "T_1-x"); err != nil {
		t.Fatal(err)
	}
	wantArgs(t, onlyCall(t, f), "pane", "run", "w1:p1", "/home/u/.local/bin/lugarn-comms run --team T_1-x")
	for _, argv := range [][]string{
		nil,
		{"/path with space/lugarn-comms"},
		{"echo", `"quoted"`},
		{"echo", "$(x)"},
		{"echo", "a;b"},
		{"-rf"},
		{"lugarn-comms", "--team=x"},
		{"echo", "%PATH%"},
		{"echo", "a,b"},
		{"echo", "@args"},
		{"echo", "~/x"},
	} {
		wantProtocol(t, c.RunCommand(ctx, "w1:p1", argv...), model.ErrorInvalidRequest, "")
	}
	if n := len(f.calls()); n != 1 {
		t.Fatalf("refused commands ran: %d calls", n)
	}
	short := `C:\Users\RUNNER~1\AppData\Local\Temp\lugarn-comms.exe`
	err := c.RunCommand(ctx, "w1:p1", short, "run")
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("8.3 short path refused: %v", err)
		}
		wantProtocol(t, c.RunCommand(ctx, "w1:p1", `~\x.exe`), model.ErrorInvalidRequest, "")
		wantProtocol(t, c.RunCommand(ctx, "w1:p1", `C:\a\~1\x.exe`), model.ErrorInvalidRequest, "")
	} else {
		wantProtocol(t, err, model.ErrorInvalidRequest, "")
	}
}

func TestRenameDetectedAgent(t *testing.T) {
	f := newFake(t)
	f.on("agent rename", ok(`{"type":"agent_renamed"}`))
	mustRename := f.client().RenameAgent(testContext(t), "w1:p5", "writer-k2m9xa")
	if mustRename != nil {
		t.Fatal(mustRename)
	}
	wantArgs(t, onlyCall(t, f), "agent", "rename", "w1:p5", "writer-k2m9xa")
	f.on("agent rename", fails("agent_name_taken", "name already used"))
	if e := f.client().RenameAgent(testContext(t), "w1:p5", "writer-k2m9xa"); e == nil {
		t.Fatal("ignored rename failure")
	}
	before := len(f.calls())
	if e := f.client().RenameAgent(testContext(t), "w1:p5", "invalid name"); e == nil {
		t.Fatal("accepted invalid name")
	}
	if len(f.calls()) != before {
		t.Fatal("ran Herdr for invalid name")
	}
}

func TestHerdrErrorsNeverExposeProcessPayload(t *testing.T) {
	const sentinel = "SENTINEL_HERDR_ENV_ARGV_OUTPUT"
	t.Setenv("LUGARN_PRIVATE_TEST", sentinel)
	for _, answer := range []response{
		{Stderr: sentinel, Exit: 2},
		fails("timeout", sentinel),
		fails(sentinel, sentinel),
		{Stdout: sentinel},
		{Stdout: `{"result":{"agents":"` + sentinel + `"}}`},
	} {
		f := newFake(t)
		f.on("agent list", answer)
		_, err := f.client().Agents(testContext(t))
		if err == nil {
			t.Fatal("wanted failure")
		}
		raw, e := json.Marshal(model.AsError(err))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(err.Error()+string(raw), sentinel) {
			t.Fatal("Herdr error leaked output")
		}
	}
	f := newFake(t)
	err := f.client().RunCommand(testContext(t), "pane", sentinel+" word")
	raw, e := json.Marshal(model.AsError(err))
	if e != nil {
		t.Fatal(e)
	}
	if err == nil || strings.Contains(string(raw), sentinel) {
		t.Fatal("command argument leaked")
	}
}

func TestFutureHerdrCodesRemainIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		code string
		keep bool
	}{
		{"future_launch_refused", true}, {"a", true}, {"a" + strings.Repeat("_", 63), true},
		{"a" + strings.Repeat("_", 64), false}, {"UpperCase", false}, {"bad code", false}, {"bad\ncode", false}, {"", false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			failure := fromHerdr("agent start", tc.code, "PRIVATE_HERDR_MESSAGE").Protocol()
			got, ok := failure.Details["herdr_code"]
			if tc.keep {
				if !ok || got != tc.code {
					t.Fatal(failure.Details)
				}
			} else if ok {
				t.Fatal("malformed code retained", got)
			}
			if failure.Details["cause"] != "herdr_failed" || failure.Details["herdr_message"] != nil {
				t.Fatal(failure.Details)
			}
		})
	}
}

func TestPaneProcessInfo(t *testing.T) {
	f := newFake(t)
	f.on("pane process-info", ok(`{"process_info":{"shell_pid":123,"foreground_processes":[{"pid":124,"name":"lugarn-comms","argv":["/tmp/lugarn-comms","run","--team","t"]}]}}`))
	info, err := f.client().ProcessInfo(testContext(t), "w1:p2")
	if err != nil || info.ShellPID != 123 || len(info.Foreground) != 1 || info.Foreground[0].Argv[0] != "/tmp/lugarn-comms" {
		t.Fatal(info, err)
	}
	wantArgs(t, onlyCall(t, f), "pane", "process-info", "--pane", "w1:p2")
}
