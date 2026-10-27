package broker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
)

func TestStopTeamRetriesBusyLaunchDirectory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
		errno    syscall.Errno
	}{{"access_then_success", 3, 5}, {"sharing_then_success", 2, 32}, {"still_busy", 100, 5}} {
		t.Run(tc.name, func(t *testing.T) {
			b, r, now := cleanupFixture(t)
			id := cleanupLaunch(t, b, "writer", 1, false)
			path := filepath.Join(b.opts.StateDir, "launch", id)
			must(t, os.Mkdir(path, 0700))
			cleanupFile(t, filepath.Join(path, "runner-launch.json"), now)
			for _, suffix := range []string{".token", ".json", ".settings.json", ".opencode.json"} {
				cleanupFile(t, path+suffix, now)
			}
			var output bytes.Buffer
			old := log.Writer()
			log.SetOutput(&output)
			defer log.SetOutput(old)
			attempts := 0
			var waited time.Duration
			b.opts.Wait = func(_ context.Context, delay time.Duration) error {
				if delay <= 0 || delay > 800*time.Millisecond {
					t.Fatalf("invalid cleanup backoff: %v", delay)
				}
				waited += delay
				return nil
			}
			b.launchCleanup = launchCleanup{platform: "windows", removeAll: func(got string) error {
				if got != path {
					t.Fatalf("unexpected recursive removal: %s", got)
				}
				attempts++
				if attempts <= tc.failures {
					return &os.PathError{Op: "unlinkat", Path: "private-launch-value", Err: tc.errno}
				}
				return os.RemoveAll(got)
			}}
			out, err := b.StopTeam(context.Background(), wire.StopTeamRequest{Team: "t"})
			must(t, err)
			if out.Team.State != model.TeamStateStopped {
				t.Fatalf("team did not stop: %s", out.Team.State)
			}
			deferred := tc.failures >= attempts
			cleanupPresent(t, path, deferred)
			for _, suffix := range []string{".token", ".json", ".settings.json", ".opencode.json"} {
				cleanupPresent(t, path+suffix, false)
			}
			if deferred {
				if attempts != 7 || waited != 1575*time.Millisecond {
					t.Fatalf("unbounded or missing retries: attempts=%d waited=%v", attempts, waited)
				}
				if !strings.Contains(output.String(), "launch directory cleanup deferred") {
					t.Fatal("missing cleanup diagnostic")
				}
				for _, value := range []string{path, id, "private-launch-value", tc.errno.Error()} {
					if strings.Contains(output.String(), value) {
						t.Fatalf("cleanup log contains a value: %q", value)
					}
				}
				// The stopped team's ended launch is eligible immediately, even
				// though its files are newer than the unknown-artifact grace period.
				b.launchCleanup.removeAll = os.RemoveAll
				must(t, r.cleanLaunchFiles(context.Background()))
				cleanupPresent(t, path, false)
			} else if attempts != tc.failures+1 || waited == 0 || output.Len() != 0 {
				t.Fatalf("attempts=%d waited=%v log=%q", attempts, waited, output.String())
			}
		})
	}
}

func TestLaunchCleanupNotFoundDuringRemoval(t *testing.T) {
	b, _, _ := cleanupFixture(t)
	id := cleanupID(t)
	path := filepath.Join(b.opts.StateDir, "launch", id)
	must(t, os.Mkdir(path, 0700))
	attempts := 0
	b.launchCleanup = launchCleanup{platform: "windows", removeAll: func(path string) error {
		attempts++
		must(t, os.RemoveAll(path))
		return fmt.Errorf("concurrent cleanup: %w", &os.PathError{Op: "unlinkat", Path: path, Err: os.ErrNotExist})
	}}
	b.opts.Wait = func(context.Context, time.Duration) error { t.Fatal("retried not-found"); return nil }
	must(t, b.removeLaunchArtifacts(context.Background(), id))
	must(t, b.removeLaunchArtifacts(context.Background(), id))
	if attempts != 1 {
		t.Fatalf("remover called %d times", attempts)
	}
}

func TestLaunchCleanupConcurrentRemoval(t *testing.T) {
	b, _, now := cleanupFixture(t)
	id := cleanupID(t)
	path := filepath.Join(b.opts.StateDir, "launch", id)
	must(t, os.Mkdir(path, 0700))
	cleanupFile(t, filepath.Join(path, "runner-launch.json"), now)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	b.launchCleanup = launchCleanup{platform: "windows", removeAll: func(path string) error {
		if calls.Add(1) <= 2 {
			entered <- struct{}{}
			<-release
			// Simulate a traversal handle retained by the other cleanup.
			return &os.PathError{Op: "unlinkat", Path: path, Err: syscall.Errno(5)}
		}
		return os.RemoveAll(path)
	}}
	b.opts.Wait = func(context.Context, time.Duration) error { return nil }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- b.removeLaunchArtifacts(ctx, id) }()
	}
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			close(release)
			t.Fatal("cleanup did not overlap")
		}
	}
	close(release)
	for range 2 {
		must(t, <-done)
	}
	cleanupPresent(t, path, false)
}

