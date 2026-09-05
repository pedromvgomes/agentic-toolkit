package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// TestRenderIndexCarriesRoutingFieldsOnly: the index is loaded on every
// session, so it must carry what routing needs and nothing that churns —
// in particular no blobs and no staleness.
func TestRenderIndexCarriesRoutingFieldsOnly(t *testing.T) {
	s := stampedStore(t)
	notes, _ := s.LoadNotes()

	out := string(memory.RenderIndex(notes))
	for _, want := range []string{
		"pins-shas", "invariant", "verified",
		"Lock resolution pins commit SHAs, never tags.",
		"internal/resolver/graph.go", "internal/lockfile/*.go",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("index missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, notes[0].Anchors[0].Blob) {
		t.Error("index leaked a blob hash; it would then churn on every source edit")
	}
	// The glob is listed as the pattern, not as its expansion.
	if strings.Contains(out, "internal/lockfile/parser.go") {
		t.Error("index listed a glob's expansion rather than the pattern")
	}
}

// TestWriteIndexIsIdempotent: regenerating an up-to-date index leaves the
// file alone, so `agtk memory index` in a hook produces no diff.
func TestWriteIndexIsIdempotent(t *testing.T) {
	s := stampedStore(t)
	notes, _ := s.LoadNotes()

	if changed, err := s.WriteIndex(notes); err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v, want changed", changed, err)
	}
	if changed, err := s.WriteIndex(notes); err != nil || changed {
		t.Fatalf("second write: changed=%v err=%v, want unchanged", changed, err)
	}
}

// TestLintCleanStore passes only once the index has been generated.
func TestLintCleanStore(t *testing.T) {
	s := stampedStore(t)
	notes, parseErrs := s.LoadNotes()
	if _, err := s.WriteIndex(notes); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if issues := s.Lint(notes, parseErrs); len(issues) != 0 {
		t.Errorf("clean store reported issues: %+v", issues)
	}
}

// TestLintFailsOnStaleIndex is the CI guard that keeps the generated file
// in lockstep with the notes.
func TestLintFailsOnStaleIndex(t *testing.T) {
	s := stampedStore(t)
	notes, parseErrs := s.LoadNotes()

	issues := s.Lint(notes, parseErrs)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "index is out of date") {
		t.Fatalf("issues = %+v, want one stale-index issue", issues)
	}
}

// TestLintStructuralProblems: each malformed note produces a message that
// names what to fix.
func TestLintStructuralProblems(t *testing.T) {
	cases := map[string]struct {
		file string
		want string
		// tree is written into the project before linting, so cases can
		// distinguish "never stamped" from "the file is gone".
		tree map[string]string
	}{
		"name mismatch": {
			file: note("other-name", "  - path: a.go\n    blob: 0123456789ab\n"),
			want: "does not match filename stem",
		},
		"bad kind": {
			file: strings.Replace(note("subject", "  - path: a.go\n    blob: 0123456789ab\n"),
				"kind: invariant", "kind: musing", 1),
			want: "kind \"musing\" is not one of",
		},
		"bad confidence": {
			file: strings.Replace(note("subject", "  - path: a.go\n    blob: 0123456789ab\n"),
				"confidence: verified", "confidence: probably", 1),
			want: "confidence \"probably\" is not one of",
		},
		"no description": {
			file: strings.Replace(note("subject", "  - path: a.go\n    blob: 0123456789ab\n"),
				"description: Lock resolution pins commit SHAs, never tags.", "description: \"\"", 1),
			want: "missing `description`",
		},
		"no anchors": {
			file: "---\nname: subject\nkind: gotcha\ndescription: d\nanchors: []\nconfidence: suspect\n---\n\nbody\n",
			want: "every claim must carry a pointer",
		},
		"unstamped anchor": {
			file: note("subject", "  - path: a.go\n"),
			tree: map[string]string{"a.go": "package a\n"},
			want: "unstamped — run `agtk memory anchor subject`",
		},
		"anchored file is gone": {
			file: note("subject", "  - path: a.go\n"),
			want: "anchored file no longer exists",
		},
		"glob matches nothing": {
			file: note("subject", "  - path: internal/*.go\n"),
			want: "matches no files",
		},
		"doublestar glob": {
			file: note("subject", "  - path: internal/**/*.go\n"),
			want: "`**` is not supported",
		},
		"blob on a glob": {
			file: note("subject", "  - path: internal/*.go\n    blob: 0123456789ab\n"),
			want: "glob anchors carry `matches`",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := project(t, tc.tree)
			writeNote(t, s, "subject", tc.file)
			notes, parseErrs := s.LoadNotes()
			if _, err := s.WriteIndex(notes); err != nil {
				t.Fatalf("write index: %v", err)
			}

			issues := s.Lint(notes, parseErrs)
			if !containsMessage(issues, tc.want) {
				t.Errorf("issues = %+v, want one containing %q", issues, tc.want)
			}
		})
	}
}

// TestLintReportsParseErrors: a malformed note is reported, and the rest of
// the store still lints.
func TestLintReportsParseErrors(t *testing.T) {
	s := stampedStore(t)
	writeNote(t, s, "broken", "no frontmatter here\n")

	notes, parseErrs := s.LoadNotes()
	if len(notes) != 1 {
		t.Errorf("a malformed note hid the rest of the store: %d notes loaded", len(notes))
	}
	if !containsMessage(s.Lint(notes, parseErrs), "frontmatter") {
		t.Error("parse error not surfaced by lint")
	}
}

