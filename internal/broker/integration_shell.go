package broker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

type paneShellConfig struct{ Path, Ready string }

// RunPaneShell is the native pane bootstrap. Its generated capsule is confined
// to this state directory. It execs a clean shell without changing the config home.
func RunPaneShell() error {
	state, e := statedir.Resolve()
	if e != nil {
		return e
	}
	state, e = filepath.Abs(state)
	if e != nil {
		return e
	}
	path := os.Getenv("LUGARN_COMMS_PANE_CONFIG")
	relative, e := filepath.Rel(filepath.Join(state, "launch"), path)
	if e != nil || strings.HasPrefix(relative, "..") || filepath.Base(path) != "clean-shell.json" {
		return invalid("invalid pane shell capsule")
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 2 || model.ValidateULID(parts[0]) != nil {
		return invalid("invalid pane shell launch")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	var config paneShellConfig
	if e = json.Unmarshal(raw, &config); e != nil {
		return e
	}
	if config.Ready != filepath.Join(filepath.Dir(path), "shell-ready") {
		return invalid("invalid pane ready path")
	}
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if upper == "ENV" || upper == "BASH_ENV" || upper == "HISTFILE" || upper == "PATH" || strings.HasPrefix(upper, "BASH_FUNC_") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "PATH="+config.Path, "LUGARN_COMMS_PANE_READY="+config.Ready)
	return execPaneShell(env)
}
func (i *Integration) preparePaneShell(ctx context.Context, l harness.Launch, binary string) (map[string]string, []string, error) {
	root := filepath.Join(l.StateDir, "launch", l.LaunchID)
	bin := filepath.Join(root, "bin")
	if e := os.MkdirAll(bin, 0700); e != nil {
		return nil, nil, e
	}
	name := string(l.Harness)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	pin := filepath.Join(bin, name)
	if e := os.Symlink(binary, pin); e != nil {
		if runtime.GOOS != "windows" {
			return nil, nil, unresolvable(e)
		}
		if e = os.Link(binary, pin); e != nil {
			return nil, nil, unresolvable(e)
		}
	}
	resolved, e := filepath.EvalSymlinks(pin)
	if e != nil {
		return nil, nil, unresolvable(e)
	}
	expected, e := filepath.EvalSymlinks(binary)
	if e != nil || !sameNativePath(resolved, expected) {
		return nil, nil, unresolvable(e)
	}
	env, e := harness.Environment(l)
	if e != nil {
		return nil, nil, e
	}
	env["LUGARN_COMMS_HOME"] = l.StateDir
	env["LUGARN_COMMS_POINTER_HOME"] = i.opts.HomeDir
	config := paneShellConfig{Path: bin + string(os.PathListSeparator) + completeSystemPath(), Ready: filepath.Join(root, "shell-ready")}
	data, e := json.Marshal(config)
	if e != nil {
		return nil, nil, e
	}
	path := filepath.Join(root, "clean-shell.json")
	if e = statedir.WriteSecret(path, data); e != nil {
		return nil, nil, e
	}
	env["LUGARN_COMMS_PANE_CONFIG"] = path
	// The runner is started from the plain original executable or native bridge.
	// Keeping that invariant avoids reparsing an original path containing spaces.
	bridge := i.opts.Binary
	if safe, e := plainNativePath(bridge); e == nil && plainLaunchPath.MatchString(safe) {
		bridge = safe
	} else {
		return nil, nil, unresolvable(e)
	}
	argv := []string{bridge, "pane-shell"}
	if runtime.GOOS != "windows" {
		argv = append([]string{"exec"}, argv...)
	}
	return env, argv, nil
}
func sameNativePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
func unresolvable(cause error) *model.Error {
	e := model.NewError(model.ErrorHarnessUnavailable, "cannot guarantee the pinned native harness launch").WithDetail("reason", "unresolvable")
	if cause != nil {
		e.WithDetail("cause", cause.Error())
	}
	return e
}
func (i *Integration) waitShell(ctx context.Context, id string) error {
	path := filepath.Join(i.b.opts.StateDir, "launch", id, "shell-ready")
	until := time.NewTimer(20 * time.Second)
	defer until.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if raw, e := os.ReadFile(path); e == nil && string(raw) == "ready" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-until.C:
			return unresolvable(nil)
		case <-tick.C:
		}
	}
}
