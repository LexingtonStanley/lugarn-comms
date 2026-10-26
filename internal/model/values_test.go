package model

import (
	"encoding/base64"
	"encoding/hex"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"
)

func requireCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s", code)
	}
	e, ok := err.(*Error)
	if !ok || e.Code != code {
		t.Fatalf("error = %T %v; want *Error with code %s", err, err, code)
	}
}

func TestULID(t *testing.T) {
	before := time.UnixMilli(1_793_023_387_123)
	first, err := NewULIDAt(before)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewULIDAt(before.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if first >= second {
		t.Fatalf("ULIDs not ordered across milliseconds: %s >= %s", first, second)
	}
	for _, id := range []string{first, second} {
		if !regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`).MatchString(id) {
			t.Errorf("invalid generated ULID %q", id)
		}
		if err := ValidateULID(id); err != nil {
			t.Fatal(err)
		}
	}
	// Known timestamp prefix from the ULID representation, independent of the
	// randomly generated suffix: 1469918176385 milliseconds => 01ARYZ6S41.
	id, err := NewULIDAt(time.UnixMilli(1_469_918_176_385))
	if err != nil || !strings.HasPrefix(id, "01ARYZ6S41") {
		t.Fatalf("ULID timestamp: %q, %v", id, err)
	}
	for _, ms := range []int64{0, 1<<48 - 1} {
		if id, err := NewULIDAt(time.UnixMilli(ms)); err != nil || ValidateULID(id) != nil {
			t.Errorf("ULID timestamp boundary %d: %s, %v", ms, id, err)
		}
	}
	for _, ms := range []int64{-1, 1 << 48} {
		_, err := NewULIDAt(time.UnixMilli(ms))
		requireCode(t, err, ErrorInvalidRequest)
	}
	for _, s := range []string{"", strings.Repeat("0", 25), strings.Repeat("0", 27), strings.Repeat("Z", 26), "8" + strings.Repeat("0", 25), "01aryz6s41tsv4rrffq69g5fav", "01ARYZ6S41TSV4RRFFQ69G5FAI"} {
		requireCode(t, ValidateULID(s), ErrorInvalidRequest)
	}
	id, err = NewULID()
	if err != nil || ValidateULID(id) != nil {
		t.Fatalf("NewULID() = %q, %v", id, err)
	}
}

func TestRandomValueFormats(t *testing.T) {
	for range 32 {
		seal, err := NewSeal()
		if err != nil || !regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{8}$`).MatchString(seal) || ValidateSeal(seal) != nil {
			t.Fatalf("NewSeal() = %q, %v", seal, err)
		}
		host, err := NewHostID()
		b, decodeErr := hex.DecodeString(host)
		if err != nil || decodeErr != nil || len(b) != 16 || host != strings.ToLower(host) || ValidateHostID(host) != nil {
			t.Fatalf("NewHostID() = %q, %v", host, err)
		}
		token, err := NewToken()
		b, decodeErr = base64.RawURLEncoding.Strict().DecodeString(token)
		if err != nil || decodeErr != nil || len(b) != 32 || len(token) != 43 || !regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`).MatchString(token) || ValidateToken(token) != nil {
			t.Fatalf("NewToken() format: err %v, decode %v, length %d", err, decodeErr, len(token))
		}
	}
	for _, s := range []string{"", "7KQ2M9X", "7KQ2M9XAA", "7kq2m9xa", "7KQ2M9XI"} {
		requireCode(t, ValidateSeal(s), ErrorInvalidRequest)
	}
	for _, s := range []string{"", strings.Repeat("a", 31), strings.Repeat("a", 33), strings.Repeat("A", 32), strings.Repeat("z", 32)} {
		requireCode(t, ValidateHostID(s), ErrorInvalidRequest)
	}
	for _, s := range []string{"", strings.Repeat("A", 42), strings.Repeat("A", 44), strings.Repeat("A", 43) + "=", strings.Repeat("A", 42) + "+", strings.Repeat("A", 42) + "B", strings.Repeat("A", 42) + "\n"} {
		requireCode(t, ValidateToken(s), ErrorInvalidRequest)
	}
}

func TestHashToken(t *testing.T) {
	for _, tt := range []struct{ token, hash string }{
		{"abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
		{strings.Repeat("A", 43), "0f007385b6f9d4b7eeb2748605afe1a984a0a3bfa3f014d09e2a784ce9e5cd1a"},
	} {
		if got := HashToken(tt.token); got != tt.hash {
			t.Errorf("HashToken vector = %s; want %s", got, tt.hash)
		}
	}
}

func TestCursor(t *testing.T) {
	const host = "9f3c2a71d4e85b06a1c7e2f490b3d58e"
	for _, seq := range []int64{0, 1, 1043, math.MaxInt64} {
		cursor, err := MakeCursor(host, seq)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ParseCursor(cursor, host)
		if err != nil || got != seq {
			t.Errorf("cursor round trip: %q => %d, %v; want %d", cursor, got, err, seq)
		}
		if seq == 1043 && cursor != host+".1043" {
			t.Errorf("cursor format: %s", cursor)
		}
	}
	for _, tt := range []struct{ cursor, host string }{
		{"", host}, {host, host}, {host + ".", host}, {host + ".-1", host},
		{host + ".+1", host}, {host + ".1.2", host}, {host + ".1e2", host},
		{host + ".1 ", host}, {host + ".１", host}, {host + ".9223372036854775808", host},
		{host + "." + strings.Repeat("0", 96), host},
		{host + ".1", strings.Repeat("0", 32)}, {host + ".1", ""},
		{"bad.1", "bad"}, {strings.ToUpper(host) + ".1", strings.ToUpper(host)},
	} {
		_, err := ParseCursor(tt.cursor, tt.host)
		requireCode(t, err, ErrorCursorInvalid)
	}
	for _, tt := range []struct {
		host string
		seq  int64
	}{{"", 0}, {"bad", 1}, {host, -1}} {
		_, err := MakeCursor(tt.host, tt.seq)
		requireCode(t, err, ErrorCursorInvalid)
	}
}

func TestTextBounds(t *testing.T) {
	for _, n := range []int{0, 1, 65_536, 65_537} {
		err := ValidateText(strings.Repeat("界", n))
		if n == 0 || n > 65_536 {
			requireCode(t, err, ErrorInvalidRequest)
		} else if err != nil {
			t.Errorf("%d code points: %v", n, err)
		}
	}
	if err := ValidateText("👩‍💻e\u0301"); err != nil {
		t.Fatal(err)
	}
	requireCode(t, ValidateText(string([]byte{0xff})), ErrorInvalidRequest)
}

func TestNameValidators(t *testing.T) {
	for _, tt := range []struct {
		name     string
		validate func(string) error
		good     []string
		bad      []string
	}{
		{"OpaqueID", ValidateOpaqueID, []string{"a", "A_9-z", strings.Repeat("x", 64)}, []string{"", strings.Repeat("x", 65), "a.b", "a b", "é", "a\n"}},
		{"TeamName", ValidateTeamName, []string{"a", "0", "team-1", strings.Repeat("a", 48)}, []string{"", "-team", "Team", "team_x", strings.Repeat("a", 49), "é", "a\n"}},
		{"MemberName", ValidateMemberName, []string{"a", "a-1", "human", "all", strings.Repeat("a", 32)}, []string{"", "0a", "-a", "A", "a_b", "é", strings.Repeat("a", 33), "a\n"}},
		{"AgentName", ValidateAgentName, []string{"a", "human-a", "all-workers", strings.Repeat("a", 32)}, []string{"", "human", "all", "0a", "-a", "A", "a_b", "é", strings.Repeat("a", 33)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, s := range tt.good {
				if err := tt.validate(s); err != nil {
					t.Errorf("valid name %q: %v", s, err)
				}
			}
			for _, s := range tt.bad {
				requireCode(t, tt.validate(s), ErrorInvalidRequest)
			}
		})
	}
}
