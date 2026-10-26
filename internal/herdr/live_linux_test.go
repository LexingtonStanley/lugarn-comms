//go:build linux

package herdr

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// The live tests run only with LUGARN_HERDR_LIVE=1, against a private Herdr
// server each starts under a fresh lugarn-test-<random> session and stops by
// that name at the end, failure or not. They never name any other session.

func init() {
	personas["pi"] = fakeAgent
	helpers["start-server"] = startServerHelper
	helpers["sleep"] = func() int { time.Sleep(2 * time.Minute); return 0 }
}

func liveBinary(t *testing.T) string {
	t.Helper()
	if os.Getenv("LUGARN_HERDR_LIVE") != "1" {
		t.Skip("set LUGARN_HERDR_LIVE=1 to run against a private Herdr server")
	}
	bin, err := exec.LookPath("herdr")
	if err != nil {
		t.Skip("herdr isn't installed")
	}
	return bin
}

// newSession is lugarn-test- and 10 random lower-case alphanumerics.
func newSession(t *testing.T) string {
	t.Helper()
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "lugarn-test-" + string(b)
}

// privateClient refuses any session that isn't a fresh test session, and one
// that already answers: it must be ours alone.
func privateClient(t *testing.T, ctx context.Context, bin, session string) *Client {
	t.Helper()
	if !strings.HasPrefix(session, "lugarn-test-") || len(session) < len("lugarn-test-")+8 {
		t.Fatalf("refusing session %q", session)
	}
	c := New(bin, session)
	st, err := c.Status(ctx)
	if err != nil || st.Running {
		t.Fatalf("fresh session %s already answers (%+v, %v): not ours, not touching it", session, st, err)
	}
	return c
}

// stopServer stops exactly this session's server, by name, then by the pid
// we started if it lingers, and deletes the stopped session's directory.
func stopServer(t *testing.T, c *Client, pid int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := c.call(ctx, 0, "server stop", "server", "stop")
	t.Logf("herdr --session %s server stop: %s %v", c.session, strings.TrimSpace(string(out)), err)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := c.Status(ctx); err == nil && !st.Running && (pid == 0 || !processAlive(pid)) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pid > 0 && processAlive(pid) {
		t.Logf("server pid %d still alive after stop; killing it", pid)
		killProcess(pid)
	}
	st, err := c.Status(ctx)
	t.Logf("after stop: session %s running=%v (%v); pid %d alive=%v", c.session, st.Running, err, pid, pid > 0 && processAlive(pid))
	out, err = c.call(ctx, 0, "session delete", "session", "delete", c.session)
	t.Logf("herdr --session %s session delete %s: %s %v", c.session, c.session, strings.TrimSpace(string(out)), err)
}

// copyHelper copies the test binary to dir/name, a path of plain words.
func copyHelper(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	copyExecutable(t, path)
	return path
}

