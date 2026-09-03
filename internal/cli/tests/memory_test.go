package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// memoryProject lays out a minimal consumer repo with one note and returns
// its root. The note is unstamped; callers run `memory anchor` when they
// want it current.
func memoryProject(t *testing.T, manifest string) string {
	t.Helper()
	work := t.TempDir()
	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"), manifest)
	writeFile(t, filepath.Join(work, "internal/resolver/graph.go"), "package resolver\n")
	writeFile(t, filepath.Join(work, "internal/lockfile/types.go"), "package lockfile\n")
	return work
}

const memoryNote = `---
name: pins-shas
kind: invariant
description: Lock resolution pins commit SHAs, never tags.
anchors:
  - path: internal/resolver/graph.go
  - path: internal/lockfile/*.go
confidence: verified
---

See internal/resolver/graph.go:88.
`

// TestMemoryIndexScaffoldsStore: `index` on a repo with no store creates
// the layout, including the gitignore that keeps hit telemetry out of
// commits and the placeholder that keeps candidates/ alive through a clone.
func TestMemoryIndexScaffoldsStore(t *testing.T) {
	work := memoryProject(t, "skills: []\n")

	if _, _, err := runCLI(t, work, "memory", "index"); err != nil {
		t.Fatalf("memory index: %v", err)
	}

	for _, rel := range []string{
		".agents/memory/INDEX.md",
		".agents/memory/notes",
		".agents/memory/candidates/.gitkeep",
		".agents/memory/.gitignore",
	} {
		if _, err := os.Stat(filepath.Join(work, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	ignore := readFile(t, filepath.Join(work, ".agents/memory/.gitignore"))
	if !strings.Contains(ignore, ".hits.jsonl") {
		t.Errorf(".gitignore does not cover the hits log: %q", ignore)
	}
}

// TestMemoryRootOverride honours `memory.root` from the entry manifest.
func TestMemoryRootOverride(t *testing.T) {
	work := memoryProject(t, "skills: []\nmemory:\n  root: docs/memory\n")

	if _, _, err := runCLI(t, work, "memory", "index"); err != nil {
		t.Fatalf("memory index: %v", err)
	}

	if _, err := os.Stat(filepath.Join(work, "docs/memory/INDEX.md")); err != nil {
		t.Errorf("store not created at the configured root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, ".agents")); err == nil {
		t.Error("store also created at the default root")
	}
}

// TestMemoryStoreLivesNextToConfig: with --config pointing into a worktree,
// the store lands beside that manifest rather than in the working
// directory — otherwise notes written on a branch would not travel with it.
func TestMemoryStoreLivesNextToConfig(t *testing.T) {
	bare := t.TempDir()
	worktree := memoryProject(t, "skills: []\n")

	if _, _, err := runCLI(t, bare, "memory", "--config", filepath.Join(worktree, ".agentic-toolkit.yaml"), "index"); err != nil {
		t.Fatalf("memory index: %v", err)
	}

	if _, err := os.Stat(filepath.Join(worktree, ".agents/memory/INDEX.md")); err != nil {
		t.Errorf("store not created next to the manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bare, ".agents")); err == nil {
		t.Error("store created in the working directory instead of the worktree")
	}
}

// TestMemoryLifecycle walks the whole deterministic surface: an unstamped
// note fails lint, `anchor` fixes the anchors, `index` fixes the index,
// then a source edit makes `audit` fail without touching any file.
func TestMemoryLifecycle(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	notePath := filepath.Join(work, ".agents/memory/notes/pins-shas.md")
	writeFile(t, notePath, memoryNote)

	stdout, _, err := runCLI(t, work, "memory", "lint")
	if err == nil {
		t.Fatal("lint should fail on an unstamped note")
	}
	if !strings.Contains(stdout, "unstamped") {
		t.Errorf("lint output should name the problem: %q", stdout)
	}

	if _, _, err := runCLI(t, work, "memory", "anchor"); err != nil {
		t.Fatalf("memory anchor: %v", err)
	}
	if _, _, err := runCLI(t, work, "memory", "index"); err != nil {
		t.Fatalf("memory index: %v", err)
	}
	if stdout, _, err := runCLI(t, work, "memory", "lint"); err != nil {
		t.Fatalf("lint after anchor+index: %v (%s)", err, stdout)
	}
	if stdout, _, err := runCLI(t, work, "memory", "audit"); err != nil {
		t.Fatalf("audit on a clean tree: %v (%s)", err, stdout)
	}

	before := readFile(t, notePath)
	writeFile(t, filepath.Join(work, "internal/resolver/graph.go"), "package resolver // changed\n")

	stdout, _, err = runCLI(t, work, "memory", "audit")
	if err == nil {
		t.Fatal("audit should exit non-zero when a note is stale")
	}
	if !strings.Contains(stdout, "pins-shas") || !strings.Contains(stdout, "graph.go") {
		t.Errorf("audit should name the note and the file: %q", stdout)
	}
	if after := readFile(t, notePath); after != before {
		t.Error("audit rewrote the note")
	}
	if stdout, _, err := runCLI(t, work, "memory", "lint"); err != nil {
		t.Errorf("lint must stay green on a stale store: %v (%s)", err, stdout)
	}
}

// TestMemoryShowRecordsHit: reading through `show` is what feeds the hit
// rate, and --no-hit opts out.
func TestMemoryShowRecordsHit(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)
	if _, _, err := runCLI(t, work, "memory", "anchor"); err != nil {
		t.Fatalf("memory anchor: %v", err)
	}

	stdout, _, err := runCLI(t, work, "memory", "show", "pins-shas")
	if err != nil {
		t.Fatalf("memory show: %v", err)
	}
	if !strings.Contains(stdout, "graph.go:88") || !strings.Contains(stdout, "stale: no") {
		t.Errorf("show output missing body or freshness: %q", stdout)
	}
	if _, _, err := runCLI(t, work, "memory", "show", "pins-shas", "--no-hit"); err != nil {
		t.Fatalf("memory show --no-hit: %v", err)
	}

	stdout, _, err = runCLI(t, work, "memory", "stats", "--json")
	if err != nil {
		t.Fatalf("memory stats: %v", err)
	}
	var stats struct {
		Hits     int     `json:"hits"`
		NotesHit int     `json:"notes_hit"`
		HitRate  float64 `json:"hit_rate"`
	}
	if err := json.Unmarshal([]byte(stdout), &stats); err != nil {
		t.Fatalf("stats json: %v (%s)", err, stdout)
	}
	if stats.Hits != 1 || stats.NotesHit != 1 || stats.HitRate != 1 {
		t.Errorf("stats = %+v, want exactly the one recorded read", stats)
	}
}

// TestMemoryShowUnknownNote fails rather than printing nothing.
func TestMemoryShowUnknownNote(t *testing.T) {
	work := memoryProject(t, "skills: []\n")

	if _, _, err := runCLI(t, work, "memory", "show", "nope"); err == nil {
		t.Fatal("expected an error for an unknown note")
	}
}

// TestMemoryAuditJSON exposes the drift detail an agent re-verifies from.
func TestMemoryAuditJSON(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)
	if _, _, err := runCLI(t, work, "memory", "anchor"); err != nil {
		t.Fatalf("memory anchor: %v", err)
	}
	writeFile(t, filepath.Join(work, "internal/lockfile/parser.go"), "package lockfile\n")

	stdout, _, err := runCLI(t, work, "memory", "audit", "--json")
	if err == nil {
		t.Fatal("expected non-zero exit for a stale store")
	}
	var out struct {
		Notes int `json:"notes"`
		Stale []struct {
			Name   string `json:"name"`
			Drifts []struct {
				Kind    string `json:"kind"`
				Path    string `json:"path"`
				Pattern string `json:"pattern"`
			} `json:"drifts"`
		} `json:"stale"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("audit json: %v (%s)", err, stdout)
	}
	if len(out.Stale) != 1 || len(out.Stale[0].Drifts) != 1 {
		t.Fatalf("want one stale note with one drift, got %+v", out)
	}
	d := out.Stale[0].Drifts[0]
	if d.Kind != "added" || d.Path != "internal/lockfile/parser.go" || d.Pattern != "internal/lockfile/*.go" {
		t.Errorf("drift = %+v, want the new file named against its pattern", d)
	}
}

// TestMemorySourceModeUsesConsumerConfig: in --source mode the store
// belongs to the consumer being applied to. The source tree's `memory:`
// must not decide where another repo commits its notes — the same rule the
// resolver enforces for stacks reached through extends:.
func TestMemorySourceModeUsesConsumerConfig(t *testing.T) {
	consumer := memoryProject(t, "skills: []\nmemory:\n  root: docs/memory\n")
	source := t.TempDir()
	writeFile(t, filepath.Join(source, ".agentic-toolkit.yaml"), "skills: []\nmemory:\n  root: upstream/notes\n")

	if _, _, err := runCLI(t, consumer, "memory", "--source", source, "index"); err != nil {
		t.Fatalf("memory index: %v", err)
	}

	if _, err := os.Stat(filepath.Join(consumer, "docs/memory/INDEX.md")); err != nil {
		t.Errorf("store not created at the consumer's configured root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(consumer, "upstream/notes")); err == nil {
		t.Error("the source tree's memory.root won; a shared source must not relocate the consumer's notes")
	}
}

// TestMemoryWarnsOnUnreadableNote: a note that fails to parse drops out of
// the index, so every command says so rather than silently narrowing the
// store.
func TestMemoryWarnsOnUnreadableNote(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)
	writeFile(t, filepath.Join(work, ".agents/memory/notes/broken.md"), "not a note\n")

	_, stderr, err := runCLI(t, work, "memory", "index")
	if err != nil {
		t.Fatalf("memory index: %v", err)
	}
	if !strings.Contains(stderr, "broken.md") {
		t.Errorf("index did not warn about the unreadable note: %q", stderr)
	}
}

// TestMemoryLintCleanWithoutStore: a consumer that has not adopted memory
// must not go red in CI.
func TestMemoryLintCleanWithoutStore(t *testing.T) {
	work := memoryProject(t, "skills: []\n")

	if stdout, _, err := runCLI(t, work, "memory", "lint"); err != nil {
		t.Errorf("lint without a store: %v (%s)", err, stdout)
	}
}

// TestMemoryAnchorContinuesPastFailure: an unstampable note is reported and
// the run continues, so the rest of the store still gets brought up to date.
func TestMemoryAnchorContinuesPastFailure(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)
	writeFile(t, filepath.Join(work, ".agents/memory/notes/deep.md"),
		strings.NewReplacer(
			"name: pins-shas", "name: deep",
			"  - path: internal/lockfile/*.go", "  - path: internal/**/*.go",
		).Replace(memoryNote))

	_, stderr, err := runCLI(t, work, "memory", "anchor")
	if err == nil {
		t.Fatal("expected a non-zero exit when a note cannot be stamped")
	}
	if !strings.Contains(stderr, "deep") {
		t.Errorf("stderr should name the failing note: %q", stderr)
	}

	// The healthy note, sorted after the failing one, was still stamped.
	if got := readFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md")); !strings.Contains(got, "blob:") {
		t.Error("a failing note prevented the rest of the store from being stamped")
	}
}

// TestMemoryToleratesBrokenManifest: all these commands want from the
// manifest is `memory.root`, and they are advertised as hook- and CI-safe.
// An unrelated `extends:` problem must not turn the memory hook red.
func TestMemoryToleratesBrokenManifest(t *testing.T) {
	work := memoryProject(t, "extends:\n  - \"!!! not a ref !!!\"\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)

	if _, _, err := runCLI(t, work, "memory", "anchor"); err != nil {
		t.Fatalf("anchor with a broken manifest: %v", err)
	}
	if _, _, err := runCLI(t, work, "memory", "index"); err != nil {
		t.Fatalf("index with a broken manifest: %v", err)
	}
	stdout, stderr, err := runCLI(t, work, "memory", "lint")
	if err != nil {
		t.Fatalf("lint with a broken manifest: %v (%s)", err, stdout)
	}
	if !strings.Contains(stderr, "memory.root") {
		t.Errorf("the narrowed read should be announced: %q", stderr)
	}
}

// TestMemoryAuditReportsUnevaluableAnchor: an anchor that cannot be
// evaluated is its own drift kind, and its reason travels in `detail` —
// `was`/`now` stay blob-shaped for consumers.
func TestMemoryAuditReportsUnevaluableAnchor(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/deep.md"),
		strings.NewReplacer(
			"name: pins-shas", "name: deep",
			"  - path: internal/lockfile/*.go", "  - path: internal/**/*.go",
		).Replace(memoryNote))

	stdout, _, err := runCLI(t, work, "memory", "audit", "--json")
	if err == nil {
		t.Fatal("expected a non-zero exit")
	}
	var out struct {
		Stale []struct {
			Drifts []struct {
				Kind   string `json:"kind"`
				Was    string `json:"was"`
				Now    string `json:"now"`
				Detail string `json:"detail"`
			} `json:"drifts"`
		} `json:"stale"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("audit json: %v (%s)", err, stdout)
	}
	if len(out.Stale) != 1 || len(out.Stale[0].Drifts) == 0 {
		t.Fatalf("want one stale note with drift, got %+v", out)
	}
	d := out.Stale[0].Drifts[0]
	if d.Kind != "invalid" || !strings.Contains(d.Detail, "`**` is not supported") {
		t.Errorf("drift = %+v, want an invalid drift explaining the pattern", d)
	}
	if d.Was != "" || d.Now != "" {
		t.Errorf("was/now should stay blob-shaped, got was=%q now=%q", d.Was, d.Now)
	}
}

// TestMemoryRejectsEscapingRoot: a store outside the repo is neither
// committed nor branch-scoped, which is the whole point of the design.
func TestMemoryRejectsEscapingRoot(t *testing.T) {
	for name, root := range map[string]string{
		"absolute": "/tmp/agtk-escape-demo",
		"climbing": "../escaped",
	} {
		t.Run(name, func(t *testing.T) {
			work := memoryProject(t, "skills: []\nmemory:\n  root: "+root+"\n")

			if _, _, err := runCLI(t, work, "memory", "index"); err == nil {
				t.Fatalf("scaffolded a store at %q", root)
			}
		})
	}
}

// TestMemoryLintCatchesMisconfiguredRoot: a typo in memory.root must fail,
// not report a clean store that nobody is looking at.
func TestMemoryLintCatchesMisconfiguredRoot(t *testing.T) {
	work := memoryProject(t, "skills: []\nmemory:\n  root: docs/memry\n")
	writeFile(t, filepath.Join(work, "docs/memory/notes/pins-shas.md"), memoryNote)

	stdout, _, err := runCLI(t, work, "memory", "lint")
	if err == nil {
		t.Fatalf("lint passed for a configured root that does not exist: %s", stdout)
	}
	if !strings.Contains(stdout, "configured memory root") {
		t.Errorf("lint output should point at memory.root: %q", stdout)
	}
}

// TestMemoryShowScaffoldsGitignore: reading from a hand-created store must
// not leave the hits log exposed to `git add -A`.
func TestMemoryShowScaffoldsGitignore(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)

	if _, _, err := runCLI(t, work, "memory", "show", "pins-shas"); err != nil {
		t.Fatalf("memory show: %v", err)
	}

	ignore := readFile(t, filepath.Join(work, ".agents/memory/.gitignore"))
	if !strings.Contains(ignore, ".hits.jsonl") {
		t.Errorf(".gitignore = %q, want it to cover the hits log", ignore)
	}
}

// TestMemoryKeepsConfiguredRootWhenManifestIsBroken: tolerating an
// unrelated manifest error must not silently move the store. Falling back
// to the default root would make lint green over an empty directory while
// the real store drifts — worse than the error being tolerated.
func TestMemoryKeepsConfiguredRootWhenManifestIsBroken(t *testing.T) {
	work := memoryProject(t, "memory:\n  root: docs/memory\nextends:\n  - \"!!! not a ref !!!\"\n")
	writeFile(t, filepath.Join(work, "docs/memory/notes/pins-shas.md"), memoryNote)

	stdout, _, err := runCLI(t, work, "memory", "lint")
	if err == nil {
		t.Fatalf("lint passed over the wrong store: %s", stdout)
	}
	if !strings.Contains(stdout, "unstamped") {
		t.Errorf("lint should have read the configured store: %q", stdout)
	}
	if _, statErr := os.Stat(filepath.Join(work, ".agents")); statErr == nil {
		t.Error("a second store was created at the default root")
	}
}

// TestMemoryAnchorReportsNothingStampedOnStdout: with every note failing,
// stdout must not read as success — a hook log often shows only stdout.
func TestMemoryAnchorReportsNothingStampedOnStdout(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/deep.md"),
		strings.NewReplacer(
			"name: pins-shas", "name: deep",
			"  - path: internal/lockfile/*.go", "  - path: internal/**/*.go",
		).Replace(memoryNote))

	stdout, _, err := runCLI(t, work, "memory", "anchor")
	if err == nil {
		t.Fatal("expected a non-zero exit")
	}
	if strings.Contains(stdout, "already current") {
		t.Errorf("stdout claims success after a total failure: %q", stdout)
	}
}

// TestMemoryRefusesOnMalformedManifest: a manifest whose YAML does not
// parse leaves the store's location genuinely unknown. Falling back to the
// default would recreate the bug the narrowed read exists to fix — quietly
// operating on the wrong store.
func TestMemoryRefusesOnMalformedManifest(t *testing.T) {
	work := memoryProject(t, "memory:\n  root: docs/memory\nskills: [a, b\n")
	writeFile(t, filepath.Join(work, "docs/memory/notes/pins-shas.md"), memoryNote)

	if _, _, err := runCLI(t, work, "memory", "lint"); err == nil {
		t.Fatal("lint passed with an unparseable manifest")
	}
	if _, _, err := runCLI(t, work, "memory", "index"); err == nil {
		t.Fatal("index ran with an unparseable manifest")
	}
	if _, statErr := os.Stat(filepath.Join(work, ".agents")); statErr == nil {
		t.Error("a second store was created at the default root")
	}
}

// TestMemoryAnchorOnEmptyStore: a store with no notes yet is healthy, and
// must not report like a failed run.
func TestMemoryAnchorOnEmptyStore(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	if _, _, err := runCLI(t, work, "memory", "index"); err != nil {
		t.Fatalf("memory index: %v", err)
	}

	stdout, _, err := runCLI(t, work, "memory", "anchor")
	if err != nil {
		t.Fatalf("anchor on an empty store: %v", err)
	}
	if strings.Contains(stdout, "could not be stamped") {
		t.Errorf("an empty store reported as a failure: %q", stdout)
	}
}

// TestMemoryShowJSON: the JSON shape is what an explorer agent consumes, so
// it is asserted field by field rather than just parsed.
func TestMemoryShowJSON(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)
	if _, _, err := runCLI(t, work, "memory", "anchor"); err != nil {
		t.Fatalf("memory anchor: %v", err)
	}

	stdout, _, err := runCLI(t, work, "memory", "show", "pins-shas", "--json")
	if err != nil {
		t.Fatalf("memory show --json: %v", err)
	}
	var out struct {
		Name        string   `json:"name"`
		Kind        string   `json:"kind"`
		Confidence  string   `json:"confidence"`
		Description string   `json:"description"`
		Stale       bool     `json:"stale"`
		Anchors     []string `json:"anchors"`
		Body        string   `json:"body"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("show json: %v (%s)", err, stdout)
	}
	if out.Name != "pins-shas" || out.Kind != "invariant" || out.Confidence != "verified" {
		t.Errorf("frontmatter fields wrong: %+v", out)
	}
	if out.Stale {
		t.Error("freshly anchored note reported stale")
	}
	// Anchors carry the authored pattern, not its expansion, so an agent can
	// match them against the paths it is already working on.
	if len(out.Anchors) != 2 || out.Anchors[1] != "internal/lockfile/*.go" {
		t.Errorf("anchors = %v, want the authored paths", out.Anchors)
	}
	if !strings.Contains(out.Body, "graph.go:88") {
		t.Errorf("body missing its pointer: %q", out.Body)
	}
}

// TestMemoryAnchorSelectsNamedNotes: `anchor <name>` must stamp only what it
// was asked for, and reject a name that is not in the store rather than
// silently stamping nothing.
func TestMemoryAnchorSelectsNamedNotes(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)
	writeFile(t, filepath.Join(work, ".agents/memory/notes/other.md"),
		strings.Replace(memoryNote, "name: pins-shas", "name: other", 1))

	if _, _, err := runCLI(t, work, "memory", "anchor", "pins-shas"); err != nil {
		t.Fatalf("memory anchor pins-shas: %v", err)
	}
	if got := readFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md")); !strings.Contains(got, "blob:") {
		t.Error("the named note was not stamped")
	}
	if got := readFile(t, filepath.Join(work, ".agents/memory/notes/other.md")); strings.Contains(got, "blob:") {
		t.Error("an unnamed note was stamped too")
	}

	if _, _, err := runCLI(t, work, "memory", "anchor", "no-such-note"); err == nil {
		t.Error("expected an error for a name that is not in the store")
	}
}

// TestMemoryStatsText covers the human-readable report, including the hit
// line that only appears once something has been read.
func TestMemoryStatsText(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)
	if _, _, err := runCLI(t, work, "memory", "anchor"); err != nil {
		t.Fatalf("memory anchor: %v", err)
	}

	stdout, _, err := runCLI(t, work, "memory", "stats")
	if err != nil {
		t.Fatalf("memory stats: %v", err)
	}
	for _, want := range []string{"notes:", "invariant:", "verified:", "anchors:", "stale:", "candidates:", "none recorded"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stats output missing %q:\n%s", want, stdout)
		}
	}

	if _, _, err := runCLI(t, work, "memory", "show", "pins-shas"); err != nil {
		t.Fatalf("memory show: %v", err)
	}
	stdout, _, err = runCLI(t, work, "memory", "stats")
	if err != nil {
		t.Fatalf("memory stats: %v", err)
	}
	if !strings.Contains(stdout, "hit rate") || !strings.Contains(stdout, "window:") {
		t.Errorf("stats did not report the recorded read:\n%s", stdout)
	}
}

// TestMemoryAuditTextNamesEveryDrift: the text report is what a human reads
// in a hook log, and each drift kind renders differently.
func TestMemoryAuditTextNamesEveryDrift(t *testing.T) {
	work := memoryProject(t, "skills: []\n")
	writeFile(t, filepath.Join(work, ".agents/memory/notes/pins-shas.md"), memoryNote)
	if _, _, err := runCLI(t, work, "memory", "anchor"); err != nil {
		t.Fatalf("memory anchor: %v", err)
	}

	writeFile(t, filepath.Join(work, "internal/resolver/graph.go"), "package resolver // changed\n")
	writeFile(t, filepath.Join(work, "internal/lockfile/added.go"), "package lockfile\n")
	if err := os.Remove(filepath.Join(work, "internal/lockfile/types.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	stdout, _, err := runCLI(t, work, "memory", "audit")
	if err == nil {
		t.Fatal("expected a non-zero exit for a stale store")
	}
	for _, want := range []string{
		"stale (1 of 1)",
		"added, matches internal/lockfile/*.go",
		"removed, matched internal/lockfile/*.go",
		"->",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("audit text missing %q:\n%s", want, stdout)
		}
	}
}
