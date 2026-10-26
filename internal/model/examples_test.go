package model

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type recordExample struct {
	name   string
	record any
	file   string
	line   int // 1-based NDJSON line, or zero for a JSON file
	path   string
}

// Paths are JSON pointers into the real examples, not synthesized fixtures.
var recordExamples = []recordExample{
	{"Error", Error{}, "app-ops/apply-team--revision-conflict.response.json", 0, "/error"},
	{"MemberRef", MemberRef{}, "app-ops/hold.response.json", 0, "/result/member"},
	{"Worktree", Worktree{}, "app-ops/members.response.json", 0, "/result/members/2/worktree"},
	{"Lease", Lease{}, "app-ops/members.response.json", 0, "/result/members/1/lease"},
	{"Lease", Lease{}, "app-ops/timeline.response.json", 0, "/result/events/0/data/member/lease"},
	{"MemberSession", MemberSession{}, "app-ops/members.response.json", 0, "/result/members/1/sessions/0"},
	{"HerdrPlacement", HerdrPlacement{}, "app-ops/members.response.json", 0, "/result/members/1/herdr"},
	{"Member", Member{}, "app-ops/members.response.json", 0, "/result/members/0"},
	{"Member", Member{}, "app-ops/members.response.json", 0, "/result/members/1"},
	{"Member", Member{}, "app-ops/members.response.json", 0, "/result/members/2"},
	{"Member", Member{}, "app-ops/timeline.response.json", 0, "/result/events/0/data/member"},
	{"Member", Member{}, "app-ops/watch.ndjson", 4, "/data/member"},
	{"Delivery", Delivery{}, "app-ops/send.response.json", 0, "/result/message/deliveries/0"},
	{"Delivery", Delivery{}, "app-ops/timeline.response.json", 0, "/result/events/3/data/message/deliveries/0"},
	{"Delivery", Delivery{}, "app-ops/watch.ndjson", 6, "/data/delivery"},
	{"Message", Message{}, "app-ops/send.response.json", 0, "/result/message"},
	{"Message", Message{}, "app-ops/timeline.response.json", 0, "/result/events/1/data/message"},
	{"Message", Message{}, "app-ops/timeline.response.json", 0, "/result/events/3/data/message"},
	{"Usage", Usage{}, "app-ops/watch.ndjson", 1, "/data/turn/usage"},
	{"Turn", Turn{}, "app-ops/watch.ndjson", 1, "/data/turn"},
	{"Request", Request{}, "app-ops/open-terminal.response.json", 0, "/result/request"},
	{"Request", Request{}, "app-ops/watch.ndjson", 3, "/data/request"},
	{"Hops", Hops{}, "app-ops/status.response.json", 0, "/result/teams/0/hops"},
	{"Runner", Runner{}, "app-ops/status.response.json", 0, "/result/teams/0/runner"},
	{"TeamStatus", TeamStatus{}, "app-ops/status.response.json", 0, "/result/teams/0"},
	{"TeamStatus", TeamStatus{}, "app-ops/apply-team.response.json", 0, "/result/team"},
	{"TeamCounts", TeamCounts{}, "app-ops/status.response.json", 0, "/result/teams/0/counts"},
	{"HerdrInfo", HerdrInfo{}, "app-ops/hello.response.json", 0, "/result/herdr"},
	{"HarnessInfo", HarnessInfo{}, "app-ops/hello.response.json", 0, "/result/harnesses/0"},
	{"HarnessInfo", HarnessInfo{}, "app-ops/hello.response.json", 0, "/result/harnesses/3"},
}

// Rounds has no record instance anywhere under schema/examples (rounds arrays
// are empty). Keep this explicit so a newly introduced record cannot be missed.
var recordsWithoutExamples = map[string]bool{"Rounds": true}

func TestCommonExampleRoundTrips(t *testing.T) {
	covered := map[string]bool{}
	for _, example := range recordExamples {
		covered[example.name] = true
		t.Run(example.name+"/"+example.file+example.path, func(t *testing.T) {
			data := exampleAt(t, example.file, example.line, example.path)
			dst := reflect.New(reflect.TypeOf(example.record)).Interface()
			assertRoundTrip(t, data, dst)
		})
	}
	for name, def := range readSchema(t, "common").Defs {
		if string(def.Type) != `"object"` {
			continue
		}
		if !covered[name] && !recordsWithoutExamples[name] {
			t.Errorf("common record %s has no example test or reported gap", name)
		}
	}
}

