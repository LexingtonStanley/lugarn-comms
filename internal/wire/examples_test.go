package wire

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
)

func TestExamplesRoundTrip(t *testing.T) {
	for _, folder := range []string{"app-ops", "mcp"} {
		files, err := filepath.Glob(filepath.Join("..", "..", "schema", "examples", folder, "*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatal("no examples")
		}
		for _, file := range files {
			t.Run(folder+"/"+filepath.Base(file), func(t *testing.T) {
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				lines := [][]byte{data}
				if strings.HasSuffix(file, ".ndjson") {
					lines = nil
					scanner := bufio.NewScanner(strings.NewReader(string(data)))
					for scanner.Scan() {
						lines = append(lines, append([]byte(nil), scanner.Bytes()...))
					}
					if scanner.Err() != nil {
						t.Fatal(scanner.Err())
					}
				}
				for _, b := range lines {
					var v any
					if folder == "app-ops" {
						switch {
						case strings.HasSuffix(file, ".ndjson"):
							v, err = DecodeWatchLine(b)
						case strings.HasSuffix(file, ".request.json"):
							var env struct {
								Op string `json:"op"`
							}
							if err = json.Unmarshal(b, &env); err == nil {
								v, err = DecodeRequest(env.Op, b)
							}
						default:
							v = new(Response)
							err = model.DecodeStrict(b, v)
						}
					} else {
						name := strings.Split(filepath.Base(file), ".")[0]
						name = strings.Split(name, "--")[0]
						switch {
						case strings.HasSuffix(file, ".input.json"):
							v, err = DecodeToolInput(name, b)
						case strings.HasSuffix(file, ".error.json"):
							v = new(ToolError)
							err = model.DecodeStrict(b, v)
						default:
							v = NewToolOutput(name)
							err = model.DecodeStrict(b, v)
						}
					}
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := json.Marshal(v)
					if err != nil {
						t.Fatal(err)
					}
					var before, after any
					if err = json.Unmarshal(b, &before); err != nil {
						t.Fatal(err)
					}
					if err = json.Unmarshal(encoded, &after); err != nil {
						t.Fatal(err)
					}
					if folder == "app-ops" {
						normalizeAppExample(before)
						normalizeAppExample(after)
					}
					if !reflect.DeepEqual(before, after) {
						t.Fatalf("round trip changed JSON\noriginal: %s\nencoded: %s", b, encoded)
					}
				}
			})
		}
	}
}

func TestStrictNestedAndSchemaValidation(t *testing.T) {
	for _, b := range []string{`{"to":"human","text":"hello","extra":1}`, `{"to":"human"}`, `{"to":"all","text":"hello"}`, `{"to":"a","text":""}`, `{"to":"a","text":null}`, `{"to":"a","text":"ok","reply_to":"bad"}`} {
		if _, err := DecodeToolInput("send", []byte(b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
	for _, b := range []string{`null`, `[]`, `{"limit":0}`, `{"limit":101}`, `{"direction":"sideways"}`, `{"direction":null}`} {
		if _, err := DecodeToolInput("inbox", []byte(b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
	if _, err := DecodeWatchLine([]byte(`{"type":"member.activity_changed","cursor":"x","at":null,"team":null,"timeline":false,"data":{"member":{"id":"a","name":"a","surprise":true},"from":null,"to":"idle"}}`)); err == nil {
		t.Fatal("accepted unknown nested event field")
	}
}
func TestEveryEventDataType(t *testing.T) {
	for _, typ := range (model.EventType("")).Values() {
		original := Event{Type: typ, Data: newEventData(string(typ))}
		if original.Data == nil {
			t.Fatalf("missing event %s", typ)
		}
		b, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var got Event
		if err = model.DecodeStrict(b, &got); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		if reflect.TypeOf(got.Data) != reflect.TypeOf(original.Data) {
			t.Fatalf("%s wrong data type", typ)
		}
	}
}
func TestNewHelloIncludesLogindNull(t *testing.T) {
	b, err := json.Marshal(HelloResultHost{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"logind":null`) {
		t.Fatal(string(b))
	}
}

func TestApplyTeamPreservesLargeRevision(t *testing.T) {
	file := filepath.Join("..", "..", "schema", "examples", "app-ops", "apply-team.request.json")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	b = []byte(strings.Replace(string(b), `"if_revision": 0`, `"if_revision": 9007199254740993`, 1))
	v, err := DecodeRequest("apply-team", b)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"if_revision":9007199254740993`) {
		t.Fatal(string(encoded))
	}
}

// Omitted and null nullable fields have identical meanings in a team spec.
func normalizeSpecExample(value any) {
	spec, ok := value.(map[string]any)
	if !ok {
		return
	}
	members, _ := spec["members"].([]any)
	for _, value := range members {
		member, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"task", "host"} {
			if _, present := member[key]; !present {
				member[key] = nil
			}
		}
		if spawn, ok := member["spawn"].(map[string]any); ok {
			for _, key := range []string{"harnesses", "brief"} {
				if _, present := spawn[key]; !present {
					spawn[key] = nil
				}
			}
		}
	}
}
func normalizeAppExample(value any) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	normalizeSpecExample(object["spec"])
	result, _ := object["result"].(map[string]any)
	normalizeSpecExample(result["spec"])
	if object["op"] == "hello" {
		if host, ok := result["host"].(map[string]any); ok {
			// PR #3 adds host.logind to the hello schema and examples.
			if _, present := host["logind"]; !present {
				host["logind"] = nil
			}
		}
	}
}

func TestExampleNormalizationScope(t *testing.T) {
	var got, expected any
	if err := json.Unmarshal([]byte(`{"spec":{"members":[{"brief":"required","spawn":{}}]},"result":{"spec":{"members":[{"task":"keep","host":null,"spawn":{"brief":"keep","harnesses":[]}}]}},"data":{"members":[{"spawn":{}}]},"host":{}}`), &got); err != nil {
		t.Fatal(err)
	}
	normalizeAppExample(got)
	if err := json.Unmarshal([]byte(`{"spec":{"members":[{"brief":"required","task":null,"host":null,"spawn":{"brief":null,"harnesses":null}}]},"result":{"spec":{"members":[{"task":"keep","host":null,"spawn":{"brief":"keep","harnesses":[]}}]}},"data":{"members":[{"spawn":{}}]},"host":{}}`), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("normalization changed unrelated fields: %#v", got)
	}
}
