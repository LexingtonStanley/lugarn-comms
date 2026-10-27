package broker

import (
	"context"
	"path/filepath"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// The wait's context has expired; use its live parent for one bounded snapshot
// before cleanup. Failed inspection leaves startup unconfirmed, not proven.
func startupTimeout(ctx context.Context, code model.ErrorCode, client HerdrAPI, pane, binary, cause string) *model.Error {
	inspect, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	info, e := client.ProcessInfo(inspect, pane)
	if e == nil {
		for _, process := range info.Foreground {
			executable := process.Argv0
			if len(process.Argv) > 0 {
				executable = process.Argv[0]
			}
			broker := executable != "" && sameNativePath(executable, binary)
			// Some platforms expose only a process name. Never treat another
			// foreground program as the broker merely because it isn't a shell.
			if executable == "" {
				broker = process.Name == filepath.Base(binary)
			}
			if broker {
				message := "runner started but did not heartbeat within 15 seconds"
				if cause == "bootstrap_no_marker" {
					message = "pane bootstrap started but did not write its readiness marker within 20 seconds"
				}
				return model.NewError(code, message).WithDetail("cause", cause)
			}
		}
	}
	return shellNotReady(code)
}
