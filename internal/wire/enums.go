package wire

// OpName is the v1 OpName enum, absent from internal/model.
type OpName string

const (
	OpNameHello           OpName = "hello"
	OpNameApplyTeam       OpName = "apply-team"
	OpNameGetTeam         OpName = "get-team"
	OpNameStartTeam       OpName = "start-team"
	OpNameStopTeam        OpName = "stop-team"
	OpNameStatus          OpName = "status"
	OpNameMembers         OpName = "members"
	OpNameTimeline        OpName = "timeline"
	OpNameSend            OpName = "send"
	OpNameContinueBudget  OpName = "continue-budget"
	OpNameResolveDelivery OpName = "resolve-delivery"
	OpNameHold            OpName = "hold"
	OpNameOpenTerminal    OpName = "open-terminal"
	OpNameReturnHeadless  OpName = "return-headless"
	OpNameStopMember      OpName = "stop-member"
	OpNameWorktrees       OpName = "worktrees"
	OpNameRemoveWorktrees OpName = "remove-worktrees"
	OpNameStartHerdr      OpName = "start-herdr"
	OpNameWatch           OpName = "watch"
)

func (OpName) Values() []OpName {
	return []OpName{OpNameHello, OpNameApplyTeam, OpNameGetTeam, OpNameStartTeam, OpNameStopTeam, OpNameStatus, OpNameMembers, OpNameTimeline, OpNameSend, OpNameContinueBudget, OpNameResolveDelivery, OpNameHold, OpNameOpenTerminal, OpNameReturnHeadless, OpNameStopMember, OpNameWorktrees, OpNameRemoveWorktrees, OpNameStartHerdr, OpNameWatch}
}
func (v OpName) Valid() bool {
	for _, item := range v.Values() {
		if v == item {
			return true
		}
	}
	return false
}

// HerdrStartMethod is the v1 HerdrStartMethod enum, absent from internal/model.
type HerdrStartMethod string

const (
	HerdrStartMethodDetached HerdrStartMethod = "detached"
	HerdrStartMethodWmi      HerdrStartMethod = "wmi"
)

func (HerdrStartMethod) Values() []HerdrStartMethod {
	return []HerdrStartMethod{HerdrStartMethodDetached, HerdrStartMethodWmi}
}
func (v HerdrStartMethod) Valid() bool {
	for _, item := range v.Values() {
		if v == item {
			return true
		}
	}
	return false
}

// GitState is the v1 GitState enum, absent from internal/model.
type GitState string

const (
	GitStateMissing GitState = "missing"
	GitStateClean   GitState = "clean"
	GitStateDirty   GitState = "dirty"
)

func (GitState) Values() []GitState { return []GitState{GitStateMissing, GitStateClean, GitStateDirty} }
func (v GitState) Valid() bool {
	for _, item := range v.Values() {
		if v == item {
			return true
		}
	}
	return false
}

// WorktreeRefusal is the v1 WorktreeRefusal enum, absent from internal/model.
type WorktreeRefusal string

const (
	WorktreeRefusalMemberNotStopped WorktreeRefusal = "member_not_stopped"
	WorktreeRefusalDirty            WorktreeRefusal = "dirty"
	WorktreeRefusalUnknownPath      WorktreeRefusal = "unknown_path"
	WorktreeRefusalGitFailed        WorktreeRefusal = "git_failed"
)

func (WorktreeRefusal) Values() []WorktreeRefusal {
	return []WorktreeRefusal{WorktreeRefusalMemberNotStopped, WorktreeRefusalDirty, WorktreeRefusalUnknownPath, WorktreeRefusalGitFailed}
}
func (v WorktreeRefusal) Valid() bool {
	for _, item := range v.Values() {
		if v == item {
			return true
		}
	}
	return false
}

// BranchOutcome is the v1 BranchOutcome enum, absent from internal/model.
type BranchOutcome string

const (
	BranchOutcomeDeleted      BranchOutcome = "deleted"
	BranchOutcomeKeptUnmerged BranchOutcome = "kept_unmerged"
	BranchOutcomeMissing      BranchOutcome = "missing"
)

func (BranchOutcome) Values() []BranchOutcome {
	return []BranchOutcome{BranchOutcomeDeleted, BranchOutcomeKeptUnmerged, BranchOutcomeMissing}
}
func (v BranchOutcome) Valid() bool {
	for _, item := range v.Values() {
		if v == item {
			return true
		}
	}
	return false
}

// ToolName is the v1 ToolName enum, absent from internal/model.
type ToolName string

const (
	ToolNameSend  ToolName = "send"
	ToolNameReply ToolName = "reply"
	ToolNameInbox ToolName = "inbox"
	ToolNameTeam  ToolName = "team"
	ToolNameSpawn ToolName = "spawn"
	ToolNameStop  ToolName = "stop"
)

func (ToolName) Values() []ToolName {
	return []ToolName{ToolNameSend, ToolNameReply, ToolNameInbox, ToolNameTeam, ToolNameSpawn, ToolNameStop}
}
func (v ToolName) Valid() bool {
	for _, item := range v.Values() {
		if v == item {
			return true
		}
	}
	return false
}
