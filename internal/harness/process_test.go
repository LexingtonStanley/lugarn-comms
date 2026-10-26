package harness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

type fakeResolver struct {
	path string
	err  error
}

func (f fakeResolver) Binary(model.Harness) (string, error) { return f.path, f.err }

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		os.Exit(serveFakeMCP())
	}
	if mode := os.Getenv("LUGARN_HARNESS_HELPER"); mode != "" {
		os.Exit(helper(mode))
	}
	os.Exit(m.Run())
}

func helper(mode string) int {
	root := os.Getenv("LUGARN_HARNESS_HELPER_DIR")
	if mode == "grandchild" {
		_ = os.WriteFile(filepath.Join(root, "grandchild.pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			_ = os.WriteFile(filepath.Join(root, "heartbeat"), []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0600)
			time.Sleep(10 * time.Millisecond)
		}
		return 0
	}
	prompt, _ := io.ReadAll(os.Stdin)
	_ = os.WriteFile(filepath.Join(root, "prompt"), prompt, 0600)
	if mode == "overflow" {
		fmt.Print(strings.Repeat("x", (4<<20)+1))
		time.Sleep(20 * time.Second)
		return 0
	}
	if mode == "tree" || mode == "tree-detached" {
		child := exec.Command(os.Args[0])
		if mode == "tree-detached" {
			detachHelper(child)
		}
		child.Env = mergeEnv(os.Environ(), map[string]string{"LUGARN_HARNESS_HELPER": "grandchild"}, false)
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(root, "heartbeat")); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	b, err := os.ReadFile(filepath.Join(os.Getenv("LUGARN_HARNESS_FIXTURES"), "codex.jsonl"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if mode == "noack" {
		fmt.Println("not JSON")
		return 1
	}
	_, _ = os.Stdout.Write(b)
	if mode == "tree" || mode == "tree-detached" {
		time.Sleep(20 * time.Second)
	}
	return 0
}

func fakeDriver(t *testing.T, mode string) (Driver, Launch, string) {
	t.Helper()
	l := testLaunch(t, model.HarnessCodex)
	root := t.TempDir()
	fixtures, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	l.ExtraEnv = map[string]string{"LUGARN_HARNESS_HELPER": mode, "LUGARN_HARNESS_HELPER_DIR": root, "LUGARN_HARNESS_FIXTURES": fixtures}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Driver{Resolver: fakeResolver{path: executable}}, l, root
}

func waitBounded(t *testing.T, h *Handle) Outcome {
	t.Helper()
	done := make(chan Outcome, 1)
	go func() { done <- h.Wait() }()
	select {
	case o := <-done:
		return o
	case <-time.After(8 * time.Second):
		h.Cancel()
		t.Fatal("Wait did not return promptly")
		return Outcome{}
	}
}

func TestProcessSuccess(t *testing.T) {
	d, l, root := fakeDriver(t, "ok")
	prompt := "--flags $(nope) `nope` \"quotes\"\nnew line 🦊"
	h, err := d.Start(context.Background(), l, prompt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Cancel)
	o := waitBounded(t, h)
	if Status(o) != model.TurnStatusSucceeded || o.FinalText != "ok" || h.PID <= 0 {
		t.Fatalf("%+v", o)
	}
	e, ok := <-h.Events
	if !ok || e.SessionID != o.SessionID {
		t.Fatal(e)
	}
	if _, ok = <-h.Events; ok {
		t.Fatal("channel not closed")
	}
	b, err := os.ReadFile(filepath.Join(root, "prompt"))
	if err != nil || string(b) != prompt {
		t.Fatal(string(b), err)
	}
	*o.Usage.InputTokens = 999
	if *h.Wait().Usage.InputTokens == 999 {
		t.Fatal("Wait shares mutable outcome")
	}
	h.Cancel()
	if Status(h.Wait()) != model.TurnStatusSucceeded {
		t.Fatal("late cancellation changed result")
	}
}

func TestProcessCancellation(t *testing.T) {
	for _, kind := range []string{"explicit", "context", "deadline", "detached"} {
		t.Run(kind, func(t *testing.T) {
			mode := "tree"
			if kind == "detached" {
				mode = "tree-detached"
			}
			d, l, root := fakeDriver(t, mode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var deadlineCancel context.CancelFunc
			if kind == "deadline" {
				ctx, deadlineCancel = context.WithTimeout(context.Background(), 1800*time.Millisecond)
				defer deadlineCancel()
			}
			h, err := d.Start(ctx, l, "tiny")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(h.Cancel)
			select {
			case <-h.Events:
			case <-time.After(6 * time.Second):
				t.Fatal("no acknowledgement")
			}
			pidBytes, err := os.ReadFile(filepath.Join(root, "grandchild.pid"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(pidBytes))
			if err != nil {
				t.Fatal(err)
			}
			if kind == "explicit" || kind == "detached" {
				var wg sync.WaitGroup
				for range 8 {
					wg.Go(h.Cancel)
				}
				wg.Wait()
			} else if kind == "context" {
				cancel()
			}
			o := waitBounded(t, h)
			want := model.TurnStatusCancelled
			if kind == "deadline" {
				want = model.TurnStatusTimedOut
			}
			if Status(o) != want || !o.Cancelled {
				t.Fatalf("%+v", o)
			}
			assertDead(t, h.PID)
			assertDead(t, pid)
			before, err := os.ReadFile(filepath.Join(root, "heartbeat"))
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(100 * time.Millisecond)
			after, err := os.ReadFile(filepath.Join(root, "heartbeat"))
			if err != nil || string(before) != string(after) {
				t.Fatal("grandchild kept writing after cancellation", err)
			}
		})
	}
}

func TestProcessFailures(t *testing.T) {
	for _, mode := range []string{"noack", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			d, l, _ := fakeDriver(t, mode)
			h, err := d.Start(context.Background(), l, "ok")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(h.Cancel)
			o := waitBounded(t, h)
			if Status(o) != model.TurnStatusFailed || o.Error == "" {
				t.Fatal(o)
			}
		})
	}
	d, l, _ := fakeDriver(t, "ok")
	d.Resolver = fakeResolver{err: errors.New("codex: shim_only")}
	_, err := d.Start(context.Background(), l, "ok")
	var e *model.Error
	if !errors.As(err, &e) || e.Code != model.ErrorHarnessUnavailable || e.Details["reason"] != "shim_only" {
		t.Fatal(err)
	}
	d.Resolver = fakeResolver{path: filepath.Join(t.TempDir(), "missing.exe")}
	if _, err = d.Start(context.Background(), l, "ok"); !errors.As(err, &e) || e.Code != model.ErrorLaunchFailed {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = d.Start(ctx, l, "ok"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestEnvironmentMerge(t *testing.T) {
	got := mergeEnv([]string{"X=old", "OPENCODE_CONFIG_CONTENT=old", "LUGARN_COMMS_MEMBER=wrong"}, map[string]string{"X": "new", "LUGARN_COMMS_MEMBER": "right"}, true)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "old") || strings.Contains(joined, "wrong") || !strings.Contains(joined, "X=new") {
		t.Fatal(got)
	}
}

type versionResolver struct {
	fakeResolver
	info model.HarnessInfo
}

func (f versionResolver) Harnesses(context.Context) []model.HarnessInfo {
	return []model.HarnessInfo{f.info}
}

func TestVersionRejection(t *testing.T) {
	d, l, _ := fakeDriver(t, "ok")
	issue := model.HarnessIssueUnsupportedVersion
	d.Resolver = versionResolver{fakeResolver: d.Resolver.(fakeResolver), info: model.HarnessInfo{Harness: model.HarnessCodex, Installed: true, Issue: &issue}}
	_, err := d.Start(context.Background(), l, "ok")
	var e *model.Error
	if !errors.As(err, &e) || e.Code != model.ErrorHarnessUnavailable || e.Details["reason"] != "unsupported_version" {
		t.Fatal(err)
	}
}
