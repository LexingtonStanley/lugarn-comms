// Wire object declarations mirror the v1 schemas and reuse model records.

package wire

import (
	"encoding/json"
	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// RequestEnvelope mirrors its v1 wire object.
type RequestEnvelope struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
}

// HelloRequest mirrors its v1 wire object.
type HelloRequest struct {
	Protocol  int64           `json:"protocol"`
	Op        OpName          `json:"op"`
	RequestID *string         `json:"request_id,omitempty"`
	Protocols []int64         `json:"protocols"`
	App       HelloRequestApp `json:"app"`
}

// ApplyTeamRequest mirrors its v1 wire object.
type ApplyTeamRequest struct {
	Protocol   int64          `json:"protocol"`
	Op         OpName         `json:"op"`
	RequestID  *string        `json:"request_id,omitempty"`
	Spec       model.TeamSpec `json:"spec"`
	IfRevision int64          `json:"if_revision"`
}

// GetTeamRequest mirrors its v1 wire object.
type GetTeamRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Team      string  `json:"team"`
}

// StartTeamRequest mirrors its v1 wire object.
type StartTeamRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Team      string  `json:"team"`
	Resume    *bool   `json:"resume,omitempty"`
}

// StopTeamRequest mirrors its v1 wire object.
type StopTeamRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Team      string  `json:"team"`
}

// StatusRequest mirrors its v1 wire object.
type StatusRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Team      *string `json:"team,omitempty"`
}

// MembersRequest mirrors its v1 wire object.
type MembersRequest struct {
	Protocol       int64   `json:"protocol"`
	Op             OpName  `json:"op"`
	RequestID      *string `json:"request_id,omitempty"`
	Team           string  `json:"team"`
	IncludeRemoved *bool   `json:"include_removed,omitempty"`
}

// TimelineRequest mirrors its v1 wire object.
type TimelineRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Team      string  `json:"team"`
	Before    *string `json:"before,omitempty"`
	After     *string `json:"after,omitempty"`
	Limit     *int64  `json:"limit,omitempty"`
}

// SendRequest mirrors its v1 wire object.
type SendRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Team      string  `json:"team"`
	To        *string `json:"to,omitempty"`
	All       *bool   `json:"all,omitempty"`
	Text      string  `json:"text"`
	ReplyTo   *string `json:"reply_to,omitempty"`
}

// ContinueBudgetRequest mirrors its v1 wire object.
type ContinueBudgetRequest struct {
	Protocol  int64             `json:"protocol"`
	Op        OpName            `json:"op"`
	RequestID *string           `json:"request_id,omitempty"`
	Team      string            `json:"team"`
	Kind      *model.BudgetKind `json:"kind,omitempty"`
	Amount    *int64            `json:"amount,omitempty"`
	Writer    *string           `json:"writer,omitempty"`
	Reviewer  *string           `json:"reviewer,omitempty"`
}

// ResolveDeliveryRequest mirrors its v1 wire object.
type ResolveDeliveryRequest struct {
	Protocol  int64               `json:"protocol"`
	Op        OpName              `json:"op"`
	RequestID *string             `json:"request_id,omitempty"`
	Team      string              `json:"team"`
	Message   string              `json:"message"`
	Member    string              `json:"member"`
	Action    model.ResolveAction `json:"action"`
}

// HoldRequest mirrors its v1 wire object.
type HoldRequest struct {
	Protocol   int64   `json:"protocol"`
	Op         OpName  `json:"op"`
	RequestID  *string `json:"request_id,omitempty"`
	Team       string  `json:"team"`
	Member     string  `json:"member"`
	TTLSeconds *int64  `json:"ttl_seconds,omitempty"`
	Release    *bool   `json:"release,omitempty"`
}

// OpenTerminalRequest mirrors its v1 wire object.
type OpenTerminalRequest struct {
	Protocol     int64   `json:"protocol"`
	Op           OpName  `json:"op"`
	RequestID    *string `json:"request_id,omitempty"`
	Team         string  `json:"team"`
	Member       string  `json:"member"`
	CancelTurn   *bool   `json:"cancel_turn,omitempty"`
	IfGeneration *int64  `json:"if_generation,omitempty"`
}

