package broker

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Only these shell startup selectors are temporarily replaced. A nil value is
// distinct from an explicitly empty value. No harness config or credentials are copied.
type shellEnvironment map[string]*string

func captureShellEnvironment() shellEnvironment {
	env := shellEnvironment{}
	for _, key := range []string{"HOME", "ZDOTDIR", "XDG_CONFIG_HOME"} {
		env[key] = nil
		if value, ok := os.LookupEnv(key); ok {
			env[key] = &value
		}
	}
	return env
}
func (env shellEnvironment) restore() error {
	if len(env) != 3 {
		return invalid("invalid shell environment capsule")
	}
	for _, key := range []string{"HOME", "ZDOTDIR", "XDG_CONFIG_HOME"} {
		value, ok := env[key]
		if !ok {
			return invalid("invalid shell environment capsule")
		}
		if value != nil {
			if e := os.Setenv(key, *value); e != nil {
				return e
			}
		} else if e := os.Unsetenv(key); e != nil {
			return e
		}
	}
	return nil
}

func (i *Integration) paneHome(env map[string]string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	home := filepath.Join(i.b.opts.StateDir, "shell-home")
	if e := ensureShellHome(i.b.opts.StateDir); e != nil {
		return e
	}
	env["HOME"], env["ZDOTDIR"], env["XDG_CONFIG_HOME"] = home, home, filepath.Join(home, ".config")
	return nil
}

// RestoreRunnerEnvironment runs before state resolution, PATH capture, home
// discovery or any Herdr client call. The placement is written before pane run.
func RestoreRunnerEnvironment(team string) error {
	if runtime.GOOS == "windows" || os.Getenv("LUGARN_COMMS_RESTORE_ENV") != "1" {
		return nil
	}
	if e := model.ValidateOpaqueID(team); e != nil {
		return e
	}
	state := os.Getenv("LUGARN_COMMS_HOME")
	if !filepath.IsAbs(state) {
		return invalid("runner needs an explicit state directory")
	}
	i := &Integration{b: &Broker{opts: Options{StateDir: state}}}
	placement, e := i.placement(team)
	if e != nil {
		return e
	}
	if e = placement.Environment.restore(); e != nil {
		return e
	}
	return os.Unsetenv("LUGARN_COMMS_RESTORE_ENV")
}
