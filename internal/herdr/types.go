package herdr

import (
	"encoding/json"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Status is what `herdr status --json` says about the client binary and the
// session's server.
type Status struct {
	Session string
	// Installed is false when the binary couldn't be found; nothing else is set.
	Installed bool
	// Version is the client binary's version, such as "0.9.5".
	Version string
	Running bool
	// ServerVersion is the running server's version, or empty.
	ServerVersion string
	// Compatible is Herdr's own check that client and server speak the same
	// API; false when the server isn't running.
	Compatible bool
	// Socket is the session's API socket path.
	Socket string
}

// Info is the hello op's herdr block.
func (s Status) Info() model.HerdrInfo {
	info := model.HerdrInfo{Installed: s.Installed, Running: s.Running, Server: s.Session}
	if s.Version != "" {
		v := s.Version
		info.Version = &v
	}
	return info
}

// Workspace is one Herdr workspace.
type Workspace struct {
	ID    string
	Label string
	// RootTab and RootPane are the tab and pane `workspace create` made, set
	// only by CreateWorkspace. The create's --env reached that pane only.
	RootTab  string
	RootPane string
}

// Tab is one Herdr tab.
type Tab struct {
	ID        string
	Workspace string
	Label     string
	// RootPane is the pane `tab create` made, which got the create's --env.
	RootPane string
}

// Pane is one Herdr pane.
type Pane struct {
	ID        string
	Workspace string
	Tab       string
	Cwd       string
	// Agent is Herdr's label for the agent in the pane ("claude"), or empty for
	// a plain shell.
	Agent string
	// Status is Herdr's raw agent_status; Activity is its protocol mapping.
	Status   string
	Activity model.Activity
	Session  *AgentSession
}

// Agent is one live Herdr agent.
type Agent struct {
	// Name is the live agent name, or empty for an agent nobody named.
	Name string
	// Kind is Herdr's agent label: claude, codex, opencode, pi, …
	Kind      string
	Pane      string
	Workspace string
	Tab       string
	Cwd       string
	// Status is Herdr's raw agent_status; Activity is its protocol mapping.
	Status   string
	Activity model.Activity
	// Session is what the harness's Herdr integration reported about its own
	// session, or nil. See AgentSession.
	Session *AgentSession
	// Ready is Herdr's interactive_ready: it saw the agent ready for input.
	Ready bool
}

// AgentSession is Herdr's agent_session. Herdr fills it only from a harness
// integration (`herdr integration install <agent>`), which writes hooks into
// the person's global harness config; Lugarn never installs one. Checked on
// Herdr 0.9.5: Claude's hook reports at SessionStart, which a trust dialog
// holds back; Codex's reports with its first turn, not at launch; OpenCode
// reports nothing without the plugin.
type AgentSession struct {
	// Source is the reporter, such as "herdr:codex".
	Source string
	Agent  string
	// Kind is "id" or "path".
	Kind  string
	Value string
}

// SessionID returns the harness's session id when Herdr has one, else "".
func (a Agent) SessionID() string {
	if a.Session != nil && a.Session.Kind == "id" {
		return a.Session.Value
	}
	return ""
}

// Wire shapes, from `herdr api schema --json` (protocol 22).

type wireSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

func (w *wireSession) session() *AgentSession {
	if w == nil {
		return nil
	}
	s := AgentSession(*w)
	return &s
}

type wireAgent struct {
	Name             *string      `json:"name"`
	Agent            *string      `json:"agent"`
	AgentStatus      string       `json:"agent_status"`
	AgentSession     *wireSession `json:"agent_session"`
	PaneID           string       `json:"pane_id"`
	WorkspaceID      string       `json:"workspace_id"`
	TabID            string       `json:"tab_id"`
	Cwd              *string      `json:"cwd"`
	InteractiveReady *bool        `json:"interactive_ready"`
}

func (w wireAgent) agent() Agent {
	return Agent{
		Name:      str(w.Name),
		Kind:      str(w.Agent),
		Pane:      w.PaneID,
		Workspace: w.WorkspaceID,
		Tab:       w.TabID,
		Cwd:       str(w.Cwd),
		Status:    w.AgentStatus,
		Activity:  ActivityFromHerdr(w.AgentStatus),
		Session:   w.AgentSession.session(),
		Ready:     w.InteractiveReady != nil && *w.InteractiveReady,
	}
}

type wirePane struct {
	PaneID       string       `json:"pane_id"`
	WorkspaceID  string       `json:"workspace_id"`
	TabID        string       `json:"tab_id"`
	Cwd          *string      `json:"cwd"`
	Agent        *string      `json:"agent"`
	AgentStatus  string       `json:"agent_status"`
	AgentSession *wireSession `json:"agent_session"`
}

func (w wirePane) pane() Pane {
	return Pane{
		ID:        w.PaneID,
		Workspace: w.WorkspaceID,
		Tab:       w.TabID,
		Cwd:       str(w.Cwd),
		Agent:     str(w.Agent),
		Status:    w.AgentStatus,
		Activity:  ActivityFromHerdr(w.AgentStatus),
		Session:   w.AgentSession.session(),
	}
}

type wireWorkspace struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

type wireTab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

type wireStatus struct {
	Client struct {
		Version string `json:"version"`
	} `json:"client"`
	Server struct {
		Running    bool    `json:"running"`
		Version    *string `json:"version"`
		Compatible *bool   `json:"compatible"`
		Socket     string  `json:"socket"`
	} `json:"server"`
}

// envelope is a successful response: {"id": …, "result": {…}}.
type envelope struct {
	Result json.RawMessage `json:"result"`
}

// errorEnvelope is a failed one, on stderr.
type errorEnvelope struct {
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