// ReturnHeadlessRequest mirrors its v1 wire object.
type ReturnHeadlessRequest struct {
	Protocol     int64   `json:"protocol"`
	Op           OpName  `json:"op"`
	RequestID    *string `json:"request_id,omitempty"`
	Team         string  `json:"team"`
	Member       string  `json:"member"`
	IfGeneration *int64  `json:"if_generation,omitempty"`
}

// StopMemberRequest mirrors its v1 wire object.
type StopMemberRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Team      string  `json:"team"`
	Member    string  `json:"member"`
}

// WorktreesRequest mirrors its v1 wire object.
type WorktreesRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Team      string  `json:"team"`
}

// RemoveWorktreesRequest mirrors its v1 wire object.
type RemoveWorktreesRequest struct {
	Protocol  int64    `json:"protocol"`
	Op        OpName   `json:"op"`
	RequestID *string  `json:"request_id,omitempty"`
	Team      string   `json:"team"`
	Paths     []string `json:"paths"`
}

// StartHerdrRequest mirrors its v1 wire object.
type StartHerdrRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Server    *string `json:"server,omitempty"`
}

// WatchRequest mirrors its v1 wire object.
type WatchRequest struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	Team      *string `json:"team,omitempty"`
	After     *string `json:"after,omitempty"`
}

// OkEnvelope mirrors its v1 wire object.
type OkEnvelope struct {
	Protocol  int64   `json:"protocol"`
	Op        OpName  `json:"op"`
	RequestID *string `json:"request_id,omitempty"`
	OK        bool    `json:"ok"`
	Result    any     `json:"result"`
}

// ErrorResponse mirrors its v1 wire object.
type ErrorResponse struct {
	Protocol  int64       `json:"protocol"`
	Op        OpName      `json:"op"`
	RequestID *string     `json:"request_id,omitempty"`
	OK        bool        `json:"ok"`
	Error     model.Error `json:"error"`
}

// HelloResponse mirrors its v1 wire object.
type HelloResponse struct {
	Protocol  int64       `json:"protocol"`
	Op        OpName      `json:"op"`
	RequestID *string     `json:"request_id,omitempty"`
	OK        bool        `json:"ok"`
	Result    HelloResult `json:"result"`
}

// ApplyTeamResponse mirrors its v1 wire object.
type ApplyTeamResponse struct {
	Protocol  int64           `json:"protocol"`
	Op        OpName          `json:"op"`
	RequestID *string         `json:"request_id,omitempty"`
	OK        bool            `json:"ok"`
	Result    ApplyTeamResult `json:"result"`
}

// GetTeamResponse mirrors its v1 wire object.
type GetTeamResponse struct {
	Protocol  int64         `json:"protocol"`
	Op        OpName        `json:"op"`
	RequestID *string       `json:"request_id,omitempty"`
	OK        bool          `json:"ok"`
	Result    GetTeamResult `json:"result"`
}

// StartTeamResponse mirrors its v1 wire object.
type StartTeamResponse struct {
	Protocol  int64           `json:"protocol"`
	Op        OpName          `json:"op"`
	RequestID *string         `json:"request_id,omitempty"`
	OK        bool            `json:"ok"`
	Result    StartTeamResult `json:"result"`
}

// StopTeamResponse mirrors its v1 wire object.
type StopTeamResponse struct {
	Protocol  int64          `json:"protocol"`
	Op        OpName         `json:"op"`
	RequestID *string        `json:"request_id,omitempty"`
	OK        bool           `json:"ok"`
	Result    StopTeamResult `json:"result"`
}

// StatusResponse mirrors its v1 wire object.
type StatusResponse struct {
	Protocol  int64        `json:"protocol"`
	Op        OpName       `json:"op"`
	RequestID *string      `json:"request_id,omitempty"`
	OK        bool         `json:"ok"`
	Result    StatusResult `json:"result"`
}

// MembersResponse mirrors its v1 wire object.
type MembersResponse struct {
	Protocol  int64         `json:"protocol"`
	Op        OpName        `json:"op"`
	RequestID *string       `json:"request_id,omitempty"`
	OK        bool          `json:"ok"`
	Result    MembersResult `json:"result"`
}

