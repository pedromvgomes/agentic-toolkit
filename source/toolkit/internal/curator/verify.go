package curator

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// NoteLookup is what one note name resolves to in a store. Exactly one of
// three states holds:
//
//   - Note is set: a note by that name parsed.
//   - Err is set: a file for that name is in notes/ but could not be read or
//     parsed, and Err is the error LoadNotes collected for it.
//   - neither is set: the store holds no note by that name at all.
//
// The middle state is the one a caller must not fold into the last. A note that
// fails to parse is absent from LoadNotes' notes, so a lookup that only
// searched those would report "no note named X" about a file that is sitting
// right there, and send the reader looking for a typo instead of at the file.
type NoteLookup struct {
	Note *memory.Note
	Err  error
}

// Missing reports that the store holds no note by the name, parsed or not.
func (l NoteLookup) Missing() bool { return l.Note == nil && l.Err == nil }

// ResolveNote looks name up in notes and parseErrs, exactly as store.LoadNotes
// returned them, so a caller resolving many names loads the store once.
//
// name is matched the way `agtk memory anchor` matches it: a trailing ".md" is
// ignored, and a parsed note matches on its `name:` field or, failing that, on
// its filename stem. A parse error matches when it was reported for the file
// that name would live at — LoadNotes prefixes every error with the note's
// absolute path, so the path followed by a colon identifies it exactly, and a
// name that is a prefix of another name cannot claim that other note's error.
func ResolveNote(store *memory.Store, notes []*memory.Note, parseErrs []error, name string) NoteLookup {
	name = strings.TrimSuffix(name, memory.NoteExt)
	for _, n := range notes {
		if n.Name == name {
			return NoteLookup{Note: n}
		}
	}
	for _, n := range notes {
		if n.Stem() == name {
			return NoteLookup{Note: n}
		}
	}
	file := filepath.Join(store.NotesPath(), name+memory.NoteExt)
	for _, err := range parseErrs {
		msg := err.Error()
		if strings.HasPrefix(msg, file+":") || strings.HasPrefix(msg, "read "+file+":") {
			return NoteLookup{Err: err}
		}
	}
	return NoteLookup{}
}

// snapshot is the store's contents at one moment, keyed by filename stem: what
// a run found, to be diffed against what it left.
//
// It is taken from the directory listing rather than from LoadNotes and
// LoadCandidates, because both of those drop a file that does not parse — and
// a note the run wrote badly, or a malformed candidate it deleted, is exactly
// the change a report is most likely to leave out.
type snapshot struct {
	// notes maps each note's stem to the blob hash of its bytes.
	notes map[string]string
	// candidates holds each staged candidate's id.
	candidates map[string]bool
}

func takeSnapshot(store *memory.Store) (snapshot, error) {
	s := snapshot{notes: map[string]string{}, candidates: map[string]bool{}}

	notes, err := storeFiles(store.NotesPath())
	if err != nil {
		return snapshot{}, err
	}
	for _, stem := range notes {
		hash, err := memory.HashFile(filepath.Join(store.NotesPath(), stem+memory.NoteExt))
		if err != nil {
			return snapshot{}, fmt.Errorf("curator: snapshot the store: %w", err)
		}
		s.notes[stem] = hash
	}

	candidates, err := storeFiles(store.CandidatesPath())
	if err != nil {
		return snapshot{}, err
	}
	for _, stem := range candidates {
		s.candidates[stem] = true
	}
	return s, nil
}

// storeFiles lists the stems of the *.md files directly in dir, which is every
// file LoadNotes and LoadCandidates would try to read there. A directory that
// is not there holds nothing, as it does for them.
func storeFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("curator: snapshot the store: %w", err)
	}
	var stems []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), memory.NoteExt) {
			continue
		}
		stems = append(stems, strings.TrimSuffix(e.Name(), memory.NoteExt))
	}
	return stems, nil
}

