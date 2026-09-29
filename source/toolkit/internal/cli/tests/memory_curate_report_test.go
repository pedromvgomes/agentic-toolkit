package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-driver/agentictest"
)

const curateCandidateID = "20260901-pins-shas"

// curateReport is a successful turn's completion report resolving the given
// candidate ids and nothing else.
func curateReport(resolved ...string) string {
	quoted := make([]string, len(resolved))
	for i, id := range resolved {
		quoted[i] = `"` + id + `"`
	}
	return `{"type":"result","subtype":"success","is_error":false,"session_id":"s1","result":"Rejected: re-derivable",` +
		`"structured_output":{"candidatesResolved":[` + strings.Join(quoted, ",") +
		`],"notesRetracted":[],"notesTouched":[]}}`
}

// curateProject is a repo configured for the claudecode provider whose store
// holds one staged candidate when staged is set, and whose PATH resolves
// `claude` to a fake answering with stdout.
func curateProject(t *testing.T, stdout string, staged bool) string {
	t.Helper()
	work := memoryProject(t, "memory:\n  agent: claudecode\nstacks: []\n")
	if _, _, err := runCLI(t, work, "memory", "index"); err != nil {
		t.Fatalf("memory index: %v", err)
	}
	if staged {
		writeFile(t, filepath.Join(work, ".memory/candidates", curateCandidateID+".md"),
			"---\nabout: lock resolution pins SHAs\nsaw:\n  - internal/resolver/graph.go\n---\n\nSee internal/resolver/graph.go.\n")
	}

	fake := (&agentictest.Fake{Stdout: stdout}).Build(t)
	bin := t.TempDir()
	if err := os.Symlink(fake.Path(), filepath.Join(bin, "claude")); err != nil {
		t.Fatalf("link the fake as claude: %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return work
}

// A run that cleared nothing reports `cleared` as an empty array, so a script
// iterates it without a nil check.
func TestMemoryCurateJSONReportsAnEmptyClearedListAsAnArray(t *testing.T) {
	work := curateProject(t, curateReport(), false)

	stdout, _, err := runCLI(t, work, "memory", "curate", "--json")
	if err != nil {
		t.Fatalf("memory curate --json: %v\n%s", err, stdout)
	}
	var got struct {
		Cleared    []string `json:"cleared"`
		Unreadable []string `json:"unreadable"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if got.Cleared == nil || len(got.Cleared) != 0 {
		t.Errorf("cleared = %#v, want a non-nil empty list\n%s", got.Cleared, stdout)
	}
	if !strings.Contains(stdout, `"cleared": []`) {
		t.Errorf("cleared is not an empty JSON array:\n%s", stdout)
	}
}

// A candidate the report resolved and the run left staged is removed by agtk,
// and the JSON report names it under `cleared`.
func TestMemoryCurateJSONListsTheCandidatesItCleared(t *testing.T) {
	work := curateProject(t, curateReport(curateCandidateID), true)

	stdout, _, err := runCLI(t, work, "memory", "curate", "--json")
	if err != nil {
		t.Fatalf("memory curate --json: %v\n%s", err, stdout)
	}
	var got struct {
		Cleared []string `json:"cleared"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	if !slices.Equal(got.Cleared, []string{curateCandidateID}) {
		t.Errorf("cleared = %v, want the one candidate the report resolved\n%s", got.Cleared, stdout)
	}
	if _, err := os.Stat(filepath.Join(work, ".memory/candidates", curateCandidateID+".md")); !os.IsNotExist(err) {
		t.Errorf("the resolved candidate is still in candidates/ (stat error: %v)", err)
	}
}

// The text report names each candidate agtk cleared, one per line, so a file
// vanishing from candidates/ has an account of who removed it.
func TestMemoryCurateTextNamesTheCandidatesItCleared(t *testing.T) {
	work := curateProject(t, curateReport(curateCandidateID), true)

	stdout, _, err := runCLI(t, work, "memory", "curate")
	if err != nil {
		t.Fatalf("memory curate: %v\n%s", err, stdout)
	}
	want := "cleared: " + curateCandidateID + " (resolved, left in candidates/ by the run)"
	if !strings.Contains(stdout, want) {
		t.Errorf("the report does not say %q:\n%s", want, stdout)
	}
}

// A run that cleared nothing prints no `cleared:` line.
func TestMemoryCurateTextOmitsClearedWhenNothingWasCleared(t *testing.T) {
	work := curateProject(t, curateReport(), false)

	stdout, _, err := runCLI(t, work, "memory", "curate")
	if err != nil {
		t.Fatalf("memory curate: %v\n%s", err, stdout)
	}
	if strings.Contains(stdout, "cleared:") {
		t.Errorf("the report names a cleared candidate for a run that cleared none:\n%s", stdout)
	}
}