// TimelineResponse mirrors its v1 wire object.
type TimelineResponse struct {
	Protocol  int64          `json:"protocol"`
	Op        OpName         `json:"op"`
	RequestID *string        `json:"request_id,omitempty"`
	OK        bool           `json:"ok"`
	Result    TimelineResult `json:"result"`
}

// SendResponse mirrors its v1 wire object.
type SendResponse struct {
	Protocol  int64      `json:"protocol"`
	Op        OpName     `json:"op"`
	RequestID *string    `json:"request_id,omitempty"`
	OK        bool       `json:"ok"`
	Result    SendResult `json:"result"`
}

// ContinueBudgetResponse mirrors its v1 wire object.
type ContinueBudgetResponse struct {
	Protocol  int64                `json:"protocol"`
	Op        OpName               `json:"op"`
	RequestID *string              `json:"request_id,omitempty"`
	OK        bool                 `json:"ok"`
	Result    ContinueBudgetResult `json:"result"`
}

// ResolveDeliveryResponse mirrors its v1 wire object.
type ResolveDeliveryResponse struct {
	Protocol  int64                 `json:"protocol"`
	Op        OpName                `json:"op"`
	RequestID *string               `json:"request_id,omitempty"`
	OK        bool                  `json:"ok"`
	Result    ResolveDeliveryResult `json:"result"`
}

// HoldResponse mirrors its v1 wire object.
type HoldResponse struct {
	Protocol  int64      `json:"protocol"`
	Op        OpName     `json:"op"`
	RequestID *string    `json:"request_id,omitempty"`
	OK        bool       `json:"ok"`
	Result    HoldResult `json:"result"`
}

// OpenTerminalResponse mirrors its v1 wire object.
type OpenTerminalResponse struct {
	Protocol  int64         `json:"protocol"`
	Op        OpName        `json:"op"`
	RequestID *string       `json:"request_id,omitempty"`
	OK        bool          `json:"ok"`
	Result    RequestResult `json:"result"`
}

// ReturnHeadlessResponse mirrors its v1 wire object.
type ReturnHeadlessResponse struct {
	Protocol  int64         `json:"protocol"`
	Op        OpName        `json:"op"`
	RequestID *string       `json:"request_id,omitempty"`
	OK        bool          `json:"ok"`
	Result    RequestResult `json:"result"`
}

// StopMemberResponse mirrors its v1 wire object.
type StopMemberResponse struct {
	Protocol  int64         `json:"protocol"`
	Op        OpName        `json:"op"`
	RequestID *string       `json:"request_id,omitempty"`
	OK        bool          `json:"ok"`
	Result    RequestResult `json:"result"`
}

// WorktreesResponse mirrors its v1 wire object.
type WorktreesResponse struct {
	Protocol  int64           `json:"protocol"`
	Op        OpName          `json:"op"`
	RequestID *string         `json:"request_id,omitempty"`
	OK        bool            `json:"ok"`
	Result    WorktreesResult `json:"result"`
}

// RemoveWorktreesResponse mirrors its v1 wire object.
type RemoveWorktreesResponse struct {
	Protocol  int64                 `json:"protocol"`
	Op        OpName                `json:"op"`
	RequestID *string               `json:"request_id,omitempty"`
	OK        bool                  `json:"ok"`
	Result    RemoveWorktreesResult `json:"result"`
}

// StartHerdrResponse mirrors its v1 wire object.
type StartHerdrResponse struct {
	Protocol  int64            `json:"protocol"`
	Op        OpName           `json:"op"`
	RequestID *string          `json:"request_id,omitempty"`
	OK        bool             `json:"ok"`
	Result    StartHerdrResult `json:"result"`
}

// HelloResult mirrors its v1 wire object.
type HelloResult struct {
	Broker       HelloResultBroker   `json:"broker"`
	Schemas      HelloResultSchemas  `json:"schemas"`
	Capabilities []string            `json:"capabilities"`
	Host         HelloResultHost     `json:"host"`
	Herdr        model.HerdrInfo     `json:"herdr"`
	Harnesses    []model.HarnessInfo `json:"harnesses"`
}

