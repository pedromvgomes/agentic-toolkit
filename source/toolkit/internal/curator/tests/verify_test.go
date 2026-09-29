package tests

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

const (
	pinsNote      = "pins-shas"
	pinsCandidate = "20260901-pins-shas"
)

// wantRefused fails the test unless err is a verification failure whose text
// carries every one of wants.
func wantRefused(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("a run whose report does not match the store was accepted")
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not say %q:\n%v", want, err)
		}
	}
}

// A report is the run's account of itself. A candidate it says it resolved
// and left staged is still in the backlog, and the next run rules on it again
// having been told it was done.
func TestAResolvedCandidateStillStagedFails(t *testing.T) {
	p := newProject(t)
	p.stage(t, pinsCandidate)

	_, err := p.curate(t, p.fork(t), curator.Report{CandidatesResolved: []string{pinsCandidate}}, curator.Options{})
	wantRefused(t, err, `candidate "`+pinsCandidate+`"`, "still in candidates/")
}

// A note the run wrote and never anchored carries no hash, so audit has
// nothing to compare against and the note can never be reported stale — a
// claim nobody will be told to re-check.
func TestATouchedNoteThatIsNotStampedFails(t *testing.T) {
	p := newProject(t)
	after := p.fork(t)
	after.writeNote(t, pinsNote, false)
	after.reindex(t)

	_, err := p.curate(t, after, curator.Report{NotesTouched: []string{pinsNote}}, curator.Options{})
	wantRefused(t, err, `note "`+pinsNote+`"`, "unstamped", "agtk memory anchor "+pinsNote)
}

// A run that writes a note and reports nothing is the failure a check of the
// report's own claims cannot see: there is no claim to check, so it passes. A
// run cut off after writing is exactly this run. Only diffing the store
// against what it held before the child started names the note.
func TestANoteChangedOnDiskButNotReportedFails(t *testing.T) {
	for name, tc := range map[string]struct {
		before func(*testing.T, *project)
		during func(*testing.T, *project)
		want   string
	}{
		"a new note": {
			before: func(*testing.T, *project) {},
			during: func(t *testing.T, after *project) { after.writeNote(t, pinsNote, false) },
			want:   "is new on disk but was not reported",
		},
		"an edited note": {
			before: func(t *testing.T, p *project) { p.writeNote(t, pinsNote, true) },
			during: func(t *testing.T, after *project) { after.editNote(t, pinsNote) },
			want:   "changed on disk but was not reported",
		},
		"a deleted note": {
			before: func(t *testing.T, p *project) { p.writeNote(t, pinsNote, true) },
			during: func(t *testing.T, after *project) { after.remove(t, after.notePath(pinsNote)) },
			want:   "was removed but was not reported",
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			tc.before(t, p)
			p.reindex(t)
			after := p.fork(t)
			tc.during(t, after)
			after.reindex(t)

			_, err := p.curate(t, after, curator.Report{}, curator.Options{})
			wantRefused(t, err, `note "`+pinsNote+`"`, tc.want)
		})
	}
}

// writeAnchorlessNote writes a note with no anchors at all — a lint defect
// independent of stamping, so it exercises touchedProblems' lint-filtering
// loop without any DriftUnstamped noise from an anchor that exists but was
// never stamped.
func writeAnchorlessNote(t *testing.T, p *project, name string) {
	t.Helper()
	raw := "---\nname: " + name + "\nkind: invariant\n" +
		"description: A claim with nothing pointing at it.\nconfidence: verified\n---\n\n" +
		"No anchors here.\n"
	writeFile(t, p.notePath(name), raw)
}

// touchedProblems filters lint issues to the touched note's own file, so a
// note reported touched fails on its own defects and is never blamed for a
// different note's.
func TestATouchedNoteIsBlamedForItsOwnLintIssueAndNotAnothers(t *testing.T) {
	const flawed = "flawed-note"
	const otherFlawed = "other-flawed-note"
	p := newProject(t)
	writeAnchorlessNote(t, p, otherFlawed)
	p.reindex(t)

	after := p.fork(t)
	writeAnchorlessNote(t, after, flawed)
	after.reindex(t)

	_, err := p.curate(t, after, curator.Report{NotesTouched: []string{flawed}}, curator.Options{})
	wantRefused(t, err, `note "`+flawed+`"`, "no anchors")
	if err != nil && strings.Contains(err.Error(), `note "`+flawed+`" is reported touched but fails lint`) &&
		strings.Contains(err.Error(), otherFlawed) {
		t.Errorf("a note's own verification failure was attributed using another note's file: %v", err)
	}
}

