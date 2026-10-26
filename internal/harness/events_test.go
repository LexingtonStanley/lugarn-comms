package harness

import (
	"bufio"
	"os"
	"strings"
	"testing"

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