func (v HelloResult) MarshalJSON() ([]byte, error) {
	type plain HelloResult
	if v.Capabilities == nil {
		v.Capabilities = []string{}
	}
	if v.Harnesses == nil {
		v.Harnesses = []model.HarnessInfo{}
	}
	return json.Marshal(plain(v))
}

// Warning mirrors its v1 wire object.
type Warning struct {
	Code    model.WarningCode `json:"code"`
	Member  *string           `json:"member"`
	Message string            `json:"message"`
}

// ApplyTeamResult mirrors its v1 wire object.
type ApplyTeamResult struct {
	Team     model.TeamStatus `json:"team"`
	Warnings []Warning        `json:"warnings"`
}

func (v ApplyTeamResult) MarshalJSON() ([]byte, error) {
	type plain ApplyTeamResult
	if v.Warnings == nil {
		v.Warnings = []Warning{}
	}
	return json.Marshal(plain(v))
}

// GetTeamResult mirrors its v1 wire object.
type GetTeamResult struct {
	Spec     model.TeamSpec `json:"spec"`
	Revision int64          `json:"revision"`
	Cursor   string         `json:"cursor"`
}

// StartTeamResult mirrors its v1 wire object.
type StartTeamResult struct {
	Team           model.TeamStatus `json:"team"`
	AlreadyRunning bool             `json:"already_running"`
}

// StopTeamResult mirrors its v1 wire object.
type StopTeamResult struct {
	Team    model.TeamStatus `json:"team"`
	Expired int64            `json:"expired"`
}

// StatusResult mirrors its v1 wire object.
type StatusResult struct {
	Teams  []model.TeamStatus `json:"teams"`
	Cursor string             `json:"cursor"`
}

func (v StatusResult) MarshalJSON() ([]byte, error) {
	type plain StatusResult
	if v.Teams == nil {
		v.Teams = []model.TeamStatus{}
	}
	return json.Marshal(plain(v))
}

// MembersResult mirrors its v1 wire object.
type MembersResult struct {
	Members []model.Member `json:"members"`
	Cursor  string         `json:"cursor"`
}

func (v MembersResult) MarshalJSON() ([]byte, error) {
	type plain MembersResult
	if v.Members == nil {
		v.Members = []model.Member{}
	}
	return json.Marshal(plain(v))
}

// TimelineResult mirrors its v1 wire object.
type TimelineResult struct {
	Events        []Event `json:"events"`
	HasMoreBefore bool    `json:"has_more_before"`
	HasMoreAfter  bool    `json:"has_more_after"`
}

func (v TimelineResult) MarshalJSON() ([]byte, error) {
	type plain TimelineResult
	if v.Events == nil {
		v.Events = []Event{}
	}
	return json.Marshal(plain(v))
}

// SendResult mirrors its v1 wire object.
type SendResult struct {
	Message model.Message `json:"message"`
}

// ContinueBudgetResult mirrors its v1 wire object.
type ContinueBudgetResult struct {
	Hops     model.Hops     `json:"hops"`
	Rounds   []model.Rounds `json:"rounds"`
	Released int64          `json:"released"`
}

func (v ContinueBudgetResult) MarshalJSON() ([]byte, error) {
	type plain ContinueBudgetResult
	if v.Rounds == nil {
		v.Rounds = []model.Rounds{}
	}
	return json.Marshal(plain(v))
}

// ResolveDeliveryResult mirrors its v1 wire object.
type ResolveDeliveryResult struct {
	Message  string         `json:"message"`
	Delivery model.Delivery `json:"delivery"`
}

// HoldResult mirrors its v1 wire object.
type HoldResult struct {
	Member    model.MemberRef `json:"member"`
	HeldUntil *model.Time     `json:"held_until"`
	Released  int64           `json:"released"`
}

// RequestResult mirrors its v1 wire object.
type RequestResult struct {
	Request model.Request `json:"request"`
}

