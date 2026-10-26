package herdr

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// unitName is the server's systemd unit: lugarn-herdr-<server>.service. A
// Herdr session name is [A-Za-z0-9._-]{1,64}, and every one of those
// characters is valid in a unit name, so it needs no escaping (systemd-escape
// --mangle leaves it unchanged) and stays well under the 255-byte limit. The
// .service suffix is explicit, so a session such as "x.timer" can't name a
// unit of another type.
func unitName(session string) string { return "lugarn-herdr-" + session + ".service" }

// The unit states `systemctl is-active` prints that mean a process is (still)
// there.
var unitRunning = map[string]bool{"active": true, "activating": true, "reloading": true, "refreshing": true}

// startSystemd runs the server as a transient user service:
//
//	systemd-run --user --unit=lugarn-herdr-<server>.service --collect -- <herdr> --session <server> server
//
// systemd-run refuses a unit name that's still loaded (a failed or dead unit
// --collect hasn't cleaned up), so it first asks `systemctl --user is-active`
// and runs `systemctl --user reset-failed` on a loaded, stopped unit. A unit
// that's already running is somebody else's start in flight: it waits for that
// server instead of starting another. The service's stdout and stderr go to
// the journal.
func startSystemd(ctx context.Context, c *Client, bin string, st Status) (StartResult, error) {
	session := c.session
	unit := unitName(session)
	res := StartResult{Status: st}
	state, loaded, err := unitState(ctx, unit)
	if err != nil {
		return res, startFailed(0, err.Error())
	}
	for deadline := time.Now().Add(5 * time.Second); state == "deactivating" && time.Now().Before(deadline); {
		time.Sleep(pollInterval)
		if state, loaded, err = unitState(ctx, unit); err != nil {
			return res, startFailed(0, err.Error())
		}
	}
	if unitRunning[state] {
		return waitForServer(ctx, c, res, unitStopped(ctx, unit))
	}
	if loaded {
		// Its own failure doesn't matter: systemd-run's says whether it worked.
		_, _, _ = systemctl(ctx, "reset-failed", unit)
	}
	out, code, err := runSystemd(ctx, "systemd-run", "--user", "--unit="+unit, "--collect", "--", bin, "--session", session, "server")
	if err != nil || code != 0 {
		return res, startFailed(0, "systemd-run: "+strings.TrimSpace(out)+errText(err))
	}
	res.Started, res.Method = true, MethodSystemd
	res, err = waitForServer(ctx, c, res, unitStopped(ctx, unit))
	if res.Started {
		if pid, _, perr := systemctl(ctx, "show", "-p", "MainPID", "--value", unit); perr == nil {
			res.PID, _ = strconv.Atoi(strings.TrimSpace(pid))
		}
	}
	return res, err
}

// unitStopped reports whether the unit has stopped (or been collected).
func unitStopped(ctx context.Context, unit string) func() bool {
	return func() bool {
		state, _, err := unitState(ctx, unit)
		return err == nil && (state == "inactive" || state == "failed")
	}
}

// unitState is `systemctl --user is-active <unit>`: its state, and whether the
// unit is loaded at all. is-active exits 0 when active, 3 when loaded but not
// active, and 4 when there's no such unit (systemd 255).
func unitState(ctx context.Context, unit string) (string, bool, error) {
	out, code, err := systemctl(ctx, "is-active", unit)
	if err != nil {
		return "", false, err
	}
	state := strings.TrimSpace(out)
	if i := strings.IndexByte(state, '\n'); i >= 0 {
		state = state[:i]
	}
	if state == "" {
		return "", false, errors.New("systemctl --user is-active " + unit + ": no state (exit " + strconv.Itoa(code) + ")")
	}
	return state, code != 4, nil
}

func systemctl(ctx context.Context, args ...string) (string, int, error) {
	return runSystemd(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

// runSystemd runs systemctl or systemd-run with this process's environment,
// less HERDR_*. Over SSH, pam_systemd sets XDG_RUNTIME_DIR, where the user
// manager's bus lives; it's filled in if missing. A non-zero exit isn't an
// error: the caller reads the code.
func runSystemd(ctx context.Context, name string, args ...string) (string, int, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", -1, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	env := cleanEnv(os.Environ())
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		env = append(env, "XDG_RUNTIME_DIR=/run/user/"+strconv.Itoa(os.Getuid()))
	}
	cmd.Env = env
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out.String(), exitErr.ExitCode(), nil
	}
	if err != nil {
		return out.String(), -1, err
	}
	return out.String(), 0, nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return " (" + err.Error() + ")"
}
