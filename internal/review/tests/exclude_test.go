package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// withExclude returns the complete manifest carrying an exclude block.
func withExclude(body string) string {
	return complete + "\nexclude:\n" + body
}

// The manifest's globs exclude what only the repo can recognise: source a
// person wrote that no reviewer should spend its budget on.
func TestTheManifestsGlobsExcludeAHandWrittenPath(t *testing.T) {
	m := mustParse(t, withExclude("  - \"**/testdata/**\"\n  - \"src/snapshot_*.go\"\n"))

	for _, tc := range []struct {
		path string
		want review.Exclusion
	}{
		{"src/snapshot_data.go", review.ExcludedByManifest},
		{"pkg/testdata/big.json", review.ExcludedByManifest},
		{"src/a.go", review.NotExcluded},
		// The glob is anchored the way it is written: a pattern naming one
		// directory does not match a same-named directory somewhere else.
		{"other/src/snapshot_data.go", review.NotExcluded},
	} {
		got := review.Classify(review.DiffFile{Path: tc.path}, nil, m.Exclude)
		if got != tc.want {
			t.Errorf("%s classified %q, want %q", tc.path, got, tc.want)
		}
	}
}

// A repo's own statement is reported ahead of a mechanical one. Both exclude
// the file; only one of the two reasons points at a line somebody can edit.
func TestTheManifestsReasonIsReportedOverAMechanicalOne(t *testing.T) {
	m := mustParse(t, withExclude("  - \"go.sum\"\n"))

	got := review.Classify(review.DiffFile{Path: "go.sum"}, nil, m.Exclude)
	if got != review.ExcludedByManifest {
		t.Errorf("go.sum classified %q, want the manifest's reason", got)
	}
}

// The built-in exclusions stand without any manifest entry, so a repo never
// has to list a lockfile or a vendored tree to get them skipped.
func TestTheBuiltinExclusionsNeedNoManifestEntry(t *testing.T) {
	m := mustParse(t, complete)

	for _, tc := range []struct {
		path string
		want review.Exclusion
	}{
		{"go.sum", review.ExcludedLockfile},
		{"vendor/dep/d.go", review.ExcludedVendored},
		{"api/v1.pb.go", review.ExcludedGenerated},
	} {
		if got := review.Classify(review.DiffFile{Path: tc.path}, nil, m.Exclude); got != tc.want {
			t.Errorf("%s classified %q, want %q", tc.path, got, tc.want)
		}
	}
}

// A pattern that can never match is refused rather than kept: the repo
// believes a path is excluded and every review reads it.
func TestARootedExclusionPatternIsRefused(t *testing.T) {
	err := refuse(t, withExclude("  - \"/src/gen.go\"\n"))

	if !review.IsKind(err, review.ErrUnknownName) {
		t.Fatalf("kind = %v, want unknown_name", err)
	}
	// The message carries the pattern that would work, because the fix is not
	// obvious from the rule alone.
	if !strings.Contains(err.Error(), `"src/gen.go"`) {
		t.Errorf("error = %q, want it to suggest the unrooted pattern", err)
	}
}

// Excluding everything empties the review, and an empty review reads exactly
// like a clean one — which is what unblocks approval.
func TestAnExclusionMatchingEveryFileIsRefused(t *testing.T) {
	for _, pattern := range []string{"**", "*"} {
		err := refuse(t, withExclude("  - \""+pattern+"\"\n"))
		if !review.IsKind(err, review.ErrUnknownName) {
			t.Errorf("%q: kind = %v, want unknown_name", pattern, err)
		}
	}
}

func TestAnEmptyExclusionIsRefused(t *testing.T) {
	err := refuse(t, withExclude("  - \"\"\n"))

	if !review.IsKind(err, review.ErrMissingRequired) {
		t.Fatalf("kind = %v, want missing_required", err)
	}
}
