package hc

import (
	"encoding/json"
	"testing"
)

func TestViewSequenceMissedAndLost(t *testing.T) {
	v := NewView()
	v.core = "core"
	pid := int64(7)
	terminal := Terminal{Address: "t7", State: "live", PID: &pid, Labels: map[string]string{"lugarn.team": "team"}, Status: &Status{State: "idle"}}
	v.snapshot(Snapshot{Seq: 10, Terminals: []Terminal{terminal}})
	apply := func(method string, params any) {
		t.Helper()
		raw, _ := json.Marshal(params)
		if !v.apply(Notification{Method: method, Params: raw}) {
			t.Fatal("event refused")
		}
	}
	event := func(seq int, kind string, data any) {
		apply("event", map[string]any{"seq": seq, "terminal": "t7", "type": kind, "data": data})
	}
	event(11, "status", map[string]any{"status": Status{State: "error"}, "reported": true})
	event(10, "status", map[string]any{"status": Status{State: "working"}})
	view, ok := v.Current("core")
	if !ok || view["t7"].Status.State != "error" {
		t.Fatal(view, ok)
	}
	apply("missed", Snapshot{Seq: 20, Terminals: []Terminal{terminal}})
	event(19, "removed", nil)
	view, _ = v.Current("core")
	if len(view) != 1 {
		t.Fatal(view)
	}
	event(21, "lost", map[string]any{})
	event(22, "restarted", terminal)
	view, _ = v.Current("core")
	if view["t7"].State != "lost" {
		t.Fatal(view)
	}
	if _, ok = v.Current("different"); ok {
		t.Fatal("view from different core accepted")
	}
	apply("heartbeat", map[string]any{"seq": 22})
	event(23, "removed", map[string]any{})
	view, _ = v.Current("core")
	if len(view) != 0 {
		t.Fatal(view)
	}
}
