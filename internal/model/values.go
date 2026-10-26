package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	crockford         = base32.NewEncoding(crockfordAlphabet).WithPadding(base32.NoPadding)
	ulidPattern       = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)
	opaqueIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	teamNamePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	memberNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	sealPattern       = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{8}$`)
	hostIDPattern     = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// NewULID generates a ULID using the current millisecond and 80 random bits.
// Within a millisecond, random suffixes do not guarantee monotonic order.
func NewULID() (string, error) { return NewULIDAt(time.Now()) }

// NewULIDAt generates a ULID at t, which must fit an unsigned 48-bit Unix
// millisecond timestamp. It uses the OS CSPRNG for the remaining 80 bits.
func NewULIDAt(t time.Time) (string, error) {
	ms := t.UnixMilli()
	if ms < 0 || ms > 1<<48-1 {
		return "", NewError(ErrorInvalidRequest, "ULID timestamp is outside the 48-bit range")
	}
	var entropy [10]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", AsError(err)
	}
	var timestamp [10]byte
	for i := len(timestamp) - 1; i >= 0; i-- {
		timestamp[i] = crockfordAlphabet[ms&31]
		ms >>= 5
	}
	return string(timestamp[:]) + crockford.EncodeToString(entropy[:]), nil
}

// ValidateULID checks upper-case Crockford encoding and the 128-bit ULID range.
// common.v1's Ulid pattern is broader: it does not constrain the first character
// to 0-7, so it also accepts strings that overflow a ULID's 128 bits.
func ValidateULID(s string) error { return validate(ulidPattern.MatchString(s), "ULID") }

// ValidateOpaqueID checks common.v1's OpaqueId pattern.
func ValidateOpaqueID(s string) error { return validate(opaqueIDPattern.MatchString(s), "opaque id") }

// ValidateTeamName checks common.v1's TeamName pattern.
func ValidateTeamName(s string) error { return validate(teamNamePattern.MatchString(s), "team name") }

// ValidateMemberName checks common.v1's MemberName pattern. The reserved names
// human and all are valid member names, but cannot name agents.
func ValidateMemberName(s string) error {
	return validate(memberNamePattern.MatchString(s), "member name")
}

// ValidateAgentName checks MemberName and rejects the reserved human and all.
func ValidateAgentName(s string) error {
	return validate(memberNamePattern.MatchString(s) && s != "human" && s != "all", "agent name")
}

// NewSeal returns eight Crockford base32 characters from 40 random bits.
func NewSeal() (string, error) {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", AsError(err)
	}
	return crockford.EncodeToString(b[:]), nil
}

// ValidateSeal checks the eight-character Crockford base32 seal format.
func ValidateSeal(s string) error { return validate(sealPattern.MatchString(s), "seal") }

// NewHostID returns 128 random bits as 32 lower-case hex characters.
func NewHostID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", AsError(err)
	}
	return hex.EncodeToString(b[:]), nil
}

// ValidateHostID checks the host's 32-character lower-case hex format.
func ValidateHostID(s string) error { return validate(hostIDPattern.MatchString(s), "host id") }

// MakeCursor encodes a nonnegative event sequence, including zero for an empty
// log. Sequences use int64 to match the store's SQLite INTEGER range.
func MakeCursor(hostID string, seq int64) (string, error) {
	if ValidateHostID(hostID) != nil || seq < 0 {
		return "", NewError(ErrorCursorInvalid, "invalid cursor host or sequence")
	}
	return hostID + "." + strconv.FormatInt(seq, 10), nil
}

// ParseCursor verifies the cursor belongs to hostID and returns its sequence.
// The store must separately reject a sequence ahead of its current event log.
func ParseCursor(cursor, hostID string) (int64, error) {
	host, sequence, ok := strings.Cut(cursor, ".")
	if !ok || len(cursor) > 128 || host != hostID || ValidateHostID(host) != nil || sequence == "" {
		return 0, NewError(ErrorCursorInvalid, "invalid cursor or host mismatch")
	}
	for _, c := range sequence {
		if c < '0' || c > '9' {
			return 0, NewError(ErrorCursorInvalid, "invalid cursor sequence")
		}
	}
	seq, err := strconv.ParseInt(sequence, 10, 64)
	if err != nil {
		return 0, NewError(ErrorCursorInvalid, "invalid cursor sequence")
	}
	return seq, nil
}

// NewToken returns 32 random bytes as unpadded base64url (43 characters).
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", AsError(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// ValidateToken checks canonical, unpadded base64url encoding of 32 bytes.
func ValidateToken(s string) error {
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return validate(err == nil && len(s) == 43 && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == s, "token")
}

// HashToken hashes the encoded token string, without trimming or decoding it.
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

const (
	MinTextCodePoints = 1
	MaxTextCodePoints = 65_536
)

// ValidateText checks valid UTF-8 and the protocol's Unicode code point bounds.
func ValidateText(s string) error {
	n := utf8.RuneCountInString(s)
	return validate(utf8.ValidString(s) && n >= MinTextCodePoints && n <= MaxTextCodePoints, "text")
}

func validate(ok bool, what string) error {
	if !ok {
		return NewError(ErrorInvalidRequest, "invalid "+what)
	}
	return nil
}