// WorktreeItem mirrors its v1 wire object.
type WorktreeItem struct {
	Path        string            `json:"path"`
	Branch      string            `json:"branch"`
	Repo        string            `json:"repo"`
	Base        string            `json:"base"`
	Member      model.MemberRef   `json:"member"`
	MemberState model.MemberState `json:"member_state"`
	CreatedAt   model.Time        `json:"created_at"`
	Git         WorktreeItemGit   `json:"git"`
}

// WorktreesResult mirrors its v1 wire object.
type WorktreesResult struct {
	Worktrees []WorktreeItem `json:"worktrees"`
}

func (v WorktreesResult) MarshalJSON() ([]byte, error) {
	type plain WorktreesResult
	if v.Worktrees == nil {
		v.Worktrees = []WorktreeItem{}
	}
	return json.Marshal(plain(v))
}

// RemoveWorktreesResult mirrors its v1 wire object.
type RemoveWorktreesResult struct {
	Removed []RemoveWorktreesResultRemovedItem `json:"removed"`
	Refused []RemoveWorktreesResultRefusedItem `json:"refused"`
}

func (v RemoveWorktreesResult) MarshalJSON() ([]byte, error) {
	type plain RemoveWorktreesResult
	if v.Removed == nil {
		v.Removed = []RemoveWorktreesResultRemovedItem{}
	}
	if v.Refused == nil {
		v.Refused = []RemoveWorktreesResultRefusedItem{}
	}
	return json.Marshal(plain(v))
}

// StartHerdrResult mirrors its v1 wire object.
type StartHerdrResult struct {
	Herdr   model.HerdrInfo   `json:"herdr"`
	Started bool              `json:"started"`
	Method  *HerdrStartMethod `json:"method"`
}

// HeartbeatLine mirrors its v1 wire object.
type HeartbeatLine struct {
	Type   string     `json:"type"`
	Cursor string     `json:"cursor"`
	At     model.Time `json:"at"`
}

// WatchLiveLine mirrors its v1 wire object.
type WatchLiveLine struct {
	Type     string     `json:"type"`
	Cursor   string     `json:"cursor"`
	At       model.Time `json:"at"`
	Protocol int64      `json:"protocol"`
}

// WatchErrorLine mirrors its v1 wire object.
type WatchErrorLine struct {
	Type  string      `json:"type"`
	At    model.Time  `json:"at"`
	Error model.Error `json:"error"`
}

// ToolError mirrors its v1 wire object.
type ToolError struct {
	Error model.Error `json:"error"`
}

// SendInput mirrors its v1 wire object.
type SendInput struct {
	To      string  `json:"to"`
	Text    string  `json:"text"`
	ReplyTo *string `json:"reply_to,omitempty"`
}

// SendOutput mirrors its v1 wire object.
type SendOutput struct {
	ID       string                `json:"id"`
	To       model.MemberRef       `json:"to"`
	State    model.DeliveryState   `json:"state"`
	Reason   *model.DeliveryReason `json:"reason"`
	WillWake bool                  `json:"will_wake"`
}

// ReplyInput mirrors its v1 wire object.
type ReplyInput struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type ReplyOutput = SendOutput

// InboxInput mirrors its v1 wire object.
type InboxInput struct {
	Direction *model.InboxDirection `json:"direction,omitempty"`
	Limit     *int64                `json:"limit,omitempty"`
	Before    *string               `json:"before,omitempty"`
}

// InboxMessage mirrors its v1 wire object.
type InboxMessage struct {
	ID        string                `json:"id"`
	From      string                `json:"from"`
	To        string                `json:"to"`
	Kind      model.MessageKind     `json:"kind"`
	ReplyTo   *string               `json:"reply_to"`
	Text      string                `json:"text"`
	CreatedAt model.Time            `json:"created_at"`
	State     model.DeliveryState   `json:"state"`
	Reason    *model.DeliveryReason `json:"reason"`
}

// InboxOutput mirrors its v1 wire object.
type InboxOutput struct {
	Messages []InboxMessage `json:"messages"`
	HasMore  bool           `json:"has_more"`
}

func (v InboxOutput) MarshalJSON() ([]byte, error) {
	type plain InboxOutput
	if v.Messages == nil {
		v.Messages = []InboxMessage{}
	}
	return json.Marshal(plain(v))
}

