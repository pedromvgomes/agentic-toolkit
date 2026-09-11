package reviewrun

import (
	"encoding/json"
	"testing"
)

// providerSchemas is every schema this package hands a provider, by the name a
// failure should say. A schema reachable from request() and absent here is a
// schema nothing below checks.
func providerSchemas() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"findingSchema":   findingSchema,
		"validatorSchema": validatorSchema,
		"judgeSchema":     judgeSchema,
	}
}

// requireEveryProperty walks one schema node and reports each object whose
// `required` omits a key its `properties` declares.
//
// The walk is generic rather than a list of today's field names: a schema that
// gains a field has to fail here, not on the next pull request review.
func requireEveryProperty(t *testing.T, where string, node any) {
	t.Helper()
	switch n := node.(type) {
	case map[string]any:
		props, isObject := n["properties"].(map[string]any)
		if isObject {
			required := map[string]bool{}
			list, _ := n["required"].([]any)
			for _, key := range list {
				if s, ok := key.(string); ok {
					required[s] = true
				}
			}
			for key := range props {
				if !required[key] {
					t.Errorf("%s declares %q in properties and omits it from required: "+
						"OpenAI strict structured-output mode refuses the whole schema, "+
						"so every run using it dies having reviewed nothing", where, key)
				}
			}
		}
		for key, child := range n {
			requireEveryProperty(t, where+"."+key, child)
		}
	case []any:
		for _, child := range n {
			requireEveryProperty(t, where, child)
		}
	}
}

func TestProviderSchemasRequireEveryDeclaredProperty(t *testing.T) {
	for name, raw := range providerSchemas() {
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		requireEveryProperty(t, name, doc)
	}
}

// nullableTypes reports the type union a property declares, as a set.
func nullableTypes(prop any) map[string]bool {
	spec, ok := prop.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]bool{}
	switch t := spec["type"].(type) {
	case string:
		out[t] = true
	case []any:
		for _, one := range t {
			if s, ok := one.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

// findProperty walks a schema and returns every declaration of one property
// name, wherever it is nested.
func findProperty(node any, name string) []any {
	var out []any
	switch n := node.(type) {
	case map[string]any:
		if props, ok := n["properties"].(map[string]any); ok {
			if prop, ok := props[name]; ok {
				out = append(out, prop)
			}
		}
		for _, child := range n {
			out = append(out, findProperty(child, name)...)
		}
	case []any:
		for _, child := range n {
			out = append(out, findProperty(child, name)...)
		}
	}
	return out
}

// Being in `required` is what keeps a schema valid under strict mode, but a
// field a reviewer may have nothing to say about must still be declinable, or
// `required` becomes a demand to invent one. The two hold together only when
// the type admits null.
func TestProviderSchemasLetADeclinedSuggestionBeNull(t *testing.T) {
	seen := 0
	for name, raw := range providerSchemas() {
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		for _, prop := range findProperty(doc, "suggestion") {
			seen++
			if types := nullableTypes(prop); !types["null"] || !types["string"] {
				t.Errorf("%s declares suggestion as %v; want both string and null, "+
					"so a reviewer with nothing to suggest can say so", name, types)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no schema declares suggestion; this test no longer guards anything")
	}
}

// A declined suggestion arrives as JSON null. It has to land as the empty
// string every consumer already guards on — the literal "null" would be posted
// to a pull request as advice.
func TestNullSuggestionDecodesToEmptyRatherThanTheWordNull(t *testing.T) {
	var reviewer reviewerAnswer
	body := `{"findings":[{"path":"a.go","start_line":null,"end_line":null,` +
		`"category":"correctness","severity":"RED","confidence":"high",` +
		`"issue":"x","evidence":"y","suggestion":null}]}`
	if err := json.Unmarshal([]byte(body), &reviewer); err != nil {
		t.Fatalf("decoding a reviewer answer with a null suggestion: %v", err)
	}
	if len(reviewer.Findings) != 1 {
		t.Fatalf("want 1 finding, got %d", len(reviewer.Findings))
	}
	if got := reviewer.Findings[0].Suggestion; got != "" {
		t.Errorf("reviewer suggestion: want empty, got %q", got)
	}

	var judge judgeAnswer
	if err := json.Unmarshal([]byte(
		`{"findings":[{"id":"a","severity":"RED","issue":"x","suggestion":null}],"good":[]}`,
	), &judge); err != nil {
		t.Fatalf("decoding a judge answer with a null suggestion: %v", err)
	}
	if len(judge.Findings) != 1 {
		t.Fatalf("want 1 judged finding, got %d", len(judge.Findings))
	}
	if got := judge.Findings[0].Suggestion; got != "" {
		t.Errorf("judge suggestion: want empty, got %q", got)
	}
}
