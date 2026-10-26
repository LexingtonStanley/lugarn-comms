package wire

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Each enum wire defines has its schema's values, in the schema's order.
// HerdrStartMethod lacked systemd before this test existed.
func TestEnumsMatchSchemas(t *testing.T) {
	strs := func(values any) []string {
		v := reflect.ValueOf(values)
		out := make([]string, v.Len())
		for i := range out {
			out[i] = v.Index(i).String()
		}
		return out
	}
	for _, tc := range []struct {
		file, def string
		values    []string
	}{
		{"app-ops.v1.schema.json", "OpName", strs(OpName("").Values())},
		{"app-ops.v1.schema.json", "HerdrStartMethod", strs(HerdrStartMethod("").Values())},
		{"app-ops.v1.schema.json", "GitState", strs(GitState("").Values())},
		{"app-ops.v1.schema.json", "WorktreeRefusal", strs(WorktreeRefusal("").Values())},
		{"app-ops.v1.schema.json", "BranchOutcome", strs(BranchOutcome("").Values())},
		{"mcp-tools.v1.schema.json", "ToolName", strs(ToolName("").Values())},
	} {
		b, err := os.ReadFile(filepath.Join("..", "..", "schema", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Defs map[string]struct {
				Enum []string `json:"enum"`
			} `json:"$defs"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		if want := doc.Defs[tc.def].Enum; len(want) == 0 || !reflect.DeepEqual(tc.values, want) {
			t.Errorf("%s: Go %q, schema %q", tc.def, tc.values, want)
		}
	}
}
