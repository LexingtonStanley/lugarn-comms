package broker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/harness"
	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
)

type paneLaunchCapsule struct {
	Harness model.Harness
	Binary  string
	Args    []string
	Workdir string
	Env     map[string]string
}

// readPaneCapsule shares the internal launch helpers' confinement rules. Only
// the exact file under this explicit state's launch/<ULID>/ is read; linked
// parents or files, traversal and a different supplied launch id are refused.
func readPaneCapsule(filename, launch string, out any) (string, error) {
	if launch != "" {
		if e := model.ValidateULID(launch); e != nil {
			return "", e
		}
	}
	state := os.Getenv("LUGARN_COMMS_HOME")
	if state == "" {
		return "", invalid("pane helper needs an explicit state directory")
	}
	state, e := filepath.Abs(state)
	if e != nil {
		return "", e
	}
	path := os.Getenv("LUGARN_COMMS_PANE_CONFIG")
	if !filepath.IsAbs(path) || filepath.Base(path) != filename {
		return "", invalid("invalid pane capsule path")
	}
	relative, e := filepath.Rel(filepath.Join(state, "launch"), path)
	if e != nil {
		return "", invalid("invalid pane capsule path")
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 2 || model.ValidateULID(parts[0]) != nil || parts[1] != filename || launch != "" && parts[0] != launch {
		return "", invalid("invalid pane capsule launch")
	}
	for _, parent := range []string{state, filepath.Join(state, "launch"), filepath.Dir(path)} {
		info, e := os.Lstat(parent)
		if e != nil {
			return "", e
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", invalid("linked pane capsule directory")
		}
	}
	info, e := os.Lstat(path)
	if e != nil {
		return "", e
	}
	if !info.Mode().IsRegular() {
		return "", invalid("pane capsule must be a regular file")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	if e = model.DecodeStrict(raw, out); e != nil {
		return "", e
	}
	return path, nil
}

// RunPaneLaunch starts a harness directly from its private capsule. Production
// supplies the pane's console files as stdin/stdout/stderr. The wrapper consumes
// its own Ctrl+C notifications, without setting a Windows inherited ignore flag
// or cancelling the child; the harness handles its console's Ctrl+C itself.
// A harness exit code is returned unchanged, including nonzero exits.
func RunPaneLaunch(launch string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if e := model.ValidateULID(launch); e != nil {
		return 0, e
	}
	var config paneLaunchCapsule
	path, e := readPaneCapsule("pane-launch.json", launch, &config)
	if e != nil {
		return 0, e
	}
	if !config.Harness.Valid() || !filepath.IsAbs(config.Binary) || !filepath.IsAbs(config.Workdir) || strings.ContainsAny(config.Binary+config.Workdir, "\x00\r\n") {
		return 0, invalid("invalid pane executable or workdir")
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(config.Binary), ".exe") {
		return 0, unresolvable(nil)
	}
	for _, arg := range config.Args {
		if strings.ContainsRune(arg, 0) {
			return 0, invalid("NUL in pane argument")
		}
	}
	env := map[string]string{}
	key := func(s string) string {
		if runtime.GOOS == "windows" {
			return strings.ToUpper(s)
		}
		return s
	}
	for _, entry := range os.Environ() {
		k, v, ok := strings.Cut(entry, "=")
		if ok {
			env[key(k)] = k + "=" + v
		}
	}
	// Host config-content overrides must not bypass the launch-only OpenCode file.
	if config.Harness == model.HarnessOpenCode {
		delete(env, key("OPENCODE_CONFIG_CONTENT"))
	}
	for k, v := range config.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return 0, invalid("invalid pane environment")
		}
		env[key(k)] = k + "=" + v
	}
	if config.Env["PATH"] == "" {
		return 0, invalid("pane capsule has no PATH")
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	command := exec.Command(config.Binary, config.Args...)
	for _, k := range keys {
		command.Env = append(command.Env, env[k])
	}
	command.Dir = config.Workdir
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	// Distinguish a swallowed bootstrap from a harness that starts but is never
	// detected. This file lives beside the validated private launch capsule.
	if e := statedir.WriteSecret(filepath.Join(filepath.Dir(path), "pane-started"), []byte("ready")); e != nil {
		return 0, e
	}
	err := command.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 0, err
	}
	return 0, nil
}

func (i *Integration) prepareWindowsPane(l harness.Launch, binary string, args []string) (map[string]string, []string, error) {
	root := filepath.Join(l.StateDir, "launch", l.LaunchID)
	bin := filepath.Join(root, "bin")
	if e := os.MkdirAll(bin, 0700); e != nil {
		return nil, nil, e
	}
	env, e := harness.Environment(l)
	if e != nil {
		return nil, nil, e
	}
	env["LUGARN_COMMS_HOME"] = l.StateDir
	env["LUGARN_COMMS_POINTER_HOME"] = i.opts.HomeDir
	env["PATH"] = bin + string(os.PathListSeparator) + completeSystemPath()
	path := filepath.Join(root, "pane-launch.json")
	env["LUGARN_COMMS_PANE_CONFIG"] = path
	raw, e := json.Marshal(paneLaunchCapsule{Harness: l.Harness, Binary: binary, Args: args, Workdir: l.WorkDir, Env: env})
	if e != nil {
		return nil, nil, e
	}
	if e = statedir.WriteSecret(path, raw); e != nil {
		return nil, nil, e
	}
	bridge, e := plainNativePath(i.opts.Binary)
	if e != nil || !plainLaunchPath.MatchString(bridge) {
		return nil, nil, unresolvable(e)
	}
	return env, []string{bridge, "pane-launch", l.LaunchID}, nil
}
func (i *Integration) detectWindowsPane(ctx context.Context, client HerdrAPI, pane, workspace, name string, kind model.Harness) (herdr.Agent, error) {
	waiting, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	elapsed := time.Duration(0)
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for {
		agents, e := client.Agents(waiting)
		last = e
		if e == nil {
			for _, agent := range agents {
				if agent.Pane == pane && agent.Kind == string(kind) && agent.Workspace == workspace {
					if e = client.RenameAgent(waiting, pane, name); e != nil {
						return herdr.Agent{}, e
					}
					agent.Name = name
					return agent, nil
				}
			}
		}
		if ctx.Err() != nil {
			return herdr.Agent{}, ctx.Err()
		}
		remaining := min(30*time.Second-elapsed, time.Until(deadline))
		if remaining <= 0 || waiting.Err() != nil {
			break
		}
		pause := min(250*time.Millisecond, remaining)
		if e = i.b.opts.Wait(waiting, pause); e != nil {
			if ctx.Err() != nil {
				return herdr.Agent{}, ctx.Err()
			}
			last = e
			break
		}
		elapsed += pause
	}
	failure := model.NewError(model.ErrorLaunchFailed, "Herdr did not detect the pane agent").WithDetail("cause", "agent_not_detected")
	if last != nil {
		failure.WithDetail("observation_error", model.AsError(last))
	}
	return herdr.Agent{}, failure
}
