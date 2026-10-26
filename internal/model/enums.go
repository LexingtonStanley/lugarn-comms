package model

// Enum values follow schema/common.v1.schema.json in schema order.
// Harness is the protocol Harness enum.
type Harness string

const (
	HarnessClaude   Harness = "claude"
	HarnessCodex    Harness = "codex"
	HarnessOpenCode Harness = "opencode"
	HarnessPi       Harness = "pi"
)

// Values returns all Harness values in schema order in a new slice.
func (Harness) Values() []Harness {
	return []Harness{HarnessClaude, HarnessCodex, HarnessOpenCode, HarnessPi}
}

// Valid reports whether v is a defined Harness.
func (v Harness) Valid() bool {
	switch v {
	case HarnessClaude, HarnessCodex, HarnessOpenCode, HarnessPi:
		return true
	default:
		return false
	}
}

// Profile is the protocol Profile enum.
type Profile string

const (
	ProfileReadOnly  Profile = "read-only"
	ProfileWorkspace Profile = "workspace"
	ProfileBypass    Profile = "bypass"
)

// Values returns all Profile values in schema order in a new slice.
func (Profile) Values() []Profile {
	return []Profile{ProfileReadOnly, ProfileWorkspace, ProfileBypass}
}

// Valid reports whether v is a defined Profile.
func (v Profile) Valid() bool {
	switch v {
	case ProfileReadOnly, ProfileWorkspace, ProfileBypass:
		return true
	default:
		return false
	}
}

// Mode is the protocol Mode enum.
type Mode string

const (
	ModeHeadless Mode = "headless"
	ModePane     Mode = "pane"
)

// Values returns all Mode values in schema order in a new slice.
func (Mode) Values() []Mode {
	return []Mode{ModeHeadless, ModePane}
}

// Valid reports whether v is a defined Mode.
func (v Mode) Valid() bool {
	switch v {
	case ModeHeadless, ModePane:
		return true
	default:
		return false
	}
}

// Driver is the protocol Driver enum.
type Driver string

const (
	DriverHeadless Driver = "headless"
	DriverPane     Driver = "pane"
	DriverNone     Driver = "none"
)

// Values returns all Driver values in schema order in a new slice.
func (Driver) Values() []Driver {
	return []Driver{DriverHeadless, DriverPane, DriverNone}
}

// Valid reports whether v is a defined Driver.
func (v Driver) Valid() bool {
	switch v {
	case DriverHeadless, DriverPane, DriverNone:
		return true
	default:
		return false
	}
}

// Pattern is the protocol Pattern enum.
type Pattern string

const (
	PatternOrchestrator   Pattern = "orchestrator"
	PatternManagerWorkers Pattern = "manager_workers"
	PatternWriterReviewer Pattern = "writer_reviewer"
	PatternPipeline       Pattern = "pipeline"
	PatternCustom         Pattern = "custom"
)

// Values returns all Pattern values in schema order in a new slice.
func (Pattern) Values() []Pattern {
	return []Pattern{PatternOrchestrator, PatternManagerWorkers, PatternWriterReviewer, PatternPipeline, PatternCustom}
}

// Valid reports whether v is a defined Pattern.
func (v Pattern) Valid() bool {
	switch v {
	case PatternOrchestrator, PatternManagerWorkers, PatternWriterReviewer, PatternPipeline, PatternCustom:
		return true
	default:
		return false
	}
}

// Role is the protocol Role enum.
type Role string

const (
	RoleOrchestrator Role = "orchestrator"
	RoleManager      Role = "manager"
	RoleWorker       Role = "worker"
	RoleWriter       Role = "writer"
	RoleReviewer     Role = "reviewer"
	RoleStage        Role = "stage"
	RoleCustom       Role = "custom"
)

// Values returns all Role values in schema order in a new slice.
func (Role) Values() []Role {
	return []Role{RoleOrchestrator, RoleManager, RoleWorker, RoleWriter, RoleReviewer, RoleStage, RoleCustom}
}

// Valid reports whether v is a defined Role.
func (v Role) Valid() bool {
	switch v {
	case RoleOrchestrator, RoleManager, RoleWorker, RoleWriter, RoleReviewer, RoleStage, RoleCustom:
		return true
	default:
		return false
	}
}

// GridCapability is the protocol GridCapability enum.
type GridCapability string

const (
	GridCapabilityMessage GridCapability = "message"
	GridCapabilityWake    GridCapability = "wake"
)

// Values returns all GridCapability values in schema order in a new slice.
func (GridCapability) Values() []GridCapability {
	return []GridCapability{GridCapabilityMessage, GridCapabilityWake}
}