// verify checks a run's completion report against the store it left behind,
// in both directions.
//
// Claim to disk: everything the report says happened did — a resolved
// candidate is gone, a retracted note is gone, a touched note is stamped and
// lints clean. Disk to claim: everything that happened is in the report —
// every note that appeared, changed or vanished, and every candidate that
// vanished, is named. Checking only the first direction verifies what a run
// chose to say, so a run cut off half-way, having written unstamped notes and
// reported nothing, passes it; the second direction is what catches that run.
//
// It writes nothing. A failing store is left exactly as the run left it, and
// the error names every check that failed, with the command that repairs it
// where one does.
func verify(store *memory.Store, before snapshot, r Report) error {
	after, err := takeSnapshot(store)
	if err != nil {
		return err
	}
	notes, parseErrs := store.LoadNotes()

	var problems []string
	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	for _, id := range r.CandidatesResolved {
		if after.candidates[strings.TrimSuffix(id, memory.NoteExt)] {
			fail("candidate %q is reported resolved but is still in candidates/; re-run curate", id)
		}
	}
	for _, name := range r.NotesRetracted {
		if !ResolveNote(store, notes, parseErrs, name).Missing() {
			fail("note %q is reported retracted but is still in notes/; re-run curate", name)
		}
	}
	for _, name := range r.NotesTouched {
		problems = append(problems, touchedProblems(store, notes, parseErrs, name)...)
	}

	touched := reported(store, notes, parseErrs, r.NotesTouched)
	retracted := reported(store, notes, parseErrs, r.NotesRetracted)
	for _, stem := range sortedKeys(after.notes) {
		was, existed := before.notes[stem]
		switch {
		case existed && was == after.notes[stem]:
		case touched[stem] || retracted[stem]:
		case existed:
			fail("note %q changed on disk but was not reported; review it before trusting the store, then re-run curate", stem)
		default:
			fail("note %q is new on disk but was not reported; review it before trusting the store, then re-run curate", stem)
		}
	}
	for _, stem := range sortedKeys(before.notes) {
		if _, still := after.notes[stem]; !still && !retracted[stem] {
			fail("note %q was removed but was not reported retracted; restore it from git if the removal was not intended", stem)
		}
	}
	resolved := map[string]bool{}
	for _, id := range r.CandidatesResolved {
		resolved[strings.TrimSuffix(id, memory.NoteExt)] = true
	}
	for _, id := range sortedKeys(before.candidates) {
		if !after.candidates[id] && !resolved[id] {
			fail("candidate %q was removed but was not reported resolved; restore it from git if it was not ruled on", id)
		}
	}

	// A store that is not there after the run was not there before it either,
	// since the run's grant cannot remove the directory: there is nothing to
	// index, and INDEX.md's absence is not something the run left undone.
	if store.Exists() {
		current, err := store.IndexCurrent(notes)
		switch {
		case err != nil:
			fail("INDEX.md could not be checked: %v", err)
		case !current:
			fail("INDEX.md does not match the notes on disk; run `agtk memory index`")
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf(
		"curator: the run's completion report does not match the store; nothing is rolled back, so the store is as the run left it:\n  - %s",
		strings.Join(problems, "\n  - "))
}

// touchedProblems is why a note reported touched does not hold up: it has to
// exist, parse, be stamped and lint clean, since a note the run wrote and did
// not anchor is one whose staleness signal says nothing.
func touchedProblems(store *memory.Store, notes []*memory.Note, parseErrs []error, name string) []string {
	l := ResolveNote(store, notes, parseErrs, name)
	switch {
	case l.Err != nil:
		return []string{fmt.Sprintf("note %q is reported touched but does not parse: %v; re-run curate", name, l.Err)}
	case l.Missing():
		return []string{fmt.Sprintf("note %q is reported touched but no such note is in notes/; re-run curate", name)}
	}
	n := l.Note

	var problems []string
	for _, d := range store.AuditNote(n).Drifts {
		if d.Kind == memory.DriftUnstamped {
			problems = append(problems, fmt.Sprintf(
				"note %q is reported touched but its anchor %s is unstamped; run `agtk memory anchor %s`",
				name, d.Path, n.Name))
		}
	}
	// Filtered by file as well as by name: a note whose `name:` is missing or
	// disagrees with its filename carries that very issue under a name nobody
	// reported, and one named the same as another note is flagged on whichever
	// file LoadNotes read second.
	for _, issue := range store.Lint(notes, nil) {
		if issue.File == n.File || (n.Name != "" && issue.Note == n.Name) {
			problems = append(problems, fmt.Sprintf("note %q is reported touched but fails lint: %s", name, issue.Message))
		}
	}
	return problems
}

// reported resolves the names a report lists to the stems of the files they
// name, so a note reported by its `name:` is matched to its file even where
// the two disagree. A name that resolves to nothing is kept as given: it may
// be a note the run deleted, whose stem is then exactly that name.
func reported(store *memory.Store, notes []*memory.Note, parseErrs []error, names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSuffix(name, memory.NoteExt)
		out[name] = true
		if l := ResolveNote(store, notes, parseErrs, name); l.Note != nil {
			out[l.Note.Stem()] = true
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
