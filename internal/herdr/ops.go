package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Status reports whether Herdr is installed, its version, and whether this
// client's server is running. A missing binary is not an error: Installed is
// false.
func (c *Client) Status(ctx context.Context) (Status, error) {
	st := Status{Session: c.session}
	stdout, err := c.call(ctx, 0, "status", "status", "--json")
	if err != nil {
		if kindOf(err) == kindNotInstalled {
			return st, nil
		}
		return st, err
	}
	var w wireStatus
	if err := json.Unmarshal(stdout, &w); err != nil {
		return st, clientError("status", kindOther, "unexpected output: "+err.Error())
	}
	st.Installed = true
	st.Version = w.Client.Version
	st.Running = w.Server.Running
	st.ServerVersion = str(w.Server.Version)
	st.Compatible = w.Server.Compatible != nil && *w.Server.Compatible
	st.Socket = w.Server.Socket
	return st, nil
}

// CreateWorkspace creates a workspace labelled label, unfocused, with its
// root pane's shell in cwd (empty: Herdr's default) and env added to that
// pane's environment: the runner's pane. env reaches the root pane only; a
// member's environment goes on its own tab (OpenTab). Herdr doesn't keep
// labels unique, so record the returned id and look the workspace up by it
// (Workspace); never adopt one by its label.
func (c *Client) CreateWorkspace(ctx context.Context, label, cwd string, env map[string]string) (Workspace, error) {
	envs, err := envArgs(env)
	if err != nil {
		return Workspace{}, err
	}
	args := []string{"workspace", "create", "--label", label}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	args = append(append(args, envs...), "--no-focus")
	var r struct {
		Workspace wireWorkspace `json:"workspace"`
		Tab       wireTab       `json:"tab"`
		RootPane  wirePane      `json:"root_pane"`
	}
	if err := c.result(ctx, 0, "workspace create", &r, args...); err != nil {
		return Workspace{}, err
	}
	return Workspace{ID: r.Workspace.WorkspaceID, Label: r.Workspace.Label, RootTab: r.Tab.TabID, RootPane: r.RootPane.PaneID}, nil
}

// Workspace looks a workspace up by the id CreateWorkspace returned. One that
// no longer exists is IsGone; the next start creates a new one.
func (c *Client) Workspace(ctx context.Context, id string) (Workspace, error) {
	var r struct {
		Workspace wireWorkspace `json:"workspace"`
	}
	if err := c.result(ctx, 0, "workspace get", &r, "workspace", "get", id); err != nil {
		return Workspace{}, err
	}
	return Workspace{ID: r.Workspace.WorkspaceID, Label: r.Workspace.Label}, nil
}

// OpenTab creates a tab in workspace, unfocused, labelled label (empty:
// Herdr's number), with its one pane's shell in cwd (empty: Herdr's default)
// and env added to that pane's environment, and returns it with its root pane:
// a member's own tab (Identity, Panes). A workspace that's gone is IsGone.
func (c *Client) OpenTab(ctx context.Context, workspace, label, cwd string, env map[string]string) (Tab, error) {
	envs, err := envArgs(env)
	if err != nil {
		return Tab{}, err
	}
	args := []string{"tab", "create", "--workspace", workspace}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if label != "" {
		args = append(args, "--label", label)
	}
	args = append(append(args, envs...), "--no-focus")
	var r struct {
		Tab      wireTab  `json:"tab"`
		RootPane wirePane `json:"root_pane"`
	}
	if err := c.result(ctx, 0, "tab create", &r, args...); err != nil {
		return Tab{}, err
	}
	return Tab{ID: r.Tab.TabID, Workspace: r.Tab.WorkspaceID, Label: r.Tab.Label, RootPane: r.RootPane.PaneID}, nil
}

// CloseTab closes a tab, its panes and whatever runs in them; Herdr hangs up
// their processes. Closing a tab that's already gone succeeds. Closing a tab's
// only pane (ClosePane) closes the tab too.
func (c *Client) CloseTab(ctx context.Context, id string) error {
	err := c.result(ctx, 0, "tab close", nil, "tab", "close", id)
	if IsGone(err) {
		return nil
	}
	return err
}

// Pane returns one pane. A pane that doesn't exist is IsGone.
func (c *Client) Pane(ctx context.Context, id string) (Pane, error) {
	var r struct {
		Pane wirePane `json:"pane"`
	}
	if err := c.result(ctx, 0, "pane get", &r, "pane", "get", id); err != nil {
		return Pane{}, err
	}
	return r.Pane.pane(), nil
}

