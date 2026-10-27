package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestRecordedStreams(t *testing.T) {
	for _, tc := range []struct {
		h                             model.Harness
		session                       string
		input, output, cached, reason int64
		cost                          float64
	}{
		{model.HarnessClaude, "b09c3f46-e877-4e7d-bbb6-c7db563a68f4", 2, 4, 11333, 0, 0.0688533},
		{model.HarnessCodex, "01a17948-efed-7ca3-956c-0779d1d88fe8", 13451, 5, 11520, 0, 0},
		{model.HarnessOpenCode, "ses_e86b70f40ffeFeFsKdTEVKcdeL", 0, 0, 0, 0, 0},
	} {
		t.Run(string(tc.h), func(t *testing.T) {
			f, err := os.Open("testdata/" + string(tc.h) + ".jsonl")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			p := NewParser(tc.h, "")
			s := bufio.NewScanner(f)
			s.Buffer(make([]byte, 65536), 4<<20)
			acks := 0
			for s.Scan() {
				if e := p.Line(s.Bytes()); e != nil {
					acks++
					if e.Type != "acknowledged" || e.SessionID != tc.session {
						t.Fatal(e)
					}
				}
			}
			if err = s.Err(); err != nil {
				t.Fatal(err)
			}
			o := p.Finish(0, false, false, "")
			if Status(o) != model.TurnStatusSucceeded || acks != 1 || o.SessionID != tc.session || o.FinalText != "ok" {
				t.Fatalf("%+v, acks=%d", o, acks)
			}
			if tc.h == model.HarnessOpenCode {
				if o.Usage != nil {
					t.Fatal(o.Usage)
				}
				return
			}
			if o.Usage == nil || *o.Usage.InputTokens != tc.input || *o.Usage.OutputTokens != tc.output || *o.Usage.CachedInputTokens != tc.cached {
				t.Fatal(o.Usage)
			}
			if tc.h == model.HarnessCodex {
				if o.Usage.CostUSD != nil || o.Usage.ReasoningTokens == nil || *o.Usage.ReasoningTokens != 0 {
					t.Fatal(o.Usage)
				}
			} else if o.Usage.CostUSD == nil || *o.Usage.CostUSD != tc.cost {
				t.Fatal(o.Usage)
			}
		})
	}
}

func TestParserEdges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		lines  []string
		exit   int
		failed bool
		text   string
	}{
		{"unknown and malformed success", []string{`nonsense`, `{"type":"future"}`, `{"type":"thread.started","thread_id":"` + testSessionID + `"}`, `{"type":"item.completed","item":{"type":"agent_message","text":"old"}}`, `{"type":"item.completed","item":{"type":"agent_message","text":"last"}}`, `{"type":"turn.completed"}`}, 0, false, "last"},
		{"before ack", []string{`{"type":"turn.completed"}`}, 0, true, ""},
		{"invalid ack", []string{`{"type":"thread.started","thread_id":"--bad"}`}, 0, true, ""},
		{"no final", []string{`{"type":"thread.started","thread_id":"` + testSessionID + `"}`}, 0, true, ""},
		{"provider zero exit", []string{`{"type":"thread.started","thread_id":"` + testSessionID + `"}`, `{"type":"turn.failed","error":{"message":"bad\nprovider"}}`}, 0, true, ""},
		{"malformed failed", []string{`not JSON`}, 1, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewParser(model.HarnessCodex, "")
			for _, line := range tc.lines {
				p.Line([]byte(line))
			}
			o := p.Finish(tc.exit, false, false, "stderr\ndetail")
			if (Status(o) == model.TurnStatusFailed) != tc.failed || o.FinalText != tc.text || strings.Contains(o.Error, "\n") {
				t.Fatalf("%+v", o)
			}
			if tc.name == "malformed failed" && !strings.Contains(o.Error, "malformed") {
				t.Fatal(o)
			}
		})
	}
	p := NewParser(model.HarnessOpenCode, "")
	for _, line := range []string{`{"type":"future_event","error":{"message":"ignored"},"sessionID":"ses_other"}`, `{"type":"text","sessionID":"ses_test","part":{"messageID":"msg_a","text":"old"}}`, `{"type":"step_start","sessionID":"ses_test"}`, `{"type":"text","part":{"messageID":"msg_b","text":"new"}}`, `{"type":"text","part":{"messageID":"msg_b","text":" text"}}`, `{"type":"step_finish","part":{"reason":"stop","cost":0.1,"tokens":{"input":10,"output":3,"reasoning":2,"cache":{"read":4}}}}`, `{"type":"step_finish","part":{"reason":"stop","cost":0.2,"tokens":{"input":5}}}`} {
		p.Line([]byte(line))
	}
	o := p.Finish(0, false, false, "")
	if o.FinalText != "new text" || *o.Usage.InputTokens != 15 || *o.Usage.OutputTokens != 3 || *o.Usage.CachedInputTokens != 4 || *o.Usage.ReasoningTokens != 2 {
		t.Fatalf("%+v", o)
	}
}

