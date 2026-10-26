package harness

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

// Event acknowledges that the harness has accepted the prompt. Exactly one
// acknowledgement is emitted, carrying the validated harness-reported session.
// Later session changes are available in Outcome.SessionID.
type Event struct {
	Type      string
	SessionID string
}

// Outcome is the final result of a headless process. Usage is nil when no usage
// was reported, with individual missing counters kept nil rather than guessed.
type Outcome struct {
	ExitCode     int
	Acknowledged bool
	SessionID    string
	FinalText    string
	Usage        *model.Usage
	Error        string
	Cancelled    bool
	TimedOut     bool
}

// Status maps an outcome to the shared protocol status. Timeouts win over other
// cancellation, and a clean exit before acknowledgement is always a failure.
func Status(o Outcome) model.TurnStatus {
	if o.TimedOut {
		return model.TurnStatusTimedOut
	}
	if o.Cancelled {
		return model.TurnStatusCancelled
	}
	if o.ExitCode != 0 || !o.Acknowledged || o.Error != "" {
		return model.TurnStatusFailed
	}
	return model.TurnStatusSucceeded
}

// Parser consumes one JSON event at a time. Unknown events are ignored. It is
// intentionally single-consumer; a running Handle owns its parser.
type Parser struct {
	harness   model.Harness
	outcome   Outcome
	malformed string
	completed bool
	messageID string
}

func NewParser(h model.Harness, sessionID string) *Parser {
	return &Parser{harness: h, outcome: Outcome{SessionID: sessionID}}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 2000 {
		s = s[:2000]
	}
	return s
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func str(v any) string            { s, _ := v.(string); return s }
func integer(v any) *int64 {
	n, ok := v.(json.Number)
	if !ok {
		return nil
	}
	i, e := n.Int64()
	if e != nil || i < 0 {
		return nil
	}
	return &i
}
func number(v any) *float64 {
	n, ok := v.(json.Number)
	if !ok {
		return nil
	}
	f, e := n.Float64()
	if e != nil || f < 0 {
		return nil
	}
	return &f
}
func contentText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	parts, _ := v.([]any)
	var text []string
	for _, part := range parts {
		m := object(part)
		if m["type"] == "text" || m["type"] == "output_text" {
			if s := str(m["text"]); s != "" {
				text = append(text, s)
			}
		}
	}
	return strings.Join(text, "\n")
}

func (p *Parser) fail(s string) {
	if p.outcome.Error == "" {
		p.outcome.Error = oneLine(s)
	}
}
func (p *Parser) session(id string) {
	if validSession(p.harness, id) {
		p.outcome.SessionID = id
	}
}

// Line returns an acknowledgement only for the specified harness init event.
// A malformed line is remembered, but cannot turn an otherwise successful turn
// into a failure. Untrusted text from the stream never becomes a launch argument.
func (p *Parser) Line(line []byte) *Event {
	if strings.TrimSpace(string(line)) == "" {
		return nil
	}
	var e map[string]any
	d := json.NewDecoder(strings.NewReader(string(line)))
	d.UseNumber()
	if err := d.Decode(&e); err != nil || e == nil || !json.Valid(line) {
		if p.malformed == "" {
			p.malformed = "harness emitted malformed JSON"
		}
		return nil
	}
	typ := str(e["type"])
	ack := false
	switch p.harness {
	case model.HarnessClaude:
		if typ == "system" && e["subtype"] == "init" {
			p.session(str(e["session_id"]))
			ack = validSession(p.harness, str(e["session_id"]))
		}
		if typ == "assistant" && e["parent_tool_use_id"] == nil {
			if text := contentText(object(e["message"])["content"]); text != "" {
				p.outcome.FinalText = text
			}
		}
		if typ == "result" {
			p.session(str(e["session_id"]))
			p.completed = true
			if text := str(e["result"]); text != "" {
				p.outcome.FinalText = text
			}
			if e["is_error"] == true {
				reason := str(e["result"])
				if reason == "" {
					b, _ := json.Marshal(e["errors"])
					reason = string(b)
				}
				p.fail("Claude result: " + reason)
			}
			u := object(e["usage"])
			p.setUsage(u, "cache_read_input_tokens", "reasoning_tokens")
			if reason := integer(object(u["output_tokens_details"])["thinking_tokens"]); reason != nil {
				p.usage().ReasoningTokens = reason
			}
			if cost := number(e["total_cost_usd"]); cost != nil {
				p.usage().CostUSD = cost
			}
		}
	case model.HarnessCodex:
		if typ == "thread.started" {
			id := str(e["thread_id"])
			if id == "" {
				id = str(e["threadId"])
			}
			if id == "" {
				id = str(e["id"])
			}
			p.session(id)
			ack = validSession(p.harness, id)
		}
		if typ == "item.completed" {
			item := object(e["item"])
			if item["type"] == "agent_message" {
				p.outcome.FinalText = str(item["text"])
			}
		}
		if typ == "turn.completed" {
			p.completed = true
			p.setUsage(object(e["usage"]), "cached_input_tokens", "reasoning_output_tokens")
		}
		if typ == "turn.failed" || typ == "error" {
			reason := str(object(e["error"])["message"])
			if reason == "" {
				reason = str(e["message"])
			}
			if reason == "" {
				reason = string(line)
			}
			p.fail(reason)
		}
	case model.HarnessOpenCode:
		if typ != "step_start" && typ != "text" && typ != "step_finish" && typ != "error" {
			break
		}
		id := str(e["sessionID"])
		p.session(id)
		if typ == "step_start" {
			ack = validSession(p.harness, id)
		}
		part := object(e["part"])
		if typ == "text" {
			messageID := str(part["messageID"])
			if messageID != "" && messageID != p.messageID {
				p.messageID = messageID
				p.outcome.FinalText = ""
			}
			p.outcome.FinalText += str(part["text"])
		}
		if typ == "step_finish" {
			if part["reason"] == "stop" {
				p.completed = true
			}
			p.addOpenCodeUsage(part)
		}
		if typ == "error" || e["error"] != nil {
			b, _ := json.Marshal(e["error"])
			p.fail("OpenCode: " + string(b))
		}
	}
	if ack && !p.outcome.Acknowledged {
		p.outcome.Acknowledged = true
		return &Event{Type: "acknowledged", SessionID: p.outcome.SessionID}
	}
	return nil
}

