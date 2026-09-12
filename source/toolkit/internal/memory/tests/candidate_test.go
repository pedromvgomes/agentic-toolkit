package tests

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// writeCandidate drops a candidate into the store and returns its path.
func writeCandidate(t *testing.T, s *memory.Store, name, content string) string {
	t.Helper()
	path := filepath.Join(s.CandidatesPath(), name+memory.NoteExt)
	write(t, path, content)
	return path
}

// candidate is the canonical well-formed candidate: a re-check of an existing
// note, which is the shape carrying every optional field.
const candidate = `---
about: nothing in CI regenerates the schema docs
saw:
  - .github/workflows/*.yml
  - Makefile
targets: generated-schema-docs-have-no-ci-guard
verdict: still-true
---

grep over the workflows returns nothing, so the claim holds.
`

func TestACandidateCarriesTheExplorersCheckWithoutHashingAnything(t *testing.T) {
	s := project(t, nil)
	writeCandidate(t, s, "20260905-schema-docs", candidate)

	got, errs := s.LoadCandidates()
	if len(errs) > 0 {
		t.Fatalf("LoadCandidates: %v", errs)
	}
	if len(got) != 1 {
		t.Fatalf("loaded %d candidates, want 1", len(got))
	}

	c := got[0]
	if c.About != "nothing in CI regenerates the schema docs" {
		t.Errorf("About = %q", c.About)
	}
	if len(c.Saw) != 2 || c.Saw[0] != ".github/workflows/*.yml" {
		t.Errorf("Saw = %v, want the paths as the explorer wrote them", c.Saw)
	}
	if c.Targets != "generated-schema-docs-have-no-ci-guard" {
		t.Errorf("Targets = %q", c.Targets)
	}
	if c.Verdict != memory.VerdictStillTrue {
		t.Errorf("Verdict = %q", c.Verdict)
	}
	if !strings.Contains(c.Body, "the claim holds") {
		t.Errorf("Body = %q, want the evidence kept", c.Body)
	}
	if c.Stem() != "20260905-schema-docs" {
		t.Errorf("Stem = %q", c.Stem())
	}
}

// An explorer computes no hashes and rules on no confidence, so a candidate
// carrying either was written by something that misunderstood its job — and
// strict decoding is what makes that loud instead of silent.
func TestACandidateCarryingAnchorsOrConfidenceIsRefused(t *testing.T) {
	for name, frontmatter := range map[string]string{
		"anchors":    "about: a\nanchors:\n  - path: x.go\n",
		"blob":       "about: a\nblob: a3f9c2189d41\n",
		"confidence": "about: a\nconfidence: verified\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := memory.ParseCandidate("c.md", []byte("---\n"+frontmatter+"---\n\nbody\n"))
			if err == nil {
				t.Fatalf("ParseCandidate accepted %s", name)
			}
		})
	}
}

// A candidate that lost its `targets:` reads as a new finding rather than a
// re-check, so a typo'd key must not be dropped in silence.
func TestAnUnknownCandidateKeyIsRefusedRatherThanDropped(t *testing.T) {
	_, err := memory.ParseCandidate("c.md", []byte("---\nabout: a\ntarget: n\n---\n\nbody\n"))
	if err == nil {
		t.Fatal("ParseCandidate accepted an unknown key")
	}
}

// A candidate is the one input to curation nothing else checks: a malformed
// one becomes either a bad note or a silently dropped finding.
func TestStructuralProblemsInACandidateAreReported(t *testing.T) {
	for name, tc := range map[string]struct {
		frontmatter string
		body        string
		want        string
	}{
		"no about":            {"saw:\n  - x.go\n", "body", "`about:`"},
		"no body":             {"about: a\n", "\n  \n", "no body"},
		"verdict alone":       {"about: a\nverdict: still-true\n", "body", "without `targets:`"},
		"targets alone":       {"about: a\ntargets: n\n", "body", "without `verdict:`"},
		"verdict off the set": {"about: a\ntargets: n\nverdict: maybe\n", "body", "not one of"},
	} {
		t.Run(name, func(t *testing.T) {
			c, err := memory.ParseCandidate("c.md", []byte("---\n"+tc.frontmatter+"---\n"+tc.body))
			if err != nil {
				t.Fatalf("ParseCandidate: %v", err)
			}
			issues := strings.Join(c.CandidateIssues(), "; ")
			if !strings.Contains(issues, tc.want) {
				t.Errorf("issues = %q, want one mentioning %q", issues, tc.want)
			}
		})
	}
}

func TestAWellFormedCandidateHasNoIssues(t *testing.T) {
	c, err := memory.ParseCandidate("c.md", []byte(candidate))
	if err != nil {
		t.Fatalf("ParseCandidate: %v", err)
	}
	if issues := c.CandidateIssues(); len(issues) > 0 {
		t.Errorf("issues = %v, want none", issues)
	}
}

// A new finding names no note, so it carries no verdict either — that is the
// ordinary shape, not an omission.
func TestANewFindingNeedsNoTargetOrVerdict(t *testing.T) {
	c, err := memory.ParseCandidate("c.md", []byte("---\nabout: a\nsaw:\n  - x.go\n---\n\nevidence\n"))
	if err != nil {
		t.Fatalf("ParseCandidate: %v", err)
	}
	if issues := c.CandidateIssues(); len(issues) > 0 {
		t.Errorf("issues = %v, want none", issues)
	}
}

// One malformed candidate must not hide the rest of the backlog from a
// curator, the same way one malformed note does not hide the store.
func TestOneUnreadableCandidateDoesNotHideTheOthers(t *testing.T) {
	s := project(t, nil)
	writeCandidate(t, s, "20260905-good", candidate)
	writeCandidate(t, s, "20260905-broken", "no frontmatter at all\n")

	got, errs := s.LoadCandidates()
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one", errs)
	}
	if len(got) != 1 || got[0].Stem() != "20260905-good" {
		t.Fatalf("loaded %d candidates, want the readable one", len(got))
	}
}

// The scaffold's .gitkeep exists so candidates/ survives a clone; counting it
// as a finding would report a backlog on an empty store.
func TestTheGitkeepIsNotACandidate(t *testing.T) {
	s := project(t, nil)

	got, errs := s.LoadCandidates()
	if len(errs) > 0 || len(got) != 0 {
		t.Fatalf("LoadCandidates = %d candidates, %v; want an empty backlog", len(got), errs)
	}
}

// A store that has never been scaffolded is not an error: an explorer stages
// into a repo that may not have adopted memory yet.
func TestAnAbsentCandidatesDirectoryIsAnEmptyBacklog(t *testing.T) {
	s := memory.New(t.TempDir(), "")

	got, errs := s.LoadCandidates()
	if len(errs) > 0 || len(got) != 0 {
		t.Fatalf("LoadCandidates = %d candidates, %v; want an empty backlog", len(got), errs)
	}
}
