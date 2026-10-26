// Package harness builds isolated agent launches and runs headless turns.
// Prompts are supplied on stdin; callers must never add agent text to launch arguments.
package harness

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Launch describes one lease generation. SessionID is required for Claude, even
// on a first turn; Resume distinguishes an assigned new id from an existing one.
// For Codex/OpenCode, a nonempty SessionID requires Resume. ExtraEnv contains
// trusted caller configuration, never agent-supplied text. Identity and per-launch
// config environment variables cannot be overridden.
type Launch struct {
	Harness   model.Harness
	Profile   model.Profile
	WorkDir   string
	StateDir  string
	LaunchID  string
	MemberID  string
	Binary    string // Absolute lugarn-comms executable, not the harness executable.
	SessionID string
	Resume    bool
	ExtraEnv  map[string]string
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var openCodeSessionPattern = regexp.MustCompile(`^ses_[A-Za-z0-9]{1,128}$`)
var envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validSession(h model.Harness, id string) bool {
	if h == model.HarnessOpenCode {
		return openCodeSessionPattern.MatchString(id)
	}
	return (h == model.HarnessClaude || h == model.HarnessCodex) && uuidPattern.MatchString(id)
}

// Validate checks all caller-controlled values that can reach argv or files.
func (l Launch) Validate() error {
	bad := func(s string) error { return model.NewError(model.ErrorInvalidRequest, s) }
	if !l.Harness.Valid() || !l.Profile.Valid() {
		return bad("invalid harness or profile")
	}
	if err := model.ValidateULID(l.LaunchID); err != nil {
		return err
	}
	if err := model.ValidateOpaqueID(l.MemberID); err != nil {
		return err
	}
	for _, p := range []string{l.WorkDir, l.StateDir, l.Binary} {
		if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\r\n") {
			return bad("launch paths must be absolute and contain no NUL or newline")
		}
	}
	if l.SessionID != "" && !validSession(l.Harness, l.SessionID) {
		return bad("invalid harness session id")
	}
	if l.Harness == model.HarnessClaude && l.SessionID == "" {
		return bad("Claude requires a caller-assigned session id")
	}
	if l.Resume && l.SessionID == "" {
		return bad("resume requires a session id")
	}
	if l.Harness != model.HarnessClaude && l.SessionID != "" && !l.Resume {
		return bad("reported sessions must be resumed")
	}
	for k, v := range l.ExtraEnv {
		if !envPattern.MatchString(k) || strings.ContainsRune(v, 0) {
			return bad("invalid environment entry")
		}
		for _, reserved := range []string{"LUGARN_COMMS_MEMBER", "LUGARN_COMMS_TOKEN_FILE", "OPENCODE_CONFIG", "OPENCODE_CONFIG_CONTENT"} {
			if strings.EqualFold(k, reserved) {
				return bad("reserved launch environment: " + k)
			}
		}
	}
	return nil
}

func (l Launch) path(suffix string) string {
	return filepath.Join(l.StateDir, "launch", l.LaunchID+suffix)
}

// TokenPath is the private token file that the caller writes before launching.
func (l Launch) TokenPath() string { return l.path(".token") }
