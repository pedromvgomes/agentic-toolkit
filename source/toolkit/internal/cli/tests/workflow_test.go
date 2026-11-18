package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const emptyLockfile = "version: 2\nsources: []\n"

func workflowBody(id, title string) string {
	return "id: " + id + "\nname: " + title + "\nversion: 1\nstatus: draft\nsteps: []\n"
}

// workflowRepo lays out an entry manifest extending a local stack, the way a
// consumer repository does, and returns the working directory.
func workflowRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	work := t.TempDir()
	for name, body := range files {
		writeFile(t, filepath.Join(work, name), body)
	}
	writeFile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), emptyLockfile)
	return work
}

func TestWorkflowShowPrintsTheSourceByteForByte(t *testing.T) {
	src := "# a comment that must survive\n" + workflowBody("triage", "Triage")
	work := workflowRepo(t, map[string]string{
		".agentic-toolkit.yaml":             "stacks:\n  - ./stacks/team.yaml\n",
		"stacks/team.yaml":                  "workflows:\n  - triage\n",
		"definitions/workflows/triage.yaml": src,
	})

	stdout, _, err := runCLI(t, work, "workflow", "show", "triage", "--cache", t.TempDir())
	if err != nil {
		t.Fatalf("workflow show: %v", err)
	}
	if stdout != src {
		t.Errorf("stdout = %q, want the source %q", stdout, src)
	}
}

func TestWorkflowShowFindsARepoLocalWorkflowUnderTheRoot(t *testing.T) {
	work := workflowRepo(t, map[string]string{
		".agentic-toolkit.yaml":          "stacks: []\n",
		"agentic/workflows/release.yaml": workflowBody("release", "Release"),
	})

	stdout, _, err := runCLI(t, work, "workflow", "show", "release", "--cache", t.TempDir())
	if err != nil {
		t.Fatalf("workflow show: %v", err)
	}
	if !strings.Contains(stdout, "name: Release") {
		t.Errorf("stdout = %q, want the repo-local definition", stdout)
	}
}

func TestWorkflowShowPrintsTheDefinitionALaterStackLayersOverAnEarlierOne(t *testing.T) {
	work := workflowRepo(t, map[string]string{
		".agentic-toolkit.yaml":             "stacks:\n  - ./stacks/base.yaml\n  - ./stacks/team.yaml\n",
		"stacks/base.yaml":                  "workflows:\n  - triage\n",
		"stacks/team.yaml":                  "workflows:\n  - ./team-triage.yaml\n",
		"definitions/workflows/triage.yaml": workflowBody("triage", "Base triage"),
		"stacks/team-triage.yaml":           workflowBody("triage", "Team triage"),
	})

	stdout, _, err := runCLI(t, work, "workflow", "show", "triage", "--cache", t.TempDir())
	if err != nil {
		t.Fatalf("workflow show: %v", err)
	}
	if !strings.Contains(stdout, "Team triage") || strings.Contains(stdout, "Base triage") {
		t.Errorf("stdout = %q, want the later stack's definition", stdout)
	}
}