// Valid reports whether v is a defined GridCapability.
func (v GridCapability) Valid() bool {
	switch v {
	case GridCapabilityMessage, GridCapabilityWake:
		return true
	default:
		return false
	}
}

// MemberKind is the protocol MemberKind enum.
type MemberKind string

const (
	MemberKindAgent MemberKind = "agent"
	MemberKindHuman MemberKind = "human"
)

// Values returns all MemberKind values in schema order in a new slice.
func (MemberKind) Values() []MemberKind {
	return []MemberKind{MemberKindAgent, MemberKindHuman}
}

// Valid reports whether v is a defined MemberKind.
func (v MemberKind) Valid() bool {
	switch v {
	case MemberKindAgent, MemberKindHuman:
		return true
	default:
		return false
	}
}

// MemberOrigin is the protocol MemberOrigin enum.
type MemberOrigin string

const (
	MemberOriginSpec     MemberOrigin = "spec"
	MemberOriginSpawn    MemberOrigin = "spawn"
	MemberOriginImplicit MemberOrigin = "implicit"
)

// Values returns all MemberOrigin values in schema order in a new slice.
func (MemberOrigin) Values() []MemberOrigin {
	return []MemberOrigin{MemberOriginSpec, MemberOriginSpawn, MemberOriginImplicit}
}

// Valid reports whether v is a defined MemberOrigin.
func (v MemberOrigin) Valid() bool {
	switch v {
	case MemberOriginSpec, MemberOriginSpawn, MemberOriginImplicit:
		return true
	default:
		return false
	}
}

// MemberState is the protocol MemberState enum.
type MemberState string

const (
	MemberStateStarting MemberState = "starting"
	MemberStateRunning  MemberState = "running"
	MemberStateStopping MemberState = "stopping"
	MemberStateStopped  MemberState = "stopped"
)

// Values returns all MemberState values in schema order in a new slice.
func (MemberState) Values() []MemberState {
	return []MemberState{MemberStateStarting, MemberStateRunning, MemberStateStopping, MemberStateStopped}
}

// Valid reports whether v is a defined MemberState.
func (v MemberState) Valid() bool {
	switch v {
	case MemberStateStarting, MemberStateRunning, MemberStateStopping, MemberStateStopped:
		return true
	default:
		return false
	}
}

// StopReason is the protocol StopReason enum.
type StopReason string

const (
	StopReasonPerson       StopReason = "person"
	StopReasonSpawner      StopReason = "spawner"
	StopReasonTeamStopped  StopReason = "team_stopped"
	StopReasonExited       StopReason = "exited"
	StopReasonDied         StopReason = "died"
	StopReasonLaunchFailed StopReason = "launch_failed"
)

// Values returns all StopReason values in schema order in a new slice.
func (StopReason) Values() []StopReason {
	return []StopReason{StopReasonPerson, StopReasonSpawner, StopReasonTeamStopped, StopReasonExited, StopReasonDied, StopReasonLaunchFailed}
}

// Valid reports whether v is a defined StopReason.
func (v StopReason) Valid() bool {
	switch v {
	case StopReasonPerson, StopReasonSpawner, StopReasonTeamStopped, StopReasonExited, StopReasonDied, StopReasonLaunchFailed:
		return true
	default:
		return false
	}
}

// RemovedReason is the protocol RemovedReason enum.
type RemovedReason string

const (
	RemovedReasonRemovedFromSpec RemovedReason = "removed_from_spec"
	RemovedReasonNewRun          RemovedReason = "new_run"
)

// Values returns all RemovedReason values in schema order in a new slice.
func (RemovedReason) Values() []RemovedReason {
	return []RemovedReason{RemovedReasonRemovedFromSpec, RemovedReasonNewRun}
}

// Valid reports whether v is a defined RemovedReason.
func (v RemovedReason) Valid() bool {
	switch v {
	case RemovedReasonRemovedFromSpec, RemovedReasonNewRun:
		return true
	default:
		return false
	}
}

// Activity is the protocol Activity enum.
type Activity string

const (
	ActivityIdle    Activity = "idle"
	ActivityWorking Activity = "working"
	ActivityBlocked Activity = "blocked"
	ActivityDone    Activity = "done"
	ActivityUnknown Activity = "unknown"
)

// Values returns all Activity values in schema order in a new slice.
func (Activity) Values() []Activity {
	return []Activity{ActivityIdle, ActivityWorking, ActivityBlocked, ActivityDone, ActivityUnknown}
}

