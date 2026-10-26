package herdr

import (
	"errors"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Herdr's own error codes, as its CLI prints them on stderr (exit status 1) in
// {"id": …, "error": {"code", "message"}}. These are the ones the client
// classifies; Error.Code carries whatever Herdr sent.
const (
	CodeServerNotRunning   = "server_not_running"
	CodeAgentNotFound      = "agent_not_found"
	CodeAgentNotRunning    = "agent_not_running"
	CodeAgentPaneNotFound  = "agent_pane_not_found"
	CodePaneNotFound       = "pane_not_found"
	CodeWorkspaceNotFound  = "workspace_not_found"
	CodeAgentBlocked       = "agent_blocked"
	CodeAgentNotReady      = "agent_not_ready"
	CodeAgentPaneBusy      = "agent_pane_busy"
	CodeAgentNameTaken     = "agent_name_taken"
	CodeInvalidAgentName   = "invalid_agent_name"
	CodeAgentPromptStalled = "agent_prompt_stalled"
	CodeEmptyAgentPrompt   = "empty_agent_prompt"
	CodeTimeout            = "timeout"
)

type kind int

const (
	kindOther kind = iota
	kindNotInstalled
	kindNotRunning
	kindGone
	kindBlocked
	kindTimeout
	kindInvalid
)

// Error is a failed Herdr call. It unwraps to the mapped protocol error, so
// model.AsError(err) returns it, with Herdr's code and message in Details.
type Error struct {
	// Op is the Herdr command, such as "agent prompt".
	Op string
	// Code is Herdr's own error code, or empty when Herdr didn't give one.
	Code string
	// Message is Herdr's own message, or what the client saw instead.
	Message string

	kind     kind
	protocol *model.Error
}

func (e *Error) Error() string {
	if e.Code != "" {
		return "herdr " + e.Op + ": " + e.Code + ": " + e.Message
	}
	return "herdr " + e.Op + ": " + e.Message
}

// Unwrap returns the protocol error.
func (e *Error) Unwrap() error { return e.protocol }

// Protocol returns the mapped protocol error.
func (e *Error) Protocol() *model.Error { return e.protocol }

// IsGone reports whether the agent, pane or workspace the call named no longer
// exists: Herdr's agent_not_found, agent_not_running, agent_pane_not_found,
// pane_not_found or workspace_not_found. The runner treats it as a pane exit.
// An agent that exited leaves its pane behind (Client.PaneExists tells which).
func IsGone(err error) bool { return kindOf(err) == kindGone }

// IsBlocked reports whether Herdr refused a prompt because the agent sits at an
// approval or question dialog (agent_blocked). Herdr sent nothing.
func IsBlocked(err error) bool { return kindOf(err) == kindBlocked }

// IsTimeout reports whether Herdr's own bounded wait ran out (agent wait, or
// agent start's readiness wait).
func IsTimeout(err error) bool { return kindOf(err) == kindTimeout }

// IsUnavailable reports herdr_unavailable: Herdr isn't installed, or its server
// isn't running or didn't answer within the call's timeout.
func IsUnavailable(err error) bool {
	k := kindOf(err)
	return k == kindNotInstalled || k == kindNotRunning
}

func kindOf(err error) kind {
	var e *Error
	if errors.As(err, &e) {
		return e.kind
	}
	return kindOther
}

// launchOps are the commands whose failure is a launch failure (launch_failed):
// the workspace, the pane or the harness didn't come up.
var launchOps = map[string]bool{"workspace create": true, "pane split": true, "agent start": true}

// fromHerdr classifies an error Herdr reported.
func fromHerdr(op, code, message string) *Error {
	e := &Error{Op: op, Code: code, Message: message}
	switch code {
	case CodeServerNotRunning, "server_unavailable":
		e.kind = kindNotRunning
	case CodeAgentNotFound, CodeAgentNotRunning, CodeAgentPaneNotFound, CodePaneNotFound, CodeWorkspaceNotFound:
		e.kind = kindGone
	case CodeAgentBlocked:
		e.kind = kindBlocked
	case CodeTimeout:
		e.kind = kindTimeout
	case CodeInvalidAgentName, CodeEmptyAgentPrompt, "invalid_agent_timeout":
		e.kind = kindInvalid
	}
	return e.mapped()
}

// clientError is a failure the client saw rather than one Herdr reported.
func clientError(op string, k kind, message string) *Error {
	return (&Error{Op: op, Message: message, kind: k}).mapped()
}

func (e *Error) mapped() *Error {
	details := map[string]any{"herdr_op": e.Op, "herdr_message": e.Message}
	if e.Code != "" {
		details["herdr_code"] = e.Code
	}
	var p *model.Error
	switch e.kind {
	case kindNotInstalled:
		details["reason"] = "not_installed"
		p = model.NewError(model.ErrorHerdrUnavailable, "Herdr isn't installed")
	case kindNotRunning:
		details["reason"] = "not_running"
		p = model.NewError(model.ErrorHerdrUnavailable, "the Herdr server isn't running")
	case kindGone:
		details["reason"] = "gone"
		p = model.NewError(model.ErrorNotFound, "the Herdr agent or pane is gone")
	case kindTimeout:
		if launchOps[e.Op] {
			// agent start's readiness wait: the harness never came up.
			details["cause"] = e.Message
			p = model.NewError(model.ErrorLaunchFailed, "herdr "+e.Op+" timed out")
		} else {
			p = model.NewError(model.ErrorTimeout, "Herdr's wait ran out")
		}
	case kindInvalid:
		p = model.NewError(model.ErrorInvalidRequest, "Herdr refused the request")
	default:
		details["cause"] = e.Message
		if launchOps[e.Op] {
			p = model.NewError(model.ErrorLaunchFailed, "herdr "+e.Op+" failed")
		} else {
			p = model.NewError(model.ErrorInternal, "herdr "+e.Op+" failed")
		}
	}
	e.protocol = p.WithDetails(details)
	return e
}
