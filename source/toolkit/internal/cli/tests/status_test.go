package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatus_FullyClean: config + lockfile + cache + render are all in
// sync. Status reports clean and exits 0.
func TestStatus_FullyClean(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryManifest(t, work, url, "main")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	if _, _, err := runCLI(t, work, "fetch", "--cache", cache); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if _, _, err := runCLI(t, work, "render", "--cache", cache); err != nil {
		t.Fatalf("render: %v", err)
	}

	stdout, _, err := runCLI(t, work, "status", "--cache", cache)
	if err != nil {
		t.Fatalf("status on clean state: %v", err)
	}
	if !strings.Contains(stdout, "ok") {
		t.Errorf("expected 'ok' in clean status output: %q", stdout)
	}
}

// TestStatus_MissingLockfile: bucket 1 surfaces the missing lockfile;
// status exits non-zero.
func TestStatus_MissingLockfile(t *testing.T) {
	url, _ := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryManifest(t, work, url, "main")

	stdout, _, err := runCLI(t, work, "status", "--cache", cache)
	if err == nil {
		t.Fatal("expected non-zero exit when lockfile is missing")
	}
	if !strings.Contains(stdout, "lock") {
		t.Errorf("status output should hint at running `agtk lock`: %q", stdout)
	}
}

// TestStatus_MissingCache: lockfile present but the cache is empty —
// bucket 2 surfaces the missing source.
func TestStatus_MissingCache(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryManifest(t, work, url, "main")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	stdout, _, err := runCLI(t, work, "status", "--cache", cache)
	if err == nil {
		t.Fatal("expected non-zero exit when cache is empty")
	}
	if !strings.Contains(stdout, "agtk fetch") {
		t.Errorf("status output should hint at running `agtk fetch`: %q", stdout)
	}
}

// TestStatus_RenderDrift: cache and lockfile are clean, but a tracked
// rendered file has been modified — bucket 3 picks it up.
func TestStatus_RenderDrift(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryManifest(t, work, url, "main")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)

	if _, _, err := runCLI(t, work, "fetch", "--cache", cache); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if _, _, err := runCLI(t, work, "render", "--cache", cache); err != nil {
		t.Fatalf("render: %v", err)
	}

	// Tamper with a rendered file.
	skill := filepath.Join(work, ".claude/skills/foo/SKILL.md")
	if err := os.WriteFile(skill, []byte("user-edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runCLI(t, work, "status", "--cache", cache)
	if err == nil {
		t.Fatal("expected non-zero exit on render drift")
	}
	if !strings.Contains(stdout, "would update") && !strings.Contains(stdout, "skills/foo/SKILL.md") {
		t.Errorf("status should call out the modified skill: %q", stdout)
	}
}

// TestStatus_SourceStack_ConfigVsLockfileDrift: with --stack there is no
// entry manifest whose `stacks:` could be checked, so the named stack's own
// external sources are — both the ones it extends and the ones its
// per-category entries name. Either pinned to a ref the lockfile does not
// hold is drift.
func TestStatus_SourceStack_ConfigVsLockfileDrift(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")

	cases := []struct {
		name string
		body string
	}{
		{
			name: "extends",
			body: "description: Extends an external stack.\nextends:\n  - " + url + "/stacks/default.yaml@main\n",
		},
		{
			name: "category entry",
			body: "description: Names an external skill.\nskills:\n  - " + url + "/definitions/skills/foo@main\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := t.TempDir()
			if err := copyTree("testdata/primary", source); err != nil {
				t.Fatalf("copy source tree: %v", err)
			}
			writeFile(t, filepath.Join(source, "stacks", "drifted.yaml"), tc.body)

			work := t.TempDir()
			cache := t.TempDir()
			// The lockfile pins the same source at a different ref, so the
			// stack's pin is genuinely unresolvable from it.
			writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "other", sha)

			stdout, _, err := runCLI(t, work, "--source", source, "--stack", "drifted", "status", "--cache", cache, "--json")
			if err == nil {
				t.Fatal("expected non-zero exit when the named stack's source is not pinned")
			}
			var got map[string]any
			if jerr := json.Unmarshal([]byte(stdout), &got); jerr != nil {
				t.Fatalf("parse json: %v\n%s", jerr, stdout)
			}
			drift, ok := got["drift"].(map[string]any)
			if !ok {
				t.Fatalf("drift field missing or wrong type: %v", got["drift"])
			}
			lines, ok := drift["config_vs_lockfile"].([]any)
			if !ok || len(lines) == 0 {
				t.Fatalf("config_vs_lockfile = %v, want the unpinned source; output=%s", drift["config_vs_lockfile"], stdout)
			}
			if first, _ := lines[0].(string); !strings.Contains(first, url) || !strings.Contains(first, "not pinned") {
				t.Errorf("drift line = %q, want the unpinned source %s", first, url)
			}
		})
	}
}

// TestStatus_JSON: the --json output has the expected schema.
func TestStatus_JSON(t *testing.T) {
	url, sha := fixtureRepoFromDir(t, "testdata/primary")
	work := t.TempDir()
	cache := t.TempDir()

	writeEntryManifest(t, work, url, "main")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)
	if _, _, err := runCLI(t, work, "fetch", "--cache", cache); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if _, _, err := runCLI(t, work, "render", "--cache", cache); err != nil {
		t.Fatalf("render: %v", err)
	}

	stdout, _, err := runCLI(t, work, "status", "--cache", cache, "--json")
	if err != nil {
		t.Fatalf("status --json on clean: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("parse json: %v\n%s", err, stdout)
	}
	if got["version"] != float64(1) {
		t.Errorf("version = %v, want 1", got["version"])
	}
	if got["clean"] != true {
		t.Errorf("clean = %v, want true; output=%s", got["clean"], stdout)
	}
	drift, ok := got["drift"].(map[string]any)
	if !ok {
		t.Fatalf("drift field missing or wrong type: %v", got["drift"])
	}
	for _, key := range []string{"config_vs_lockfile", "lockfile_vs_cache", "render"} {
		if _, ok := drift[key]; !ok {
			t.Errorf("drift.%s missing", key)
		}
	}
}