// Valid reports whether v is a defined Activity.
func (v Activity) Valid() bool {
	switch v {
	case ActivityIdle, ActivityWorking, ActivityBlocked, ActivityDone, ActivityUnknown:
		return true
	default:
		return false
	}
}

// SessionSource is the protocol SessionSource enum.
type SessionSource string

const (
	SessionSourceAssigned SessionSource = "assigned"
	SessionSourceReported SessionSource = "reported"
	SessionSourcePointer  SessionSource = "pointer"
)

// Values returns all SessionSource values in schema order in a new slice.
func (SessionSource) Values() []SessionSource {
	return []SessionSource{SessionSourceAssigned, SessionSourceReported, SessionSourcePointer}
}

// Valid reports whether v is a defined SessionSource.
func (v SessionSource) Valid() bool {
	switch v {
	case SessionSourceAssigned, SessionSourceReported, SessionSourcePointer:
		return true
	default:
		return false
	}
}

// TeamState is the protocol TeamState enum.
type TeamState string

const (
	TeamStateStopped  TeamState = "stopped"
	TeamStateStarting TeamState = "starting"
	TeamStateRunning  TeamState = "running"
	TeamStateStopping TeamState = "stopping"
)

// Values returns all TeamState values in schema order in a new slice.
func (TeamState) Values() []TeamState {
	return []TeamState{TeamStateStopped, TeamStateStarting, TeamStateRunning, TeamStateStopping}
}

// Valid reports whether v is a defined TeamState.
func (v TeamState) Valid() bool {
	switch v {
	case TeamStateStopped, TeamStateStarting, TeamStateRunning, TeamStateStopping:
		return true
	default:
		return false
	}
}

// RunnerState is the protocol RunnerState enum.
type RunnerState string

const (
	RunnerStateUp   RunnerState = "up"
	RunnerStateDown RunnerState = "down"
)

// Values returns all RunnerState values in schema order in a new slice.
func (RunnerState) Values() []RunnerState {
	return []RunnerState{RunnerStateUp, RunnerStateDown}
}

// Valid reports whether v is a defined RunnerState.
func (v RunnerState) Valid() bool {
	switch v {
	case RunnerStateUp, RunnerStateDown:
		return true
	default:
		return false
	}
}

// MessageKind is the protocol MessageKind enum.
type MessageKind string

const (
	MessageKindMessage   MessageKind = "message"
	MessageKindReply     MessageKind = "reply"
	MessageKindAutoReply MessageKind = "auto_reply"
	MessageKindTask      MessageKind = "task"
)

// Values returns all MessageKind values in schema order in a new slice.
func (MessageKind) Values() []MessageKind {
	return []MessageKind{MessageKindMessage, MessageKindReply, MessageKindAutoReply, MessageKindTask}
}

// Valid reports whether v is a defined MessageKind.
func (v MessageKind) Valid() bool {
	switch v {
	case MessageKindMessage, MessageKindReply, MessageKindAutoReply, MessageKindTask:
		return true
	default:
		return false
	}
}

// DeliveryState is the protocol DeliveryState enum.
type DeliveryState string

const (
	DeliveryStateQueued     DeliveryState = "queued"
	DeliveryStateDelivering DeliveryState = "delivering"
	DeliveryStateDelivered  DeliveryState = "delivered"
	DeliveryStateHeld       DeliveryState = "held"
	DeliveryStateUncertain  DeliveryState = "uncertain"
	DeliveryStateDenied     DeliveryState = "denied"
	DeliveryStateExpired    DeliveryState = "expired"
)

// Values returns all DeliveryState values in schema order in a new slice.
func (DeliveryState) Values() []DeliveryState {
	return []DeliveryState{DeliveryStateQueued, DeliveryStateDelivering, DeliveryStateDelivered, DeliveryStateHeld, DeliveryStateUncertain, DeliveryStateDenied, DeliveryStateExpired}
}

// Valid reports whether v is a defined DeliveryState.
func (v DeliveryState) Valid() bool {
	switch v {
	case DeliveryStateQueued, DeliveryStateDelivering, DeliveryStateDelivered, DeliveryStateHeld, DeliveryStateUncertain, DeliveryStateDenied, DeliveryStateExpired:
		return true
	default:
		return false
	}
}

// DeliveryReason is the protocol DeliveryReason enum.
type DeliveryReason string

