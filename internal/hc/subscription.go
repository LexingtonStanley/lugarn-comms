package hc

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// View retains terminal observations, but only returns them while subscribed.
// Callers poll List when Current reports false. A lost/restarted terminal is
// permanently retired in this view even if hc restores a shell at its address.
type View struct {
	mu        sync.RWMutex
	terminals map[string]Terminal
	lost      map[string]bool
	seq       uint64
	connected bool
	core      string
}

func NewView() *View { return &View{terminals: map[string]Terminal{}, lost: map[string]bool{}} }
func (v *View) Current(core string) (map[string]Terminal, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make(map[string]Terminal, len(v.terminals))
	for k, t := range v.terminals {
		if v.lost[k] {
			t.State = "lost"
		}
		out[k] = t
	}
	return out, v.connected && v.core == core
}
func (v *View) snapshot(s Snapshot) {
	v.seq = s.Seq
	v.terminals = map[string]Terminal{}
	for _, t := range s.Terminals {
		if !strings.Contains(t.Address, "/") {
			v.terminals[t.Address] = t
		}
	}
	v.connected = true
}
func (v *View) apply(n Notification) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	var e struct {
		Type     string          `json:"type"`
		Terminal string          `json:"terminal"`
		Seq      uint64          `json:"seq"`
		Data     json.RawMessage `json:"data"`
	}
	if n.Method == "heartbeat" {
		return true
	}
	if n.Method == "missed" {
		var s Snapshot
		if json.Unmarshal(n.Params, &s) == nil && s.Terminals != nil {
			v.snapshot(s)
		} else {
			v.connected = false
		}
		return true
	}
	if n.Method != "event" || json.Unmarshal(n.Params, &e) != nil {
		return false
	}
	if e.Type == "heartbeat" {
		return true
	}
	if e.Type == "missed" || e.Type == "snapshot" {
		var s Snapshot
		if json.Unmarshal(e.Data, &s) == nil && s.Terminals != nil {
			v.snapshot(s)
		} else {
			v.connected = false
		}
		return true
	}
	if e.Seq <= v.seq {
		return true
	}
	v.seq = e.Seq
	if strings.Contains(e.Terminal, "/") {
		return true
	}
	t := v.terminals[e.Terminal]
	switch e.Type {
	case "created":
		if json.Unmarshal(e.Data, &t) != nil {
			return false
		}
	case "removed":
		delete(v.terminals, e.Terminal)
		return true
	case "lost", "restarted":
		v.lost[e.Terminal] = true
	case "status":
		var data struct {
			Status   *Status `json:"status"`
			Reported bool    `json:"reported"`
		}
		if json.Unmarshal(e.Data, &data) != nil {
			return false
		}
		t.Status = data.Status
		t.Reported = data.Reported
	case "foreground":
		// Decode a fresh pointer: readers may still hold the prior snapshot.
		var foreground *Foreground
		if json.Unmarshal(e.Data, &foreground) != nil {
			return false
		}
		t.Foreground = foreground
	case "turn":
		var data struct {
			Turn uint64 `json:"turn"`
		}
		if json.Unmarshal(e.Data, &data) != nil {
			return false
		}
		t.Turn = data.Turn
	case "labels":
		var data struct {
			Record *Terminal         `json:"record"`
			Labels map[string]string `json:"labels"`
		}
		if json.Unmarshal(e.Data, &data) != nil {
			return false
		}
		if data.Record != nil {
			t = *data.Record
		}
		t.Labels = data.Labels
	case "finished":
		t.State = "finished"
		t.Foreground = nil
		t.Status = nil
		var data struct {
			ExitCode *int    `json:"exit_code"`
			Reason   *string `json:"reason"`
		}
		if json.Unmarshal(e.Data, &data) != nil {
			return false
		}
		t.ExitCode = data.ExitCode
		t.Reason = data.Reason
	}
	if t.Address != "" {
		v.terminals[t.Address] = t
	}
	return true
}
func (v *View) Run(ctx context.Context, binary, session string, labels map[string]string) {
	backoff := time.Second
	for ctx.Err() == nil {
		dial, cancel := context.WithTimeout(ctx, 10*time.Second)
		c, e := Connect(dial, binary, session)
		cancel()
		if e == nil {
			sub, done := context.WithTimeout(ctx, 10*time.Second)
			var s Snapshot
			e = c.Call(sub, "events.subscribe", map[string]any{"labels": labels}, &s)
			done()
			if e == nil {
				v.mu.Lock()
				v.core = c.Hello.Core.ID
				v.snapshot(s)
				v.mu.Unlock()
				timer := time.NewTimer(45 * time.Second)
			loop:
				for {
					select {
					case <-ctx.Done():
						break loop
					case <-c.done:
						break loop
					case <-timer.C:
						break loop
					case n, ok := <-c.notes:
						if !ok || !v.apply(n) {
							break loop
						}
						backoff = time.Second
						if !timer.Stop() {
							select {
							case <-timer.C:
							default:
							}
						}
						timer.Reset(45 * time.Second)
					}
				}
				timer.Stop()
			}
			c.Close()
		}
		v.mu.Lock()
		v.connected = false
		v.mu.Unlock()
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}
