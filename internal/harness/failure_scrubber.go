package harness

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const redacted = "‹redacted›"
const failureReasonRunes = 300

var secretEnvName = regexp.MustCompile(`(?i)(TOKEN|SECRET|PASSW|API_?KEY|ACCESS_?KEY|PRIVATE_?KEY|AUTH|CREDENTIAL|COOKIE|SESSION)`)

type failureScrubber func(string) string

// newFailureScrubber snapshots sensitive values before a child starts. Replace
// exact values before normalizing or truncating, which could split a secret.
func newFailureScrubber(overrides map[string]string, host []string, token string) failureScrubber {
	values := map[string]bool{}
	add := func(value string) {
		if utf8.RuneCountInString(value) >= 8 {
			values[value] = true
		}
	}
	for _, value := range overrides {
		add(value)
	}
	for _, entry := range host {
		name, value, ok := strings.Cut(entry, "=")
		if ok && secretEnvName.MatchString(name) {
			add(value)
		}
	}
	add(token)
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	// Prefer a complete longer match when two values share a prefix.
	sort.Slice(keys, func(a, b int) bool {
		if len(keys[a]) == len(keys[b]) {
			return keys[a] < keys[b]
		}
		return len(keys[a]) > len(keys[b])
	})
	pairs := make([]string, 0, 2*len(keys))
	for _, value := range keys {
		pairs = append(pairs, value, redacted)
	}
	replace := strings.NewReplacer(pairs...)
	return func(reason string) string {
		reason = replace.Replace(reason)
		reason = strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return ' '
			}
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return -1
			}
			return r
		}, reason)
		reason = strings.Join(strings.Fields(reason), " ")
		// Removing controls can join pieces into a sensitive value.
		reason = replace.Replace(reason)
		runes := []rune(reason)
		if len(runes) > failureReasonRunes {
			reason = string(runes[:failureReasonRunes])
		}
		return reason
	}
}
