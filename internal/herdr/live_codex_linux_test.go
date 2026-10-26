//go:build linux

package herdr

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func init() {
	// Codex starts the launch's MCP server: here a stub that exits at once,
	// so the copy never runs the test suite.
	personas["lugarn-mcp-stub"] = func(string) int { return 0 }
}

// TestLiveCodexPaneLeavesNoDaemon launches a real Codex pane exactly as the
// runner would (harness.Args for a resumed pane, through herdr agent start in
// a member tab), then closes the tab: no `codex app-server` may appear or
// remain. It resumes a thread of the person's own, so no model call is made.
func TestLiveCodexPaneLeavesNoDaemon(t *testing.T) {
	bin := liveBinary(t)
	thread, dir := os.Getenv("LUGARN_HERDR_LIVE_CODEX_THREAD"), os.Getenv("LUGARN_HERDR_LIVE_CODEX_DIR")
	if thread == "" || dir == "" {
		t.Skip("set LUGARN_HERDR_LIVE_CODEX_THREAD to a Codex thread id of yours and LUGARN_HERDR_LIVE_CODEX_DIR to its folder")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex isn't installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	session := newSession(t)
	c := privateClient(t, ctx, bin, session)
	res, err := StartServer(ctx, bin, session)
	t.Cleanup(func() { stopServer(t, c, res.PID) })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("private session %s, server pid %d", session, res.PID)

	before := appServers()
	t.Logf("codex app-server processes before: %v", before)
	ws, err := c.CreateWorkspace(ctx, WorkspaceName("livetest"), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	launchID, err := model.NewULID()
	if err != nil {
		t.Fatal(err)
	}
	l := harness.Launch{
		Harness: model.HarnessCodex, Profile: model.ProfileReadOnly, WorkDir: dir, StateDir: t.TempDir(),
		LaunchID: launchID, MemberID: "m_live", Binary: copyHelper(t, t.TempDir(), "lugarn-mcp-stub"),
		SessionID: thread, Resume: true,
	}
	args, err := harness.Args(l, model.ModePane)
	if err != nil {
		t.Fatal(err)
	}
	env, err := harness.Environment(l)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := c.OpenTab(ctx, ws.ID, "codex", dir, env)
	if err != nil {
		t.Fatal(err)
	}
	name := AgentName("T_live-1", "codex")
	a, err := c.StartAgent(ctx, StartOptions{Name: name, Harness: model.HarnessCodex, Pane: tab.RootPane, Args: args, Timeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("agent start %s --kind codex --pane %s -- %q\n-> status %s ready %v", name, tab.RootPane, args, a.Status, a.Ready)
	if args[0] != "--no-daemon" {
		t.Fatalf("pane args don't start with --no-daemon: %q", args)
	}
	shell := shellPID(t, ctx, c, tab.RootPane)
	tree := descendants(shell)
	for _, pid := range tree {
		t.Logf("pane process %d: %s", pid, cmdline(pid))
	}
	if extra := newAppServers(before, appServers()); len(extra) > 0 {
		t.Errorf("app-server processes appeared while the pane ran: %v", extra)
	}

	if err := c.CloseTab(ctx, tab.ID); err != nil {
		t.Fatal(err)
	}
	for _, pid := range tree {
		if !waitDead(pid) {
			t.Errorf("pane process %d (%s) survived its tab", pid, cmdline(pid))
		}
	}
	time.Sleep(2 * time.Second)
	after := appServers()
	t.Logf("codex app-server processes after the tab closed: %v", after)
	for pid, line := range newAppServers(before, after) {
		cwd, _ := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "cwd"))
		if cwd == dir {
			// It started from our pane's folder after our snapshot: ours.
			killProcess(pid)
			t.Errorf("our pane left %d behind (%s); killed it", pid, line)
		} else {
			t.Errorf("an app-server %d (%s, cwd %s) appeared meanwhile, not provably ours; left alone", pid, line, cwd)
		}
	}
}

// appServers is every `codex … app-server …` process by pid.
func appServers() map[int]string {
	out := map[int]string{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		line := cmdline(pid)
		fields := strings.Fields(line)
		if len(fields) > 1 && strings.Contains(filepath.Base(fields[0]), "codex") && strings.Contains(line, " app-server") {
			out[pid] = line
		}
	}
	return out
}

func newAppServers(before, after map[int]string) map[int]string {
	out := map[int]string{}
	for pid, line := range after {
		if _, ok := before[pid]; !ok {
			out[pid] = line
		}
	}
	return out
}

func cmdline(pid int) string {
	b, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\x00", " "))
}

// descendants is root and every process below it, by /proc's parent links.
func descendants(root int) []int {
	children := map[int][]int{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			parent := parentPID(pid)
			children[parent] = append(children[parent], pid)
		}
	}
	out := []int{root}
	for i := 0; i < len(out); i++ {
		out = append(out, children[out[i]]...)
	}
	return out
}
