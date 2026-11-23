// Package hc speaks herdr-core generation 1 over its line JSON-RPC transport.
package hc

import (
	"errors"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

type Core struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Hello struct {
	Generation              int    `json:"generation"`
	Version                 string `json:"version"`
	Core                    Core   `json:"core"`
	KeepsRunningAfterLogout *bool  `json:"keeps_running_after_logout"`
}
type Status struct {
	App   string `json:"app"`
	State string `json:"state"`
	Kind  string `json:"kind"`
	Msg   string `json:"msg"`
}
type Foreground struct {
	PID  int64    `json:"pid"`
	Name string   `json:"name"`
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
}
type Terminal struct {
	Address    string            `json:"address"`
	Name       *string           `json:"name"`
	PID        *int64            `json:"pid"`
	Labels     map[string]string `json:"labels"`
	State      string            `json:"state"`
	Reason     *string           `json:"reason"`
	ExitCode   *int              `json:"exit_code"`
	Argv       []string          `json:"argv"`
	Cwd        string            `json:"cwd"`
	Foreground *Foreground       `json:"foreground"`
	Status     *Status           `json:"status"`
	Reported   bool              `json:"reported"`
	Parent     *string           `json:"parent"`
	Keep       bool              `json:"keep"`
	Turn       uint64            `json:"turn"`
}

func (t Terminal) Activity() model.Activity {
	if t.State != "live" || t.Status == nil {
		return model.ActivityUnknown
	}
	switch t.Status.State {
	case "idle":
		return model.ActivityIdle
	case "working":
		return model.ActivityWorking
	case "done":
		return model.ActivityDone
	case "blocked", "error":
		return model.ActivityBlocked
	}
	return model.ActivityUnknown
}

type RunOptions struct {
	Argv     []string          `json:"argv"`
	Cwd      string            `json:"cwd"`
	Env      map[string]string `json:"env"`
	Labels   map[string]string `json:"labels"`
	Name     *string           `json:"name,omitempty"`
	NoParent bool              `json:"no_parent"`
	NoNotify bool              `json:"no_notify"`
	Keep     bool              `json:"keep"`
}
type SendResult struct {
	Started *bool   `json:"started"`
	Status  *string `json:"status"`
	Turn    uint64  `json:"turn"`
}
type WaitResult struct {
	Terminal string  `json:"terminal"`
	Ended    bool    `json:"ended"`
	Reason   string  `json:"reason"`
	Status   *string `json:"status"`
	Turn     uint64  `json:"turn"`
}
type Snapshot struct {
	Seq       uint64     `json:"seq"`
	Terminals []Terminal `json:"terminals"`
}
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Kind string `json:"kind"`
		Hint string `json:"hint"`
	} `json:"data"`
}

func (e *Error) Error() string { return "hc: " + e.Data.Kind }
func IsKind(err error, kind string) bool {
	var e *Error
	return errors.As(err, &e) && e.Data.Kind == kind
}
func IsGone(err error) bool { return IsKind(err, "not_found") || IsKind(err, "finished") }
func (e *Error) Protocol() *model.Error {
	code := model.ErrorLaunchFailed
	switch e.Data.Kind {
	case "name_taken":
		code = model.ErrorNameTaken
	case "not_found", "finished":
		code = model.ErrorNotFound
	case "invalid_params":
		code = model.ErrorInvalidRequest
	case "timeout":
		code = model.ErrorTimeout
	}
	return model.NewError(code, "herdr-core request failed").WithDetail("reason", e.Data.Kind)
}

var ErrGeneration = errors.New("unsupported hc protocol generation")

// GenerationError retains the hello observation without accepting its protocol.
type GenerationError struct{ Hello Hello }

func (e *GenerationError) Error() string { return ErrGeneration.Error() }
func (e *GenerationError) Unwrap() error { return ErrGeneration }