// TeamInput mirrors its v1 wire object.
type TeamInput struct {
}

// TeamOutput mirrors its v1 wire object.
type TeamOutput struct {
	Team    TeamOutputTeam `json:"team"`
	Me      TeamOutputMe   `json:"me"`
	Members []TeamMember   `json:"members"`
	Hops    TeamOutputHops `json:"hops"`
	Spawn   *SpawnStatus   `json:"spawn"`
}

func (v TeamOutput) MarshalJSON() ([]byte, error) {
	type plain TeamOutput
	if v.Members == nil {
		v.Members = []TeamMember{}
	}
	return json.Marshal(plain(v))
}

// TeamMember mirrors its v1 wire object.
type TeamMember struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Kind        model.MemberKind       `json:"kind"`
	Role        *model.Role            `json:"role"`
	Harness     *model.Harness         `json:"harness"`
	Mode        *model.Mode            `json:"mode"`
	State       model.MemberState      `json:"state"`
	Activity    *model.Activity        `json:"activity"`
	SpawnedBy   *string                `json:"spawned_by"`
	Can         []model.GridCapability `json:"can"`
	SpawnedByMe bool                   `json:"spawned_by_me"`
}

func (v TeamMember) MarshalJSON() ([]byte, error) {
	type plain TeamMember
	if v.Can == nil {
		v.Can = []model.GridCapability{}
	}
	return json.Marshal(plain(v))
}

// SpawnStatus mirrors its v1 wire object.
type SpawnStatus struct {
	Running        int64             `json:"running"`
	RunningAtOnce  int64             `json:"running_at_once"`
	SpawnedThisRun int64             `json:"spawned_this_run"`
	PerRun         int64             `json:"per_run"`
	Harnesses      []model.Harness   `json:"harnesses"`
	Mode           model.Mode        `json:"mode"`
	Folder         model.SpawnFolder `json:"folder"`
	MaxProfile     model.Profile     `json:"max_profile"`
}

func (v SpawnStatus) MarshalJSON() ([]byte, error) {
	type plain SpawnStatus
	if v.Harnesses == nil {
		v.Harnesses = []model.Harness{}
	}
	return json.Marshal(plain(v))
}

// SpawnInput mirrors its v1 wire object.
type SpawnInput struct {
	Name    string         `json:"name"`
	Harness model.Harness  `json:"harness"`
	Task    string         `json:"task"`
	Profile *model.Profile `json:"profile,omitempty"`
	Mode    *model.Mode    `json:"mode,omitempty"`
}

// SpawnOutput mirrors its v1 wire object.
type SpawnOutput struct {
	Member      SpawnOutputMember `json:"member"`
	TaskMessage string            `json:"task_message"`
}

// StopInput mirrors its v1 wire object.
type StopInput struct {
	Member string `json:"member"`
}

// StopOutput mirrors its v1 wire object.
type StopOutput struct {
	Member StopOutputMember `json:"member"`
}

// TeamAppliedEventData mirrors its v1 wire object.
type TeamAppliedEventData struct {
	Team model.TeamStatus `json:"team"`
}

func (TeamAppliedEventData) eventData() {}

// TeamStateChangedEventData mirrors its v1 wire object.
type TeamStateChangedEventData struct {
	Team model.TeamStatus `json:"team"`
	From model.TeamState  `json:"from"`
	To   model.TeamState  `json:"to"`
}

func (TeamStateChangedEventData) eventData() {}

// RunnerStateChangedEventData mirrors its v1 wire object.
type RunnerStateChangedEventData struct {
	Team model.TeamStatus  `json:"team"`
	From model.RunnerState `json:"from"`
	To   model.RunnerState `json:"to"`
}

func (RunnerStateChangedEventData) eventData() {}

// MemberAddedEventData mirrors its v1 wire object.
type MemberAddedEventData struct {
	Member model.Member `json:"member"`
}

func (MemberAddedEventData) eventData() {}

// MemberStateChangedEventData mirrors its v1 wire object.
type MemberStateChangedEventData struct {
	Member model.Member      `json:"member"`
	From   model.MemberState `json:"from"`
	To     model.MemberState `json:"to"`
	Reason *model.StopReason `json:"reason"`
}