func (p *Parser) usage() *model.Usage {
	if p.outcome.Usage == nil {
		p.outcome.Usage = &model.Usage{}
	}
	return p.outcome.Usage
}
func (p *Parser) setUsage(u map[string]any, cache, reason string) {
	value := model.Usage{InputTokens: integer(u["input_tokens"]), OutputTokens: integer(u["output_tokens"]), CachedInputTokens: integer(u[cache]), ReasoningTokens: integer(u[reason]), CostUSD: number(u["cost_usd"])}
	if value.InputTokens != nil || value.OutputTokens != nil || value.CachedInputTokens != nil || value.ReasoningTokens != nil || value.CostUSD != nil {
		p.outcome.Usage = &value
	}
}
func addInt(dst **int64, v *int64) {
	if v == nil {
		return
	}
	if *dst == nil {
		x := int64(0)
		*dst = &x
	}
	**dst += *v
}
func (p *Parser) addOpenCodeUsage(part map[string]any) {
	tokens := object(part["tokens"])
	cache := object(tokens["cache"])
	input, output, cached, reason, cost := integer(tokens["input"]), integer(tokens["output"]), integer(cache["read"]), integer(tokens["reasoning"]), number(part["cost"])
	if input == nil && output == nil && cached == nil && reason == nil && cost == nil {
		return
	}
	u := p.usage()
	addInt(&u.InputTokens, input)
	addInt(&u.OutputTokens, output)
	addInt(&u.CachedInputTokens, cached)
	addInt(&u.ReasoningTokens, reason)
	if cost != nil {
		if u.CostUSD == nil {
			x := float64(0)
			u.CostUSD = &x
		}
		*u.CostUSD += *cost
	}
}

// Finish combines stream state and the actual process result. Explicit provider
// failures count even with exit zero. OpenCode v2 omits step_finish, so its final
// text is completion evidence; counters it omits remain unknown.
func (p *Parser) Finish(exitCode int, cancelled, timedOut bool, stderr string) Outcome {
	o := p.outcome
	o.ExitCode = exitCode
	o.Cancelled = cancelled
	o.TimedOut = timedOut
	if !cancelled && !timedOut {
		if o.Error == "" && !o.Acknowledged {
			o.Error = "harness ended before acknowledgement"
		}
		if o.Error == "" && !p.completed && strings.TrimSpace(o.FinalText) == "" {
			o.Error = "harness ended without a final response"
		}
		if o.Error == "" && exitCode != 0 {
			o.Error = fmt.Sprintf("harness exited %d", exitCode)
			if s := oneLine(stderr); s != "" {
				o.Error += ": " + s
			}
		}
		if o.Error != "" && p.malformed != "" {
			o.Error += "; " + p.malformed
		}
	}
	o.Error = oneLine(o.Error)
	return o
}
