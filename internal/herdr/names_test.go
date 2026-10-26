package herdr

import (
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestNames(t *testing.T) {
	if got := WorkspaceName("auth-rewrite"); got != "lugarn-auth-rewrite" {
		t.Fatalf("WorkspaceName = %q", got)
	}
	for _, tc := range []struct {
		team, member, want string
	}{
		{"auth-rewrite", "reviewer", "auth-rewrite-reviewer"},
		{"a", "b", "a-b"},
		{strings.Repeat("t", 16), strings.Repeat("m", 15), strings.Repeat("t", 16) + "-" + strings.Repeat("m", 15)},
	} {
		got, err := AgentName(tc.team, tc.member)
		if err != nil || got != tc.want {
			t.Fatalf("AgentName(%q, %q) = %q, %v", tc.team, tc.member, got, err)
		}
	}
	// Team names may start with a digit and run to 48, member names to 32:
	// Herdr refuses both kinds of overflow.
	for _, tc := range [][2]string{
		{"0team", "writer"},
		{strings.Repeat("t", 16), strings.Repeat("m", 16)},
		{strings.Repeat("t", 48), strings.Repeat("m", 32)},
	} {
		_, err := AgentName(tc[0], tc[1])
		p := model.AsError(err)
		if p == nil || p.Code != model.ErrorInvalidRequest || p.Details["reason"] != "herdr_agent_name" {
			t.Fatalf("AgentName(%q, %q) err = %v", tc[0], tc[1], err)
		}
	}
	for name, valid := range map[string]bool{
		"a": true, "alpha-writer": true, "under_score": true, strings.Repeat("a", 32): true,
		"": false, strings.Repeat("a", 33): false, "0team": false, "-lead": false, "UPPER": false, "dot.name": false,
	} {
		if (ValidateAgentName(name) == nil) != valid {
			t.Fatalf("ValidateAgentName(%q) valid = %v", name, !valid)
		}
	}
	for name, valid := range map[string]bool{
		"default": true, "lugarn-test-abc12345": true, "UPPER.Dots_ok-1": true, strings.Repeat("s", 64): true,
		"": false, ".": false, "..": false, "bad/name": false, "sp ace": false, "déf": false, strings.Repeat("s", 65): false,
	} {
		if (ValidateSession(name) == nil) != valid {
			t.Fatalf("ValidateSession(%q) valid = %v", name, !valid)
		}
	}
}

func TestActivityFromHerdr(t *testing.T) {
	for in, want := range map[string]model.Activity{
		"idle": model.ActivityIdle, "working": model.ActivityWorking, "blocked": model.ActivityBlocked,
		"done": model.ActivityDone, "unknown": model.ActivityUnknown,
		"": model.ActivityUnknown, "Idle": model.ActivityUnknown, "waiting": model.ActivityUnknown, "ready": model.ActivityUnknown,
	} {
		if got := ActivityFromHerdr(in); got != want {
			t.Fatalf("ActivityFromHerdr(%q) = %q, want %q", in, got, want)
		}
	}
}