// writeNamelessAnchorlessNote writes a note with neither `name:` nor
// `anchors:` — the case the file-match half of the filter exists for. With
// no name, matching by name is structurally impossible (the check is guarded
// by n.Name != ""), so this note's own issue can only ever be found by file.
func writeNamelessAnchorlessNote(t *testing.T, p *project, stem string) {
	t.Helper()
	raw := "---\nkind: invariant\n" +
		"description: A claim with no name and nothing pointing at it.\nconfidence: verified\n---\n\n" +
		"No name, no anchors.\n"
	writeFile(t, p.notePath(stem), raw)
}

// A note with no `name:` field can only be matched to its own lint issues by
// file, since the name half of the filter is guarded off entirely when there
// is no name to compare.
func TestATouchedNamelessNoteIsStillBlamedForItsOwnLintIssue(t *testing.T) {
	const stem = "nameless-note"
	p := newProject(t)
	after := p.fork(t)
	writeNamelessAnchorlessNote(t, after, stem)
	after.reindex(t)

	_, err := p.curate(t, after, curator.Report{NotesTouched: []string{stem}}, curator.Options{})
	wantRefused(t, err, `note "`+stem+`"`, "no anchors")
}

// Two notes whose `name:` fields collide — one of them lying about which file
// it names — are matched by name as well as by file, precisely because a
// note's `name:` can disagree with its filename. The touched note here is
// otherwise clean; the other note's lint issue reaches it only through that
// name match.
func TestATouchedNoteIsBlamedForACollidingNamesakesLintIssueToo(t *testing.T) {
	const shared = "shared-name"
	// Sorted after "shared-name" alphabetically, so LoadNotes' file order
	// (and so ResolveNote's first match) lands on the genuine shared-name.md
	// note, not this one — the point being tested is that a note resolved by
	// its own file can still be blamed for a different file's issue solely
	// because the two share a `name:` value.
	const impostorStem = "zzz-impostor-note"
	p := newProject(t)
	after := p.fork(t)
	after.writeNote(t, shared, true)
	writeFile(t, after.notePath(impostorStem), "---\nname: "+shared+"\nkind: invariant\n"+
		"description: A different file claiming the same name.\nconfidence: verified\n---\n\n"+
		"No anchors here, filed under someone else's name.\n")
	after.reindex(t)

	_, err := p.curate(t, after, curator.Report{NotesTouched: []string{shared}}, curator.Options{})
	wantRefused(t, err, `note "`+shared+`" is reported touched but fails lint`, "no anchors")
}

// A candidate that vanished with no ruling on record is a finding lost: the
// explorer's evidence is gone, and nothing says whether it was promoted,
// merged or rejected.
func TestACandidateRemovedButNotReportedFails(t *testing.T) {
	p := newProject(t)
	p.stage(t, pinsCandidate)
	after := p.fork(t)
	after.remove(t, after.candidatePath(pinsCandidate))

	_, err := p.curate(t, after, curator.Report{}, curator.Options{})
	wantRefused(t, err, `candidate "`+pinsCandidate+`"`, "was removed but was not reported")
}

// A run that promoted, retracted and cleared everything it says it did, and
// nothing else, is accepted with its report intact.
func TestAReportThatAccountsForTheStoreIsAccepted(t *testing.T) {
	const (
		rejected  = "20260902-where-render-lives"
		falseNote = "render-lives-in-cli"
	)
	p := newProject(t)
	p.stage(t, pinsCandidate)
	p.stage(t, rejected)
	p.writeNote(t, falseNote, true)
	p.reindex(t)

	after := p.fork(t)
	after.writeNote(t, pinsNote, true)
	after.remove(t, after.notePath(falseNote))
	after.remove(t, after.candidatePath(pinsCandidate))
	after.remove(t, after.candidatePath(rejected))
	after.reindex(t)

	report := curator.Report{
		CandidatesResolved: []string{pinsCandidate, rejected},
		NotesRetracted:     []string{falseNote},
		NotesTouched:       []string{pinsNote},
	}
	res, err := p.curate(t, after, report, curator.Options{})
	if err != nil {
		t.Fatalf("a report consistent with the store was refused: %v", err)
	}
	if !slices.Equal(res.Report.NotesTouched, report.NotesTouched) ||
		!slices.Equal(res.Report.NotesRetracted, report.NotesRetracted) ||
		!slices.Equal(res.Report.CandidatesResolved, report.CandidatesResolved) {
		t.Errorf("Report = %+v, want %+v", res.Report, report)
	}
}