// TestLintFlagsDuplicateNames: two files claiming one name make the index
// ambiguous and `show` non-deterministic.
func TestLintFlagsDuplicateNames(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "subject", note("subject", "  - path: a.go\n    blob: 0123456789ab\n"))
	writeNote(t, s, "subject-copy", note("subject", "  - path: a.go\n    blob: 0123456789ab\n"))

	notes, parseErrs := s.LoadNotes()
	if !containsMessage(s.Lint(notes, parseErrs), "duplicate name") {
		t.Error("duplicate names not reported")
	}
}

func containsMessage(issues []memory.Issue, want string) bool {
	for _, i := range issues {
		if strings.Contains(i.Message, want) {
			return true
		}
	}
	return false
}

// TestLintPassesWithoutAStore: a repo that has not adopted memory is not a
// broken store, so a hook or CI job running lint there must stay green.
func TestLintPassesWithoutAStore(t *testing.T) {
	s := memory.New(t.TempDir(), "")

	notes, parseErrs := s.LoadNotes()
	if issues := s.Lint(notes, parseErrs); len(issues) != 0 {
		t.Errorf("lint on a repo with no store reported %+v", issues)
	}
}

// TestLintFlagsNotesInSubdirectories: LoadNotes does not walk, so a note
// parked in a subdirectory would be invisible to the index, audit and show.
func TestLintFlagsNotesInSubdirectories(t *testing.T) {
	s := stampedStore(t)
	write(t, filepath.Join(s.NotesPath(), "auth", "buried.md"), note("buried", "  - path: a.go\n    blob: 0123456789ab\n"))
	notes, parseErrs := s.LoadNotes()
	if _, err := s.WriteIndex(notes); err != nil {
		t.Fatalf("write index: %v", err)
	}

	if !containsMessage(s.Lint(notes, parseErrs), "notes/ is flat") {
		t.Error("a note in a subdirectory was not flagged")
	}
}

// TestLintDoesNotStackDuplicatesOnUnnamedNotes: unnamed notes all key on
// "", which would otherwise report every one after the first as a
// duplicate on top of the real "missing name" issue.
func TestLintDoesNotStackDuplicatesOnUnnamedNotes(t *testing.T) {
	s := project(t, nil)
	unnamed := "---\nkind: gotcha\ndescription: d\nanchors:\n  - path: a.go\n    blob: 0123456789ab\nconfidence: suspect\n---\n\nbody\n"
	writeNote(t, s, "one", unnamed)
	writeNote(t, s, "two", unnamed)
	notes, parseErrs := s.LoadNotes()
	if _, err := s.WriteIndex(notes); err != nil {
		t.Fatalf("write index: %v", err)
	}

	issues := s.Lint(notes, parseErrs)
	if containsMessage(issues, "duplicate name") {
		t.Errorf("unnamed notes reported as duplicates: %+v", issues)
	}
	if !containsMessage(issues, "missing `name`") {
		t.Errorf("missing name not reported: %+v", issues)
	}
}

// TestLintNamesDirectoryAnchors: `anchor` skips directories, so the hint
// must not prescribe it — that loop can never go green.
func TestLintNamesDirectoryAnchors(t *testing.T) {
	s := project(t, map[string]string{"internal/resolver/graph.go": "package resolver\n"})
	writeNote(t, s, "dir", note("dir", "  - path: internal/resolver\n"))
	notes, parseErrs := s.LoadNotes()
	if _, err := s.WriteIndex(notes); err != nil {
		t.Fatalf("write index: %v", err)
	}

	issues := s.Lint(notes, parseErrs)
	if !containsMessage(issues, "anchors a directory") {
		t.Errorf("issues = %+v, want the directory called out", issues)
	}
	if containsMessage(issues, "run `agtk memory anchor") {
		t.Error("hint prescribes a command that cannot fix a directory anchor")
	}
}

// TestLintFailsOnConfiguredRootThatIsMissing: a typo in memory.root must
// not read as "this repo has not adopted memory".
func TestLintFailsOnConfiguredRootThatIsMissing(t *testing.T) {
	s := memory.New(t.TempDir(), "docs/memry")

	notes, parseErrs := s.LoadNotes()
	if !containsMessage(s.Lint(notes, parseErrs), "configured memory root does not exist") {
		t.Error("a configured but absent store linted clean")
	}
}

// TestValidateRootRejectsEscapes: the store is committed and travels with
// its branch; an absolute or climbing root defeats both.
func TestValidateRootRejectsEscapes(t *testing.T) {
	for _, root := range []string{"/tmp/elsewhere", "../escaped", "docs/../../up"} {
		if err := memory.ValidateRoot(root); err == nil {
			t.Errorf("ValidateRoot(%q) = nil, want an error", root)
		}
	}
	for _, root := range []string{"", "docs/memory", ".agents/memory"} {
		if err := memory.ValidateRoot(root); err != nil {
			t.Errorf("ValidateRoot(%q) = %v, want nil", root, err)
		}
	}
}

// TestLintGlobHintMatchesStampBehaviour: stamping skips directories, so a
// pattern matching only directories must not be told to run `anchor`.
func TestLintGlobHintMatchesStampBehaviour(t *testing.T) {
	s := project(t, map[string]string{"pkg/sub/deep.go": "package sub\n"})
	writeNote(t, s, "dirs", note("dirs", "  - path: pkg/*\n"))
	notes, parseErrs := s.LoadNotes()
	if _, err := s.WriteIndex(notes); err != nil {
		t.Fatalf("write index: %v", err)
	}

	issues := s.Lint(notes, parseErrs)
	if !containsMessage(issues, "matches no files") {
		t.Errorf("issues = %+v, want the pattern called out", issues)
	}
	if containsMessage(issues, "run `agtk memory anchor") {
		t.Error("hint prescribes a command that would change nothing")
	}
}
