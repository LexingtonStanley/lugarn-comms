package harness

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestFailureScrubberValues(t *testing.T) {
	for _, tc := range []struct {
		name               string
		overrides          map[string]string
		host               []string
		token, input, want string
	}{
		{"launch override", map[string]string{"PLAIN_NAME": "launch-value"}, nil, "", "bad launch-value launch-value", "bad ‹redacted› ‹redacted›"},
		{"token", nil, nil, "launch-token", "bad launch-token", "bad ‹redacted›"},
		{"ordinary host", nil, []string{"ORDINARY=ordinary-host"}, "", "ordinary-host", "ordinary-host"},
		{"minimum", map[string]string{"SHORT": "1234567", "LONG": "12345678"}, nil, "", "1234567 12345678", "1234567 ‹redacted›"},
		{"unicode minimum", map[string]string{"SHORT": "狐狐狐狐狐狐狐", "LONG": "狸狸狸狸狸狸狸狸"}, nil, "", "狐狐狐狐狐狐狐 狸狸狸狸狸狸狸狸", "狐狐狐狐狐狐狐 ‹redacted›"},
		{"exact case", map[string]string{"VALUE": "CaseSensitive"}, nil, "", "casesensitive CaseSensitive", "casesensitive ‹redacted›"},
		{"overlap", map[string]string{"A": "secret-value", "B": "secret-value-long"}, nil, "", "secret-value-long secret-value", "‹redacted› ‹redacted›"},
		{"joined by controls", map[string]string{"VALUE": "secret-value"}, nil, "", "bad secret-\x00value", "bad ‹redacted›"},
		{"before controls", map[string]string{"VALUE": "secret\nvalue"}, nil, "", "bad secret\nvalue", "bad ‹redacted›"},
		{"before limit", map[string]string{"VALUE": strings.Repeat("secret", 100)}, nil, "", strings.Repeat("secret", 100), "‹redacted›"},
		{"controls", nil, nil, "", "  error:\x00 unknown\noption\t'--standalone'\x1b\u202e\u2028", "error: unknown option '--standalone'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := newFailureScrubber(tc.overrides, tc.host, tc.token)(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
func TestFailureScrubberSecretHostNames(t *testing.T) {
	for _, name := range []string{"token", "SECRET", "Password", "aPiKeY", "API_KEY", "ACCESSKEY", "ACCESS_KEY", "PRIVATEKEY", "PRIVATE_KEY", "AUTH", "CREDENTIAL", "COOKIE", "SESSION"} {
		t.Run(name, func(t *testing.T) {
			scrub := newFailureScrubber(nil, []string{"PREFIX_" + name + "_SUFFIX=host-secret-value"}, "")
			if got := scrub("bad host-secret-value"); got != "bad ‹redacted›" {
				t.Fatal(got)
			}
		})
	}
}
func TestFailureScrubberRuneBound(t *testing.T) {
	got := newFailureScrubber(nil, nil, "")(strings.Repeat("狐", 301))
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 300 {
		t.Fatal("bound must be Unicode characters")
	}
	for _, r := range got {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			t.Fatal("control character survived")
		}
	}
}
