package tests

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestPlanJSON_Schema: --json emits the expected versioned schema with
// sources/definitions/diagnostics arrays.
func TestPlanJSON_Schema(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"),
		"source: "+url+"@main\npresets:\n  - default\n")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	stdout, _, err := runCLI(t, work, "plan", "--cache", cache, "--json")
	if err != nil {
		t.Fatalf("plan --json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("parse json: %v\n%s", err, stdout)
	}
	if got["version"] != float64(1) {
		t.Errorf("version = %v, want 1", got["version"])
	}
	for _, key := range []string{"sources", "definitions", "diagnostics"} {
		if _, ok := got[key]; !ok {
			t.Errorf("plan json missing %q (got keys: %v)", key, mapKeys(got))
		}
	}
	sources, _ := got["sources"].([]any)
	if len(sources) == 0 {
		t.Errorf("expected at least one source")
	} else if first, ok := sources[0].(map[string]any); ok {
		for _, k := range []string{"url", "ref", "sha", "kind"} {
			if _, ok := first[k]; !ok {
				t.Errorf("source[0] missing %q (got keys: %v)", k, mapKeys(first))
			}
		}
	}
}

// TestPlanQuiet_SuppressesDiagnostics is best-effort: the `default`
// fixture preset has no override-conflict, so the test only verifies
// --quiet does not break the plan output. (Override coverage lives in
// the resolver tests.)
func TestPlanQuiet_DoesNotBreak(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"),
		"source: "+url+"@main\npresets:\n  - default\n")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	if _, _, err := runCLI(t, work, "plan", "--cache", cache, "--quiet"); err != nil {
		t.Fatalf("plan --quiet: %v", err)
	}
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
