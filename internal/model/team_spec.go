package model

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

// TeamSpec is the v1 team specification shared by requests and get-team
// responses. Optional non-nullable settings remain omitted until supplied;
// nullable fields always marshal, using null when unset.
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

// MemberSpec configures one agent. An absent or null Task means no initial task;
// Host is reserved as null in v1. Both fields marshal as null when nil.
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
	Task     *string        `json:"task"`
	Spawn    *SpawnSettings `json:"spawn,omitempty"`
	Host     *Null          `json:"host"`
}

// SpawnSettings controls whether and how a member may spawn workers. Absent or
// null Harnesses allows every supported harness; absent or null Brief means no
// worker brief. Both fields marshal as null when nil.
type SpawnSettings struct {
	Enabled       bool         `json:"enabled"`
	RunningAtOnce *int         `json:"running_at_once,omitempty"`
	PerRun        *int         `json:"per_run,omitempty"`
	Harnesses     *[]Harness   `json:"harnesses"`
	Mode          *Mode        `json:"mode,omitempty"`
	Folder        *SpawnFolder `json:"folder,omitempty"`
	MaxProfile    *Profile     `json:"max_profile,omitempty"`
	Brief         *string      `json:"brief"`
}

// GridEdge lists the capabilities from one agent to another.
type GridEdge struct {
	From string           `json:"from"`
	To   string           `json:"to"`
	Can  []GridCapability `json:"can"`
}
