package model

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type schemaNode struct {
	Type       json.RawMessage       `json:"type"`
	Ref        string                `json:"$ref"`
	Enum       []string              `json:"enum"`
	Properties map[string]schemaNode `json:"properties"`
	Required   []string              `json:"required"`
	Defs       map[string]schemaNode `json:"$defs"`
	OneOf      []schemaNode          `json:"oneOf"`
	Items      *schemaNode           `json:"items"`
	Default    json.RawMessage       `json:"default"`
}

func readSchema(t *testing.T, name string) schemaNode {
	t.Helper()
	var schema schemaNode
	if err := json.Unmarshal(readFile(t, "../../schema/"+name+".v1.schema.json"), &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

var commonRecords = map[string]any{
	"Error": Error{}, "MemberRef": MemberRef{}, "Worktree": Worktree{},
	"Lease": Lease{}, "MemberSession": MemberSession{}, "HerdrPlacement": HerdrPlacement{},
	"Member": Member{}, "Delivery": Delivery{}, "Message": Message{}, "Usage": Usage{},
	"Turn": Turn{}, "Request": Request{}, "Hops": Hops{}, "Rounds": Rounds{},
	"Runner": Runner{}, "TeamStatus": TeamStatus{}, "HerdrInfo": HerdrInfo{}, "HarnessInfo": HarnessInfo{},
}

func TestRecordSchemaShapes(t *testing.T) {
	for _, group := range []struct {
		name  string
		types map[string]any
	}{
		{"common", commonRecords},
		{"team-spec", map[string]any{
			"TeamSpec": TeamSpec{}, "HerdrSettings": HerdrSettings{}, "Limits": Limits{},
			"MemberSpec": MemberSpec{}, "SpawnSettings": SpawnSettings{}, "GridEdge": GridEdge{},
		}},
	} {
		schema := readSchema(t, group.name)
		for name, def := range schema.Defs {
			if string(def.Type) != `"object"` {
				continue
			}
			record, ok := group.types[name]
			if !ok {
				t.Errorf("%s has no Go record", name)
				continue
			}
			t.Run(group.name+"/"+name, func(t *testing.T) {
				checkRecordFields(t, reflect.TypeOf(record), def)
				if group.name == "common" {
					checkRecordEmission(t, reflect.TypeOf(record), def)
				}
			})
		}
	}
}

func checkRecordFields(t *testing.T, typ reflect.Type, def schemaNode) {
	t.Helper()
	seen := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")
		property, ok := def.Properties[tag[0]]
		if !ok {
			t.Errorf("%s.%s is not a schema property", typ, field.Name)
			continue
		}
		seen[tag[0]] = true
		required := slices.Contains(def.Required, tag[0])
		nullable := strings.Contains(string(property.Type), `"null"`) || len(property.OneOf) > 0
		if omitted, want := slices.Contains(tag, "omitempty"), !required && !nullable; omitted != want {
			t.Errorf("%s.%s omitempty=%v; want %v (required=%v, nullable=%v)", typ, field.Name, omitted, want, required, nullable)
		}
		if wantPointer := !required || nullable; (field.Type.Kind() == reflect.Pointer) != wantPointer {
			t.Errorf("%s.%s pointer=%v; want %v", typ, field.Name, field.Type.Kind() == reflect.Pointer, wantPointer)
		}
		ft := field.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
			if ft.Kind() == reflect.Pointer {
				t.Errorf("%s.%s must use a single pointer", typ, field.Name)
				continue
			}
		}
		if len(property.OneOf) > 0 {
			property = property.OneOf[0]
		}
		if property.Ref != "" {
			ref := property.Ref[strings.LastIndex(property.Ref, "/")+1:]
			wantName := ref
			switch ref {
			case "Timestamp":
				wantName = "Time"
			case "OpaqueId", "Ulid", "TeamName", "MemberName", "AgentName", "SessionId", "Path", "Text":
				wantName = "string"
			}
			if ft.Name() != wantName {
				t.Errorf("%s.%s type %s; want %s", typ, field.Name, ft, wantName)
			}
		}
		if len(property.Properties) > 0 {
			checkRecordFields(t, ft, property)
		}
		if property.Items != nil && ft.Kind() != reflect.Slice {
			t.Errorf("%s.%s must be a slice", typ, field.Name)
		}
	}
	for name := range def.Properties {
		if !seen[name] {
			t.Errorf("%s lacks schema property %s", typ, name)
		}
	}
}

func checkRecordEmission(t *testing.T, typ reflect.Type, def schemaNode) {
	t.Helper()
	v := reflect.New(typ).Elem()
	// Populate required time values; other zero-valued fields expose omitted keys,
	// accidental null collections, and nullable fields encoded as zero values.
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).Type() == reflect.TypeFor[Time]() {
			v.Field(i).Set(reflect.ValueOf(Time{time.Unix(1, 0)}))
		}
	}
	data, err := json.Marshal(v.Interface())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for name, property := range def.Properties {
		raw, ok := fields[name]
		if !ok {
			t.Errorf("%s omitted required record field %s", typ, name)
			continue
		}
		nullable := strings.Contains(string(property.Type), `"null"`) || len(property.OneOf) > 0
		if nullable != (string(raw) == "null") {
			t.Errorf("zero %s.%s = %s, nullable=%v", typ, name, raw, nullable)
		}
	}
}

func TestSchemaDefaultParity(t *testing.T) {
	defaults := map[string]any{
		"HerdrSettings.server":          DefaultHerdrServer,
		"Limits.hop_budget":             DefaultHopBudget,
		"Limits.review_rounds":          DefaultReviewRounds,
		"Limits.per_recipient":          DefaultPerRecipient,
		"Limits.headless_turns":         DefaultHeadlessTurns,
		"Limits.turn_timeout_seconds":   DefaultTurnTimeoutSeconds,
		"MemberSpec.worktree":           DefaultWorktree,
		"SpawnSettings.running_at_once": DefaultSpawnRunningAtOnce,
		"SpawnSettings.per_run":         DefaultSpawnPerRun,
		"SpawnSettings.mode":            DefaultSpawnMode,
		"SpawnSettings.folder":          DefaultSpawnFolder,
		"SpawnSettings.max_profile":     DefaultSpawnMaxProfile,
	}
	for name, def := range readSchema(t, "team-spec").Defs {
		for field, property := range def.Properties {
			if property.Default == nil {
				continue
			}
			key := name + "." + field
			value, ok := defaults[key]
			if !ok {
				t.Errorf("schema default %s has no constant", key)
				continue
			}
			got, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(property.Default) {
				t.Errorf("default %s = %s; schema = %s", key, got, property.Default)
			}
			delete(defaults, key)
		}
	}
	for key := range defaults {
		t.Errorf("constant for %s has no schema default", key)
	}
}
