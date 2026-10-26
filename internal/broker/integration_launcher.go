package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/herdr"
	"github.com/LexingtonStanley/lugarn-comms/internal/hostprobe"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	"github.com/LexingtonStanley/lugarn-comms/internal/statedir"
	"github.com/LexingtonStanley/lugarn-comms/internal/store"
)

// Attach enables real app lifecycle operations for this broker. Configure it
// before serving any concurrent call. Bare broker.New retains injectable defaults.
func (i *Integration) Attach() { i.b.integration = i; i.b.opts.Launcher = i; i.b.opts.Teardown = i }

type runnerPlacement struct {
	Workspace, Pane, BridgeDir, Binary string
	PID                                int64
	Birth                              string
}

func (i *Integration) placementPath(team string) string {
	return filepath.Join(i.b.opts.StateDir, "runner", team+".json")
}
func (i *Integration) placement(team string) (runnerPlacement, error) {
	var p runnerPlacement
	raw, e := os.ReadFile(i.placementPath(team))
	if os.IsNotExist(e) {
		return p, nil
	}
	if e != nil {
		return p, e
	}
	e = json.Unmarshal(raw, &p)
	return p, e
}
func (i *Integration) savePlacement(team string, p runnerPlacement) error {
	raw, e := json.Marshal(p)
	if e != nil {
		return e
	}
	path := i.placementPath(team)
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	return statedir.WriteSecret(path, raw)
}

var plainLaunchPath = regexp.MustCompile(`^[A-Za-z0-9._/:\\-]+$`)

func (i *Integration) runnerBridge() (string, string, error) {
	if plainLaunchPath.MatchString(i.opts.Binary) {
		return i.opts.Binary, "", nil
	}
	base := ""
	if plainLaunchPath.MatchString(i.b.opts.StateDir) {
		base = i.b.opts.StateDir
	}
	dir, e := os.MkdirTemp(base, "lugarn-runner-")
	if e != nil {
		return "", "", e
	}
	fail := func(e error) (string, string, error) {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		return "", "", e
	}
	if e = statedir.Ensure(dir); e != nil {
		return fail(e)
	}
	safe, e := plainNativePath(dir)
	if e != nil || !plainLaunchPath.MatchString(safe) {
		return fail(model.NewError(model.ErrorRunnerUnavailable, "cannot create a shell-safe native runner bridge").WithDetail("reason", "unresolvable"))
	}
	name := "lugarn-comms"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(dir, name)
	// Only the trusted broker executable is linked/copied, never configuration or
	// credentials. The kernel resolves its original path, including spaces.
	if e = os.Link(i.opts.Binary, target); e != nil {
		raw, e := os.ReadFile(i.opts.Binary)
		if e != nil {
			return fail(e)
		}
		if e = statedir.WriteSecret(target, raw); e != nil {
			return fail(e)
		}
		if runtime.GOOS != "windows" {
			if e = os.Chmod(target, 0700); e != nil {
				return fail(e)
			}
		}
	}
	return filepath.Join(safe, name), dir, nil
}
func (i *Integration) rootEnv() map[string]string {
	env := map[string]string{"LUGARN_COMMS_HOME": i.b.opts.StateDir, "LUGARN_COMMS_POINTER_HOME": i.opts.HomeDir, "PATH": completeSystemPath()}
	if i.opts.HerdrBinary != "" {
		env["LUGARN_COMMS_HERDR_BINARY"] = i.opts.HerdrBinary
	}
	for h, m := range i.opts.Models {
		env["LUGARN_COMMS_MODEL_"+strings.ToUpper(string(h))] = m
	}
	return env
}
func completeSystemPath() string { return (hostprobe.PathInfo{Inherited: os.Getenv("PATH")}).Merged() }

