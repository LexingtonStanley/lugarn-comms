// Package store provides transactional persistence for the lugarn-comms protocol.
package store

import (
	"encoding/json"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Team is the stored team configuration and runtime state.
type Team struct {
	ID                 string
	Name               string
	Pattern            model.Pattern
	SpecVersion        int
	Revision           int64
	Run                int64
	State              model.TeamState
	HerdrServer        string
	HerdrWorkspace     *string
	HopBudget          int64
	ReviewRounds       int64
	PerRecipient       int64
	HeadlessTurns      int64
	TurnTimeoutSeconds int64
	Runner             model.Runner
	LastAppContactAt   *model.Time
	CreatedAt          model.Time
	UpdatedAt          model.Time
	StartedAt          *model.Time
	StoppedAt          *model.Time
}

// TeamPatch changes only non-nil fields. Revision and run are changed by their
// dedicated accessors so concurrent revision checks remain explicit.
type TeamPatch struct {
	Name               *string
	Pattern            *model.Pattern
	State              *model.TeamState
	HerdrServer        *string
	HerdrWorkspace     **string
	HopBudget          *int64
	ReviewRounds       *int64
	PerRecipient       *int64
	HeadlessTurns      *int64
	TurnTimeoutSeconds *int64
	StartedAt          **model.Time
	StoppedAt          **model.Time
}

// MemberParams contains the stored fields needed to insert a member.
type MemberParams struct {
	ID         string
	TeamID     string
	Kind       model.MemberKind
	Name       string
	Role       *model.Role
	Origin     model.MemberOrigin
	Position   *int64
	SpawnedBy  *string
	SpawnedRun *int64
	Harness    *model.Harness
	Profile    *model.Profile
	Mode       *model.Mode
	Folder     *string
	Worktree   bool
	WorktreeID *string
	Brief      *string
	Task       *string
	State      model.MemberState
	StopReason *model.StopReason
	Activity   *model.Activity
	LastDriver *model.Mode
	SessionID  *string
	HerdrAgent *string
	StartedAt  *model.Time
	StoppedAt  *model.Time
	RemovedAt  *model.Time
	RemovedWhy *model.RemovedReason
	CreatedAt  *model.Time
	UpdatedAt  *model.Time
}

// StoredMember adds configuration needed to rebuild a team spec to Member.
type StoredMember struct {
	Member     model.Member
	Position   *int64
	SpawnedRun *int64
	Worktree   bool
	WorktreeID *string
	Brief      *string
	Task       *string
	UpdatedAt  model.Time
}

// MemberPatch changes a member's stored configuration. State, activity,
// removal and current session have dedicated accessors.
type MemberPatch struct {
	Name       *string
	Role       **model.Role
	Position   **int64
	Harness    **model.Harness
	Profile    **model.Profile
	Mode       **model.Mode
	Folder     **string
	Worktree   *bool
	WorktreeID **string
	Brief      **string
	Task       **string
	LastDriver **model.Mode
	HerdrAgent **string
}

// GridRow is one stored grid capability.
type GridRow struct {
	TeamID     string
	FromID     string
	ToID       string
	Capability model.GridCapability
	CreatedAt  model.Time
}

// GridEdge identifies a directional pair and all its granted capabilities.
type GridEdge struct {
	TeamID string
	FromID string
	ToID   string
	Can    []model.GridCapability
}

// WorktreeRecord is the store-only identity and lifecycle of a worktree.
type WorktreeRecord struct {
	ID        string
	TeamID    string
	MemberID  string
	Repo      string
	Path      string
	Branch    string
	Base      string
	CreatedAt model.Time
	RemovedAt *model.Time
}

// Hold is a person's hold on a member.
type Hold struct {
	MemberID   string
	TeamID     string
	Since      model.Time
	HeldUntil  model.Time
	ReleasedAt *model.Time
}

// SpawnSettings is the fully defaulted stored spawn configuration.
type SpawnSettings struct {
	MemberID      string
	Enabled       bool
	RunningAtOnce int64
	PerRun        int64
	Harnesses     *[]model.Harness
	Mode          model.Mode
	Folder        model.SpawnFolder
	MaxProfile    model.Profile
	Brief         *string
}

// SpawnCount is one row of the spawn_counts view.
type SpawnCount struct {
	SpawnerID string
	TeamID    string
	Run       int64
	Running   int64
	ThisRun   int64
}

// LeaseRecord adds the member identity and update time to a common lease.
type LeaseRecord struct {
	MemberID  string
	Lease     model.Lease
	UpdatedAt model.Time
}

// Launch is a stored launch and its lifecycle.
type Launch struct {
	ID             string
	MemberID       string
	Generation     int64
	Driver         model.Mode
	SessionIDStart *string
	PaneID         *string
	StartedAt      model.Time
	EndedAt        *model.Time
	EndReason      *model.LaunchEndReason
}

// Token is a stored token hash; the plaintext token is never accepted here.
type Token struct {
	LaunchID  string
	MemberID  string
	Hash      string
	CreatedAt model.Time
	RevokedAt *model.Time
}

// MemberSessionRecord includes the seal, which is deliberately absent from
// the common.v1 MemberSession record.
type MemberSessionRecord struct {
	MemberID    string
	Ordinal     int64
	SessionID   string
	Harness     model.Harness
	Source      model.SessionSource
	LaunchID    *string
	Seal        string
	BriefedAt   *model.Time
	FirstSeenAt model.Time
}

// HopCounter is a team's stored hop budget.
type HopCounter struct {
	TeamID      string
	Used        int64
	Allowance   int64
	ExhaustedAt *model.Time
	UpdatedAt   model.Time
}

// RoundCounter is a writer/reviewer pair's stored review budget.
type RoundCounter struct {
	TeamID      string
	WriterID    string
	ReviewerID  string
	Used        int64
	Allowance   int64
	ExhaustedAt *model.Time
	UpdatedAt   model.Time
}

// TurnParams contains store-only turn fields in addition to model.Turn.
type TurnParams struct {
	model.Turn
	MemberID string
	Seal     *string
	UsageRaw json.RawMessage
}

// TurnPatch changes runtime fields on a turn. EndedAt is required by SQLite
// for terminal statuses and prohibited for live statuses.
type TurnPatch struct {
	Status       *model.TurnStatus
	SessionIDEnd **string
	PID          **int64
	ExitCode     **int64
	Error        **string
	Usage        **model.Usage
	UsageRaw     *json.RawMessage
	EndedAt      **model.Time
}

// MessageParams contains the stored references omitted from model.Message.
type MessageParams struct {
	model.Message
	SenderID string
	Run      int64
}

// DeliveryParams contains the stored references omitted from model.Delivery.
type DeliveryParams struct {
	model.Delivery
	MessageID       string
	RecipientID     string
	LeaseGeneration *int64
	CreatedAt       *model.Time
}

// DeliveryPatch is a guarded delivery update. Every non-nil field is written.
type DeliveryPatch struct {
	State           model.DeliveryState
	Reason          **model.DeliveryReason
	Via             **model.DeliveryVia
	Wake            *bool
	Attempts        *int64
	Notify          **bool
	Turn            **string
	LeaseGeneration **int64
	UpdatedAt       model.Time
	DeliveredAt     **model.Time
	// ExpectedLeaseGeneration also checks the recipient's current lease.
	ExpectedLeaseGeneration *int64
}

// DeliveryItem adds store ordering and message identity to a common delivery.
type DeliveryItem struct {
	MessageID       string
	MessageSeq      int64
	LeaseGeneration *int64
	Delivery        model.Delivery
}

// RequestParams contains the member id omitted from model.Request's reference.
type RequestParams struct {
	model.Request
	MemberID      *string
	RequestedByID string
}

// RequestPatch changes a request's runtime status and result.
type RequestPatch struct {
	Status     model.RequestStatus
	Error      **model.Error
	StartedAt  **model.Time
	FinishedAt **model.Time
}

// Event is a stored event. Seq is converted to a protocol cursor by the
// service layer; Data is retained as the recorded JSON object.
type Event struct {
	Seq       int64
	TeamID    *string
	Type      model.EventType
	At        model.Time
	Timeline  bool
	MemberID  *string
	MessageID *string
	Data      json.RawMessage
}

// TimelinePage contains ascending events and paging indicators.
type TimelinePage struct {
	Events        []Event
	HasMoreBefore bool
	HasMoreAfter  bool
}