func waitFile(t *testing.T, path string, min int) []byte {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil && len(b) >= min {
			return b
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %d bytes, %v", path, len(b), err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestLiveHerdr(t *testing.T) {
	bin := liveBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	session := newSession(t)
	c := privateClient(t, ctx, bin, session)
	t.Logf("private session %s", session)

	res, err := StartServer(ctx, bin, session)
	t.Cleanup(func() { stopServer(t, c, res.PID) })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("StartServer: started=%v method=%s pid=%d running=%v version=%s socket=%s",
		res.Started, res.Method, res.PID, res.Status.Running, res.Status.ServerVersion, res.Status.Socket)
	if !res.Started || res.Method != MethodDetached || !res.Status.Running {
		t.Fatalf("StartServer = %+v", res)
	}
	if sid, _ := unix.Getsid(res.PID); sid != res.PID {
		t.Fatalf("server session %d, want %d", sid, res.PID)
	}
	again, err := StartServer(ctx, bin, session)
	if err != nil || again.Started {
		t.Fatalf("second StartServer = %+v, %v", again, err)
	}

	work := t.TempDir()
	helper := copyHelper(t, t.TempDir(), "lugarn-helper")
	weird := "a b\"c' $(touch " + filepath.Join(work, "pwned") + ") `id` %PATH% \\n ü\nsecond line"

	// The workspace's --env reaches its root pane: the runner's.
	rootOut := filepath.Join(work, "root-env.json")
	ws, err := c.CreateWorkspace(ctx, WorkspaceName("livetest"), work, map[string]string{
		"LUGARN_HERDR_HELPER": "env", "LUGARN_HERDR_HELPER_OUT": rootOut,
		"LUGARN_COMMS_MEMBER": "m-root", "LUGARN_X": weird,
	})
	if err != nil || ws.RootPane == "" || ws.RootTab == "" {
		t.Fatalf("CreateWorkspace = %+v, %v", ws, err)
	}
	t.Logf("CreateWorkspace: id %s label %q root tab %s root pane %s", ws.ID, ws.Label, ws.RootTab, ws.RootPane)
	if got, err := c.Workspace(ctx, ws.ID); err != nil || got.ID != ws.ID || got.Label != ws.Label {
		t.Fatalf("Workspace(%s) = %+v, %v", ws.ID, got, err)
	}
	_, err = c.Workspace(ctx, "w999")
	t.Logf("Workspace(w999): %v (IsGone %v)", err, IsGone(err))
	if !IsGone(err) {
		t.Fatalf("Workspace(w999): %v", err)
	}
	if err := c.RunCommand(ctx, ws.RootPane, helper); err != nil {
		t.Fatal(err)
	}
	var rootEnv map[string]string
	if err := json.Unmarshal(waitFile(t, rootOut, 2), &rootEnv); err != nil {
		t.Fatal(err)
	}
	if rootEnv["LUGARN_COMMS_MEMBER"] != "m-root" || rootEnv["LUGARN_X"] != weird {
		t.Fatalf("root pane env = %q", rootEnv)
	}
	t.Logf("root pane saw LUGARN_COMMS_MEMBER=%q LUGARN_X=%q", rootEnv["LUGARN_COMMS_MEMBER"], rootEnv["LUGARN_X"])

	// A member's tab: its --env reaches its pane, the workspace's doesn't.
	tabOut := filepath.Join(work, "tab-env.json")
	token := filepath.Join(work, "launch dir", "01J.token")
	tab, err := c.OpenTab(ctx, ws.ID, "writer", work, map[string]string{
		"LUGARN_HERDR_HELPER": "env", "LUGARN_HERDR_HELPER_OUT": tabOut,
		"LUGARN_COMMS_MEMBER": "m-tab", "LUGARN_COMMS_TOKEN_FILE": token,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("OpenTab: tab %s label %q in %s, root pane %s", tab.ID, tab.Label, tab.Workspace, tab.RootPane)
	if err := c.RunCommand(ctx, tab.RootPane, helper); err != nil {
		t.Fatal(err)
	}
	var tabEnv map[string]string
	if err := json.Unmarshal(waitFile(t, tabOut, 2), &tabEnv); err != nil {
		t.Fatal(err)
	}
	if tabEnv["LUGARN_COMMS_MEMBER"] != "m-tab" || tabEnv["LUGARN_COMMS_TOKEN_FILE"] != token {
		t.Fatalf("tab pane env = %q", tabEnv)
	}
	_, inherited := tabEnv["LUGARN_X"]
	t.Logf("tab pane %s saw LUGARN_COMMS_MEMBER=%q LUGARN_COMMS_TOKEN_FILE=%q; workspace's LUGARN_X inherited: %v",
		tab.RootPane, tabEnv["LUGARN_COMMS_MEMBER"], tabEnv["LUGARN_COMMS_TOKEN_FILE"], inherited)
	if inherited {
		t.Fatal("the tab inherited the workspace's env")
	}
	if _, err := os.Stat(filepath.Join(work, "pwned")); err == nil {
		t.Fatal("an env value was executed")
	}

	// Closing the tab closes its pane and ends its shell.
	shell := shellPID(t, ctx, c, tab.RootPane)
	if err := c.CloseTab(ctx, tab.ID); err != nil {
		t.Fatal(err)
	}
	if exists, err := c.PaneExists(ctx, tab.RootPane); exists || err != nil {
		t.Fatalf("pane after its tab closed: %v, %v", exists, err)
	}
	if !waitDead(shell) {
		t.Fatalf("the tab's shell %d survived", shell)
	}
	if err := c.CloseTab(ctx, tab.ID); err != nil {
		t.Fatalf("closing it again: %v", err)
	}
	if _, err := c.Workspace(ctx, ws.ID); err != nil {
		t.Fatalf("the workspace went with the tab: %v", err)
	}
	t.Logf("CloseTab(%s): pane %s gone, shell %d gone, second close ok, workspace %s still there", tab.ID, tab.RootPane, shell, ws.ID)

	t.Run("fake agent", func(t *testing.T) { liveFakeAgent(t, ctx, c, ws, work) })
	t.Run("names", func(t *testing.T) { liveNames(t, ctx, c, ws, work) })
	t.Run("opencode", func(t *testing.T) { liveOpenCode(t, ctx, c, ws, work) })

	// Closing the runner's root pane, the last one, ends the workspace: the
	// next start would create a new one.
	if err := c.ClosePane(ctx, ws.RootPane); err != nil {
		t.Fatal(err)
	}
	_, err = c.Workspace(ctx, ws.ID)
	t.Logf("after its last pane closed, Workspace(%s): %v", ws.ID, err)
	if !IsGone(err) {
		t.Fatalf("workspace after its last pane closed: %v", err)
	}
}

// shellPID is the pane's shell process.
func shellPID(t *testing.T, ctx context.Context, c *Client, pane string) int {
	t.Helper()
	pids := paneProcesses(t, ctx, c, pane)
	return pids[0]
}

// paneProcesses is the pane's shell, then its foreground processes.
func paneProcesses(t *testing.T, ctx context.Context, c *Client, pane string) []int {
	t.Helper()
	out, err := c.call(ctx, 0, "pane process-info", "pane", "process-info", "--pane", pane)
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Result struct {
			ProcessInfo struct {
				Shell      int `json:"shell_pid"`
				Foreground []struct {
					PID int `json:"pid"`
				} `json:"foreground_processes"`
			} `json:"process_info"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &info); err != nil || info.Result.ProcessInfo.Shell <= 0 {
		t.Fatalf("process-info: %s, %v", out, err)
	}
	pids := []int{info.Result.ProcessInfo.Shell}
	for _, p := range info.Result.ProcessInfo.Foreground {
		if p.PID != pids[0] {
			pids = append(pids, p.PID)
		}
	}
	return pids
}

func waitDead(pid int) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if !processAlive(pid) {
			return true
		}
	}
	return !processAlive(pid)
}

// liveFakeAgent starts the test binary as "pi" through a real `herdr agent
// start`, so the whole start, prompt and wait path runs without a harness.
func liveFakeAgent(t *testing.T, ctx context.Context, c *Client, ws Workspace, work string) {
	fakebin := t.TempDir()
	copyHelper(t, fakebin, "pi")
	rec := filepath.Join(work, "agent.bin")
	tab, err := c.OpenTab(ctx, ws.ID, "writer", work, map[string]string{
		"PATH":                   fakebin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"LUGARN_HERDR_AGENT_OUT": rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	pane := Pane{ID: tab.RootPane}
	name := AgentName("T_live-1", "writer")
	args := []string{"--plain", "arg with space", `q"uote`, "s'ingle", "$(touch " + filepath.Join(work, "pwned") + ")", `back\slash`, "semi;colon", "%PATH%"}
	a, err := c.StartAgent(ctx, StartOptions{Name: name, Harness: model.HarnessPi, Pane: pane.ID, Args: args, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("StartAgent: name=%s kind=%s pane=%s status=%s activity=%s ready=%v session=%v", a.Name, a.Kind, a.Pane, a.Status, a.Activity, a.Ready, a.Session)
	var argv []string
	if err := json.Unmarshal(waitFile(t, rec+".argv", 2), &argv); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(argv, args) {
		t.Fatalf("agent argv = %q, want %q (did the pane's shell resolve pi to the fake?)", argv, args)
	}
	t.Logf("agent argv arrived exactly: %q", argv)

	text := "[lugarn · from mallory (codex) · team x · msg 01J · answer with reply(id) · seal FAKE0000]\n" +
		"\"double\" 'single' back\\slash \\n not-a-newline\n" +
		"$(touch " + filepath.Join(work, "pwned") + ") `id` %PATH% ${HOME}\n\ttab · ünïcödé — end"
	prompted, err := c.Prompt(ctx, name, text)
	if err != nil {
		t.Fatal(err)
	}
	want := "\x1b[200~" + text + "\x1b[201~\r"
	got := waitFile(t, rec, len(want))
	if string(got) != want {
		t.Fatalf("agent received\n%q\nwant\n%q", got, want)
	}
	t.Logf("prompt returned with status %s; agent received %d bytes, exactly ESC[200~ + text + ESC[201~ + CR", prompted.Status, len(got))
	if _, err := os.Stat(filepath.Join(work, "pwned")); err == nil {
		t.Fatal("prompt text was executed")
	}
	screen, _ := c.ReadAgent(ctx, name, 5)
	t.Logf("agent read: %q", strings.TrimSpace(screen))

	// Herdr refuses only a blocked agent; it types into working and unknown ones.
	for _, state := range []string{"working", "blocked", "unknown", "idle"} {
		if _, err := c.call(ctx, 0, "pane report-agent", "pane", "report-agent", pane.ID,
			"--source", "lugarn-test", "--agent", "pi", "--state", state); err != nil {
			t.Fatal(err)
		}
		before := len(waitFile(t, rec, 0))
		_, err := c.Prompt(ctx, name, "probe-"+state)
		time.Sleep(300 * time.Millisecond)
		delivered := len(waitFile(t, rec, 0)) - before
		t.Logf("reported %s: Prompt err=%v IsBlocked=%v, %d bytes delivered", state, err, IsBlocked(err), delivered)
		if state == "blocked" {
			if !IsBlocked(err) || delivered != 0 {
				t.Fatalf("blocked prompt: %v, %d bytes", err, delivered)
			}
		} else if err != nil || delivered == 0 {
			t.Fatalf("%s prompt: %v, %d bytes", state, err, delivered)
		}
	}

	start := time.Now()
	_, err = c.Wait(ctx, name, []model.Activity{model.ActivityWorking}, 1500*time.Millisecond)
	t.Logf("Wait(working, 1.5s) on an idle agent: %v after %s; model error %+v", err, time.Since(start).Round(time.Millisecond), model.AsError(err))
	if !IsTimeout(err) || model.AsError(err).Code != model.ErrorTimeout {
		t.Fatalf("wait timeout: %v", err)
	}
	if a, err := c.Wait(ctx, name, nil, 5*time.Second); err != nil || a.Activity != model.ActivityIdle {
		t.Fatalf("Wait(default) = %+v, %v", a, err)
	}

	// Missing agents and vanished panes.
	_, err = c.Agent(ctx, "nosuch-agent")
	t.Logf("Agent(nosuch-agent): %v", err)
	if !IsGone(err) || model.AsError(err).Code != model.ErrorNotFound {
		t.Fatalf("missing agent: %v", err)
	}
	if _, err := c.Prompt(ctx, "nosuch-agent", "hi"); !IsGone(err) {
		t.Fatalf("prompt to missing agent: %v", err)
	}

	// The agent exits: its name goes, its pane stays (the member "exited").
	pidBytes := waitFile(t, rec+".pid", 1)
	pid, _ := strconv.Atoi(string(pidBytes))
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitGone(t, ctx, c, name)
	if exists, err := c.PaneExists(ctx, pane.ID); !exists || err != nil {
		t.Fatalf("pane after the agent exited: %v, %v", exists, err)
	}
	t.Logf("agent exited: Agent(%s) is gone, pane %s still exists", name, pane.ID)

	// Restart it, then close its pane mid-wait (the member "died").
	if err := os.Remove(rec + ".pid"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StartAgent(ctx, StartOptions{Name: name, Harness: model.HarnessPi, Pane: pane.ID, Timeout: 20 * time.Second}); err != nil {
		t.Fatal(err)
	}
	pid, _ = strconv.Atoi(string(waitFile(t, rec+".pid", 1)))
	waitErr := make(chan error, 1)
	go func() {
		_, err := c.Wait(ctx, name, []model.Activity{model.ActivityWorking}, 20*time.Second)
		waitErr <- err
	}()
	time.Sleep(time.Second)
	if err := c.ClosePane(ctx, pane.ID); err != nil {
		t.Fatal(err)
	}
	err = <-waitErr
	t.Logf("Wait while its pane closed: %v", err)
	if !IsGone(err) {
		t.Fatalf("wait across a closed pane: %v", err)
	}
	if _, err := c.Agent(ctx, name); !IsGone(err) {
		t.Fatalf("agent after its pane closed: %v", err)
	}
	if _, err := c.Pane(ctx, pane.ID); !IsGone(err) {
		t.Fatalf("closed pane: %v", err)
	}
	if exists, err := c.PaneExists(ctx, pane.ID); exists || err != nil {
		t.Fatalf("PaneExists(closed) = %v, %v", exists, err)
	}
	if err := c.ClosePane(ctx, pane.ID); err != nil {
		t.Fatalf("closing it again: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if processAlive(pid) {
		t.Fatalf("agent process %d survived its pane", pid)
	}
	t.Logf("pane closed: agent and pane gone, agent process %d gone", pid)
}

func waitGone(t *testing.T, ctx context.Context, c *Client, name string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := c.Agent(ctx, name)
		if IsGone(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent %s still there: %v", name, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// liveNames checks the derived agent names, and ValidateAgentName, against
// Herdr itself (decision 49) by renaming a live agent, and workspace labels at
// the spec's longest.
func liveNames(t *testing.T, ctx context.Context, c *Client, ws Workspace, work string) {
	fakebin := t.TempDir()
	copyHelper(t, fakebin, "pi")
	tab, err := c.OpenTab(ctx, ws.ID, "names", work, map[string]string{
		"PATH":                   fakebin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"LUGARN_HERDR_AGENT_OUT": filepath.Join(work, "names.bin"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseTab(ctx, tab.ID)
	if _, err := c.StartAgent(ctx, StartOptions{Name: "namecheck", Harness: model.HarnessPi, Pane: tab.RootPane, Timeout: 20 * time.Second}); err != nil {
		t.Fatal(err)
	}
	longMember := "abcdefghijklmnopqrstuvwxyz012345"
	derived := []string{
		AgentName(strings.Repeat("Z", 64), longMember),
		AgentName("0team", "a"),
		AgentName("T_1", "abcdefghijklmnopqrstuvwx-yz"),
		AgentName("auth-rewrite", "reviewer"),
	}
	for _, name := range append(derived, "a", strings.Repeat("a", 32), strings.Repeat("a", 33),
		"0team-writer", "UPPER", "-lead", strings.Repeat("t", 48)+"-"+longMember) {
		_, err := c.call(ctx, 0, "agent rename", "agent", "rename", tab.RootPane, name)
		herdrOK := err == nil
		t.Logf("agent rename to %q (%d bytes): Herdr accepts=%v; ValidateAgentName accepts=%v %v", name, len(name), herdrOK, ValidateAgentName(name) == nil, err)
		if herdrOK != (ValidateAgentName(name) == nil) {
			t.Fatalf("ValidateAgentName disagrees with Herdr on %q", name)
		}
	}
	for _, name := range derived {
		if ValidateAgentName(name) != nil {
			t.Fatalf("derived name %q refused", name)
		}
	}
	for _, team := range []string{strings.Repeat("t", 48), "0digit-team"} {
		label := WorkspaceName(team)
		w, err := c.CreateWorkspace(ctx, label, work, nil)
		if err != nil || w.Label != label {
			t.Fatalf("workspace %q: %+v %v", label, w, err)
		}
		t.Logf("workspace label %q (%d bytes) accepted as %s", label, len(label), w.ID)
		if err := c.ClosePane(ctx, w.RootPane); err != nil {
			t.Fatal(err)
		}
	}
}

// liveOpenCode starts a real harness that reaches its idle prompt without a
// model call: OpenCode's TUI with --standalone, so no background service
// outlives the pane. It sends no prompt.
func liveOpenCode(t *testing.T, ctx context.Context, c *Client, ws Workspace, work string) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode isn't installed")
	}
	tab, err := c.OpenTab(ctx, ws.ID, "opencode", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	name := AgentName("T_live-1", "opencode")
	a, err := c.StartAgent(ctx, StartOptions{Name: name, Harness: model.HarnessOpenCode, Pane: tab.RootPane, Args: []string{"--standalone"}, Timeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Agent(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("opencode %s: start status=%s; agent get: kind=%s status=%s activity=%s ready=%v session=%v", name, a.Status, got.Kind, got.Status, got.Activity, got.Ready, got.Session)
	if got.Kind != "opencode" || got.Pane != tab.RootPane {
		t.Fatalf("agent = %+v", got)
	}
	pids := paneProcesses(t, ctx, c, tab.RootPane)
	if err := c.CloseTab(ctx, tab.ID); err != nil {
		t.Fatal(err)
	}
	for _, pid := range pids {
		dead := waitDead(pid)
		t.Logf("opencode tab process %d gone after CloseTab: %v", pid, dead)
		if !dead {
			t.Fatalf("process %d survived its tab", pid)
		}
	}
	if _, err := c.Agent(ctx, name); !IsGone(err) {
		t.Fatalf("opencode after close: %v", err)
	}
}

// fakeAgent is a stand-in interactive agent: it asks for bracketed paste,
// puts its terminal in raw mode, and appends every byte it reads to
// $LUGARN_HERDR_AGENT_OUT, its argv to .argv and its pid to .pid.
func fakeAgent(string) int {
	out := os.Getenv("LUGARN_HERDR_AGENT_OUT")
	argv, _ := json.Marshal(os.Args[1:])
	_ = os.WriteFile(out+".argv", argv, 0o600)
	_ = os.WriteFile(out+".pid", []byte(strconv.Itoa(os.Getpid())), 0o600)
	if t, err := unix.IoctlGetTermios(0, unix.TCGETS); err == nil {
		raw := *t
		raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
		raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		raw.Cflag |= unix.CS8
		raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0
		_ = unix.IoctlSetTermios(0, unix.TCSETS, &raw)
		defer unix.IoctlSetTermios(0, unix.TCSETS, t)
	}
	f, err := os.OpenFile(out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}
	defer f.Close()
	os.Stdout.WriteString("\x1b[?2004hlugarn fake agent ready\r\n> ")
	buf := make([]byte, 64<<10)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			_, _ = f.Write(buf[:n])
			fmt.Printf("got %d bytes\r\n> ", n)
		}
		if err != nil {
			return 0
		}
	}
}

// startServerHelper plays the SSH session: it runs as the leader of a session
// with a controlling terminal, starts a plain child in its own process group
// (the control), calls StartServer, reports, and exits.
func startServerHelper() int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	exe, _ := os.Executable()
	control := exec.Command(exe)
	control.Env = append(os.Environ(), "LUGARN_HERDR_HELPER=sleep")
	_ = control.Start()
	res, err := StartServer(ctx, os.Getenv("LUGARN_HERDR_HELPER_BIN"), os.Getenv("LUGARN_HERDR_HELPER_SESSION"))
	report := map[string]any{"helper_pid": os.Getpid(), "control_pid": 0, "server_pid": res.PID, "started": res.Started, "method": res.Method}
	if control.Process != nil {
		report["control_pid"] = control.Process.Pid
	}
	if err != nil {
		report["error"] = err.Error()
	}
	b, _ := json.Marshal(report)
	_ = os.WriteFile(os.Getenv("LUGARN_HERDR_HELPER_OUT"), b, 0o600)
	return 0
}

// TestLiveServerSurvivesSessionEnd starts a server from a process that leads
// a session with a controlling terminal, as an SSH login does, then ends that
// session: the leader exits and the terminal hangs up. A plain child of the
// leader dies of the hangup; the server keeps answering.
func TestLiveServerSurvivesSessionEnd(t *testing.T) {
	bin := liveBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session := newSession(t)
	c := privateClient(t, ctx, bin, session)
	t.Logf("private session %s", session)
	serverPID := 0
	t.Cleanup(func() { stopServer(t, c, serverPID) })

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetUint32(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	tty, err := os.OpenFile("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, master) }()

	out := filepath.Join(t.TempDir(), "start.json")
	helper := exec.Command(copyHelper(t, t.TempDir(), "lugarn-helper"))
	helper.Env = append(os.Environ(), "LUGARN_HERDR_HELPER=start-server", "LUGARN_HERDR_HELPER_BIN="+bin,
		"LUGARN_HERDR_HELPER_SESSION="+session, "LUGARN_HERDR_HELPER_OUT="+out)
	helper.Stdin, helper.Stdout, helper.Stderr = tty, tty, tty
	helper.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	helperSID, _ := unix.Getsid(helper.Process.Pid)
	waitErr := helper.Wait()
	tty.Close()
	master.Close()

	var report struct {
		HelperPID  int    `json:"helper_pid"`
		ControlPID int    `json:"control_pid"`
		ServerPID  int    `json:"server_pid"`
		Started    bool   `json:"started"`
		Method     string `json:"method"`
		Error      string `json:"error"`
	}
	if err := json.Unmarshal(waitFile(t, out, 2), &report); err != nil {
		t.Fatal(err)
	}
	serverPID = report.ServerPID
	t.Logf("helper %d (session %d, exit %v) reported %+v", report.HelperPID, helperSID, waitErr, report)
	if report.Error != "" || !report.Started || report.Method != string(MethodDetached) {
		t.Fatalf("StartServer in the helper: %+v", report)
	}

	time.Sleep(2 * time.Second)
	controlAlive := report.ControlPID > 0 && processAlive(report.ControlPID)
	if controlAlive {
		killProcess(report.ControlPID)
	}
	st, err := c.Status(ctx)
	serverSID, _ := unix.Getsid(serverPID)
	ppid := parentPID(serverPID)
	t.Logf("after the session ended: control child %d alive=%v; server %d alive=%v sid=%d ppid=%d; status running=%v (%v)",
		report.ControlPID, controlAlive, serverPID, processAlive(serverPID), serverSID, ppid, st.Running, err)
	if controlAlive {
		t.Fatal("the control child survived the hangup, so this test shows nothing")
	}
	if err != nil || !st.Running || !processAlive(serverPID) || serverSID != serverPID || ppid == report.HelperPID {
		t.Fatal("the server didn't survive its parent's session ending")
	}
}

func parentPID(pid int) int {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return -1
	}
	// pid (comm) state ppid …; comm may hold spaces, so split after ")".
	s := string(b)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(fields) < 2 {
		return -1
	}
	ppid, _ := strconv.Atoi(fields[1])
	return ppid
}