const (
	DeliveryReasonRecipientBlocked DeliveryReason = "recipient_blocked"
	DeliveryReasonRecipientUnknown DeliveryReason = "recipient_unknown"
	DeliveryReasonPersonControl    DeliveryReason = "person_control"
	DeliveryReasonBudget           DeliveryReason = "budget"
	DeliveryReasonRounds           DeliveryReason = "rounds"
	DeliveryReasonDeliveryFailed   DeliveryReason = "delivery_failed"
	DeliveryReasonNoActivity       DeliveryReason = "no_activity"
	DeliveryReasonRunnerRestart    DeliveryReason = "runner_restart"
	DeliveryReasonTurnCancelled    DeliveryReason = "turn_cancelled"
	DeliveryReasonGrid             DeliveryReason = "grid"
	DeliveryReasonTeamStopped      DeliveryReason = "team_stopped"
	DeliveryReasonCancelled        DeliveryReason = "cancelled"
)

// Values returns all DeliveryReason values in schema order in a new slice.
func (DeliveryReason) Values() []DeliveryReason {
	return []DeliveryReason{DeliveryReasonRecipientBlocked, DeliveryReasonRecipientUnknown, DeliveryReasonPersonControl, DeliveryReasonBudget, DeliveryReasonRounds, DeliveryReasonDeliveryFailed, DeliveryReasonNoActivity, DeliveryReasonRunnerRestart, DeliveryReasonTurnCancelled, DeliveryReasonGrid, DeliveryReasonTeamStopped, DeliveryReasonCancelled}
}

// Valid reports whether v is a defined DeliveryReason.
func (v DeliveryReason) Valid() bool {
	switch v {
	case DeliveryReasonRecipientBlocked, DeliveryReasonRecipientUnknown, DeliveryReasonPersonControl, DeliveryReasonBudget, DeliveryReasonRounds, DeliveryReasonDeliveryFailed, DeliveryReasonNoActivity, DeliveryReasonRunnerRestart, DeliveryReasonTurnCancelled, DeliveryReasonGrid, DeliveryReasonTeamStopped, DeliveryReasonCancelled:
		return true
	default:
		return false
	}
}

// DeliveryVia is the protocol DeliveryVia enum.
type DeliveryVia string

const (
	DeliveryViaPane     DeliveryVia = "pane"
	DeliveryViaHeadless DeliveryVia = "headless"
	DeliveryViaHuman    DeliveryVia = "human"
)

// Values returns all DeliveryVia values in schema order in a new slice.
func (DeliveryVia) Values() []DeliveryVia {
	return []DeliveryVia{DeliveryViaPane, DeliveryViaHeadless, DeliveryViaHuman}
}

// Valid reports whether v is a defined DeliveryVia.
func (v DeliveryVia) Valid() bool {
	switch v {
	case DeliveryViaPane, DeliveryViaHeadless, DeliveryViaHuman:
		return true
	default:
		return false
	}
}

// TurnStatus is the protocol TurnStatus enum.
type TurnStatus string

const (
	TurnStatusStarting  TurnStatus = "starting"
	TurnStatusRunning   TurnStatus = "running"
	TurnStatusSucceeded TurnStatus = "succeeded"
	TurnStatusFailed    TurnStatus = "failed"
	TurnStatusCancelled TurnStatus = "cancelled"
	TurnStatusTimedOut  TurnStatus = "timed_out"
	TurnStatusLost      TurnStatus = "lost"
)

// Values returns all TurnStatus values in schema order in a new slice.
func (TurnStatus) Values() []TurnStatus {
	return []TurnStatus{TurnStatusStarting, TurnStatusRunning, TurnStatusSucceeded, TurnStatusFailed, TurnStatusCancelled, TurnStatusTimedOut, TurnStatusLost}
}

// Valid reports whether v is a defined TurnStatus.
func (v TurnStatus) Valid() bool {
	switch v {
	case TurnStatusStarting, TurnStatusRunning, TurnStatusSucceeded, TurnStatusFailed, TurnStatusCancelled, TurnStatusTimedOut, TurnStatusLost:
		return true
	default:
		return false
	}
}

// RequestKind is the protocol RequestKind enum.
type RequestKind string

const (
	RequestKindOpenTerminal   RequestKind = "open-terminal"
	RequestKindReturnHeadless RequestKind = "return-headless"
	RequestKindStopMember     RequestKind = "stop-member"
	RequestKindSpawn          RequestKind = "spawn"
)

// Values returns all RequestKind values in schema order in a new slice.
func (RequestKind) Values() []RequestKind {
	return []RequestKind{RequestKindOpenTerminal, RequestKindReturnHeadless, RequestKindStopMember, RequestKindSpawn}
}

