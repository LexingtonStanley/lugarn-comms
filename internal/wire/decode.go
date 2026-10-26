// Package wire defines the v1 objects shared by all broker entry points.
package wire

import (
	"encoding/json"
	"fmt"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// EventData is the closed union of typed event snapshots.
type EventData interface{ eventData() }

// Event is a stored event envelope. Its data is selected by Type when decoding.
type Event struct {
	Type     model.EventType `json:"type"`
	Cursor   string          `json:"cursor"`
	At       model.Time      `json:"at"`
	Team     *string         `json:"team"`
	Timeline bool            `json:"timeline"`
	Data     EventData       `json:"data"`
}

func (e *Event) UnmarshalJSON(b []byte) error {
	var raw struct {
		Type     model.EventType `json:"type"`
		Cursor   string          `json:"cursor"`
		At       model.Time      `json:"at"`
		Team     *string         `json:"team"`
		Timeline bool            `json:"timeline"`
		Data     json.RawMessage `json:"data"`
	}
	if err := model.DecodeStrict(b, &raw); err != nil {
		return err
	}
	data := newEventData(string(raw.Type))
	if data == nil {
		return model.NewError(model.ErrorInvalidRequest, "unknown event type")
	}
	if err := model.DecodeStrict(raw.Data, data); err != nil {
		return err
	}
	*e = Event{raw.Type, raw.Cursor, raw.At, raw.Team, raw.Timeline, data}
	return nil
}

// WatchLine is an event or one of watch's three transient control lines.
type WatchLine interface{ watchLine() }

func (Event) watchLine()          {}
func (HeartbeatLine) watchLine()  {}
func (WatchLiveLine) watchLine()  {}
func (WatchErrorLine) watchLine() {}

// DecodeWatchLine decodes a closed NDJSON line by its type discriminator.
func DecodeWatchLine(b []byte) (WatchLine, error) {
	var tag struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &tag); err != nil {
		return nil, model.AsError(err)
	}
	var line WatchLine
	switch tag.Type {
	case "heartbeat":
		line = new(HeartbeatLine)
	case "watch.live":
		line = new(WatchLiveLine)
	case "error":
		line = new(WatchErrorLine)
	default:
		line = new(Event)
	}
	if err := model.DecodeStrict(b, line); err != nil {
		return nil, err
	}
	return line, nil
}

// Response decodes an app response by its op and ok discriminators.
// RequestID is the one optional response-envelope field specified by v1.
type Response struct {
	Protocol  int64        `json:"protocol"`
	Op        OpName       `json:"op"`
	RequestID *string      `json:"request_id,omitempty"`
	OK        bool         `json:"ok"`
	Result    any          `json:"result,omitempty"`
	Error     *model.Error `json:"error,omitempty"`
}

func (r *Response) UnmarshalJSON(b []byte) error {
	type rawResponse struct {
		Protocol  int64           `json:"protocol"`
		Op        OpName          `json:"op"`
		RequestID *string         `json:"request_id,omitempty"`
		OK        bool            `json:"ok"`
		Result    json.RawMessage `json:"result"`
		Error     *model.Error    `json:"error"`
	}
	var raw rawResponse
	if err := model.DecodeStrict(b, &raw); err != nil {
		return err
	}
	*r = Response{Protocol: raw.Protocol, Op: raw.Op, RequestID: raw.RequestID, OK: raw.OK, Error: raw.Error}
	if raw.OK {
		r.Result = NewResult(string(raw.Op))
		if r.Result == nil {
			return fmt.Errorf("unknown result op %q", raw.Op)
		}
		return model.DecodeStrict(raw.Result, r.Result)
	}
	return nil
}

// DecodeRequest validates against the closed schema and then uses DecodeStrict.
func DecodeRequest(op string, b []byte) (any, error) {
	v := NewRequest(op)
	if v == nil {
		return nil, model.NewError(model.ErrorInvalidRequest, "unknown app op")
	}
	if err := Validate("app-ops", requestDefinition(op), b); err != nil {
		return nil, err
	}
	return v, model.DecodeStrict(b, v)
}

// DecodeToolInput validates a tool's closed schema before decoding its typed input.
func DecodeToolInput(name string, b []byte) (any, error) {
	v := NewToolInput(name)
	if v == nil {
		return nil, model.NewError(model.ErrorInvalidRequest, "unknown tool")
	}
	if err := Validate("mcp-tools", toolDefinition(name, "Input"), b); err != nil {
		return nil, err
	}
	return v, model.DecodeStrict(b, v)
}
