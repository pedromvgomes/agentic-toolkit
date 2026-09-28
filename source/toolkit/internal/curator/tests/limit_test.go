package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-driver/agentictest"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
)

// Candidate filenames are date-prefixed, so ascending by name is ascending by
// age; these are already in that order.
var limitTestCandidateIDs = []string{
	"20260901-first",
	"20260902-second",
	"20260903-third",
	"20260904-fourth",
	"20260905-fifth",
}

// TestLimitNamesTheOldestCandidatesInTheJobDescription checks that a limited
// run points the job at exactly the oldest N candidates by id, rather than at
// the backlog generically.
func TestLimitNamesTheOldestCandidatesInTheJobDescription(t *testing.T) {
	fake := (&agentictest.Fake{Stdout: emptyReportEnvelope}).Build(t)
	dir := t.TempDir()
	p := newProjectIn(t, dir)
	for _, id := range limitTestCandidateIDs {
		p.stage(t, id)
	}

	_, err := curator.Run(t.Context(), curator.Options{
		Provider:      "claudecode",
		Binary:        fake.Path(),
		WorkDir:       dir,
		CandidatesDir: p.store.CandidatesPath(),
		NotesDir:      p.store.NotesPath(),
		Limit:         2,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	prompt := fake.Stdin(t)
	for _, want := range limitTestCandidateIDs[:2] {
		if !strings.Contains(prompt, want) {
			t.Errorf("job description does not name the oldest candidate %q: %q", want, prompt)
		}
	}
	for _, notWant := range limitTestCandidateIDs[2:] {
		if strings.Contains(prompt, notWant) {
			t.Errorf("job description names a candidate beyond the limit (%q): %q", notWant, prompt)
		}
	}
}

// TestLimitBeyondTheBacklogNamesWhatExists checks that a limit larger than the
// backlog names every staged candidate rather than padding or erroring.
func TestLimitBeyondTheBacklogNamesWhatExists(t *testing.T) {
	fake := (&agentictest.Fake{Stdout: emptyReportEnvelope}).Build(t)
	dir := t.TempDir()
	p := newProjectIn(t, dir)
	p.stage(t, limitTestCandidateIDs[0])

	_, err := curator.Run(t.Context(), curator.Options{
		Provider:      "claudecode",
		Binary:        fake.Path(),
		WorkDir:       dir,
		CandidatesDir: p.store.CandidatesPath(),
		NotesDir:      p.store.NotesPath(),
		Limit:         5,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	prompt := fake.Stdin(t)
	if !strings.Contains(prompt, limitTestCandidateIDs[0]) {
		t.Errorf("job description does not name the one staged candidate: %q", prompt)
	}
}

// TestLimitIsIgnoredUnderStale checks that --stale's job still points at the
// stale list rather than at a limited candidate set, since Limit only shapes
// the non-stale backlog job.
func TestLimitIsIgnoredUnderStale(t *testing.T) {
	fake, _, err := run(t, curatedEnvelope, curator.Options{Stale: true, Limit: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompt := fake.Stdin(t)
	if !strings.Contains(prompt, "audit") {
		t.Errorf("a stale sweep with --limit set did not point at the stale list: %q", prompt)
	}
	if strings.Contains(prompt, "Curate exactly these") {
		t.Errorf("a stale sweep with --limit set named a limited candidate set: %q", prompt)
	}
}

// TestLimitUnsetLeavesTheBacklogJobUnchanged checks that Limit's zero value —
// unset — leaves the non-stale job pointed at the backlog generically, the
// same as a run with no Limit field at all.
//
// Asserts on the job's own sentence, not merely on a substring that also
// appears in the embedded policy preamble ("agtk memory candidates --json"
// is already there, in the "what you are given" section, so a prompt that
// took the limited-run branch by mistake would still contain it).
func TestLimitUnsetLeavesTheBacklogJobUnchanged(t *testing.T) {
	fake, _, err := run(t, curatedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompt := fake.Stdin(t)
	if !strings.Contains(prompt, "Curate the memory store's staged candidates: run") {
		t.Errorf("an unlimited run does not point the curator at the backlog generically: %q", prompt)
	}
	if strings.Contains(prompt, "Curate exactly these") {
		t.Errorf("an unlimited run named a limited candidate set: %q", prompt)
	}
}

// TestLimitOnAnEmptyBacklogNamesNothingPlainly checks that --limit against a
// backlog with nothing staged produces a plain statement rather than the
// job's usual sentence with an empty id list spliced in.
func TestLimitOnAnEmptyBacklogNamesNothingPlainly(t *testing.T) {
	fake, _, err := run(t, curatedEnvelope, curator.Options{Limit: 5})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompt := fake.Stdin(t)
	if strings.Contains(prompt, "Curate exactly these 0 staged candidates") {
		t.Errorf("an empty-backlog limited run produced the garbled zero-candidate sentence: %q", prompt)
	}
	if !strings.Contains(prompt, "nothing staged") && !strings.Contains(prompt, "nothing to curate") {
		t.Errorf("an empty-backlog limited run does not say plainly there is nothing to do: %q", prompt)
	}
}