// Valid reports whether v is a defined RequestKind.
func (v RequestKind) Valid() bool {
	switch v {
	case RequestKindOpenTerminal, RequestKindReturnHeadless, RequestKindStopMember, RequestKindSpawn:
		return true
	default:
		return false
	}
}

// RequestStatus is the protocol RequestStatus enum.
type RequestStatus string

const (
	RequestStatusPending   RequestStatus = "pending"
	RequestStatusRunning   RequestStatus = "running"
	RequestStatusSucceeded RequestStatus = "succeeded"
	RequestStatusFailed    RequestStatus = "failed"
)

// Values returns all RequestStatus values in schema order in a new slice.
func (RequestStatus) Values() []RequestStatus {
	return []RequestStatus{RequestStatusPending, RequestStatusRunning, RequestStatusSucceeded, RequestStatusFailed}
}

// Valid reports whether v is a defined RequestStatus.
func (v RequestStatus) Valid() bool {
	switch v {
	case RequestStatusPending, RequestStatusRunning, RequestStatusSucceeded, RequestStatusFailed:
		return true
	default:
		return false
	}
}

// LeaseCause is the protocol LeaseCause enum.
type LeaseCause string

const (
	LeaseCauseStart          LeaseCause = "start"
	LeaseCauseWake           LeaseCause = "wake"
	LeaseCauseOpenTerminal   LeaseCause = "open_terminal"
	LeaseCauseReturnHeadless LeaseCause = "return_headless"
	LeaseCausePaneExit       LeaseCause = "pane_exit"
	LeaseCauseStop           LeaseCause = "stop"
	LeaseCauseDied           LeaseCause = "died"
)

// Values returns all LeaseCause values in schema order in a new slice.
func (LeaseCause) Values() []LeaseCause {
	return []LeaseCause{LeaseCauseStart, LeaseCauseWake, LeaseCauseOpenTerminal, LeaseCauseReturnHeadless, LeaseCausePaneExit, LeaseCauseStop, LeaseCauseDied}
}

// Valid reports whether v is a defined LeaseCause.
func (v LeaseCause) Valid() bool {
	switch v {
	case LeaseCauseStart, LeaseCauseWake, LeaseCauseOpenTerminal, LeaseCauseReturnHeadless, LeaseCausePaneExit, LeaseCauseStop, LeaseCauseDied:
		return true
	default:
		return false
	}
}

// LaunchEndReason is the protocol LaunchEndReason enum.
type LaunchEndReason string

const (
	LaunchEndReasonConverted LaunchEndReason = "converted"
	LaunchEndReasonStopped   LaunchEndReason = "stopped"
	LaunchEndReasonDied      LaunchEndReason = "died"
)

// Values returns all LaunchEndReason values in schema order in a new slice.
func (LaunchEndReason) Values() []LaunchEndReason {
	return []LaunchEndReason{LaunchEndReasonConverted, LaunchEndReasonStopped, LaunchEndReasonDied}
}

// Valid reports whether v is a defined LaunchEndReason.
func (v LaunchEndReason) Valid() bool {
	switch v {
	case LaunchEndReasonConverted, LaunchEndReasonStopped, LaunchEndReasonDied:
		return true
	default:
		return false
	}
}

// BudgetKind is the protocol BudgetKind enum.
type BudgetKind string

const (
	BudgetKindHops   BudgetKind = "hops"
	BudgetKindRounds BudgetKind = "rounds"
)

// Values returns all BudgetKind values in schema order in a new slice.
func (BudgetKind) Values() []BudgetKind {
	return []BudgetKind{BudgetKindHops, BudgetKindRounds}
}

// Valid reports whether v is a defined BudgetKind.
func (v BudgetKind) Valid() bool {
	switch v {
	case BudgetKindHops, BudgetKindRounds:
		return true
	default:
		return false
	}
}

// BudgetCause is the protocol BudgetCause enum.
type BudgetCause string

const (
	BudgetCauseUsed      BudgetCause = "used"
	BudgetCauseExhausted BudgetCause = "exhausted"
	BudgetCauseContinued BudgetCause = "continued"
	BudgetCauseReset     BudgetCause = "reset"
)

// Values returns all BudgetCause values in schema order in a new slice.
func (BudgetCause) Values() []BudgetCause {
	return []BudgetCause{BudgetCauseUsed, BudgetCauseExhausted, BudgetCauseContinued, BudgetCauseReset}
}

// Valid reports whether v is a defined BudgetCause.
func (v BudgetCause) Valid() bool {
	switch v {
	case BudgetCauseUsed, BudgetCauseExhausted, BudgetCauseContinued, BudgetCauseReset:
		return true
	default:
		return false
	}
}