func TestWorkflowShowRefusesAnUnknownIDAndNamesTheAvailableOnes(t *testing.T) {
	work := workflowRepo(t, map[string]string{
		".agentic-toolkit.yaml":          "stacks: []\n",
		"agentic/workflows/release.yaml": workflowBody("release", "Release"),
		"agentic/workflows/triage.yaml":  workflowBody("triage", "Triage"),
	})

	stdout, _, err := runCLI(t, work, "workflow", "show", "nope", "--cache", t.TempDir())
	if err == nil {
		t.Fatal("workflow show nope succeeded, want an error")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing on failure", stdout)
	}
	for _, want := range []string{`"nope"`, "release, triage"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
}

func TestWorkflowShowJSONCarriesIDNameSourceAndTheConvertedDefinition(t *testing.T) {
	src := workflowBody("triage", "Triage")
	work := workflowRepo(t, map[string]string{
		".agentic-toolkit.yaml":         "stacks: []\n",
		"agentic/workflows/triage.yaml": src,
	})

	stdout, _, err := runCLI(t, work, "workflow", "show", "triage", "--json", "--cache", t.TempDir())
	if err != nil {
		t.Fatalf("workflow show --json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("parse json: %v\n%s", err, stdout)
	}
	if got["version"] != float64(1) || got["id"] != "triage" || got["name"] != "Triage" || got["source"] != src {
		t.Errorf("json = %v", got)
	}
	def, ok := got["definition"].(map[string]any)
	if !ok {
		t.Fatalf("definition = %#v, want an object", got["definition"])
	}
	if def["id"] != "triage" || def["status"] != "draft" || def["version"] != float64(1) {
		t.Errorf("definition = %v, want the source document's own fields", def)
	}
}

func TestWorkflowShowWritesNothingToTheRepository(t *testing.T) {
	work := workflowRepo(t, map[string]string{
		".agentic-toolkit.yaml":         "stacks: []\n",
		"agentic/workflows/triage.yaml": workflowBody("triage", "Triage"),
	})
	before := listTree(t, work)

	if _, _, err := runCLI(t, work, "workflow", "show", "triage", "--cache", t.TempDir()); err != nil {
		t.Fatalf("workflow show: %v", err)
	}
	if after := listTree(t, work); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("tree changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestRenderWritesNothingForAWorkflow(t *testing.T) {
	work := workflowRepo(t, map[string]string{
		".agentic-toolkit.yaml":         "stacks: []\n",
		"agentic/workflows/triage.yaml": workflowBody("triage", "Triage"),
	})
	if _, _, err := runCLI(t, work, "render", "--cache", t.TempDir()); err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, p := range listTree(t, work) {
		if strings.Contains(p, "triage") && !strings.HasPrefix(p, "agentic/") {
			t.Errorf("render wrote %s for a workflow", p)
		}
	}
}

func TestWorkflowSchemaIsADraft2020JSONSchemaThatShowOutputSatisfies(t *testing.T) {
	stdout, _, err := runCLI(t, t.TempDir(), "workflow", "schema")
	if err != nil {
		t.Fatalf("workflow schema: %v", err)
	}
	var schema struct {
		Dialect    string   `json:"$schema"`
		Type       string   `json:"type"`
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(stdout), &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	if schema.Dialect != "https://json-schema.org/draft/2020-12/schema" || schema.Type != "object" {
		t.Errorf("dialect/type = %q/%q", schema.Dialect, schema.Type)
	}

	work := workflowRepo(t, map[string]string{
		".agentic-toolkit.yaml":         "stacks: []\n",
		"agentic/workflows/triage.yaml": workflowBody("triage", "Triage"),
	})
	shown, _, err := runCLI(t, work, "workflow", "show", "triage", "--json", "--cache", t.TempDir())
	if err != nil {
		t.Fatalf("workflow show --json: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(shown), &out); err != nil {
		t.Fatalf("parse show output: %v", err)
	}

	want := []string{"definition", "id", "name", "source", "version"}
	got := append([]string(nil), schema.Required...)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("required = %v, want %v", got, want)
	}
	for _, key := range schema.Required {
		val, ok := out[key]
		if !ok {
			t.Errorf("show output lacks required key %q", key)
			continue
		}
		if !jsonTypeMatches(schema.Properties[key].Type, val) {
			t.Errorf("%s = %#v, want JSON type %q", key, val, schema.Properties[key].Type)
		}
	}
	for key := range out {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("show output has %q, which the schema does not declare", key)
		}
	}
}

func jsonTypeMatches(typ string, v any) bool {
	switch typ {
	case "string":
		_, ok := v.(string)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	case "object":
		_, ok := v.(map[string]any)
		return ok
	}
	return false
}

func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}
