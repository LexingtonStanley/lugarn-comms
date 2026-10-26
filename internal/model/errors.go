package model

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
)

// Error is the shared error record for app ops, MCP tools and the CLI.
type Error struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

// MarshalJSON emits an empty object, never null, for absent details.
func (e Error) MarshalJSON() ([]byte, error) {
	type record Error
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	return json.Marshal(record(e))
}

// NewError constructs a protocol error with empty details.
func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message, Details: map[string]any{}}
}

// WithDetails replaces the details and returns e for chaining.
func (e *Error) WithDetails(details map[string]any) *Error {
	e.Details = details
	return e
}

// WithDetail sets one detail and returns e for chaining.
func (e *Error) WithDetail(key string, value any) *Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details[key] = value
	return e
}

// AsError preserves protocol errors, including wrapped ones. Unknown errors
// become internal errors with only safe name/path diagnostics. A nil error returns nil.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var mapped interface{ Protocol() *Error }
	if errors.As(err, &mapped) {
		return mapped.Protocol()
	}
	var protocolError *Error
	if errors.As(err, &protocolError) {
		return protocolError
	}
	return NewError(ErrorInternal, "internal error").WithDetails(ErrorLocation(err))
}

// ExitCode returns the CLI status specified by docs/protocol.md's Errors table.
// Unknown codes use the internal-error status.
func ExitCode(code ErrorCode) int {
	switch code {
	case ErrorInvalidRequest:
		return 2
	case ErrorUnauthenticated:
		return 3
	case ErrorUnsupportedVersion:
		return 4
	case ErrorNotFound:
		return 10
	case ErrorUnknownMember:
		return 11
	case ErrorDenied:
		return 20
	case ErrorSpawnDisabled:
		return 21
	case ErrorSpawnCapReached:
		return 22
	case ErrorHarnessNotAllowed:
		return 23
	case ErrorProfileTooHigh:
		return 24
	case ErrorNestingNotAllowed:
		return 25
	case ErrorNameTaken:
		return 26
	case ErrorTeamNotRunning:
		return 30
	case ErrorTeamRunning:
		return 31
	case ErrorRunnerUnavailable:
		return 32
	case ErrorHerdrUnavailable:
		return 33
	case ErrorHarnessUnavailable:
		return 34
	case ErrorLaunchFailed:
		return 35
	case ErrorLeaseConflict:
		return 40
	case ErrorRevisionConflict:
		return 41
	case ErrorCursorInvalid:
		return 42
	case ErrorTimeout:
		return 43
	default:
		return 1
	}
}

// ErrorLocation extracts names and paths without formatting an error's payload.
// In particular, exec output, JSON values and arbitrary wrapped text are private.
func ErrorLocation(err error) map[string]any {
	details := map[string]any{}
	var path *os.PathError
	var link *os.LinkError
	var executable *exec.Error
	switch {
	case errors.As(err, &path):
		details["op"], details["path"] = path.Op, path.Path
	case errors.As(err, &link):
		details["op"], details["path"], details["target"] = link.Op, link.Old, link.New
	case errors.As(err, &executable):
		details["name"] = executable.Name
	}
	return details
}
