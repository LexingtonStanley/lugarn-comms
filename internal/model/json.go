package model

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// DecodeStrict decodes one UTF-8 JSON value, rejecting unknown struct fields and
// trailing data. It deliberately does not apply defaults or validate policy.
func DecodeStrict(data []byte, v any) error {
	if !utf8.Valid(data) {
		return NewError(ErrorInvalidRequest, "JSON must be UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return NewError(ErrorInvalidRequest, "invalid JSON").WithDetail("cause", err.Error())
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return NewError(ErrorInvalidRequest, "expected a single JSON value")
	}
	return nil
}

// Null represents the host field reserved as null in v1. A nil *Null marshals
// as null in records; a non-nil *Null also marshals as null.
type Null struct{}

func (Null) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

func (*Null) UnmarshalJSON(data []byte) error {
	if !bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return NewError(ErrorInvalidRequest, "expected null")
	}
	return nil
}
