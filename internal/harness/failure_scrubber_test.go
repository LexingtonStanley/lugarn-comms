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

func TestFailureScrubberTokenShapes(t *testing.T) {
	scrub := newFailureScrubber(nil, nil, "")
	for _, tc := range []struct {
		name, credential, replacement string
	}{
		{"OpenAI", "sk-" + strings.Repeat("aB0_", 5), redacted},
		{"Anthropic", "sk-ant-api03-" + strings.Repeat("a", 30), redacted},
		{"OpenAI project", "sk-proj-" + strings.Repeat("a", 30), redacted},
		{"GitHub personal", "ghp_" + strings.Repeat("aB0", 12), redacted},
		{"GitHub OAuth", "gho_" + strings.Repeat("a", 36), redacted},
		{"GitHub user", "ghu_" + strings.Repeat("a", 36), redacted},
		{"GitHub server", "ghs_" + strings.Repeat("a", 36), redacted},
		{"GitHub refresh", "ghr_" + strings.Repeat("a", 36), redacted},
		{"GitHub fine grained", "github_pat_" + strings.Repeat("a_", 11), redacted},
		{"Slack app", "xoxa-" + strings.Repeat("aB0-", 3), redacted},
		{"Slack bot", "xoxb-" + strings.Repeat("a", 10), redacted},
		{"Slack personal", "xoxp-" + strings.Repeat("a", 10), redacted},
		{"Slack OAuth", "xoxo-" + strings.Repeat("a", 10), redacted},
		{"Slack service", "xoxs-" + strings.Repeat("a", 10), redacted},
		{"Slack refresh", "xoxr-" + strings.Repeat("a", 10), redacted},
		{"AWS permanent", "AKIA" + strings.Repeat("A0", 8), redacted},
		{"AWS temporary", "ASIA" + strings.Repeat("A0", 8), redacted},
		{"Google", "AIza" + strings.Repeat("aB0_-", 7), redacted},
		{"JWT", "eyJ" + strings.Repeat("a_", 4) + "." + strings.Repeat("b-", 4) + "." + strings.Repeat("C0", 4), redacted},
		{"Bearer", "Bearer abcdefgh", "Bearer " + redacted},
		{"Bearer mixed case", "bEaReR abcdefgh", "bEaReR " + redacted},
		{"PEM", "-----BEGIN PRIVATE KEY----- fake-key-data -----END PRIVATE KEY-----", redacted},
		{"PEM RSA", "-----BEGIN RSA PRIVATE KEY----- fake-key-data -----END RSA PRIVATE KEY-----", redacted},
		{"PEM encrypted", "-----BEGIN ENCRYPTED PRIVATE KEY----- fake-key-data -----END ENCRYPTED PRIVATE KEY-----", redacted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, position := range []struct{ name, before, after string }{
				{"start", "", " authentication failed"},
				{"middle", "error: invalid credential ", " rejected"},
				{"end", "error: invalid credential ", ""},
			} {
				t.Run(position.name, func(t *testing.T) {
					input := position.before + tc.credential + position.after
					want := position.before + tc.replacement + position.after
					if got := scrub(input); got != want {
						t.Errorf("got %q, want %q", got, want)
					}
				})
			}
			for _, prefix := range []string{"word", "0", "_"} {
				t.Run("inside word "+prefix, func(t *testing.T) {
					input := "error: " + prefix + tc.credential
					if got := scrub(input); got != input {
						t.Errorf("got %q, want %q", got, input)
					}
				})
			}
		})
	}
}

func TestFailureScrubberTokenShapeNegatives(t *testing.T) {
	scrub := newFailureScrubber(nil, nil, "")
	for _, input := range []string{
		"task-runner-failed", "risk-assessment", "mask-" + strings.Repeat("a", 40),
		"sk-abc", "sk-" + strings.Repeat("a", 19), "AKIA",
		"0123456789abcdef0123456789abcdef01234567",
		"550e8400-e29b-41d4-a716-446655440000", "/tmp/harness/config.json", "--standalone",
		"ghp_" + strings.Repeat("a", 35), "github_pat_" + strings.Repeat("a", 21),
		"xoxb-" + strings.Repeat("a", 9), "AKIA" + strings.Repeat("A", 15),
		"AIza" + strings.Repeat("a", 34), "eyJaaaaaaa.bbbbbbbb.cccccccc", "Bearer abcdefg",
		"-----BEGIN PUBLIC KEY----- public-data -----END PUBLIC KEY-----",
	} {
		t.Run(input, func(t *testing.T) {
			if got := scrub(input); got != input {
				t.Errorf("got %q, want %q", got, input)
			}
		})
	}
}

func TestFailureScrubberTokenShapeOrdering(t *testing.T) {
	token := "sk-ant-api03-" + strings.Repeat("a", 40)
	prefix := strings.Repeat("狐", 280) + " "
	for _, tc := range []struct {
		name, input, want string
	}{
		{"joined by controls", "error: sk-ant-\x00api03-" + strings.Repeat("a", 40), "error: " + redacted},
		{"before rune limit", prefix + token, prefix + redacted},
		{"multiple adjacent tokens", token + "," + token, redacted + "," + redacted},
		{"Bearer token shape", "Bearer " + token, "Bearer " + redacted},
		{"Bearer whitespace", "error: BEARER\t\nabcdefgh", "error: BEARER " + redacted},
		{"PEM multiline", "error: -----BEGIN EC PRIVATE KEY-----\nfake-key-data\n-----END EC PRIVATE KEY----- rejected", "error: " + redacted + " rejected"},
		{"PEM unterminated", "error: -----BEGIN PRIVATE KEY----- fake-key-data", "error: " + redacted},
		{"PEM mismatched end", "error: -----BEGIN RSA PRIVATE KEY----- fake-key-data -----END EC PRIVATE KEY----- still-secret", "error: " + redacted},
		{"PEM matching end", "error: -----BEGIN RSA PRIVATE KEY----- fake-key-data -----END EC PRIVATE KEY----- more-key-data -----END RSA PRIVATE KEY----- rejected", "error: " + redacted + " rejected"},
		{"PEM multiple blocks", "-----BEGIN PRIVATE KEY----- fake-key-data -----END PRIVATE KEY-----;-----BEGIN EC PRIVATE KEY----- more-key-data -----END EC PRIVATE KEY-----", redacted + ";" + redacted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := newFailureScrubber(nil, nil, "")(tc.input); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	for _, input := range []string{token + " private-label", "sk-ant-\x00api03-" + strings.Repeat("a", 40) + " private-label"} {
		t.Run("exact match wins "+input, func(t *testing.T) {
			scrub := newFailureScrubber(map[string]string{"KEY": token + " private-label"}, nil, "")
			if got := scrub(input); got != redacted {
				t.Errorf("got %q, want %q", got, redacted)
			}
		})
	}
}