func (MemberStateChangedEventData) eventData() {}

// MemberActivityChangedEventData mirrors its v1 wire object.
type MemberActivityChangedEventData struct {
	Member model.MemberRef `json:"member"`
	From   *model.Activity `json:"from"`
	To     *model.Activity `json:"to"`
}

func (MemberActivityChangedEventData) eventData() {}

// MemberSessionAddedEventData mirrors its v1 wire object.
type MemberSessionAddedEventData struct {
	Member  model.Member        `json:"member"`
	Session model.MemberSession `json:"session"`
}

func (MemberSessionAddedEventData) eventData() {}

// MemberRemovedEventData mirrors its v1 wire object.
type MemberRemovedEventData struct {
	Member model.Member `json:"member"`
}

func (MemberRemovedEventData) eventData() {}

// LeaseChangedEventData mirrors its v1 wire object.
type LeaseChangedEventData struct {
	Member     model.Member     `json:"member"`
	From       model.Driver     `json:"from"`
	To         model.Driver     `json:"to"`
	Generation int64            `json:"generation"`
	Cause      model.LeaseCause `json:"cause"`
}

func (LeaseChangedEventData) eventData() {}

// MessageCreatedEventData mirrors its v1 wire object.
type MessageCreatedEventData struct {
	Message model.Message `json:"message"`
}

func (MessageCreatedEventData) eventData() {}

// DeliveryChangedEventData mirrors its v1 wire object.
type DeliveryChangedEventData struct {
	Message  string              `json:"message"`
	From     model.DeliveryState `json:"from"`
	Delivery model.Delivery      `json:"delivery"`
}

func (DeliveryChangedEventData) eventData() {}

// TurnStartedEventData mirrors its v1 wire object.
type TurnStartedEventData struct {
	Turn model.Turn `json:"turn"`
}

func (TurnStartedEventData) eventData() {}

// TurnFinishedEventData mirrors its v1 wire object.
type TurnFinishedEventData struct {
	Turn model.Turn `json:"turn"`
}

func (TurnFinishedEventData) eventData() {}

// PolicyDeniedEventData mirrors its v1 wire object.
type PolicyDeniedEventData struct {
	Action    model.PolicyAction `json:"action"`
	Actor     model.MemberRef    `json:"actor"`
	Target    *model.MemberRef   `json:"target"`
	MessageID *string            `json:"message_id"`
	Error     model.Error        `json:"error"`
}

func (PolicyDeniedEventData) eventData() {}

// BudgetChangedEventData mirrors its v1 wire object.
type BudgetChangedEventData struct {
	Hops  model.Hops        `json:"hops"`
	Cause model.BudgetCause `json:"cause"`
	By    *model.MemberRef  `json:"by"`
}

func (BudgetChangedEventData) eventData() {}

// RoundsChangedEventData mirrors its v1 wire object.
type RoundsChangedEventData struct {
	Rounds model.Rounds      `json:"rounds"`
	Cause  model.BudgetCause `json:"cause"`
	By     *model.MemberRef  `json:"by"`
}

func (RoundsChangedEventData) eventData() {}

// RequestChangedEventData mirrors its v1 wire object.
type RequestChangedEventData struct {
	Request model.Request `json:"request"`
}

func (RequestChangedEventData) eventData() {}

// AuthRejectedEventData mirrors its v1 wire object.
type AuthRejectedEventData struct {
	ClaimedMember *string           `json:"claimed_member"`
	Failure       model.AuthFailure `json:"failure"`
	Face          model.Face        `json:"face"`
}

func (AuthRejectedEventData) eventData() {}

// WorktreeRemovedEventData mirrors its v1 wire object.
type WorktreeRemovedEventData struct {
	Path          string          `json:"path"`
	Branch        string          `json:"branch"`
	Member        model.MemberRef `json:"member"`
	BranchOutcome BranchOutcome   `json:"branch_outcome"`
}

func (WorktreeRemovedEventData) eventData() {}

