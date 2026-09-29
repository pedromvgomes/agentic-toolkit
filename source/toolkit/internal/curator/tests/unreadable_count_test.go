package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-driver/agentictest"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
)

// limitedRun runs a limited curation over p and returns the prompt the provider
// received and the error the run ended with.
func limitedRun(t *testing.T, p *project) (string, error) {
	t.Helper()
	fake := (&agentictest.Fake{Stdout: emptyReportEnvelope}).Build(t)
	_, err := curator.Run(t.Context(), curator.Options{
		Provider:      "claudecode",
		Binary:        fake.Path(),
		WorkDir:       p.root,
		CandidatesDir: p.store.CandidatesPath(),
		NotesDir:      p.store.NotesPath(),
		Limit:         1,
	})
	return fake.Stdin(t), err
}

// The count of unreadable candidates agrees with its noun in the failure the
// run returns: one is singular, several plural.
func TestTheUnreadableFailureCountsCandidatesInTheRightNumber(t *testing.T) {
	for name, tc := range map[string]struct {
		ids  []string
		want string
		not  string
	}{
		"one":  {[]string{unreadableCandidate}, "holds 1 unreadable candidate the curator", "1 unreadable candidates"},
		"two":  {[]string{unreadableCandidate, "20260831-second-colon"}, "holds 2 unreadable candidates the curator", "2 unreadable candidate the"},
		"many": {[]string{unreadableCandidate, "20260831-second-colon", "20260901-third-colon"}, "holds 3 unreadable candidates the curator", "3 unreadable candidate the"},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			for _, id := range tc.ids {
				p.stageUnreadable(t, id)
			}

			res, err := p.curate(t, nil, curator.Report{}, curator.Options{Stale: true})
			if err == nil {
				t.Fatal("a run that ended with unreadable candidates succeeded")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not say %q: %v", tc.want, err)
			}
			if strings.Contains(err.Error(), tc.not) {
				t.Errorf("error miscounts the candidates (%q): %v", tc.not, err)
			}
			if len(res.Unreadable) != len(tc.ids) {
				t.Errorf("Unreadable = %v, want %d", res.Unreadable, len(tc.ids))
			}
		})
	}
}

// The limited job's note about the backlog's unreadable candidates agrees in
// number with how many there are.
func TestTheLimitedJobCountsUnreadableCandidatesInTheRightNumber(t *testing.T) {
	for name, tc := range map[string]struct {
		ids  []string
		want string
		not  string
	}{
		"one": {[]string{unreadableCandidate}, "The backlog also holds 1 unreadable candidate, not counted", "1 unreadable candidates"},
		"two": {[]string{unreadableCandidate, "20260831-second-colon"}, "The backlog also holds 2 unreadable candidates, not counted", "2 unreadable candidate,"},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			p.stage(t, pinsCandidate)
			for _, id := range tc.ids {
				p.stageUnreadable(t, id)
			}

			prompt, _ := limitedRun(t, p)
			if !strings.Contains(prompt, tc.want) {
				t.Errorf("the limited job does not say %q: %q", tc.want, prompt)
			}
			if strings.Contains(prompt, tc.not) {
				t.Errorf("the limited job miscounts the unreadable candidates (%q): %q", tc.not, prompt)
			}
		})
	}
}

// A limited run over a backlog that parses in full has no unreadable
// candidates to mention, so its job text never brings them up.
func TestTheLimitedJobSaysNothingOfUnreadableCandidatesWhenThereAreNone(t *testing.T) {
	p := newProject(t)
	p.stage(t, pinsCandidate)

	prompt, err := limitedRun(t, p)
	if err != nil {
		t.Fatalf("a run over a readable backlog failed: %v", err)
	}
	if !strings.Contains(prompt, "Curate exactly these 1 staged candidates") {
		t.Fatalf("the limited job did not take the limited branch: %q", prompt)
	}
	if strings.Contains(prompt, "The backlog also holds") || strings.Contains(prompt, "not counted among these") {
		t.Errorf("the limited job mentions unreadable candidates over a fully readable backlog: %q", prompt)
	}
}

// An empty backlog is no different: nothing unreadable, nothing said of it.
func TestTheLimitedJobOverAnEmptyBacklogSaysNothingOfUnreadableCandidates(t *testing.T) {
	p := newProject(t)

	prompt, err := limitedRun(t, p)
	if err != nil {
		t.Fatalf("a run over an empty backlog failed: %v", err)
	}
	if !strings.Contains(prompt, "There is nothing to curate this run") {
		t.Fatalf("the limited job did not take the empty-backlog branch: %q", prompt)
	}
	if strings.Contains(prompt, "The backlog also holds") {
		t.Errorf("the limited job mentions unreadable candidates over an empty backlog: %q", prompt)
	}
}
