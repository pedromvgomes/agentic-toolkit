package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bannedTerms are glossary synonyms that must not appear in the code-review
// packages, mapped to the canonical term CONTEXT.md gives instead.
//
// A subset of CONTEXT.md's `_Avoid_` lists rather than all of it: a word is
// listed here only where it has no other ordinary meaning in this code. "run"
// and "flag" are banned as synonyms too, and are excluded, because Go code
// legitimately runs things and parses flags — a check that fired on those
// would be turned off within a week, and a check nobody keeps on protects
// nothing.
//
// The glossary is otherwise enforced by nobody, which is what let four
// separate synonym slips into one change.
var bannedTerms = map[string]string{
	"invocation":  "Runner",
	"panelist":    "Reviewer",
	"referee":     "Judge",
	"arbiter":     "Judge",
	"second pass": "Validator",
}

// glossaryScope are the packages CONTEXT.md's review vocabulary governs.
var glossaryScope = []string{
	"internal/reviewrun",
	"internal/review",
}

// CONTEXT.md's 35 terms each carry an `_Avoid_` list, and until this check
// existed nothing enforced them: the vocabulary was a rule the repo wrote down
// and then relied on a reader remembering.
func TestTheReviewVocabularyAvoidsItsOwnBannedSynonyms(t *testing.T) {
	repo := repoRoot(t)

	for _, dir := range glossaryScope {
		err := filepath.Walk(filepath.Join(repo, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			body, err := os.ReadFile(path) // #nosec G304 -- a .go file inside this repository
			if err != nil {
				return err
			}
			rel := filepath.ToSlash(mustRel(t, repo, path))
			for lineNo, line := range strings.Split(string(body), "\n") {
				lower := strings.ToLower(line)
				for banned, canonical := range bannedTerms {
					if strings.Contains(lower, banned) {
						t.Errorf("%s:%d uses %q; CONTEXT.md names that concept %s\n\t%s",
							rel, lineNo+1, banned, canonical, strings.TrimSpace(line))
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// The bare noun belongs to the memory subsystem. In review it is always a
// "candidate finding", which is the distinction CONTEXT.md's flagged
// ambiguities exist to keep.
func TestTheReviewPackagesSayCandidateFinding(t *testing.T) {
	repo := repoRoot(t)

	err := filepath.Walk(filepath.Join(repo, "internal/reviewrun"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		body, err := os.ReadFile(path) // #nosec G304 -- a .go file inside this repository
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(mustRel(t, repo, path))
		for lineNo, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			// Comments only: identifiers are checked by eye, and a local
			// variable named `candidates` inside a function whose whole
			// subject is candidate findings reads fine.
			if !strings.HasPrefix(trimmed, "//") {
				continue
			}
			lower := strings.ToLower(trimmed)
			for _, phrase := range []string{"candidate ", "candidates "} {
				idx := strings.Index(lower, phrase)
				if idx < 0 {
					continue
				}
				rest := lower[idx+len(phrase):]
				if strings.HasPrefix(rest, "finding") {
					continue
				}
				t.Errorf("%s:%d says %q where CONTEXT.md requires \"candidate finding\"\n\t%s",
					rel, lineNo+1, strings.TrimSpace(phrase), trimmed)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
