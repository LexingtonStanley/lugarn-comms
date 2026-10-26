package wire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/LexingtonStanley/lugarn-comms/internal/model"
	brokerschema "github.com/LexingtonStanley/lugarn-comms/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaBase = "https://raw.githubusercontent.com/LexingtonStanley/lugarn-comms/main/schema/"

var schemas = sync.OnceValues(loadSchemas)

type schemaSet struct {
	docs     map[string]map[string]any
	compiled map[string]*jsonschema.Schema
}

func loadSchemas() (*schemaSet, error) {
	set := &schemaSet{docs: map[string]map[string]any{}, compiled: map[string]*jsonschema.Schema{}}
	compiler := jsonschema.NewCompiler()
	for _, name := range []string{"common", "team-spec", "app-ops", "mcp-tools"} {
		b, err := brokerschema.Files.ReadFile(name + ".v1.schema.json")
		if err != nil {
			return nil, err
		}
		var doc map[string]any
		if err = json.Unmarshal(b, &doc); err != nil {
			return nil, err
		}
		set.docs[name] = doc
		// The prose lets hello accept any integer envelope version. Version support
		// for other ops is checked before schema validation by appops.
		if name == "app-ops" {
			defs := doc["$defs"].(map[string]any)
			// Only hello relaxes the envelope minimum. Keep other request
			// schemas exactly as embedded.
			encoded, err := json.Marshal(defs["RequestEnvelope"])
			if err != nil {
				return nil, err
			}
			var envelope map[string]any
			if err = json.Unmarshal(encoded, &envelope); err != nil {
				return nil, err
			}
			props := envelope["properties"].(map[string]any)
			delete(props["protocol"].(map[string]any), "minimum")
			defs["HelloRequest"].(map[string]any)["allOf"] = []any{envelope}
		}
		if err = compiler.AddResource(schemaBase+name+".v1.schema.json", doc); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"app-ops", "mcp-tools"} {
		for def := range set.docs[name]["$defs"].(map[string]any) {
			if strings.HasSuffix(def, "Request") || strings.HasSuffix(def, "Input") {
				s, err := compiler.Compile(schemaBase + name + ".v1.schema.json#/$defs/" + def)
				if err != nil {
					return nil, err
				}
				set.compiled[name+"/"+def] = s
			}
		}
	}
	return set, nil
}

// Validate checks required fields, nullability, enums, ranges and relationships
// against the embedded v1 request/input schema, without applying defaults.
func Validate(document, definition string, b []byte) error {
	set, err := schemas()
	if err != nil {
		return model.AsError(err)
	}
	s := set.compiled[document+"/"+definition]
	if s == nil {
		return model.NewError(model.ErrorInternal, "unknown schema")
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return model.NewError(model.ErrorInvalidRequest, "invalid JSON")
	}
	if err = s.Validate(v); err != nil {
		return model.NewError(model.ErrorInvalidRequest, "schema violation")
	}
	return nil
}

func toolDefinition(name, suffix string) string {
	if name == "" {
		return ""
	}
	return strings.ToUpper(name[:1]) + name[1:] + suffix
}
func requestDefinition(op string) string {
	var b strings.Builder
	for _, p := range strings.Split(op, "-") {
		b.WriteString(toolDefinition(p, ""))
	}
	return b.String() + "Request"
}

// ToolSchema returns a standalone schema with all local and common references
// resolved. No remote schema fetch is needed by an MCP client.
func ToolSchema(name string, output bool) (map[string]any, error) {
	set, err := schemas()
	if err != nil {
		return nil, err
	}
	suffix := "Input"
	if output {
		suffix = "Output"
	}
	defs := set.docs["mcp-tools"]["$defs"].(map[string]any)
	root, ok := defs[toolDefinition(name, suffix)]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	var expand func(any, string) any
	expand = func(v any, doc string) any {
		switch x := v.(type) {
		case map[string]any:
			out := map[string]any{}
			if ref, ok := x["$ref"].(string); ok {
				file, path, _ := strings.Cut(ref, "#/$defs/")
				target := doc
				if file != "" {
					target = strings.TrimSuffix(file, ".v1.schema.json")
				}
				base := expand(set.docs[target]["$defs"].(map[string]any)[path], target).(map[string]any)
				for k, v := range base {
					out[k] = v
				}
			}
			for k, v := range x {
				if k != "$ref" {
					out[k] = expand(v, doc)
				}
			}
			return out
		case []any:
			out := make([]any, len(x))
			for i, v := range x {
				out[i] = expand(v, doc)
			}
			return out
		default:
			return x
		}
	}
	return expand(root, "mcp-tools").(map[string]any), nil
}
