package model

// Records mirror common.v1; nullable fields deliberately have no omitempty.

// MemberRef is the common.v1 MemberRef record.
type MemberRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Worktree is the common.v1 Worktree record.
type Worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// Lease is the common.v1 Lease record.
type Lease struct {
	Driver     Driver  `json:"driver"`
	Generation int64   `json:"generation"`
	Launch     *string `json:"launch"`
	Pane       *string `json:"pane"`
}

// MemberSession is the common.v1 MemberSession record.
type MemberSession struct {
	ID          string        `json:"id"`
	Ordinal     int64         `json:"ordinal"`
	Source      SessionSource `json:"source"`
	Launch      *string       `json:"launch"`
	FirstSeenAt Time          `json:"first_seen_at"`
}

// HerdrPlacement is the common.v1 HerdrPlacement record.
type HerdrPlacement struct {
	Server    string  `json:"server"`
	Workspace *string `json:"workspace"`
	Agent     *string `json:"agent"`
}

// Member is the common.v1 Member record.
type Member struct {
	ID            string          `json:"id"`
	Team          string          `json:"team"`
	Name          string          `json:"name"`
	Kind          MemberKind      `json:"kind"`
	Role          *Role           `json:"role"`
	Origin        MemberOrigin    `json:"origin"`
	SpawnedBy     *MemberRef      `json:"spawned_by"`
	Harness       *Harness        `json:"harness"`
	Profile       *Profile        `json:"profile"`
	Mode          *Mode           `json:"mode"`
	Folder        *string         `json:"folder"`
	Worktree      *Worktree       `json:"worktree"`
	Workdir       *string         `json:"workdir"`
	State         MemberState     `json:"state"`
	StopReason    *StopReason     `json:"stop_reason"`
	Activity      *Activity       `json:"activity"`
	Lease         *Lease          `json:"lease"`
	LastDriver    *Mode           `json:"last_driver"`
	HeldUntil     *Time           `json:"held_until"`
	Herdr         *HerdrPlacement `json:"herdr"`
	Session       *string         `json:"session"`
	Sessions      []MemberSession `json:"sessions"`
	Host          *Null           `json:"host"`
	CreatedAt     Time            `json:"created_at"`
	StartedAt     *Time           `json:"started_at"`
	StoppedAt     *Time           `json:"stopped_at"`
	RemovedAt     *Time           `json:"removed_at"`
	RemovedReason *RemovedReason  `json:"removed_reason"`
}

// Delivery is the common.v1 Delivery record.
type Delivery struct {
	To          MemberRef       `json:"to"`
	State       DeliveryState   `json:"state"`
	Reason      *DeliveryReason `json:"reason"`
	Via         *DeliveryVia    `json:"via"`
	Wake        bool            `json:"wake"`
	Attempts    int64           `json:"attempts"`
	Notify      *bool           `json:"notify"`
	Turn        *string         `json:"turn"`
	UpdatedAt   Time            `json:"updated_at"`
	DeliveredAt *Time           `json:"delivered_at"`
}

// Message is the common.v1 Message record.
type Message struct {
	ID         string      `json:"id"`
	Team       string      `json:"team"`
	From       MemberRef   `json:"from"`
	Kind       MessageKind `json:"kind"`
	Broadcast  bool        `json:"broadcast"`
	ReplyTo    *string     `json:"reply_to"`
	Text       string      `json:"text"`
	Hop        *int64      `json:"hop"`
	Round      *int64      `json:"round"`
	Turn       *string     `json:"turn"`
	CreatedAt  Time        `json:"created_at"`
	Deliveries []Delivery  `json:"deliveries"`
}

// Usage is the common.v1 Usage record.
type Usage struct {
	InputTokens       *int64   `json:"input_tokens"`
	OutputTokens      *int64   `json:"output_tokens"`
	CachedInputTokens *int64   `json:"cached_input_tokens"`
	ReasoningTokens   *int64   `json:"reasoning_tokens"`
	CostUSD           *float64 `json:"cost_usd"`
}