// PolicyAction is the protocol PolicyAction enum.
type PolicyAction string

const (
	PolicyActionMessage PolicyAction = "message"
	PolicyActionSpawn   PolicyAction = "spawn"
	PolicyActionStop    PolicyAction = "stop"
)

// Values returns all PolicyAction values in schema order in a new slice.
func (PolicyAction) Values() []PolicyAction {
	return []PolicyAction{PolicyActionMessage, PolicyActionSpawn, PolicyActionStop}
}

// Valid reports whether v is a defined PolicyAction.
func (v PolicyAction) Valid() bool {
	switch v {
	case PolicyActionMessage, PolicyActionSpawn, PolicyActionStop:
		return true
	default:
		return false
	}
}

// SpawnFolder is the protocol SpawnFolder enum.
type SpawnFolder string

const (
	SpawnFolderWorktree SpawnFolder = "worktree"
	SpawnFolderShared   SpawnFolder = "shared"
)

// Values returns all SpawnFolder values in schema order in a new slice.
func (SpawnFolder) Values() []SpawnFolder {
	return []SpawnFolder{SpawnFolderWorktree, SpawnFolderShared}
}

// Valid reports whether v is a defined SpawnFolder.
func (v SpawnFolder) Valid() bool {
	switch v {
	case SpawnFolderWorktree, SpawnFolderShared:
		return true
	default:
		return false
	}
}

// SpawnCap is the protocol SpawnCap enum.
type SpawnCap string

const (
	SpawnCapRunningAtOnce SpawnCap = "running_at_once"
	SpawnCapPerRun        SpawnCap = "per_run"
	SpawnCapTeamSize      SpawnCap = "team_size"
)

// Values returns all SpawnCap values in schema order in a new slice.
func (SpawnCap) Values() []SpawnCap {
	return []SpawnCap{SpawnCapRunningAtOnce, SpawnCapPerRun, SpawnCapTeamSize}
}

// Valid reports whether v is a defined SpawnCap.
func (v SpawnCap) Valid() bool {
	switch v {
	case SpawnCapRunningAtOnce, SpawnCapPerRun, SpawnCapTeamSize:
		return true
	default:
		return false
	}
}

// InboxDirection is the protocol InboxDirection enum.
type InboxDirection string

const (
	InboxDirectionIn   InboxDirection = "in"
	InboxDirectionOut  InboxDirection = "out"
	InboxDirectionBoth InboxDirection = "both"
)

// Values returns all InboxDirection values in schema order in a new slice.
func (InboxDirection) Values() []InboxDirection {
	return []InboxDirection{InboxDirectionIn, InboxDirectionOut, InboxDirectionBoth}
}

// Valid reports whether v is a defined InboxDirection.
func (v InboxDirection) Valid() bool {
	switch v {
	case InboxDirectionIn, InboxDirectionOut, InboxDirectionBoth:
		return true
	default:
		return false
	}
}

// ResolveAction is the protocol ResolveAction enum.
type ResolveAction string

const (
	ResolveActionResend        ResolveAction = "resend"
	ResolveActionMarkDelivered ResolveAction = "mark_delivered"
	ResolveActionCancel        ResolveAction = "cancel"
)

// Values returns all ResolveAction values in schema order in a new slice.
func (ResolveAction) Values() []ResolveAction {
	return []ResolveAction{ResolveActionResend, ResolveActionMarkDelivered, ResolveActionCancel}
}

// Valid reports whether v is a defined ResolveAction.
func (v ResolveAction) Valid() bool {
	switch v {
	case ResolveActionResend, ResolveActionMarkDelivered, ResolveActionCancel:
		return true
	default:
		return false
	}
}

// WarningCode is the protocol WarningCode enum.
type WarningCode string

const (
	WarningCodeFolderMissing      WarningCode = "folder_missing"
	WarningCodeFolderNotGit       WarningCode = "folder_not_git"
	WarningCodeHarnessUnavailable WarningCode = "harness_unavailable"
	WarningCodeNextLaunch         WarningCode = "next_launch"
)

// Values returns all WarningCode values in schema order in a new slice.
func (WarningCode) Values() []WarningCode {
	return []WarningCode{WarningCodeFolderMissing, WarningCodeFolderNotGit, WarningCodeHarnessUnavailable, WarningCodeNextLaunch}
}

// Valid reports whether v is a defined WarningCode.
func (v WarningCode) Valid() bool {
	switch v {
	case WarningCodeFolderMissing, WarningCodeFolderNotGit, WarningCodeHarnessUnavailable, WarningCodeNextLaunch:
		return true
	default:
		return false
	}
}