// Every note can be accounted for and stamped and the index still left behind
// them, which makes every note the run wrote invisible to routing.
func TestAnIndexLeftBehindTheNotesFails(t *testing.T) {
	p := newProject(t)
	after := p.fork(t)
	after.writeNote(t, pinsNote, true)

	_, err := p.curate(t, after, curator.Report{NotesTouched: []string{pinsNote}}, curator.Options{})
	wantRefused(t, err, "INDEX.md", "agtk memory index")
}

// A retraction that did not happen leaves the store asserting the claim the
// run ruled false.
func TestARetractedNoteStillOnDiskFails(t *testing.T) {
	p := newProject(t)
	p.writeNote(t, pinsNote, true)
	p.reindex(t)

	_, err := p.curate(t, p.fork(t), curator.Report{NotesRetracted: []string{pinsNote}}, curator.Options{})
	wantRefused(t, err, `note "`+pinsNote+`"`, "still in notes/")
}

// A note the report names and the store does not hold is a report about some
// other run.
func TestATouchedNoteThatDoesNotExistFails(t *testing.T) {
	p := newProject(t)

	_, err := p.curate(t, p.fork(t), curator.Report{NotesTouched: []string{pinsNote}}, curator.Options{})
	wantRefused(t, err, `note "`+pinsNote+`"`, "no such note")
}

// A dry run's grant writes nothing, so there is no difference to account for
// and nothing for its report to be held to — even a store and report that
// would otherwise fail every check in both directions.
func TestADryRunIsNotHeldToItsReport(t *testing.T) {
	const vanished = "20260902-where-render-lives"
	report := curator.Report{CandidatesResolved: []string{pinsCandidate}, NotesTouched: []string{"never-written"}}
	curate := func(opts curator.Options) error {
		p := newProject(t)
		p.stage(t, pinsCandidate)
		p.stage(t, vanished)
		after := p.fork(t)
		after.writeNote(t, pinsNote, false)
		after.remove(t, after.candidatePath(vanished))
		_, err := p.curate(t, after, report, opts)
		return err
	}

	if err := curate(curator.Options{}); err == nil {
		t.Fatal("the fixture passes verification as a real run, so it proves nothing about a dry run")
	}
	if err := curate(curator.Options{DryRun: true}); err != nil {
		t.Fatalf("a dry run was held to its report: %v", err)
	}
}

// ResolveNote answers three ways, and the one in the middle — a file that is
// there and does not parse — must not read as the store holding no such note.
func TestResolveNoteTellsAnUnparsableNoteFromAMissingOne(t *testing.T) {
	p := newProject(t)
	p.writeNote(t, pinsNote, true)
	writeFile(t, p.notePath("broken"), "no frontmatter here\n")
	// Listed ahead of broken.md, and its path starts with broken's name, so a
	// lookup matching on the name alone would hand broken this file's error.
	writeFile(t, p.notePath("broken-too"), "no frontmatter here either\n")
	notes, parseErrs := p.store.LoadNotes()

	if l := curator.ResolveNote(p.store, notes, parseErrs, pinsNote); l.Note == nil || l.Err != nil {
		t.Errorf("a parsed note resolved to %+v", l)
	}
	if l := curator.ResolveNote(p.store, notes, parseErrs, pinsNote+memory.NoteExt); l.Note == nil {
		t.Errorf("a name given with its extension did not resolve: %+v", l)
	}

	l := curator.ResolveNote(p.store, notes, parseErrs, "broken")
	if l.Note != nil || l.Err == nil || l.Missing() {
		t.Fatalf("an unparsable note resolved to %+v, want its parse error", l)
	}
	if !strings.HasPrefix(l.Err.Error(), filepath.Join(p.store.NotesPath(), "broken"+memory.NoteExt)+":") {
		t.Errorf("an unparsable note resolved to another file's error: %v", l.Err)
	}

	if l := curator.ResolveNote(p.store, notes, parseErrs, "absent"); !l.Missing() {
		t.Errorf("a note the store does not hold resolved to %+v", l)
	}
}