// Turn is the common.v1 Turn record.
type Turn struct {
	ID              string     `json:"id"`
	Team            string     `json:"team"`
	Member          MemberRef  `json:"member"`
	Launch          string     `json:"launch"`
	LeaseGeneration int64      `json:"lease_generation"`
	Harness         Harness    `json:"harness"`
	SessionIDStart  *string    `json:"session_id_start"`
	SessionIDEnd    *string    `json:"session_id_end"`
	PID             *int64     `json:"pid"`
	Status          TurnStatus `json:"status"`
	ExitCode        *int64     `json:"exit_code"`
	Error           *string    `json:"error"`
	Usage           *Usage     `json:"usage"`
	StartedAt       Time       `json:"started_at"`
	EndedAt         *Time      `json:"ended_at"`
	Consumed        []string   `json:"consumed"`
	AutoReplies     []string   `json:"auto_replies"`
}

// Request is the common.v1 Request record.
type Request struct {
	ID          string         `json:"id"`
	Team        string         `json:"team"`
	Kind        RequestKind    `json:"kind"`
	Member      *MemberRef     `json:"member"`
	RequestedBy MemberRef      `json:"requested_by"`
	Args        map[string]any `json:"args"`
	Status      RequestStatus  `json:"status"`
	Error       *Error         `json:"error"`
	CreatedAt   Time           `json:"created_at"`
	StartedAt   *Time          `json:"started_at"`
	FinishedAt  *Time          `json:"finished_at"`
}

// Hops is the common.v1 Hops record.
type Hops struct {
	Used      int64 `json:"used"`
	Allowance int64 `json:"allowance"`
	Base      int64 `json:"base"`
	Exhausted bool  `json:"exhausted"`
}

// Rounds is the common.v1 Rounds record.
type Rounds struct {
	Writer    MemberRef `json:"writer"`
	Reviewer  MemberRef `json:"reviewer"`
	Used      int64     `json:"used"`
	Allowance int64     `json:"allowance"`
	Base      int64     `json:"base"`
	Exhausted bool      `json:"exhausted"`
}

// Runner is the common.v1 Runner record.
type Runner struct {
	State       RunnerState `json:"state"`
	PID         *int64      `json:"pid"`
	Pane        *string     `json:"pane"`
	StartedAt   *Time       `json:"started_at"`
	HeartbeatAt *Time       `json:"heartbeat_at"`
}

// TeamStatus is the common.v1 TeamStatus record.
type TeamStatus struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Pattern          Pattern    `json:"pattern"`
	Revision         int64      `json:"revision"`
	Run              int64      `json:"run"`
	State            TeamState  `json:"state"`
	Runner           Runner     `json:"runner"`
	Hops             Hops       `json:"hops"`
	Rounds           []Rounds   `json:"rounds"`
	Counts           TeamCounts `json:"counts"`
	StartedAt        *Time      `json:"started_at"`
	StoppedAt        *Time      `json:"stopped_at"`
	LastAppContactAt *Time      `json:"last_app_contact_at"`
}

// HerdrInfo is the common.v1 HerdrInfo record.
type HerdrInfo struct {
	Installed bool    `json:"installed"`
	Version   *string `json:"version"`
	Running   bool    `json:"running"`
	Server    string  `json:"server"`
}

// HarnessInfo is the common.v1 HarnessInfo record.
type HarnessInfo struct {
	Harness   Harness       `json:"harness"`
	Installed bool          `json:"installed"`
	Version   *string       `json:"version"`
	Supported bool          `json:"supported"`
	Issue     *HarnessIssue `json:"issue"`
	Headless  bool          `json:"headless"`
}

// TeamCounts contains the counters embedded in TeamStatus.
type TeamCounts struct {
	Members       int64 `json:"members"`
	Running       int64 `json:"running"`
	HeadlessTurns int64 `json:"headless_turns"`
	Queued        int64 `json:"queued"`
	Held          int64 `json:"held"`
	Uncertain     int64 `json:"uncertain"`
}
