package memory

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// Verdict is what an explorer concluded about an existing note it re-checked
// because that note was stale.
//
// It is recorded on a candidate so the curator inherits the check rather than
// repeating it. It is never written into a note: that would be writing
// Confidence, which is the curator's alone.
type Verdict string

const (
	// VerdictStillTrue means the claim holds, though its pointers may have
	// shifted.
	VerdictStillTrue Verdict = "still-true"
	// VerdictNowFalse means the claim no longer holds.
	VerdictNowFalse Verdict = "now-false"
	// VerdictUnchecked means the explorer could not check it within the task
	// it was doing.
	VerdictUnchecked Verdict = "unchecked"
)

// AllVerdicts is the closed set, in the order a report lists them.
var AllVerdicts = []Verdict{VerdictStillTrue, VerdictNowFalse, VerdictUnchecked}

// Valid reports whether v is one of AllVerdicts.
func (v Verdict) Valid() bool {
	for _, c := range AllVerdicts {
		if v == c {
			return true
		}
	}
	return false
}

// Candidate is one finding an explorer staged, with no quality bar applied.
//
// It is deliberately not a Note with fields missing. A candidate carries no
// anchors, no blob hashes and no confidence, because an explorer computes
// none of those: `saw` is the paths a finding came from, and turning them into
// stamped anchors is the curator's work.
type Candidate struct {
	// About is one line: what was learned.
	About string `yaml:"about"`
	// Saw is the paths the finding came from, as an explorer wrote them —
	// project-relative, possibly globs, never hashed.
	Saw []string `yaml:"saw,omitempty"`
	// Targets names the existing note this concerns, or is empty for a new
	// finding.
	Targets string `yaml:"targets,omitempty"`
	// Verdict is the re-check result, and is meaningful only with Targets.
	Verdict Verdict `yaml:"verdict,omitempty"`

	// Body is the evidence: the claim with a pointer for every part of it,
	// and the command or reading that establishes it.
	Body string `yaml:"-"`

	// File is the absolute path the candidate was read from.
	File string `yaml:"-"`
}

// Stem is the candidate's filename without extension.
func (c *Candidate) Stem() string {
	return strings.TrimSuffix(filepath.Base(c.File), NoteExt)
}

// ParseCandidate decodes one candidate file. file is recorded on the candidate
// and used in error messages; it is not read from.
//
// Decoding is strict, for the reason note decoding is: a typo'd key would
// otherwise be dropped in silence, and a candidate that lost its `targets:`
// reads as a new finding rather than a re-check of an existing note.
func ParseCandidate(file string, raw []byte) (*Candidate, error) {
	fm, body, err := splitFrontmatter(file, raw)
	if err != nil {
		return nil, err
	}

	var c Candidate
	dec := yaml.NewDecoder(bytes.NewReader(fm), yaml.Strict())
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: frontmatter: %w", file, err)
	}
	c.Body = string(body)
	c.File = file
	return &c, nil
}

// LoadCandidates parses every *.md under candidates/, sorted by filename.
//
// Parse failures are collected rather than returned as one error, for the
// reason LoadNotes collects them: one malformed candidate must not hide the
// rest of the backlog from a curator.
func (s *Store) LoadCandidates() ([]*Candidate, []error) {
	entries, err := os.ReadDir(s.CandidatesPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("read %s: %w", s.CandidatesPath(), err)}
	}

	var (
		candidates []*Candidate
		errs       []error
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), NoteExt) {
			continue
		}
		file := filepath.Join(s.CandidatesPath(), e.Name())
		raw, err := os.ReadFile(file) // #nosec G304 -- reads candidates from the store the invoker pointed at
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", file, err))
			continue
		}
		c, err := ParseCandidate(file, raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		candidates = append(candidates, c)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].File < candidates[j].File })
	return candidates, errs
}

// CandidateIssues reports what is structurally wrong with a candidate.
//
// A candidate is checked at all because it is the one input to curation that
// nothing else validates: a malformed one becomes either a bad note or a
// silently dropped finding, and both cost more than the check.
func (c *Candidate) CandidateIssues() []string {
	var issues []string
	if strings.TrimSpace(c.About) == "" {
		issues = append(issues, "no `about:` line, so nothing says what was learned")
	}
	if strings.TrimSpace(c.Body) == "" {
		issues = append(issues, "no body, so the curator inherits a claim with no evidence")
	}
	switch {
	case c.Targets == "" && c.Verdict != "":
		// A verdict is a statement about a specific note. Without one it
		// reads as a judgment on the finding itself, which is confidence,
		// and confidence is the curator's alone.
		issues = append(issues, "`verdict:` without `targets:`, but a verdict is about an existing note")
	case c.Targets != "" && c.Verdict == "":
		issues = append(issues, "`targets:` without `verdict:`, so the re-check result is lost")
	case c.Verdict != "" && !c.Verdict.Valid():
		issues = append(issues, fmt.Sprintf("verdict %q is not one of %s", c.Verdict, verdictList()))
	}
	return issues
}

func verdictList() string {
	names := make([]string, 0, len(AllVerdicts))
	for _, v := range AllVerdicts {
		names = append(names, string(v))
	}
	return strings.Join(names, ", ")
}
