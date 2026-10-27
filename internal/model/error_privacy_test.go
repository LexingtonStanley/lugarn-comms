package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestErrorDiagnosticsExcludePayload(t *testing.T) {
	const sentinel = "SENTINEL_PRIVATE_ENV_ARGV_FILE_CONTENT"
	cases := []error{errors.New(sentinel), fmt.Errorf("command argv %s: %w", sentinel, &os.PathError{Op: "open", Path: "/safe/config.json", Err: errors.New(sentinel)})}
	for _, err := range cases {
		raw, e := json.Marshal(AsError(err))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(raw), sentinel) {
			t.Fatal("error contains payload")
		}
	}
	if AsError(cases[1]).Details["path"] != "/safe/config.json" {
		t.Fatal("lost path")
	}
	for _, raw := range []string{`{"unknown":"` + sentinel + `"}`, `{"code":"` + sentinel + `","details":{},"message":"ok"}`} {
		var out struct {
			Code int `json:"code"`
		}
		err := DecodeStrict([]byte(raw), &out)
		b, e := json.Marshal(AsError(err))
		if e != nil {
			t.Fatal(e)
		}
		if err == nil || strings.Contains(string(b), sentinel) {
			t.Fatal("JSON diagnostic contains input", err)
		}
	}
}
