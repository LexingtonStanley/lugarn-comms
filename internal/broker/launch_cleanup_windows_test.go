//go:build windows

package broker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/wire"
	"golang.org/x/sys/windows"
)

// Exercise the real Win32 errors as well as the portable injected-remover tests.
// Release the handle at a deterministic backoff boundary; no sleep is needed.
func TestWindowsLaunchCleanupBusyHandles(t *testing.T) {
	for _, target := range []string{"directory", "child_file"} {
		for _, release := range []string{"during_stop", "before_sweep"} {
			t.Run(target+"/"+release, func(t *testing.T) {
				b, r, now := cleanupFixture(t)
				id := cleanupLaunch(t, b, "writer", 1, false)
				dir := filepath.Join(b.opts.StateDir, "launch", id)
				must(t, os.Mkdir(dir, 0700))
				child := filepath.Join(dir, "runner-launch.json")
				cleanupFile(t, child, now)
				path := dir
				flags := uint32(windows.FILE_FLAG_BACKUP_SEMANTICS)
				if target == "child_file" {
					path, flags = child, windows.FILE_ATTRIBUTE_NORMAL
				}
				name, err := windows.UTF16PtrFromString(path)
				must(t, err)
				// Omit FILE_SHARE_DELETE so this handle really prevents deletion.
				h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, flags, 0)
				must(t, err)
				closeHandle := func() {
					if h != windows.InvalidHandle {
						must(t, windows.CloseHandle(h))
						h = windows.InvalidHandle
					}
				}
				defer closeHandle()
				waits := 0
				b.opts.Wait = func(context.Context, time.Duration) error {
					waits++
					if release == "during_stop" {
						closeHandle()
					}
					return nil
				}
				out, err := b.StopTeam(context.Background(), wire.StopTeamRequest{Team: "t"})
				must(t, err)
				if out.Team.State != model.TeamStateStopped || waits == 0 || waits > 6 {
					t.Fatalf("state=%s waits=%d", out.Team.State, waits)
				}
				cleanupPresent(t, dir, release == "before_sweep")
				closeHandle()
				must(t, r.cleanLaunchFiles(context.Background()))
				cleanupPresent(t, dir, false)
			})
		}
	}
}
