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

const credentialBoundary = `(^|[^A-Za-z0-9_])`

var (
	tokenShape      = regexp.MustCompile(credentialBoundary + `(?:sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,}|xox[abposr]-[A-Za-z0-9-]{10,}|(?:AKIA|ASIA)[A-Z0-9]{16}|AIza[0-9A-Za-z_-]{35}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,})`)
	bearerShape     = regexp.MustCompile(credentialBoundary + `((?i:bearer)\s+)[^\s]{8,}`)
	privateKeyStart = regexp.MustCompile(credentialBoundary + `-----BEGIN ([A-Z ]*PRIVATE KEY)-----`)
)

// RE2 has no backreferences, so find the END marker with the same key label
// explicitly. An unterminated private key consumes the rest of the reason.
func scrubFailurePrivateKeys(reason string) string {
	var out strings.Builder
	for {
		match := privateKeyStart.FindStringSubmatchIndex(reason)
		if match == nil {
			out.WriteString(reason)
			return out.String()
		}
		out.WriteString(reason[:match[3]]) // Keep the captured boundary.
		out.WriteString(redacted)
		endMarker := "-----END " + reason[match[4]:match[5]] + "-----"
		rest := reason[match[1]:]
		end := strings.Index(rest, endMarker)
		if end < 0 {
			return out.String()
		}
		reason = rest[end+len(endMarker):]
	}
}

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
		reason = scrubFailurePrivateKeys(reason)
		reason = bearerShape.ReplaceAllString(reason, "${1}${2}"+redacted)
		reason = tokenShape.ReplaceAllString(reason, "${1}"+redacted)
		runes := []rune(reason)
		if len(runes) > failureReasonRunes {
			reason = string(runes[:failureReasonRunes])
		}
		return reason
	}
}
