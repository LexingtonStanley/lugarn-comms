package wire

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestSchemaDiagnosticsIdentifyLocationsWithoutValues(t *testing.T) {
	const sentinel = "PRIVATE_VALUE_MUST_NOT_APPEAR"
	for _, tc := range []struct{ name, definition, input, at, keyword string }{
		{"pattern", "SendInput", `{"to":"` + sentinel + `!","text":"ok"}`, "/to", "pattern"},
		{"type", "InboxInput", `{"limit":"` + sentinel + `"}`, "/limit", "type"},
		{"range", "InboxInput", `{"limit":101}`, "/limit", "maximum"},
		{"required", "SendInput", `{"to":"writer"}`, "", "required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := model.AsError(Validate("mcp-tools", tc.definition, []byte(tc.input)))
			if failure == nil || failure.Code != model.ErrorInvalidRequest {
				t.Fatal(failure)
			}
			want := map[string]any{"at": tc.at, "keyword": tc.keyword}
			if !reflect.DeepEqual(failure.Details, want) {
				t.Errorf("details=%v, want %v", failure.Details, want)
			}
			raw, err := json.Marshal(failure)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), sentinel) {
				t.Fatal("schema diagnostic echoed a value")
			}
		})
	}
}