func TestTeamSpecExampleRoundTrips(t *testing.T) {
	files, err := filepath.Glob("../../schema/examples/team-spec/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("team-spec examples: %v, count %d", err, len(files))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			assertRoundTrip(t, readFile(t, file), &TeamSpec{})
		})
	}
}

func readFile(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// A checkout with core.autocrlf may still hand Windows CRLF line endings.
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

func exampleAt(t *testing.T, file string, line int, path string) []byte {
	t.Helper()
	data := readFile(t, "../../schema/examples/"+file)
	if line > 0 {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if line > len(lines) {
			t.Fatalf("NDJSON line %d is missing", line)
		}
		data = []byte(lines[line-1])
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		switch v := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = v[part]
			if !ok {
				t.Fatalf("missing key %s in %s", part, path)
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(v) {
				t.Fatalf("invalid index %s in %s", part, path)
			}
			value = v[i]
		default:
			t.Fatalf("cannot traverse %T at %s in %s", value, part, path)
		}
	}
	if value == nil {
		t.Fatalf("%s selects null rather than a record", path)
	}
	result, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertRoundTrip(t *testing.T, data []byte, dst any) {
	t.Helper()
	if err := DecodeStrict(data, dst); err != nil {
		t.Fatalf("decode %T: %v", dst, err)
	}
	got, err := json.Marshal(dst)
	if err != nil {
		t.Fatal(err)
	}
	var wantValue, gotValue any
	if err := json.Unmarshal(data, &wantValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	normalizeSpecNulls(wantValue, dst)
	normalizeSpecNulls(gotValue, dst)
	if !reflect.DeepEqual(wantValue, gotValue) {
		t.Errorf("round trip changed JSON\nwant: %s\n got: %s", data, got)
	}
}

// Normalize only the four nullable spec fields at their schema locations.
// All other absent fields, nulls and values remain distinct in comparisons.
func normalizeSpecNulls(value any, dst any) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	var fields []string
	switch dst.(type) {
	case *TeamSpec:
		members, _ := object["members"].([]any)
		for _, member := range members {
			normalizeSpecNulls(member, (*MemberSpec)(nil))
		}
	case *MemberSpec:
		fields = []string{"task", "host"}
		normalizeSpecNulls(object["spawn"], (*SpawnSettings)(nil))
	case *SpawnSettings:
		fields = []string{"harnesses", "brief"}
	}
	for _, field := range fields {
		if _, exists := object[field]; !exists {
			object[field] = nil
		}
	}
}

func TestSpecNullComparisonScope(t *testing.T) {
	for _, tt := range []struct {
		name        string
		dst         any
		left, right string
		equal       bool
	}{
		{"member nulls", &MemberSpec{}, `{}`, `{"task":null,"host":null}`, true},
		{"spawn nulls", &SpawnSettings{}, `{}`, `{"harnesses":null,"brief":null}`, true},
		{"nested nulls", &TeamSpec{}, `{"members":[{"spawn":{"enabled":false}}]}`, `{"members":[{"task":null,"host":null,"spawn":{"enabled":false,"harnesses":null,"brief":null}}]}`, true},
		{"task value", &MemberSpec{}, `{"task":"first"}`, `{"task":"second"}`, false},
		{"harness array", &SpawnSettings{}, `{}`, `{"harnesses":[]}`, false},
		{"required brief", &MemberSpec{}, `{}`, `{"brief":null}`, false},
		{"worktree", &MemberSpec{}, `{}`, `{"worktree":false}`, false},
		{"spawn object", &MemberSpec{}, `{}`, `{"spawn":null}`, false},
		{"herdr", &TeamSpec{}, `{}`, `{"herdr":null}`, false},
		{"limits", &TeamSpec{}, `{}`, `{"limits":null}`, false},
		{"spawn mode", &SpawnSettings{}, `{}`, `{"mode":null}`, false},
		{"task outside member", &TeamSpec{}, `{}`, `{"task":null}`, false},
		{"common record", &Member{}, `{}`, `{"host":null}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var left, right any
			if err := json.Unmarshal([]byte(tt.left), &left); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tt.right), &right); err != nil {
				t.Fatal(err)
			}
			normalizeSpecNulls(left, tt.dst)
			normalizeSpecNulls(right, tt.dst)
			if got := reflect.DeepEqual(left, right); got != tt.equal {
				t.Errorf("equal=%v; want %v for %s and %s", got, tt.equal, tt.left, tt.right)
			}
		})
	}
}
