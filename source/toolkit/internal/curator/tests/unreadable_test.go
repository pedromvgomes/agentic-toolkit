package tests

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-driver/agentictest"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// unreadableCandidate is a candidate whose `about:` carries an unquoted colon,
// which ends the value early and makes the frontmatter invalid YAML.
const unreadableCandidate = "20260830-colon-in-about"

func (p *project) stageUnreadable(t *testing.T, id string) {
	t.Helper()
	writeFile(t, p.candidatePath(id),
		"---\nabout: lock resolution: pins SHAs\nsaw:\n  - "+anchoredFile+"\n---\n\nSee "+anchoredFile+".\n")
}

// wantUnreadable fails the test unless res names exactly the one unreadable
// candidate p holds, by its file.
func wantUnreadable(t *testing.T, p *project, res curator.Result) {
	t.Helper()
	if len(res.Unreadable) != 1 {
		t.Fatalf("Unreadable = %v, want the one candidate that does not parse", res.Unreadable)
	}
	var bad *memory.UnreadableCandidateError
	if !errors.As(res.Unreadable[0], &bad) || bad.File != p.candidatePath(unreadableCandidate) {
		t.Errorf("Unreadable[0] = %v, want it attributed to %s", res.Unreadable[0], p.candidatePath(unreadableCandidate))
	}
}

// The curator cannot repair a candidate that does not parse — its grant only
// deletes candidates — so one left in candidates/ is a finding still waiting
// on a person. Every job shape reports it and fails, with the curator's own
// account still on the Result for the caller to print.
func TestAnUnreadableCandidateIsReportedAndFailsEveryJob(t *testing.T) {
	for name, opts := range map[string]curator.Options{
		"the backlog run": {},
		"a scoped run":    {Notes: []string{pinsNote}},
		"a limited run":   {Limit: 1},
		"the stale job":   {Stale: true},
		"a dry run":       {DryRun: true},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			p.stageUnreadable(t, unreadableCandidate)

			res, err := p.curate(t, nil, curator.Report{}, opts)
			if err == nil {
				t.Fatal("a run that ended with an unreadable candidate in candidates/ succeeded")
			}
			wantUnreadable(t, p, res)
			if res.Text == "" {
				t.Error("the curator's own account was dropped from a run failed for an unreadable candidate")
			}
			if !staged(t, p, unreadableCandidate) {
				t.Error("the unreadable candidate was removed from candidates/")
			}
		})
	}
}

// Outside the backlog job verification passes, so the failure is the
// unreadable candidate's alone and names its file.
func TestTheUnreadableFailureNamesTheFile(t *testing.T) {
	p := newProject(t)
	p.stageUnreadable(t, unreadableCandidate)

	_, err := p.curate(t, nil, curator.Report{}, curator.Options{Limit: 1})
	if err == nil {
		t.Fatal("a run that ended with an unreadable candidate succeeded")
	}
	for _, want := range []string{unreadableCandidate + memory.NoteExt, "cannot repair", "agtk memory lint"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not say %q: %v", want, err)
		}
	}
}

// A store whose candidates all parse gives the run nothing to report and no
// reason to fail.
func TestAStoreWithNoUnreadableCandidateReportsNone(t *testing.T) {
	p := newProject(t)
	p.stage(t, pinsCandidate)

	res, err := p.curate(t, nil, curator.Report{}, curator.Options{Limit: 1})
	if err != nil {
		t.Fatalf("a run over a readable backlog failed: %v", err)
	}
	if len(res.Unreadable) != 0 {
		t.Errorf("Unreadable = %v, want none", res.Unreadable)
	}
}

// A report naming an unreadable candidate as resolved cannot have ruled on a
// finding nobody could parse, so clearing leaves the file where it is while
// still removing the readable candidates the report resolved.
func TestClearingNeverRemovesAnUnreadableCandidate(t *testing.T) {
	p := newProject(t)
	p.stage(t, pinsCandidate)
	p.stageUnreadable(t, unreadableCandidate)

	res, err := p.curate(t, p.fork(t),
		curator.Report{CandidatesResolved: []string{pinsCandidate, unreadableCandidate}},
		curator.Options{Limit: 2})
	if err == nil {
		t.Fatal("a run that ended with an unreadable candidate succeeded")
	}
	if !staged(t, p, unreadableCandidate) {
		t.Error("clearing removed a candidate that does not parse")
	}
	if !slices.Equal(res.Cleared, []string{pinsCandidate}) {
		t.Errorf("Cleared = %v, want only the readable candidate the report resolved", res.Cleared)
	}
	wantUnreadable(t, p, res)
}

// The prompt tells the curator what `candidates --json`'s unreadable list is
// and that it is not the curator's to act on: a curator that took it for work
// would delete the file, which loses the finding.
func TestThePromptTellsTheCuratorToLeaveUnreadableCandidatesAlone(t *testing.T) {
	fake, _, err := run(t, curatedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	prompt := fake.Stdin(t)
	for _, want := range []string{"`unreadable` list", "could not be parsed", "you cannot fix those", "leave them alone"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt the run received does not say %q", want)
		}
	}
}

// A limited run counts only candidates that parse towards its limit, and tells
// the curator the unreadable ones are there and not its to touch.
func TestALimitedRunSkipsAndNamesTheUnreadableCount(t *testing.T) {
	fake := (&agentictest.Fake{Stdout: emptyReportEnvelope}).Build(t)
	p := newProject(t)
	p.stageUnreadable(t, unreadableCandidate)
	p.stage(t, pinsCandidate)

	_, err := curator.Run(t.Context(), curator.Options{
		Provider:      "claudecode",
		Binary:        fake.Path(),
		WorkDir:       p.root,
		CandidatesDir: p.store.CandidatesPath(),
		NotesDir:      p.store.NotesPath(),
		Limit:         1,
	})
	if err == nil {
		t.Fatal("a run that ended with an unreadable candidate succeeded")
	}
	prompt := fake.Stdin(t)
	if !strings.Contains(prompt, "Curate exactly these 1 staged candidates, the oldest in the backlog: "+pinsCandidate) {
		t.Errorf("the limited job did not name the oldest readable candidate: %q", prompt)
	}
	if !strings.Contains(prompt, "The backlog also holds 1 unreadable candidate") {
		t.Errorf("the limited job did not tell the curator about the unreadable candidate: %q", prompt)
	}
}
