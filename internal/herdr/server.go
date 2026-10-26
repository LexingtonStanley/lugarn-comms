package herdr

import (
	"context"
	"os"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// StartMethod is how StartServer started a server: the protocol's
// HerdrStartMethod, which internal/model doesn't define (internal/wire does).
type StartMethod string

const (
	// MethodDetached is POSIX: herdr server in its own session (setsid), stdio
	// on the null device, not waited on.
	MethodDetached StartMethod = "detached"
	// MethodWMI is Windows: herdr server created through WMI's
	// Win32_Process.Create, so it belongs to the WMI service rather than the
	// SSH session's job object.
	MethodWMI StartMethod = "wmi"
)

// StartResult is what StartServer did.
type StartResult struct {
	// Started is false when the server already answered and nothing ran.
	Started bool
	// Method is empty when nothing was started.
	Method StartMethod
	// PID is the started process, or 0.
	PID int
	// Status is the last status seen.
	Status Status
}

// The start-herdr op polls for up to 15 seconds.
var (
	startTimeout = 15 * time.Second
	pollInterval = 100 * time.Millisecond
)

// StartServer starts `herdr --session <session> server` for start-herdr, unless
// it already answers, then polls its status until it does. Not installed is
// herdr_unavailable (not_installed); a server that doesn't answer within 15 s,
// or whose process exits first, is herdr_unavailable (start_timeout). A server
// that is slow rather than dead is left running: a later status sees it.
//
// The server's working directory is the home directory, so it holds no
// worktree open, and its environment is this process's without HERDR_* (on
// Windows, WMI gives it the user's own environment instead). Every pane the
// server opens inherits that environment.
func StartServer(ctx context.Context, bin, session string) (StartResult, error) {
	if err := ValidateSession(session); err != nil {
		return StartResult{}, err
	}
	c := New(bin, session)
	st, err := c.Status(ctx)
	if err != nil {
		return StartResult{Status: st}, err
	}
	if !st.Installed {
		return StartResult{Status: st}, clientError("server", kindNotInstalled, "herdr not found")
	}
	if st.Running {
		return StartResult{Status: st}, nil
	}
	path, err := c.resolve("server")
	if err != nil {
		return StartResult{Status: st}, err
	}
	pid, exited, method, err := startServerProcess(ctx, path, session, serverDir())
	if err != nil {
		return StartResult{Status: st}, err
	}
	res := StartResult{Started: true, Method: method, PID: pid, Status: st}
	deadline := time.Now().Add(startTimeout)
	for {
		st, err = c.Status(ctx)
		if err == nil && st.Running {
			res.Status = st
			return res, nil
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		select {
		case <-exited:
			// Our process is gone. Look once more: a concurrent start may have won.
			if st, err = c.Status(ctx); err == nil && st.Running {
				res.Started, res.Method, res.PID, res.Status = false, "", 0, st
				return res, nil
			}
			return res, startFailed(pid, "herdr server exited before it answered")
		default:
		}
		if time.Now().After(deadline) {
			return res, startFailed(pid, "herdr server didn't answer within "+startTimeout.String())
		}
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func startFailed(pid int, message string) error {
	e := &Error{Op: "server", Message: message, kind: kindNotRunning}
	e.protocol = model.NewError(model.ErrorHerdrUnavailable, "the Herdr server didn't start").
		WithDetails(map[string]any{"reason": "start_timeout", "herdr_op": "server", "herdr_message": message, "pid": pid})
	return e
}

func serverDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return os.TempDir()
}
