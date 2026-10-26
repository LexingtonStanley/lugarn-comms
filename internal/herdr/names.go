package herdr

import (
	"crypto/sha256"
	"encoding/base32"
	"regexp"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Herdr's own name rules, checked live on Herdr 0.9.5 (decision 49). Workspace
// labels are free text: Herdr took 5,000 characters, spaces, Unicode, a newline
// and the empty string, and it doesn't make them unique, so the broker finds a
// workspace by the id it recorded, never by its label.
var (
	agentNamePattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	sessionNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// MaxAgentName is the longest agent name Herdr accepts, in bytes.
const MaxAgentName = 32

const (
	agentNameCut = 25
	agentTagLen  = 6
)

// tagEncoding is Crockford's base32 alphabet in lower case.
var tagEncoding = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)

// WorkspaceName is a team's Herdr workspace label: lugarn-<team name>. It's a
// label for people; the broker records the workspace's id.
func WorkspaceName(team string) string { return "lugarn-" + team }

// AgentName is a member's Herdr agent name (Herdr names, decision 5): the
// member name cut to 25 characters, '-', and a tag, the first 6 characters of
// the lower-case Crockford base32 of SHA-256("<team id>/<member name>"), such as
// "writer-k2m9xa". The name is stable across launches, and the tag keeps two
// teams' members apart on one server. For any valid member name
// ([a-z][a-z0-9-]{0,31}) it's at most 32 characters and starts with a letter,
// so it always passes ValidateAgentName.
func AgentName(teamID, member string) string {
	sum := sha256.Sum256([]byte(teamID + "/" + member))
	tag := tagEncoding.EncodeToString(sum[:])[:agentTagLen]
	if len(member) > agentNameCut {
		member = member[:agentNameCut]
	}
	return member + "-" + tag
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
// digits, '.', '_' or '-', and not "." or "..". Every one of those characters
// is also valid in a systemd unit name, so the systemd method's unit name
// carries it unescaped.
func ValidateSession(name string) error {
	if sessionNamePattern.MatchString(name) && name != "." && name != ".." {
		return nil
	}
	return model.NewError(model.ErrorInvalidRequest, "invalid Herdr session name").
		WithDetails(map[string]any{"reason": "herdr_session_name", "session": name})
}