func TestLaunchCleanupCallersDeferBusyDirectory(t *testing.T) {
	for _, caller := range []string{"launch_failure", "sweep", "runner_stop", "integration_teardown"} {
		t.Run(caller, func(t *testing.T) {
			b, r, _ := cleanupFixture(t)
			id := cleanupLaunch(t, b, "writer", 1, true)
			path := filepath.Join(b.opts.StateDir, "launch", id)
			must(t, os.Mkdir(path, 0700))
			b.launchCleanup = launchCleanup{platform: "windows", removeAll: func(string) error { return syscall.Errno(32) }}
			b.opts.Wait = func(context.Context, time.Duration) error { return nil }
			ctx := context.Background()
			switch caller {
			case "launch_failure":
				must(t, r.removeLaunchFiles(ctx, id))
			case "sweep":
				must(t, r.cleanLaunchFiles(ctx))
			case "runner_stop":
				runnerReady(t, b)
				must(t, r.acquire(ctx))
				must(t, r.stop(ctx))
			case "integration_teardown":
				// Exercise the actual teardown hook used by StopTeam without
				// launching any process, pane or harness.
				i, err := NewIntegration(b, IntegrationOptions{HomeDir: r.opts.HomeDir})
				must(t, err)
				b.opts.Teardown = i
				_, err = b.StopTeam(ctx, wire.StopTeamRequest{Team: "t"})
				must(t, err)
			}
			cleanupPresent(t, path, true)
			b.launchCleanup.removeAll = os.RemoveAll
			must(t, r.cleanLaunchFiles(ctx))
			cleanupPresent(t, path, false)
		})
	}
}

func TestLaunchCleanupPreservesOtherErrors(t *testing.T) {
	for _, platform := range []string{"windows", "linux", "darwin"} {
		for _, errno := range []syscall.Errno{5, 32, 33, 87, 112} {
			t.Run(fmt.Sprintf("%s/%d", platform, errno), func(t *testing.T) {
				b, _, _ := cleanupFixture(t)
				id := cleanupLaunch(t, b, "writer", 1, false)
				must(t, os.Mkdir(filepath.Join(b.opts.StateDir, "launch", id), 0700))
				want := &os.PathError{Op: "unlinkat", Path: "private", Err: errno}
				attempts := 0
				b.launchCleanup = launchCleanup{platform: platform, removeAll: func(string) error { attempts++; return want }}
				b.opts.Wait = func(context.Context, time.Duration) error { return nil }
				_, err := b.StopTeam(context.Background(), wire.StopTeamRequest{Team: "t"})
				busy := platform == "windows" && (errno == 5 || errno == 32)
				if busy {
					must(t, err)
				} else if err == nil || attempts != 1 {
					t.Fatalf("non-transient error swallowed or retried: err=%v attempts=%d", err, attempts)
				}
			})
		}
	}
	for _, err := range []error{os.ErrPermission, errors.New("Access is denied"), errors.New("sharing violation")} {
		if windowsLaunchBusy("windows", err) {
			t.Fatalf("classified non-Win32 error as transient: %v", err)
		}
	}
}

func TestLaunchCleanupCancellationDefersBusyDirectory(t *testing.T) {
	b, _, _ := cleanupFixture(t)
	id := cleanupID(t)
	must(t, os.Mkdir(filepath.Join(b.opts.StateDir, "launch", id), 0700))
	attempts := 0
	b.launchCleanup = launchCleanup{platform: "windows", removeAll: func(string) error { attempts++; return syscall.Errno(5) }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	must(t, b.removeLaunchArtifacts(ctx, id))
	if attempts != 1 {
		t.Fatalf("cleanup ignored cancellation: %d attempts", attempts)
	}
}

func TestLaunchCleanupReturnsNewErrorAfterBusy(t *testing.T) {
	b, r, _ := cleanupFixture(t)
	id := cleanupID(t)
	must(t, os.Mkdir(filepath.Join(b.opts.StateDir, "launch", id), 0700))
	want := errors.New("filesystem failure")
	attempts := 0
	b.launchCleanup = launchCleanup{platform: "windows", removeAll: func(string) error {
		attempts++
		if attempts == 1 {
			return syscall.Errno(5)
		}
		return want
	}}
	b.opts.Wait = func(context.Context, time.Duration) error { return nil }
	if err := r.removeLaunchFiles(context.Background(), id); !errors.Is(err, want) || attempts != 2 {
		t.Fatalf("error=%v attempts=%d; want filesystem failure after one retry", err, attempts)
	}
}

func TestLaunchCleanupRejectsUnsafePaths(t *testing.T) {
	b, r, now := cleanupFixture(t)
	b.launchCleanup.removeAll = func(string) error { t.Fatal("unsafe recursive removal"); return nil }
	for _, id := range []string{"..", "../outside", cleanupID(t) + "/child"} {
		code(t, b.removeLaunchArtifacts(context.Background(), id), model.ErrorInternal)
		code(t, r.removeLaunchFiles(context.Background(), id), model.ErrorInternal)
		code(t, b.removeLaunchEntry(context.Background(), id), model.ErrorInternal)
	}
	root := filepath.Join(b.opts.StateDir, "launch")
	must(t, os.Remove(root))
	outside := t.TempDir()
	id := cleanupID(t)
	sentinel := filepath.Join(outside, id+".token")
	cleanupFile(t, sentinel, now)
	if err := os.Symlink(outside, root); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	code(t, b.removeLaunchArtifacts(context.Background(), id), model.ErrorInternal)
	code(t, r.removeLaunchFiles(context.Background(), id), model.ErrorInternal)
	cleanupPresent(t, sentinel, true)
}
