package model

import (
	"bytes"
	"encoding/json"
)

// Schema defaults are exposed for the policy layer; decoding never applies them.
const (
	DefaultHerdrServer        = "default"
	DefaultHopBudget          = 16
	DefaultReviewRounds       = 3
	DefaultPerRecipient       = 1
	DefaultHeadlessTurns      = 3
	DefaultTurnTimeoutSeconds = 1800
	DefaultWorktree           = false
	DefaultSpawnRunningAtOnce = 3
	DefaultSpawnPerRun        = 8
	DefaultSpawnMode          = ModeHeadless
	DefaultSpawnFolder        = SpawnFolderWorktree
	DefaultSpawnMaxProfile    = ProfileWorkspace
)

// TeamSpec is the request-side v1 team specification.
type TeamSpec struct {
	Version int            `json:"version"`
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Pattern Pattern        `json:"pattern"`
	Herdr   *HerdrSettings `json:"herdr,omitempty"`
	Limits  *Limits        `json:"limits,omitempty"`
	Members []MemberSpec   `json:"members"`
	Grid    []GridEdge     `json:"grid"`
}

// HerdrSettings selects the Herdr server for a team.
type HerdrSettings struct {
	Server *string `json:"server,omitempty"`
}

// Limits holds optional limits; nil fields leave defaults to the policy layer.
type Limits struct {
	HopBudget          *int `json:"hop_budget,omitempty"`
	ReviewRounds       *int `json:"review_rounds,omitempty"`
	PerRecipient       *int `json:"per_recipient,omitempty"`
	HeadlessTurns      *int `json:"headless_turns,omitempty"`
	TurnTimeoutSeconds *int `json:"turn_timeout_seconds,omitempty"`
}

// MemberSpec configures one agent. For Task, a nil outer pointer means omitted,
// a pointer to a nil *string means explicit null, and two non-nil pointers mean
// a supplied string. This preserves all three schema states on round trips.
type MemberSpec struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Role     Role           `json:"role"`
	Harness  Harness        `json:"harness"`
	Folder   string         `json:"folder"`
	Worktree *bool          `json:"worktree,omitempty"`
	Profile  Profile        `json:"profile"`
	Mode     Mode           `json:"mode"`
	Brief    string         `json:"brief"`
	Task     **string       `json:"task,omitempty"`
	Spawn    *SpawnSettings `json:"spawn,omitempty"`
	Host     *Null          `json:"host,omitempty"`
}

// SpawnSettings controls whether and how a member may spawn workers. Harnesses
// and Brief use the same omitted/null/value pointer convention as MemberSpec.Task.
type SpawnSettings struct {
	Enabled       bool         `json:"enabled"`
	RunningAtOnce *int         `json:"running_at_once,omitempty"`
	PerRun        *int         `json:"per_run,omitempty"`
	Harnesses     **[]Harness  `json:"harnesses,omitempty"`
	Mode          *Mode        `json:"mode,omitempty"`
	Folder        *SpawnFolder `json:"folder,omitempty"`
	MaxProfile    *Profile     `json:"max_profile,omitempty"`
	Brief         **string     `json:"brief,omitempty"`
}

// GridEdge lists the capabilities from one agent to another.
type GridEdge struct {
	From string           `json:"from"`
	To   string           `json:"to"`
	Can  []GridCapability `json:"can"`
}

// UnmarshalJSON preserves explicit nulls that encoding/json otherwise collapses
// into omitted pointers. The alias retains strict checking of nested objects.
func (m *MemberSpec) UnmarshalJSON(data []byte) error {
	type plain MemberSpec
	var v plain
	if err := DecodeStrict(data, &v); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if isNull(fields["task"]) {
		v.Task = new(*string)
	}
	if isNull(fields["host"]) {
		v.Host = &Null{}
	}
	*m = MemberSpec(v)
	return nil
}

func (s *SpawnSettings) UnmarshalJSON(data []byte) error {
	type plain SpawnSettings
	var v plain
	if err := DecodeStrict(data, &v); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if isNull(fields["harnesses"]) {
		v.Harnesses = new(*[]Harness)
	}
	if isNull(fields["brief"]) {
		v.Brief = new(*string)
	}
	*s = SpawnSettings(v)
	return nil
}

func isNull(data []byte) bool { return bytes.Equal(bytes.TrimSpace(data), []byte("null")) }
