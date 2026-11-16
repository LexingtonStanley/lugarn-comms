package wire

import "github.com/LexingtonStanley/lugarn-comms/internal/model"

type AdoptTeamRequest struct {
	Protocol  int64          `json:"protocol"`
	Op        OpName         `json:"op"`
	RequestID *string        `json:"request_id,omitempty"`
	Spec      model.TeamSpec `json:"spec"`
	Workspace string         `json:"workspace"`
	Members   []AdoptMember  `json:"members"`
}
type AdoptMember struct {
	Member string `json:"member"`
	Pane   string `json:"pane"`
	Agent  string `json:"agent"`
}
type BringOntoCommsRequest struct {
	Protocol        int64   `json:"protocol"`
	Op              OpName  `json:"op"`
	RequestID       *string `json:"request_id,omitempty"`
	Team            string  `json:"team"`
	Member          string  `json:"member"`
	Confirm         bool    `json:"confirm,omitempty"`
	IfGeneration    *int64  `json:"if_generation,omitempty"`
	ExpectedSession *string `json:"expected_session"`
}
type BringOntoCommsResult struct {
	RestartRequired bool           `json:"restart_required"`
	Conversation    string         `json:"conversation"`
	Session         *string        `json:"session"`
	Request         *model.Request `json:"request"`
}
type ForgetTeamRequest struct {
	Protocol   int64   `json:"protocol"`
	Op         OpName  `json:"op"`
	RequestID  *string `json:"request_id,omitempty"`
	Team       string  `json:"team"`
	IfRevision int64   `json:"if_revision"`
}
type ForgetTeamResult struct {
	Team model.TeamStatus `json:"team"`
}
