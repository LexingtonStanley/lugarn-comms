package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeStrict(t *testing.T) {
	for _, input := range []string{`{}`, `[]`, `null`, `true`, `0`, `"text"`, " \n {} \t\r\n"} {
		var v any
		if err := DecodeStrict([]byte(input), &v); err != nil {
			t.Errorf("valid JSON %q: %v", input, err)
		}
	}
	for _, input := range []string{"", " \n", `{`, `{}`, `{} {}`, `{}null`, `{} garbage`, `{"id":1}`, `{"unknown":true}`, `{"name":"a",}`, string([]byte{'"', 0xff, '"'})} {
		var v MemberRef
		var dst any = &v
		if input == "{}" {
			dst = nil // invalid destination, not an invalid empty object
		}
		requireCode(t, DecodeStrict([]byte(input), dst), ErrorInvalidRequest)
	}
	for _, input := range []string{
		`{"members":[{"spawn":{"enabled":false,"surprise":1}}]}`,
		`{"members":[{"surprise":1}]}`,
		`{"limits":{"surprise":1}}`,
		`{"grid":[{"from":"a","to":"b","can":["message"],"surprise":1}]}`,
	} {
		requireCode(t, DecodeStrict([]byte(input), &TeamSpec{}), ErrorInvalidRequest)
	}
	requireCode(t, DecodeStrict([]byte(`{"code":"internal","message":"x","details":{},"surprise":1}`), &Error{}), ErrorInvalidRequest)
	// Open object fields in common.v1 deliberately permit arbitrary keys.
	if err := DecodeStrict([]byte(`{"args":{"arbitrary":true}}`), &Request{}); err != nil {
		t.Fatal(err)
	}
}

func TestOptionalAndNullableSpecFields(t *testing.T) {
	for _, input := range []string{
		`{"id":"m","name":"a","role":"worker","harness":"codex","folder":"/repo","profile":"workspace","mode":"headless","brief":"b"}`,
		`{"id":"m","name":"a","role":"worker","harness":"codex","folder":"/repo","worktree":false,"profile":"workspace","mode":"headless","brief":"b","task":null,"host":null,"spawn":{"enabled":false,"harnesses":null,"brief":null}}`,
		`{"id":"m","name":"a","role":"worker","harness":"codex","folder":"/repo","profile":"workspace","mode":"headless","brief":"b","task":"t","spawn":{"enabled":true,"harnesses":["codex"],"brief":"b","running_at_once":0}}`,
	} {
		assertRoundTrip(t, []byte(input), &MemberSpec{})
	}
	var absent MemberSpec
	if err := DecodeStrict([]byte(`{}`), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.Task != nil || absent.Host != nil || absent.Worktree != nil || absent.Spawn != nil {
		t.Errorf("omitted optional fields were populated: %#v", absent)
	}
	var explicit MemberSpec
	if err := DecodeStrict([]byte(`{"task":null,"worktree":false,"host":null}`), &explicit); err != nil {
		t.Fatal(err)
	}
	if explicit.Task == nil || *explicit.Task != nil || explicit.Host == nil || explicit.Worktree == nil || *explicit.Worktree {
		t.Errorf("explicit null/false fields collapsed: %#v", explicit)
	}
	var limits Limits
	assertRoundTrip(t, []byte(`{}`), &limits)
	assertRoundTrip(t, []byte(`{"hop_budget":0,"review_rounds":0,"per_recipient":0,"headless_turns":0,"turn_timeout_seconds":0}`), &limits)
	// Decoding leaves schema and semantic rules to policy, even when not valid.
	assertRoundTrip(t, []byte(`{"enabled":true}`), &SpawnSettings{})
	requireCode(t, DecodeStrict([]byte(`{"host":"remote"}`), &MemberSpec{}), ErrorInvalidRequest)
	requireCode(t, DecodeStrict([]byte(`{"host":{}}`), &Member{}), ErrorInvalidRequest)
}

func TestTimeJSON(t *testing.T) {
	local := time.Date(2026, 10, 26, 16, 3, 7, 123_999_999, time.FixedZone("+02", 7200))
	if got := FormatTime(local); got != "2026-10-26T14:03:07.123Z" {
		t.Errorf("FormatTime = %s", got)
	}
	assertRoundTrip(t, []byte(`"2026-10-26T14:03:07.123Z"`), &Time{})
	for _, input := range []string{`"2026-10-26T14:03:07Z"`, `"2026-10-26T14:03:07.12Z"`, `"2026-10-26T14:03:07.1234Z"`, `"2026-10-26T14:03:07.123+00:00"`, `"2026-02-30T14:03:07.123Z"`, `"2026-10-26T14:03:07,123Z"`, `42`} {
		requireCode(t, DecodeStrict([]byte(input), &Time{}), ErrorInvalidRequest)
	}
	for _, value := range []any{(*Time)(nil), &Time{}, Time{}} {
		data, err := json.Marshal(value)
		if err != nil || string(data) != "null" {
			t.Errorf("zero time JSON: %s, %v", data, err)
		}
	}
	var value Time
	if err := json.Unmarshal([]byte(`"2026-10-26T14:03:07.123Z"`), &value); err != nil {
		t.Fatal(err)
	}
	if value.Location() != time.UTC || value.Nanosecond() != 123_000_000 {
		t.Errorf("parsed Time = %v", value)
	}
	if err := json.Unmarshal([]byte(`null`), &value); err != nil || !value.IsZero() {
		t.Errorf("null must clear an existing time: %v, %v", value, err)
	}
}