// PaneExists reports whether a pane still exists. Pane ids aren't reused.
func (c *Client) PaneExists(ctx context.Context, id string) (bool, error) {
	_, err := c.Pane(ctx, id)
	if IsGone(err) {
		return false, nil
	}
	return err == nil, err
}

// ClosePane closes a pane and with it whatever runs there; Herdr hangs up the
// pane's processes. Closing a pane that's already gone succeeds. Closing a
// workspace's last pane closes the workspace too.
func (c *Client) ClosePane(ctx context.Context, id string) error {
	err := c.result(ctx, 0, "pane close", nil, "pane", "close", id)
	if IsGone(err) {
		return nil
	}
	return err
}

// Herdr types `pane run` text into the pane's shell, so RunCommand only takes
// words no shell (sh, bash, zsh, fish, PowerShell, cmd) treats specially, and
// needs no quoting: not zsh's =word, cmd's %VAR%, PowerShell's @ and ','. On
// Windows a backslash is a path separator, not an escape.
var (
	plainWord   = regexp.MustCompile(`^[A-Za-z0-9_./:-]+$`)
	plainWordNT = regexp.MustCompile(`^[A-Za-z0-9_./:\\-]+$`)
)

// RunCommand types argv into the shell of a pane, then Enter: for the runner
// pane (`lugarn-comms run --team <id>`), never for agent-written text. Each
// word must be plain (letters, digits and _./:-, plus \ on Windows) since it
// reaches a shell unquoted, and the first can't start with '-'. A path with a
// space is refused, and so is --flag=value: pass --flag value.
func (c *Client) RunCommand(ctx context.Context, pane string, argv ...string) error {
	if len(argv) == 0 {
		return model.NewError(model.ErrorInvalidRequest, "a command is required")
	}
	word := plainWord
	if runtime.GOOS == "windows" {
		word = plainWordNT
	}
	for i, a := range argv {
		if !word.MatchString(a) || (i == 0 && strings.HasPrefix(a, "-")) {
			return model.NewError(model.ErrorInvalidRequest, "a pane command word needs shell quoting").
				WithDetails(map[string]any{"reason": "unquotable", "word": a})
		}
	}
	// pane run prints nothing on success; it acknowledges admission, not execution.
	_, err := c.call(ctx, 0, "pane run", "pane", "run", pane, strings.Join(argv, " "))
	return err
}

// StartOptions describe `herdr agent start`.
type StartOptions struct {
	// Name is the Herdr agent name; see AgentName.
	Name    string
	Harness model.Harness
	// Pane must be at its shell prompt.
	Pane string
	// Args are the harness's fixed launch flags, after "--". Herdr types the
	// launch into the pane's shell, and on Windows re-wraps it in PowerShell,
	// so they must never carry agent-written text.
	Args []string
	// Timeout is Herdr's readiness wait, over 3 s and at most 300 s. Zero
	// means 30 s, Herdr's own default.
	Timeout time.Duration
}

// StartAgent starts a harness in an existing shell pane under a name and
// returns once Herdr sees it ready, or blocked. A harness that stops at its
// trust question is blocked: StartAgent returns it with Activity blocked and
// no error, since the person answers that on the phone. A harness that never
// comes up is launch_failed (IsTimeout); a busy pane or a taken name is
// launch_failed with Herdr's code (CodeAgentPaneBusy, CodeAgentNameTaken).
func (c *Client) StartAgent(ctx context.Context, o StartOptions) (Agent, error) {
	if err := ValidateAgentName(o.Name); err != nil {
		return Agent{}, err
	}
	if !o.Harness.Valid() {
		return Agent{}, model.NewError(model.ErrorInvalidRequest, "unknown harness").WithDetail("harness", string(o.Harness))
	}
	timeout := o.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if timeout <= 3*time.Second || timeout > 300*time.Second {
		return Agent{}, model.NewError(model.ErrorInvalidRequest, "agent start timeout must be over 3 s and at most 300 s")
	}
	args := []string{"agent", "start", o.Name, "--kind", string(o.Harness), "--pane", o.Pane,
		"--timeout", strconv.FormatInt(timeout.Milliseconds(), 10)}
	if len(o.Args) > 0 {
		args = append(append(args, "--"), o.Args...)
	}
	var r struct {
		Agent wireAgent `json:"agent"`
	}
	err := c.result(ctx, timeout+10*time.Second, "agent start", &r, args...)
	if err == nil {
		return r.Agent.agent(), nil
	}
	var he *Error
	if errors.As(err, &he) && he.Code == CodeAgentNotReady {
		// "blocked during startup": Herdr keeps the name for the blocked agent.
		if a, getErr := c.Agent(ctx, o.Name); getErr == nil && a.Pane == o.Pane {
			return a, nil
		}
	}
	return Agent{}, err
}

