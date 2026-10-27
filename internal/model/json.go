package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
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
		return jsonDecodeError("invalid JSON", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return jsonDecodeError("expected a single JSON value", err)
	}
	return nil
}

// jsonDecodeError extracts only locations, never json error text or values.
func jsonDecodeError(message string, err error) *Error {
	failure := NewError(ErrorInvalidRequest, message)
	if err == nil {
		return failure
	}
	var mismatch *json.UnmarshalTypeError
	var syntax *json.SyntaxError
	switch {
	case errors.As(err, &mismatch):
		failure.WithDetail("field", mismatch.Field)
	case errors.As(err, &syntax):
		failure.WithDetail("offset", syntax.Offset)
	default:
		// encoding/json has no exported unknown-field error type. Its quoted
		// field name is the sole piece of its error string we accept.
		if quoted, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
			if field, e := strconv.Unquote(quoted); e == nil {
				failure.WithDetail("field", field)
			}
		}
	}
	return failure
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