// HostOS is the protocol HostOS enum.
type HostOS string

const (
	HostOSLinux   HostOS = "linux"
	HostOSDarwin  HostOS = "darwin"
	HostOSWindows HostOS = "windows"
)

// Values returns all HostOS values in schema order in a new slice.
func (HostOS) Values() []HostOS {
	return []HostOS{HostOSLinux, HostOSDarwin, HostOSWindows}
}

// Valid reports whether v is a defined HostOS.
func (v HostOS) Valid() bool {
	switch v {
	case HostOSLinux, HostOSDarwin, HostOSWindows:
		return true
	default:
		return false
	}
}

// HostArch is the protocol HostArch enum.
type HostArch string

const (
	HostArchAMD64 HostArch = "amd64"
	HostArchARM64 HostArch = "arm64"
)

// Values returns all HostArch values in schema order in a new slice.
func (HostArch) Values() []HostArch {
	return []HostArch{HostArchAMD64, HostArchARM64}
}

// Valid reports whether v is a defined HostArch.
func (v HostArch) Valid() bool {
	switch v {
	case HostArchAMD64, HostArchARM64:
		return true
	default:
		return false
	}
}

// HarnessIssue is the protocol HarnessIssue enum.
type HarnessIssue string

const (
	HarnessIssueNotInstalled       HarnessIssue = "not_installed"
	HarnessIssueUnsupportedVersion HarnessIssue = "unsupported_version"
	HarnessIssueShimOnly           HarnessIssue = "shim_only"
)

// Values returns all HarnessIssue values in schema order in a new slice.
func (HarnessIssue) Values() []HarnessIssue {
	return []HarnessIssue{HarnessIssueNotInstalled, HarnessIssueUnsupportedVersion, HarnessIssueShimOnly}
}

// Valid reports whether v is a defined HarnessIssue.
func (v HarnessIssue) Valid() bool {
	switch v {
	case HarnessIssueNotInstalled, HarnessIssueUnsupportedVersion, HarnessIssueShimOnly:
		return true
	default:
		return false
	}
}

// AuthFailure is the protocol AuthFailure enum.
type AuthFailure string

const (
	AuthFailureNoToken  AuthFailure = "no_token"
	AuthFailureBadToken AuthFailure = "bad_token"
	AuthFailureRevoked  AuthFailure = "revoked"
)

// Values returns all AuthFailure values in schema order in a new slice.
func (AuthFailure) Values() []AuthFailure {
	return []AuthFailure{AuthFailureNoToken, AuthFailureBadToken, AuthFailureRevoked}
}

// Valid reports whether v is a defined AuthFailure.
func (v AuthFailure) Valid() bool {
	switch v {
	case AuthFailureNoToken, AuthFailureBadToken, AuthFailureRevoked:
		return true
	default:
		return false
	}
}

// Face is the protocol Face enum.
type Face string

const (
	FaceMCP Face = "mcp"
	FaceCLI Face = "cli"
)

// Values returns all Face values in schema order in a new slice.
func (Face) Values() []Face {
	return []Face{FaceMCP, FaceCLI}
}

// Valid reports whether v is a defined Face.
func (v Face) Valid() bool {
	switch v {
	case FaceMCP, FaceCLI:
		return true
	default:
		return false
	}
}

// EventType is the protocol EventType enum.
type EventType string

const (
	EventTeamApplied           EventType = "team.applied"
	EventTeamStateChanged      EventType = "team.state_changed"
	EventRunnerStateChanged    EventType = "runner.state_changed"
	EventMemberAdded           EventType = "member.added"
	EventMemberStateChanged    EventType = "member.state_changed"
	EventMemberActivityChanged EventType = "member.activity_changed"
	EventMemberSessionAdded    EventType = "member.session_added"
	EventMemberRemoved         EventType = "member.removed"
	EventLeaseChanged          EventType = "lease.changed"
	EventMessageCreated        EventType = "message.created"
	EventDeliveryChanged       EventType = "delivery.changed"
	EventTurnStarted           EventType = "turn.started"
	EventTurnFinished          EventType = "turn.finished"
	EventPolicyDenied          EventType = "policy.denied"
	EventBudgetChanged         EventType = "budget.changed"
	EventRoundsChanged         EventType = "rounds.changed"
	EventRequestChanged        EventType = "request.changed"
	EventAuthRejected          EventType = "auth.rejected"
	EventWorktreeRemoved       EventType = "worktree.removed"
)

