package model

import (
	"bytes"
	"encoding/json"
	"time"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime formats t in UTC with exactly three fractional digits, truncating
// submillisecond precision.
func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// Time wraps a time.Time with the protocol's UTC millisecond JSON format.
// Use *Time for nullable record fields; nil and zero times marshal as null.
type Time struct{ time.Time }

func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(FormatTime(t.Time))
}

func (t *Time) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		t.Time = time.Time{}
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := time.Parse(timeLayout, s)
	if err != nil || FormatTime(parsed) != s {
		return NewError(ErrorInvalidRequest, "expected a UTC timestamp with exactly three fractional digits")
	}
	t.Time = parsed
	return nil
}