// LogindInfo mirrors its v1 wire object.
type LogindInfo struct {
	KillUserProcesses bool `json:"kill_user_processes"`
	Linger            bool `json:"linger"`
}

// HelloRequestApp mirrors its v1 wire object.
type HelloRequestApp struct {
	Version string `json:"version"`
}

// HelloResultBroker mirrors its v1 wire object.
type HelloResultBroker struct {
	Version   string  `json:"version"`
	Commit    *string `json:"commit"`
	Protocol  int64   `json:"protocol"`
	Protocols []int64 `json:"protocols"`
}

func (v HelloResultBroker) MarshalJSON() ([]byte, error) {
	type plain HelloResultBroker
	if v.Protocols == nil {
		v.Protocols = []int64{}
	}
	return json.Marshal(plain(v))
}

// HelloResultSchemas mirrors its v1 wire object.
type HelloResultSchemas struct {
	TeamSpec []int64 `json:"team_spec"`
	Store    int64   `json:"store"`
}

func (v HelloResultSchemas) MarshalJSON() ([]byte, error) {
	type plain HelloResultSchemas
	if v.TeamSpec == nil {
		v.TeamSpec = []int64{}
	}
	return json.Marshal(plain(v))
}

// HelloResultHost mirrors its v1 wire object.
type HelloResultHost struct {
	ID       string              `json:"id"`
	OS       model.HostOS        `json:"os"`
	Arch     model.HostArch      `json:"arch"`
	Hostname string              `json:"hostname"`
	StateDir string              `json:"state_dir"`
	Logind   *LogindInfo         `json:"logind"`
	Path     HelloResultHostPath `json:"path"`
}

// WorktreeItemGit mirrors its v1 wire object.
type WorktreeItemGit struct {
	State  GitState `json:"state"`
	Merged *bool    `json:"merged"`
	Ahead  *int64   `json:"ahead"`
	Behind *int64   `json:"behind"`
}

// RemoveWorktreesResultRemovedItem mirrors its v1 wire object.
type RemoveWorktreesResultRemovedItem struct {
	Path          string        `json:"path"`
	Branch        string        `json:"branch"`
	BranchOutcome BranchOutcome `json:"branch_outcome"`
}

// RemoveWorktreesResultRefusedItem mirrors its v1 wire object.
type RemoveWorktreesResultRefusedItem struct {
	Path    string          `json:"path"`
	Reason  WorktreeRefusal `json:"reason"`
	Message string          `json:"message"`
}

// TeamOutputTeam mirrors its v1 wire object.
type TeamOutputTeam struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Pattern model.Pattern   `json:"pattern"`
	State   model.TeamState `json:"state"`
	Run     int64           `json:"run"`
}

// TeamOutputMe mirrors its v1 wire object.
type TeamOutputMe struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Role      model.Role    `json:"role"`
	Harness   model.Harness `json:"harness"`
	Profile   model.Profile `json:"profile"`
	Mode      model.Mode    `json:"mode"`
	SpawnedBy *string       `json:"spawned_by"`
	CanSpawn  bool          `json:"can_spawn"`
}

// TeamOutputHops mirrors its v1 wire object.
type TeamOutputHops struct {
	Used      int64 `json:"used"`
	Allowance int64 `json:"allowance"`
}

// SpawnOutputMember mirrors its v1 wire object.
type SpawnOutputMember struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Harness  model.Harness     `json:"harness"`
	Profile  model.Profile     `json:"profile"`
	Mode     model.Mode        `json:"mode"`
	State    model.MemberState `json:"state"`
	Workdir  string            `json:"workdir"`
	Worktree *model.Worktree   `json:"worktree"`
}

// StopOutputMember mirrors its v1 wire object.
type StopOutputMember struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	State      model.MemberState `json:"state"`
	StopReason *model.StopReason `json:"stop_reason"`
}

// HelloResultHostPath describes the login PATH capture and SSH inheritance.
type HelloResultHostPath struct {
	Shell      *string     `json:"shell"`
	Login      *string     `json:"login"`
	Issue      *string     `json:"issue"`
	CapturedAt *model.Time `json:"captured_at"`
	Inherited  string      `json:"inherited"`
}