// Values returns all EventType values in schema order in a new slice.
func (EventType) Values() []EventType {
	return []EventType{EventTeamApplied, EventTeamStateChanged, EventRunnerStateChanged, EventMemberAdded, EventMemberStateChanged, EventMemberActivityChanged, EventMemberSessionAdded, EventMemberRemoved, EventLeaseChanged, EventMessageCreated, EventDeliveryChanged, EventTurnStarted, EventTurnFinished, EventPolicyDenied, EventBudgetChanged, EventRoundsChanged, EventRequestChanged, EventAuthRejected, EventWorktreeRemoved}
}

// Valid reports whether v is a defined EventType.
func (v EventType) Valid() bool {
	switch v {
	case EventTeamApplied, EventTeamStateChanged, EventRunnerStateChanged, EventMemberAdded, EventMemberStateChanged, EventMemberActivityChanged, EventMemberSessionAdded, EventMemberRemoved, EventLeaseChanged, EventMessageCreated, EventDeliveryChanged, EventTurnStarted, EventTurnFinished, EventPolicyDenied, EventBudgetChanged, EventRoundsChanged, EventRequestChanged, EventAuthRejected, EventWorktreeRemoved:
		return true
	default:
		return false
	}
}

// ErrorCode is the protocol ErrorCode enum.
type ErrorCode string

const (
	ErrorInternal           ErrorCode = "internal"
	ErrorInvalidRequest     ErrorCode = "invalid_request"
	ErrorUnauthenticated    ErrorCode = "unauthenticated"
	ErrorUnsupportedVersion ErrorCode = "unsupported_version"
	ErrorNotFound           ErrorCode = "not_found"
	ErrorUnknownMember      ErrorCode = "unknown_member"
	ErrorDenied             ErrorCode = "denied"
	ErrorSpawnDisabled      ErrorCode = "spawn_disabled"
	ErrorSpawnCapReached    ErrorCode = "spawn_cap_reached"
	ErrorHarnessNotAllowed  ErrorCode = "harness_not_allowed"
	ErrorProfileTooHigh     ErrorCode = "profile_too_high"
	ErrorNestingNotAllowed  ErrorCode = "nesting_not_allowed"
	ErrorNameTaken          ErrorCode = "name_taken"
	ErrorTeamNotRunning     ErrorCode = "team_not_running"
	ErrorTeamRunning        ErrorCode = "team_running"
	ErrorRunnerUnavailable  ErrorCode = "runner_unavailable"
	ErrorHerdrUnavailable   ErrorCode = "herdr_unavailable"
	ErrorHarnessUnavailable ErrorCode = "harness_unavailable"
	ErrorLaunchFailed       ErrorCode = "launch_failed"
	ErrorLeaseConflict      ErrorCode = "lease_conflict"
	ErrorRevisionConflict   ErrorCode = "revision_conflict"
	ErrorCursorInvalid      ErrorCode = "cursor_invalid"
	ErrorTimeout            ErrorCode = "timeout"
)

// Values returns all ErrorCode values in schema order in a new slice.
func (ErrorCode) Values() []ErrorCode {
	return []ErrorCode{ErrorInternal, ErrorInvalidRequest, ErrorUnauthenticated, ErrorUnsupportedVersion, ErrorNotFound, ErrorUnknownMember, ErrorDenied, ErrorSpawnDisabled, ErrorSpawnCapReached, ErrorHarnessNotAllowed, ErrorProfileTooHigh, ErrorNestingNotAllowed, ErrorNameTaken, ErrorTeamNotRunning, ErrorTeamRunning, ErrorRunnerUnavailable, ErrorHerdrUnavailable, ErrorHarnessUnavailable, ErrorLaunchFailed, ErrorLeaseConflict, ErrorRevisionConflict, ErrorCursorInvalid, ErrorTimeout}
}

// Valid reports whether v is a defined ErrorCode.
func (v ErrorCode) Valid() bool {
	switch v {
	case ErrorInternal, ErrorInvalidRequest, ErrorUnauthenticated, ErrorUnsupportedVersion, ErrorNotFound, ErrorUnknownMember, ErrorDenied, ErrorSpawnDisabled, ErrorSpawnCapReached, ErrorHarnessNotAllowed, ErrorProfileTooHigh, ErrorNestingNotAllowed, ErrorNameTaken, ErrorTeamNotRunning, ErrorTeamRunning, ErrorRunnerUnavailable, ErrorHerdrUnavailable, ErrorHarnessUnavailable, ErrorLaunchFailed, ErrorLeaseConflict, ErrorRevisionConflict, ErrorCursorInvalid, ErrorTimeout:
		return true
	default:
		return false
	}
}