// Entry points apply the capture before constructing their real probe. Each app
// invocation serves one operation; the runner inherits this explicit PATH.
func (i *Integration) refreshPath(ctx context.Context) error {
	if i.b.opts.LoginPath == nil {
		return nil
	} // injected test probes have no host capture
	info, e := hostprobe.LoginPath(ctx, i.b.opts.StateDir, true)
	if e != nil {
		return e
	}
	info.Inherited = i.b.opts.LoginPath.Inherited
	i.b.opts.LoginPath = &info
	if e = os.Setenv("PATH", info.Merged()); e != nil {
		return e
	}
	i.b.opts.Probe = hostprobe.New()
	i.adapter.probe = i.b.opts.Probe
	return nil
}

// Launch starts a native bridge to this broker in the recorded workspace's root
// pane. Only plain argv words reach RunCommand: no original path is shell-quoted.
func (i *Integration) Launch(ctx context.Context, t store.Team) error {
	if e := i.refreshPath(ctx); e != nil {
		return e
	}
	client := i.opts.Herdr(t.HerdrServer)
	previous, e := i.placement(t.ID)
	if e != nil {
		return e
	}
	var pane, workspace string
	if t.HerdrWorkspace != nil {
		ws, e := client.Workspace(ctx, *t.HerdrWorkspace)
		if e == nil {
			workspace = ws.ID // The persisted id is authority; labels never are.
			if previous.Workspace == ws.ID && previous.Pane != "" {
				p, e := client.Pane(ctx, previous.Pane)
				if e == nil && p.Workspace == ws.ID {
					pane = p.ID
				} else if e != nil && !herdr.IsGone(e) {
					return e
				}
			}
			if pane == "" {
				tab, e := client.OpenTab(ctx, ws.ID, "runner", i.b.opts.StateDir, i.rootEnv())
				if e != nil {
					return e
				}
				pane = tab.RootPane
			}
		} else if !herdr.IsGone(e) {
			return e
		}
	}
	if pane == "" {
		ws, e := client.CreateWorkspace(ctx, herdr.WorkspaceName(t.Name), i.b.opts.StateDir, i.rootEnv())
		if e != nil {
			return e
		}
		workspace, pane = ws.ID, ws.RootPane
	}
	bridge, dir, e := i.runnerBridge()
	if e != nil {
		_ = client.ClosePane(context.WithoutCancel(ctx), pane)
		return e
	}
	placed := runnerPlacement{Workspace: workspace, Pane: pane, BridgeDir: dir, Binary: bridge}
	if e = i.savePlacement(t.ID, placed); e != nil {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		_ = client.ClosePane(context.WithoutCancel(ctx), pane)
		return e
	}
	e = i.b.db.Write(ctx, func(tx *store.Tx) error {
		current, e := tx.GetTeam(t.ID)
		if e != nil {
			return e
		}
		if current.Run != t.Run || current.State != model.TeamStateStarting {
			return model.NewError(model.ErrorLeaseConflict, "team start changed")
		}
		if e = tx.UpdateTeam(t.ID, store.TeamPatch{HerdrWorkspace: ptr(&workspace)}, i.b.now()); e != nil {
			return e
		}
		runner := current.Runner
		runner.State = model.RunnerStateDown
		runner.Pane = &pane
		return tx.Heartbeat(t.ID, runner, i.b.now())
	})
	if e == nil {
		e = client.RunCommand(ctx, pane, bridge, "run", "--team", t.ID)
	}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	for e == nil {
		var ready bool
		e = i.b.db.Read(ctx, func(tx *store.Tx) error {
			team, e := tx.GetTeam(t.ID)
			if e != nil {
				return e
			}
			ready = team.Runner.State == model.RunnerStateUp && i.b.fresh(team) && team.Runner.Pane != nil && *team.Runner.Pane == pane
			return nil
		})
		if e != nil || ready {
			break
		}
		select {
		case <-ctx.Done():
			e = ctx.Err()
		case <-deadline.C:
			e = model.NewError(model.ErrorRunnerUnavailable, "runner did not heartbeat within 15 seconds")
		case <-poll.C:
		}
	}
	if e != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = client.ClosePane(cleanup, pane)
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		return model.NewError(model.ErrorRunnerUnavailable, "runner could not start").WithDetail("cause", fmt.Sprint(e))
	}
	return nil
}
