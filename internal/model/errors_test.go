package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestErrorJSON(t *testing.T) {
	var _ error = (*Error)(nil)
	for _, e := range []*Error{
		{Code: ErrorDenied, Message: "no edge"},
		NewError(ErrorDenied, "no edge"),
		NewError(ErrorDenied, "no edge").WithDetails(nil),
	} {
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != `{"code":"denied","message":"no edge","details":{}}` {
			t.Errorf("Error JSON: %s", data)
		}
		assertRoundTrip(t, data, &Error{})
		if e.Error() != "denied: no edge" {
			t.Errorf("Error() = %q", e.Error())
		}
	}
	e := NewError(ErrorDenied, "no edge").WithDetails(map[string]any{"action": "message"}).WithDetail("message_id", "m1")
	if !reflect.DeepEqual(e.Details, map[string]any{"action": "message", "message_id": "m1"}) {
		t.Errorf("details setters: %#v", e.Details)
	}
	assertRoundTrip(t, []byte(`{"code":"denied","message":"no edge","details":{"action":"message","message_id":"m1"}}`), e)
	if got := (&Error{}).WithDetail("reason", "test").Details["reason"]; got != "test" {
		t.Errorf("WithDetail on nil details: %v", got)
	}
}

func TestAsError(t *testing.T) {
	e := NewError(ErrorNotFound, "no such team")
	for _, err := range []error{e, fmt.Errorf("context: %w", e)} {
		if AsError(err) != e {
			t.Errorf("AsError did not preserve protocol error")
		}
	}
	if AsError(nil) != nil {
		t.Error("AsError(nil) must be nil")
	}
	var typedNil *Error
	if AsError(typedNil) != nil {
		t.Error("AsError(typed nil) must be nil")
	}
	unknown := errors.New("disk failed")
	got := AsError(unknown)
	if got.Code != ErrorInternal || !reflect.DeepEqual(got.Details, map[string]any{"cause": "disk failed"}) {
		t.Errorf("unknown error: %#v", got)
	}
}

func TestExitCodesAgainstProtocol(t *testing.T) {
	text := string(readFile(t, "../../docs/protocol.md"))
	_, section, ok := strings.Cut(text, "## Errors\n")
	if !ok {
		t.Fatal("Errors section missing")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	rows := regexp.MustCompile("(?m)^\\| `([a-z_]+)` +\\| ([0-9]+) +\\|").FindAllStringSubmatch(section, -1)
	seen := map[ErrorCode]bool{}
	for _, row := range rows {
		code := ErrorCode(row[1])
		want, err := strconv.Atoi(row[2])
		if err != nil {
			t.Fatal(err)
		}
		if !code.Valid() {
			t.Errorf("protocol error code %s has no Go constant", code)
		}
		if got := ExitCode(code); got != want {
			t.Errorf("ExitCode(%s) = %d; protocol = %d", code, got, want)
		}
		seen[code] = true
	}
	for _, code := range ErrorCode("").Values() {
		if !seen[code] {
			t.Errorf("code %s has no tested Errors table row", code)
		}
	}
	if ExitCode(ErrorCode("future_code")) != 1 {
		t.Error("unknown error code must use internal exit status")
	}
}
