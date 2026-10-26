package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/google/uuid"
)

// This server is the test binary itself, reached through the production mcp
// registration argv. It needs no inherited environment (Codex strips it).
func serveFakeMCP() int {
	token := ""
	member := ""
	for i := 2; i+1 < len(os.Args); i++ {
		switch os.Args[i] {
		case "--token-file":
			token = os.Args[i+1]
		case "--member":
			member = os.Args[i+1]
		}
	}
	if token == "" || member == "" {
		return 2
	}
	log, err := os.OpenFile(strings.TrimSuffix(token, ".token")+".mcp.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 2
	}
	defer log.Close()
	record := func(method string) {
		fmt.Fprintf(log, "%s pid=%d member=%s\n", method, os.Getpid(), member)
		_ = log.Sync()
	}
	record("start")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 65536), 1<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			return 2
		}
		if len(request.ID) == 0 {
			continue
		}
		record(request.Method + " " + request.Params.Name)
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "lugarn-harness-fake", "version": "1"}}
		case "tools/list":
			var tools []any
			for _, name := range commsTools {
				tools = append(tools, map[string]any{"name": name, "description": "Lugarn " + name + ". Call with an empty object for this test.", "annotations": map[string]any{"readOnlyHint": name == "team" || name == "inbox"}, "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}})
			}
			result = map[string]any{"tools": tools}
		case "tools/call":
			mode, _ := os.ReadFile(token)
			if string(mode) == "block\n" {
				time.Sleep(45 * time.Second)
			}
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": "ok"}}}
		case "ping":
			result = map[string]any{}
		default:
			result = map[string]any{}
		}
		if encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}) != nil {
			return 2
		}
	}
	return 0
}

// TestLive is deliberately opt-in. Each harness gets only two tiny completed
// prompts and one cancelled prompt. No pane or Herdr server is started.
func TestLive(t *testing.T) {
	if os.Getenv("LUGARN_HARNESS_LIVE") != "1" {
		t.Skip("set LUGARN_HARNESS_LIVE=1 to spend live harness budget")
	}
	for _, harness := range []model.Harness{model.HarnessClaude, model.HarnessCodex, model.HarnessOpenCode} {
		t.Run(string(harness), func(t *testing.T) {
			l := testLaunch(t, harness)
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			l.Binary = binary
			id, err := model.NewULID()
			if err != nil {
				t.Fatal(err)
			}
			l.LaunchID = id
			if harness == model.HarnessClaude {
				l.SessionID = newTestUUID(t)
				l.Model = "haiku"
			}
			if harness == model.HarnessCodex {
				l.Model = "gpt-6-luna"
			}
			if harness == model.HarnessOpenCode {
				l.Model = "opencode-go/iceman-contributor-free"
			}
			if err = statedir.Ensure(l.StateDir); err != nil {
				t.Fatal(err)
			}
			if err = statedir.WriteSecret(l.TokenPath(), []byte("test-only-token\n")); err != nil {
				t.Fatal(err)
			}
			logPath := l.path(".mcp.log")
			turns := []string{"first", "resumed"}
			if os.Getenv("LUGARN_HARNESS_LIVE_CANCEL_ONLY") == "1" {
				turns = nil
			}
			for _, turn := range turns {
				prompt := "Call the lugarn team MCP tool once with {} now, even if you called it earlier. This is the " + turn + " turn. Reply with the word ok."
				args, err := Args(l, model.ModeHeadless)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("%s %s argv=%q; stdin=%q", harness, turn, args, prompt)
				before := countCalls(logPath)
				ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
				handle, err := Start(ctx, l, prompt)
				if err != nil {
					cancel()
					t.Fatal(err)
				}
				t.Cleanup(handle.Cancel)
				outcome := handle.Wait()
				cancel()
				usage, _ := json.Marshal(outcome.Usage)
				t.Logf("%s %s pid=%d exit=%d acknowledged=%t session=%s final=%q status=%s error=%q usage=%s", harness, turn, handle.PID, outcome.ExitCode, outcome.Acknowledged, outcome.SessionID, outcome.FinalText, Status(outcome), outcome.Error, usage)
				log, _ := os.ReadFile(logPath)
				t.Logf("MCP log:\n%s", log)
				if Status(outcome) != model.TurnStatusSucceeded || countCalls(logPath) <= before || !strings.Contains(string(log), "tools/list") {
					t.Fatalf("turn failed or team tool not called: %+v", outcome)
				}
				if turn == "resumed" && outcome.SessionID != l.SessionID {
					t.Fatalf("resume changed session %s -> %s", l.SessionID, outcome.SessionID)
				}
				if outcome.SessionID == "" || strings.TrimSpace(outcome.FinalText) == "" {
					t.Fatal("missing session or text")
				}
				l.SessionID = outcome.SessionID
				l.Resume = true
			}
			if err = statedir.WriteSecret(l.TokenPath(), []byte("block\n")); err != nil {
				t.Fatal(err)
			}
			before := countLists(logPath)
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			handle, err := Start(ctx, l, "Call the lugarn team MCP tool again now with {}, even if you called it earlier. This is the cancellation test. Reply with the word ok.")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(handle.Cancel)
			deadline := time.Now().Add(120 * time.Second)
			for countLists(logPath) <= before && time.Now().Before(deadline) {
				select {
				case <-handle.done:
					t.Fatalf("cancel turn ended early: %+v", handle.Wait())
				default:
				}
				time.Sleep(25 * time.Millisecond)
			}
			if countLists(logPath) <= before {
				handle.Cancel()
				t.Fatal("cancel turn never listed MCP tools")
			}
			pids, err := liveProcessIDs(handle)
			if err != nil {
				handle.Cancel()
				t.Fatal(err)
			}
			// Check the actual MCP pid too, even if a harness detached it.
			log, _ := os.ReadFile(logPath)
			for _, line := range strings.Split(string(log), "\n") {
				if !strings.HasPrefix(line, "tools/list ") {
					continue
				}
				fields := strings.Fields(line)
				pid, e := strconv.Atoi(strings.TrimPrefix(fields[1], "pid="))
				if e != nil {
					t.Fatal(e)
				}
				found := false
				for _, existing := range pids {
					if existing == pid {
						found = true
					}
				}
				if !found {
					pids = append(pids, pid)
				}
			}
			for _, pid := range pids {
				t.Cleanup(func() {
					if !processDead(pid) {
						process, _ := os.FindProcess(pid)
						if process != nil {
							_ = process.Kill()
						}
					}
				})
			}
			t.Logf("%s cancelling pid=%d tree=%v after MCP tools/list (before turn completion)", harness, handle.PID, pids)
			handle.Cancel()
			outcome := waitBounded(t, handle)
			for _, pid := range pids {
				assertDead(t, pid)
			}
			if Status(outcome) != model.TurnStatusCancelled {
				t.Fatal(outcome)
			}
			t.Logf("%s cancelled: exit=%d acknowledged=%t session=%s status=%s; all %d processes dead", harness, outcome.ExitCode, outcome.Acknowledged, outcome.SessionID, Status(outcome), len(pids))
		})
	}
}

func countLists(path string) int {
	b, _ := os.ReadFile(path)
	return strings.Count(string(b), "tools/list ")
}

func countCalls(path string) int {
	b, _ := os.ReadFile(path)
	return strings.Count(string(b), "tools/call team ")
}

func newTestUUID(t *testing.T) string { t.Helper(); return uuid.NewString() }