func TestStatus(t *testing.T) {
	for _, tc := range []struct {
		o    Outcome
		want model.TurnStatus
	}{
		{Outcome{}, model.TurnStatusFailed},
		{Outcome{Acknowledged: true}, model.TurnStatusSucceeded},
		{Outcome{Acknowledged: true, ExitCode: 1}, model.TurnStatusFailed},
		{Outcome{Acknowledged: true, Error: "provider error"}, model.TurnStatusFailed},
		{Outcome{Cancelled: true}, model.TurnStatusCancelled},
		{Outcome{Cancelled: true, TimedOut: true}, model.TurnStatusTimedOut},
	} {
		if got := Status(tc.o); got != tc.want {
			t.Fatalf("%+v: %s", tc.o, got)
		}
	}
}

func TestFailureReasonSources(t *testing.T) {
	for _, tc := range []struct {
		name               string
		h                  model.Harness
		line, stderr, want string
		exit               int
	}{
		{"claude result", model.HarnessClaude, `{"type":"result","is_error":true,"result":"auth failed"}`, "ignored stderr", "auth failed", 1},
		{"claude errors without flag", model.HarnessClaude, `{"type":"result","errors":["auth failed"]}`, "", "auth failed", 0},
		{"claude errors", model.HarnessClaude, `{"type":"result","is_error":true,"errors":["no credits",{"message":"ignored"},"try later"]}`, "", "no credits; try later", 0},
		{"codex error", model.HarnessCodex, `{"type":"turn.failed","error":{"message":"rate limited"},"message":"ignored"}`, "", "rate limited", 1},
		{"codex message", model.HarnessCodex, `{"type":"error","message":"auth failed"}`, "", "auth failed", 0},
		{"opencode data", model.HarnessOpenCode, `{"type":"error","error":{"data":{"message":"no credits"},"name":"ignored"}}`, "", "no credits", 1},
		{"opencode name", model.HarnessOpenCode, `{"type":"error","error":{"name":"UnknownFlagError"}}`, "", "UnknownFlagError", 0},
		{"stderr before ack", model.HarnessCodex, `{"type":"future"}`, "old line\nerror: unknown option '--standalone'\n  \n\x00", "error: unknown option '--standalone'", 1},
		{"no raw fallback", model.HarnessCodex, `{"type":"error","payload":"PRIVATE_RAW_LINE"}`, "", "harness reported a failed turn", 0},
		{"no opencode raw fallback", model.HarnessOpenCode, `{"type":"error","error":{"payload":"PRIVATE_RAW_LINE"}}`, "", "harness reported a failed turn", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewParser(tc.h, "")
			p.Line([]byte(tc.line))
			out := p.Finish(tc.exit, false, false, tc.stderr)
			if out.Error != tc.want || Status(out) != model.TurnStatusFailed {
				t.Fatalf("got %+v, want error %q", out, tc.want)
			}
		})
	}
}
func TestStructuredFailureIsScrubbedBeforeOutcome(t *testing.T) {
	for _, h := range []model.Harness{model.HarnessClaude, model.HarnessCodex, model.HarnessOpenCode} {
		t.Run(string(h), func(t *testing.T) {
			p := NewParser(h, "")
			p.scrub = newFailureScrubber(map[string]string{"VALUE": "private-value"}, nil, "")
			reason := "private-value " + strings.Repeat("狐", 400)
			var event map[string]any
			switch h {
			case model.HarnessClaude:
				event = map[string]any{"type": "result", "is_error": true, "result": reason}
			case model.HarnessCodex:
				event = map[string]any{"type": "turn.failed", "error": map[string]any{"message": reason}}
			case model.HarnessOpenCode:
				event = map[string]any{"type": "error", "error": map[string]any{"data": map[string]any{"message": reason}}}
			}
			raw, e := json.Marshal(event)
			if e != nil {
				t.Fatal(e)
			}
			p.Line(raw)
			// Check the stored parser outcome too, before Finish can sanitize anything.
			if strings.Contains(p.outcome.Error+p.outcome.FinalText, "private-value") {
				t.Fatal("unscrubbed reason entered outcome")
			}
			out := p.Finish(1, false, false, "")
			if !strings.HasPrefix(out.Error, "‹redacted› ") || utf8.RuneCountInString(out.Error) != 300 {
				t.Fatal(out.Error)
			}
		})
	}
}