// Agent returns one live agent, by name or by the pane hosting it. An agent
// that exited, or whose pane closed, is IsGone.
func (c *Client) Agent(ctx context.Context, target string) (Agent, error) {
	var r struct {
		Agent wireAgent `json:"agent"`
	}
	if err := c.result(ctx, 0, "agent get", &r, "agent", "get", target); err != nil {
		return Agent{}, err
	}
	return r.Agent.agent(), nil
}

// Agents lists the server's live agents.
func (c *Client) Agents(ctx context.Context) ([]Agent, error) {
	var r struct {
		Agents []wireAgent `json:"agents"`
	}
	if err := c.result(ctx, 0, "agent list", &r, "agent", "list"); err != nil {
		return nil, err
	}
	out := make([]Agent, 0, len(r.Agents))
	for _, a := range r.Agents {
		out = append(out, a.agent())
	}
	return out, nil
}

// MaxPromptBytes is the longest inline pane delivery the spec allows (8 KiB of
// UTF-8). Longer deliveries go through a file (Untrusted text).
const MaxPromptBytes = 8192

// Prompt submits text to an agent as one argv element: Herdr pastes it
// (bracketed paste when the agent asked for it) and presses Enter, and returns
// once both are written, before the agent reacts. It returns the agent as
// Herdr saw it then, usually still idle.
//
// Herdr refuses a blocked agent (IsBlocked) and sends nothing, but it sends to
// a working or unknown agent without complaint: holding those is the
// caller's job.
func (c *Client) Prompt(ctx context.Context, target, text string) (Agent, error) {
	if text == "" || len(text) > MaxPromptBytes {
		return Agent{}, model.NewError(model.ErrorInvalidRequest, "an inline prompt is 1 to 8192 bytes").
			WithDetails(map[string]any{"reason": "prompt_size", "bytes": len(text), "limit": MaxPromptBytes})
	}
	var r struct {
		Agent wireAgent `json:"agent"`
	}
	if err := c.result(ctx, 0, "agent prompt", &r, "agent", "prompt", target, text); err != nil {
		return Agent{}, err
	}
	return r.Agent.agent(), nil
}

// Wait blocks until the agent is in one of the states, for at most timeout.
// No states means Herdr's default: idle, done or blocked. A state that already
// holds returns at once. Running out is IsTimeout (model timeout), and Herdr
// then says nothing about the agent's state. An agent that exits or loses its
// pane meanwhile is IsGone.
func (c *Client) Wait(ctx context.Context, target string, states []model.Activity, timeout time.Duration) (Agent, error) {
	if timeout <= 0 {
		return Agent{}, model.NewError(model.ErrorInvalidRequest, "a wait needs a timeout")
	}
	args := []string{"agent", "wait", target}
	for _, s := range states {
		if !s.Valid() {
			return Agent{}, model.NewError(model.ErrorInvalidRequest, "unknown agent state").WithDetail("state", string(s))
		}
		args = append(args, "--until", string(s))
	}
	ms := timeout.Milliseconds()
	if ms == 0 {
		ms = 1
	}
	args = append(args, "--timeout", strconv.FormatInt(ms, 10))
	var r struct {
		Agent wireAgent `json:"agent"`
	}
	if err := c.result(ctx, timeout+10*time.Second, "agent wait", &r, args...); err != nil {
		return Agent{}, err
	}
	return r.Agent.agent(), nil
}

// ReadAgent returns up to lines of the agent's recent output, soft wraps
// joined, as plain text (`agent read --source recent-unwrapped`).
func (c *Client) ReadAgent(ctx context.Context, target string, lines int) (string, error) {
	args := []string{"agent", "read", target, "--source", "recent-unwrapped"}
	if lines > 0 {
		args = append(args, "--lines", strconv.Itoa(lines))
	}
	out, err := c.call(ctx, 0, "agent read", args...)
	return string(out), err
}
