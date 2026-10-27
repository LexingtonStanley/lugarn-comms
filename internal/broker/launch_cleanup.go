package broker

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Keep the remover and platform together so tests can exercise Windows failures
// on every host without changing the policy used by real brokers.
type launchCleanup struct {
	removeAll func(string) error
	platform  string
}

func defaultLaunchCleanup() launchCleanup {
	return launchCleanup{removeAll: os.RemoveAll, platform: runtime.GOOS}
}

func (b *Broker) launchRoot() (root string, exists bool, err error) {
	if b.opts.StateDir == "" {
		return "", false, invalid("launch cleanup requires an explicit state directory")
	}
	root = filepath.Join(b.opts.StateDir, "launch")
	for _, path := range []string{b.opts.StateDir, root} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return root, false, nil
		}
		if err != nil {
			return root, false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return root, false, model.NewError(model.ErrorInternal, "launch cleanup parent is not a directory")
		}
	}
	return root, true, nil
}

func (b *Broker) removeLaunchArtifacts(ctx context.Context, id string) error {
	if model.ValidateULID(id) != nil {
		return model.NewError(model.ErrorInternal, "invalid stored launch id")
	}
	for _, suffix := range []string{".token", ".json", ".settings.json", ".opencode.json", ""} {
		if err := b.removeLaunchEntry(ctx, id+suffix); err != nil {
			return err
		}
	}
	return nil
}

// removeLaunchEntry is shared by stop, launch failure and reconciliation. It
// removes only an exact launch entry; links are unlinked, never traversed, and
// their errors are never treated as a busy directory.
func (b *Broker) removeLaunchEntry(ctx context.Context, name string) error {
	_, directory, ok := cleanupLaunchName(name)
	if !ok {
		return model.NewError(model.ErrorInternal, "invalid launch cleanup name")
	}
	root, exists, err := b.launchRoot()
	if err != nil || !exists {
		return err
	}
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !directory && info.Mode().IsRegular() {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !directory || !info.IsDir() {
		return model.NewError(model.ErrorInternal, "unexpected launch artifact type")
	}
	// Concurrent RemoveAll calls may temporarily keep a Windows directory open
	// or delete-pending. Each call closes its own traversal handles before return.
	// Other processes may also retain a launch file after termination. Bound the
	// backoff to 1.575 s; an ended launch remains eligible for the next sweep.
	delays := [...]time.Duration{25 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}
	for attempt := 0; ; attempt++ {
		err = b.launchCleanup.removeAll(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if !windowsLaunchBusy(b.launchCleanup.platform, err) {
			return err
		}
		if attempt == len(delays) {
			break
		}
		if err = b.opts.Wait(ctx, delays[attempt]); err != nil {
			if ctx.Err() == nil {
				return err
			}
			break
		}
	}
	// No path, id, error text or launch contents may enter the broker log.
	log.Print("launch directory cleanup deferred; will retry during artifact sweep")
	return nil
}

func windowsLaunchBusy(platform string, err error) bool {
	// These are Win32 ERROR_ACCESS_DENIED and ERROR_SHARING_VIOLATION, not
	// portable permission errors (errno 5 is EIO on Linux).
	return platform == "windows" && (errors.Is(err, syscall.Errno(5)) || errors.Is(err, syscall.Errno(32)))
}
