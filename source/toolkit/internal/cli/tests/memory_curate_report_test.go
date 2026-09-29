package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	if got.Unreadable == nil || !strings.Contains(stdout, `"unreadable": []`) {
		t.Errorf("unreadable is not an empty JSON array:\n%s", stdout)
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

// curateShard is a readable hit shard holding count reads of pins-shas.
func curateShard(count int) string {
	return `{"version": 1, "notes": {"pins-shas": {"count": ` + strconv.Itoa(count) +
		`, "first": "2026-01-01T00:00:00Z", "last": "2026-01-02T00:00:00Z"}}}`
}

// curateCompaction is the part of `memory curate --json` that reports the hit
// shards agtk compacted. UnreadableShards is a pointer so a null decodes
// distinguishably from an empty list.
type curateCompaction struct {
	CompactedShards  int       `json:"compactedShards"`
	UnreadableShards *[]string `json:"unreadableShards"`
}

func curateCompactionJSON(t *testing.T, work string) (curateCompaction, string) {
	t.Helper()
	stdout, _, err := runCLI(t, work, "memory", "curate", "--json")
	if err != nil {
		t.Fatalf("memory curate --json: %v\n%s", err, stdout)
	}
	var got curateCompaction
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("decode: %v\n%s", err, stdout)
	}
	return got, stdout
}

// A verified run folds the hit shards into hits.json, and the report says how
// many it folded, since the run changes files under the store that the
// curator's own account never mentions.
func TestMemoryCurateReportsTheShardsItCompacted(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		work := curateProject(t, curateReport(), false)
		writeFile(t, filepath.Join(work, ".memory/hits/1.json"), curateShard(1))
		writeFile(t, filepath.Join(work, ".memory/hits/2.json"), curateShard(2))

		stdout, _, err := runCLI(t, work, "memory", "curate")
		if err != nil {
			t.Fatalf("memory curate: %v\n%s", err, stdout)
		}
		if !strings.Contains(stdout, "hits: compacted 2 shard(s) into hits.json\n") {
			t.Errorf("the report does not count the compacted shards:\n%s", stdout)
		}
		if strings.Contains(stdout, "unreadable shard") {
			t.Errorf("the report names an unreadable shard where there is none:\n%s", stdout)
		}
		if _, err := os.Stat(filepath.Join(work, ".memory/hits.json")); err != nil {
			t.Errorf("hits.json not written: %v", err)
		}
	})
	t.Run("json", func(t *testing.T) {
		work := curateProject(t, curateReport(), false)
		writeFile(t, filepath.Join(work, ".memory/hits/1.json"), curateShard(1))
		writeFile(t, filepath.Join(work, ".memory/hits/2.json"), curateShard(2))

		got, stdout := curateCompactionJSON(t, work)
		if got.CompactedShards != 2 {
			t.Errorf("compactedShards = %d, want 2\n%s", got.CompactedShards, stdout)
		}
		if got.UnreadableShards == nil || len(*got.UnreadableShards) != 0 {
			t.Errorf("unreadableShards = %v, want an empty list\n%s", got.UnreadableShards, stdout)
		}
	})
}

// A shard that cannot be read is left in place and named, in the text report
// and under `unreadableShards`, while the readable ones are still compacted.
func TestMemoryCurateNamesAnUnreadableShardItLeftInPlace(t *testing.T) {
	bad := filepath.Join(".memory", "hits", "bad.json")
	t.Run("text", func(t *testing.T) {
		work := curateProject(t, curateReport(), false)
		writeFile(t, filepath.Join(work, bad), "{")
		writeFile(t, filepath.Join(work, ".memory/hits/good.json"), curateShard(1))

		stdout, _, err := runCLI(t, work, "memory", "curate")
		if err != nil {
			t.Fatalf("memory curate: %v\n%s", err, stdout)
		}
		if !strings.Contains(stdout, "hits: compacted 1 shard(s) into hits.json\n") {
			t.Errorf("the report does not count the readable shard:\n%s", stdout)
		}
		line := "hits: unreadable shard left in place: " + filepath.Join(work, bad)
		if !strings.Contains(stdout, line) {
			t.Errorf("the report does not say %q:\n%s", line, stdout)
		}
		if _, err := os.Stat(filepath.Join(work, bad)); err != nil {
			t.Errorf("the unreadable shard was not left in place: %v", err)
		}
	})
	t.Run("json", func(t *testing.T) {
		work := curateProject(t, curateReport(), false)
		writeFile(t, filepath.Join(work, bad), "{")
		writeFile(t, filepath.Join(work, ".memory/hits/good.json"), curateShard(1))

		got, stdout := curateCompactionJSON(t, work)
		if got.CompactedShards != 1 {
			t.Errorf("compactedShards = %d, want 1\n%s", got.CompactedShards, stdout)
		}
		if got.UnreadableShards == nil || len(*got.UnreadableShards) != 1 ||
			!strings.Contains((*got.UnreadableShards)[0], filepath.Join(work, bad)) {
			t.Errorf("unreadableShards = %v, want the unreadable shard named\n%s", got.UnreadableShards, stdout)
		}
	})
}

// A store with no shards compacts nothing: no `hits:` line, a count of zero
// and `unreadableShards` an empty list rather than null.
func TestMemoryCurateWithNoShardsReportsNoCompaction(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		work := curateProject(t, curateReport(), false)

		stdout, _, err := runCLI(t, work, "memory", "curate")
		if err != nil {
			t.Fatalf("memory curate: %v\n%s", err, stdout)
		}
		if strings.Contains(stdout, "hits:") {
			t.Errorf("the report has a hits line for a store with no shards:\n%s", stdout)
		}
	})
	t.Run("json", func(t *testing.T) {
		work := curateProject(t, curateReport(), false)

		got, stdout := curateCompactionJSON(t, work)
		if got.CompactedShards != 0 {
			t.Errorf("compactedShards = %d, want 0\n%s", got.CompactedShards, stdout)
		}
		if got.UnreadableShards == nil || !strings.Contains(stdout, `"unreadableShards": []`) {
			t.Errorf("unreadableShards is not an empty JSON array:\n%s", stdout)
		}
	})
}
