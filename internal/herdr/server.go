package herdr

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// StartMethod is how StartServer started a server: the protocol's
// HerdrStartMethod, which internal/model doesn't define. internal/wire's
// HerdrStartMethod has the same values in the same order (a test checks).
type StartMethod string

const (
	// MethodDetached is POSIX: herdr server in its own session (setsid), stdio
	// on the null device, not waited on.
	MethodDetached StartMethod = "detached"
	// MethodSystemd is Linux under logind's KillUserProcesses=yes: herdr
	// server as a transient systemd user service, lugarn-herdr-<server>, so it
	// belongs to user@.service rather than the SSH session's scope.
	MethodSystemd StartMethod = "systemd"
	// MethodWMI is Windows: herdr server created through WMI's
	// Win32_Process.Create, so it belongs to the WMI service rather than the
	// SSH session's job object.
	MethodWMI StartMethod = "wmi"
)

// Values returns every StartMethod in the protocol's order.
func (StartMethod) Values() []StartMethod {
	return []StartMethod{MethodDetached, MethodSystemd, MethodWMI}
}

// StartResult is what StartServer did.
type StartResult struct {
	// Started is false when the server already answered and nothing ran.
	Started bool
	// Method is empty when nothing was started.
	Method StartMethod
	// PID is the started process (for systemd, the unit's main process), or 0.
	PID int
	// Status is the last status seen.
	Status Status
}

// ServerOption adjusts StartServer.
type ServerOption func(*serverOptions)

type serverOptions struct {
	killUserProcesses bool
	environment       []string
}

// KillUserProcesses passes on logind's KillUserProcesses, as hello reports it
// (host.logind.kill_user_processes; false when logind is null). When it's true
// on Linux, StartServer runs the server as a systemd user service
// (MethodSystemd), because logind kills everything in the SSH session's scope,
// setsid or not. Anywhere else it changes nothing.
func KillUserProcesses(on bool) ServerOption {
	return func(o *serverOptions) { o.killUserProcesses = on }
}

// ServerEnvironment supplies trusted KEY=VALUE overrides to a detached or systemd server.
// Windows WMI continues using the user's registry environment.
func ServerEnvironment(env []string) ServerOption {
	return func(o *serverOptions) { o.environment = append([]string(nil), env...) }
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
// Detached, the server's working directory is the home directory, so it holds
// no worktree open, and its environment is this process's without HERDR_*.
// Under systemd and WMI it gets the service manager's or the user's own
// environment instead. Every pane the server opens inherits it.
func StartServer(ctx context.Context, bin, session string, opts ...ServerOption) (StartResult, error) {
	var o serverOptions
	for _, opt := range opts {
		opt(&o)
	}
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
	if o.killUserProcesses && runtime.GOOS == "linux" {
		return startSystemd(ctx, c, path, st, o.environment)
	}
	pid, exited, method, err := startServerProcess(ctx, path, session, serverDir(), o.environment)
	if err != nil {
		return StartResult{Status: st}, err
	}
	return waitForServer(ctx, c, StartResult{Started: true, Method: method, PID: pid, Status: st}, func() bool {
		select {
		case <-exited:
			return true
		default:
			return false
		}
	})
}

// waitForServer polls until the server answers, up to startTimeout. exited
// reports that what we started is gone, so there's no point waiting longer.
func waitForServer(ctx context.Context, c *Client, res StartResult, exited func() bool) (StartResult, error) {
	deadline := time.Now().Add(startTimeout)
	for {
		st, err := c.Status(ctx)
		if err == nil && st.Running {
			res.Status = st
			return res, nil
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if exited != nil && exited() {
			// Ours is gone. Look once more: a concurrent start may have won.
			if st, err = c.Status(ctx); err == nil && st.Running {
				return StartResult{Status: st}, nil
			}
			return res, startFailed(res.PID, "herdr server exited before it answered")
		}
		if time.Now().After(deadline) {
			return res, startFailed(res.PID, "herdr server didn't answer within "+startTimeout.String())
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
