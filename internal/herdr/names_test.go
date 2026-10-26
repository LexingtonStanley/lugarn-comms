package herdr

import (
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestAgentNameVectors(t *testing.T) {
	// Computed independently: Python's base32 of hashlib.sha256, alphabet
	// translated to lower-case Crockford.
	for _, tc := range []struct{ team, member, want string }{
		{"auth-rewrite-01", "writer", "writer-cc256k"},
		{"T_1", "reviewer", "reviewer-01e5tq"},
		{"team-b", "writer", "writer-rxw1tg"},
		{"a", "a", "a-05jbg9"},
		{strings.Repeat("A", 64), "abcdefghijklmnopqrstuvwxyz012345", "abcdefghijklmnopqrstuvwxy-fzzcx8"},
		{"x", "abcdefghijklmnopqrstuvwx-yz", "abcdefghijklmnopqrstuvwx--c2er1a"},
	} {
		if got := AgentName(tc.team, tc.member); got != tc.want {
			t.Errorf("AgentName(%q, %q) = %q, want %q", tc.team, tc.member, got, tc.want)
		}
	}
	if AgentName("team-a", "writer") == AgentName("team-b", "writer") {
		t.Fatal("two teams' writers share a name")
	}
}

// Every valid member name, at every length and with every character at the
// edges and around the cut, gives a name Herdr accepts.
func TestAgentNameAlwaysValid(t *testing.T) {
	const first = "abcdefghijklmnopqrstuvwxyz"
	const rest = "abcdefghijklmnopqrstuvwxyz0123456789-"
	teams := []string{"a", "0", "-", "_", strings.Repeat("Z", 64), strings.Repeat("_-9", 21), "T_1-x"}
	n := 0
	check := func(member string) {
		if model.ValidateMemberName(member) != nil {
			t.Fatalf("test made an invalid member name %q", member)
		}
		for _, team := range teams {
			if model.ValidateOpaqueID(team) != nil {
				t.Fatalf("test made an invalid team id %q", team)
			}
			name := AgentName(team, member)
			n++
			if err := ValidateAgentName(name); err != nil {
				t.Fatalf("AgentName(%q, %q) = %q: %v", team, member, name, err)
			}
			cut := member
			if len(cut) > 25 {
				cut = cut[:25]
			}
			tag, ok := strings.CutPrefix(name, cut+"-")
			if !ok || len(tag) != 6 || strings.Trim(tag, "0123456789abcdefghjkmnpqrstvwxyz") != "" {
				t.Fatalf("AgentName(%q, %q) = %q: not <cut>-<6 Crockford>", team, member, name)
			}
		}
	}
	for length := 1; length <= 32; length++ {
		for _, f := range first {
			for _, r := range rest {
				// First character, then the same character up to the length.
				check(string(f) + strings.Repeat(string(r), length-1))
			}
		}
		// Each character at the last position and around the cut.
		for _, r := range rest {
			for _, at := range []int{length - 1, 23, 24, 25, 26} {
				if at < 1 || at >= length {
					continue
				}
				b := []byte(strings.Repeat("m", length))
				b[at] = byte(r)
				check(string(b))
			}
		}
	}
	t.Logf("%d names checked", n)
}

func TestNames(t *testing.T) {
	if got := WorkspaceName("auth-rewrite"); got != "lugarn-auth-rewrite" {
		t.Fatalf("WorkspaceName = %q", got)
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
