package herdr

import (
	"regexp"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Herdr's own name rules, checked live on Herdr 0.9.5 (decision 49). Workspace
// labels are free text: Herdr took 5,000 characters, spaces, Unicode, a newline
// and the empty string, and it doesn't make them unique.
var (
	agentNamePattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	sessionNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// MaxAgentName is the longest agent name Herdr accepts, in bytes.
const MaxAgentName = 32

// WorkspaceName is a team's Herdr workspace label: lugarn-<team name>.
func WorkspaceName(team string) string { return "lugarn-" + team }

// AgentName is a member's Herdr agent name, <team name>-<member name>, when
// Herdr accepts it. Herdr allows [a-z][a-z0-9_-]{0,31}, so a team name that
// starts with a digit, or a team and member name longer than 31 bytes together,
// doesn't fit: that's invalid_request, details.reason "herdr_agent_name".
func AgentName(team, member string) (string, error) {
	name := team + "-" + member
	if err := ValidateAgentName(name); err != nil {
		return "", err
	}
	return name, nil
}

// ValidateAgentName checks a name against Herdr's agent name rule.
func ValidateAgentName(name string) error {
	if agentNamePattern.MatchString(name) {
		return nil
	}
	return model.NewError(model.ErrorInvalidRequest,
		"Herdr agent names start with a lower-case letter and have at most 32 lower-case letters, digits, '-' or '_'").
		WithDetails(map[string]any{"reason": "herdr_agent_name", "name": name, "limit": MaxAgentName})
}

// ValidateSession checks a Herdr session (server) name: 1 to 64 ASCII letters,
// digits, '.', '_' or '-', and not "." or "..".
func ValidateSession(name string) error {
	if sessionNamePattern.MatchString(name) && name != "." && name != ".." {
		return nil
	}
	return model.NewError(model.ErrorInvalidRequest, "invalid Herdr session name").
		WithDetails(map[string]any{"reason": "herdr_session_name", "session": name})
}
