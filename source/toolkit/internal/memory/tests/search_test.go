package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// searchNote is a well-formed note with every field a search reads under the
// test's control.
func searchNote(name, description, anchors, body string) string {
	return "---\nname: " + name + "\nkind: gotcha\n" +
		"description: " + description + "\n" +
		"anchors:\n" + anchors +
		"confidence: suspect\n---\n\n" + body + "\n"
}

func search(t *testing.T, s *memory.Store, q memory.Query) []memory.Result {
	t.Helper()
	var warn bytes.Buffer
	got, err := memory.Search(memory.NewFileCorpus(s), q, &warn)
	if err != nil {
		t.Fatalf("search %+v: %v", q, err)
	}
	if warn.Len() > 0 {
		t.Fatalf("search %+v warned over a well-formed store: %s", q, warn.String())
	}
	return got
}

func names(results []memory.Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Name
	}
	return out
}

// TestSearchBreaksScoreTiesByNameAscending: equal scores must still come
// out in one order, or two runs over the same store disagree about which
// note is first and a caller that reads only the top result gets a
// different answer each time.
func TestSearchBreaksScoreTiesByNameAscending(t *testing.T) {
	s := project(t, nil)
	for _, name := range []string{"charlie", "alpha", "echo", "bravo", "delta"} {
		writeNote(t, s, name, searchNote(name, "The throttle advances on success.", "", "Body."))
	}

	want := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	first := search(t, s, memory.Query{Words: []string{"throttle"}})
	if got := names(first); !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for _, r := range first {
		if r.Score != first[0].Score {
			t.Fatalf("scores differ, so this does not exercise the tie: %+v", first)
		}
	}
	for i := 0; i < 5; i++ {
		if got := names(search(t, s, memory.Query{Words: []string{"throttle"}})); !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d order = %v, want %v", i, got, want)
		}
	}
}

// TestSearchRanksAFileAnchorAboveEveryWordMatch: a note anchored to the file
// being worked on is the one most likely to constrain the change, however
// much another note talks about the query's words.
func TestSearchRanksAFileAnchorAboveEveryWordMatch(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "anchored-once", searchNote("anchored-once", "Unrelated wording.",
		"  - path: internal/resolver/graph.go\n", "Nothing here."))
	writeNote(t, s, "anchored-twice", searchNote("anchored-twice", "Unrelated wording.",
		"  - path: internal/resolver/graph.go\n  - path: internal/lockfile/types.go\n", "Nothing here."))
	writeNote(t, s, "lock-lock-lock", searchNote("lock-lock-lock", "Lock lock lock resolution.",
		"", strings.Repeat("lock resolution ", 50)))

	got := search(t, s, memory.Query{
		Files: []string{"internal/resolver/graph.go", "internal/lockfile/types.go"},
		Words: []string{"lock", "resolution"},
	})
	want := []string{"anchored-twice", "anchored-once", "lock-lock-lock"}
	if !reflect.DeepEqual(names(got), want) {
		t.Fatalf("order = %v, want %v (%+v)", names(got), want, got)
	}
	if !reflect.DeepEqual(got[0].Anchors, []string{"internal/resolver/graph.go", "internal/lockfile/types.go"}) {
		t.Errorf("anchors = %v, want both covering anchors", got[0].Anchors)
	}
	if len(got[2].Anchors) != 0 {
		t.Errorf("a word-only match names anchors: %v", got[2].Anchors)
	}
}

// TestSearchRanksAFileAnchorAboveANoteSaturatingEveryField: one covered file
// outweighs the most a note can score on the words, reached when every term
// hits the cap in the name, the description and the body at once.
func TestSearchRanksAFileAnchorAboveANoteSaturatingEveryField(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "anchored", searchNote("anchored", "Unrelated wording.",
		"  - path: internal/resolver/graph.go\n", "Nothing here."))
	writeNote(t, s, "throttle-throttle-throttle", searchNote("throttle-throttle-throttle",
		"Throttle throttle throttle.", "", "throttle throttle throttle"))

	got := search(t, s, memory.Query{
		Files: []string{"internal/resolver/graph.go"},
		Words: []string{"throttle"},
	})
	want := []string{"anchored", "throttle-throttle-throttle"}
	if !reflect.DeepEqual(names(got), want) {
		t.Fatalf("order = %v, want %v (%+v)", names(got), want, got)
	}
	// Name, description and body weigh 3, 2 and 1.
	if ceiling := memory.TermCountCap * (3 + 2 + 1); got[1].Score != ceiling {
		t.Errorf("saturated note score = %d, want the words-only ceiling %d", got[1].Score, ceiling)
	}
}

// TestSearchKeepsNotesSharingANameInFileOrder: two files carrying the same
// `name:` — a copied note lint has not yet caught — tie on score and name, so
// neither sorts ahead of the other and they keep the order their files load in.
func TestSearchKeepsNotesSharingANameInFileOrder(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "copy-a", searchNote("shared", "First copy.", "", "The throttle."))
	writeNote(t, s, "copy-b", searchNote("shared", "Second copy.", "", "The throttle."))

	got := search(t, s, memory.Query{Words: []string{"throttle"}})
	var descriptions []string
	for _, r := range got {
		descriptions = append(descriptions, r.Description)
	}
	if want := []string{"First copy.", "Second copy."}; !reflect.DeepEqual(descriptions, want) {
		t.Fatalf("descriptions = %v, want %v (%+v)", descriptions, want, got)
	}
	if got[0].Score != got[1].Score {
		t.Fatalf("scores differ, so this does not exercise the tie: %+v", got)
	}
}

// TestSearchCapsRepeatedTermsSoALongNoteCannotWin: a note that says a word
// five hundred times is not five hundred times as relevant as one named for
// it.
func TestSearchCapsRepeatedTermsSoALongNoteCannotWin(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "aaa-long", searchNote("aaa-long", "Notes on many things.",
		"", strings.Repeat("the throttle is mentioned again. ", 500)))
	writeNote(t, s, "throttle-advances-on-success", searchNote("throttle-advances-on-success",
		"The throttle advances only on success.", "", "Short."))

	got := search(t, s, memory.Query{Words: []string{"throttle"}})
	want := []string{"throttle-advances-on-success", "aaa-long"}
	if !reflect.DeepEqual(names(got), want) {
		t.Fatalf("order = %v, want %v (%+v)", names(got), want, got)
	}
	if got[1].Score != memory.TermCountCap {
		t.Errorf("long note score = %d, want the body cap %d", got[1].Score, memory.TermCountCap)
	}
}

// TestSearchMatchesGlobAnchorsOneDirectoryLevel: a glob anchor covers what
// it would expand to — one directory level — and a `**` anchor, which the
// store refuses, covers nothing rather than something it cannot audit.
func TestSearchMatchesGlobAnchorsOneDirectoryLevel(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "every-lockfile", searchNote("every-lockfile", "Quantified.",
		"  - path: internal/lockfile/*.go\n", "Body."))
	writeNote(t, s, "deep", searchNote("deep", "Quantified.",
		"  - path: internal/**/*.go\n", "Body."))

	got := search(t, s, memory.Query{Files: []string{"internal/lockfile/types.go"}})
	if !reflect.DeepEqual(names(got), []string{"every-lockfile"}) {
		t.Fatalf("results = %v, want only every-lockfile", names(got))
	}
	if !reflect.DeepEqual(got[0].Anchors, []string{"internal/lockfile/*.go"}) {
		t.Errorf("anchors = %v, want the glob as authored", got[0].Anchors)
	}

	if got := search(t, s, memory.Query{Files: []string{"internal/lockfile/sub/types.go"}}); len(got) != 0 {
		t.Errorf("a one-level glob covered a file two levels down: %v", names(got))
	}
}

// TestSearchResultCarriesTheNoteAndTheCommandToOpenIt: a caller decides from
// the result whether to open the note, and opens it with the command given.
func TestSearchResultCarriesTheNoteAndTheCommandToOpenIt(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "pins-shas", note("pins-shas", "  - path: internal/resolver/graph.go\n"))

	got := search(t, s, memory.Query{Words: []string{"tags"}})
	if len(got) != 1 {
		t.Fatalf("results = %+v, want one", got)
	}
	r := got[0]
	if r.Name != "pins-shas" || r.Kind != memory.KindInvariant || r.Confidence != memory.ConfidenceVerified {
		t.Errorf("frontmatter fields wrong: %+v", r)
	}
	if r.Description != "Lock resolution pins commit SHAs, never tags." {
		t.Errorf("description = %q", r.Description)
	}
	if r.Show != "agtk memory show pins-shas" {
		t.Errorf("show = %q", r.Show)
	}
}

// storeTree hashes every file under the store root, keyed by its path
// relative to the root, directories included so a created one shows up.
func storeTree(t *testing.T, s *memory.Store) map[string]string {
	t.Helper()
	tree := map[string]string{}
	err := filepath.WalkDir(s.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.Root, p)
		if d.IsDir() {
			tree[rel+"/"] = ""
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		tree[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", s.Root, err)
	}
	return tree
}

// TestSearchWritesNothingUnderTheStore: a search is not a read of a note, so
// it must not count as a Hit, and it runs from hooks, so it must leave no
// diff in notes/ or INDEX.md behind.
func TestSearchWritesNothingUnderTheStore(t *testing.T) {
	s := project(t, map[string]string{"internal/resolver/graph.go": "package resolver\n"})
	writeNote(t, s, "pins-shas", note("pins-shas", "  - path: internal/resolver/graph.go\n"))
	writeNote(t, s, "unstamped", searchNote("unstamped", "Lock resolution.", "  - path: internal/gone.go\n", "Body."))
	if _, err := s.Stamp(loadOne(t, s, "pins-shas")); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	notes, errs := s.LoadNotes()
	if len(errs) > 0 {
		t.Fatalf("load: %v", errs)
	}
	if _, err := s.WriteIndex(notes); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := s.RecordHit("pins-shas", time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatalf("record hit: %v", err)
	}
	for _, p := range []string{s.HitsPath(), s.IndexPath()} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s must exist for this test to compare it: %v", p, err)
		}
	}

	before := storeTree(t, s)
	search(t, s, memory.Query{
		Files: []string{"internal/resolver/graph.go"},
		Words: []string{"lock", "resolution"},
	})
	after := storeTree(t, s)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("search changed the store:\nbefore %v\nafter  %v", before, after)
	}
}

// TestSearchWarnsAboutAnUnreadableNoteAndReturnsTheRest: one malformed note
// must neither vanish without a word nor hide the notes around it.
func TestSearchWarnsAboutAnUnreadableNoteAndReturnsTheRest(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "broken", "no frontmatter here, throttle\n")
	writeNote(t, s, "throttle", searchNote("throttle", "The throttle.", "", "Body."))

	var warn bytes.Buffer
	got, err := memory.Search(memory.NewFileCorpus(s), memory.Query{Words: []string{"throttle"}}, &warn)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !reflect.DeepEqual(names(got), []string{"throttle"}) {
		t.Errorf("results = %v, want the readable note", names(got))
	}
	lines := strings.Split(strings.TrimRight(warn.String(), "\n"), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "warning: skipping unreadable note: ") ||
		!strings.Contains(lines[0], "broken.md") {
		t.Errorf("warnings = %q, want one line naming broken.md", warn.String())
	}
}

// TestNormaliseSearchPath: every spelling of one file comes out as the one
// project-relative path anchors use, without touching the filesystem.
func TestNormaliseSearchPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	sub := filepath.Join(root, "internal")
	cases := []struct {
		name, cwd, path, want string
	}{
		{"dot-relative from the root", root, "./internal/resolver/graph.go", "internal/resolver/graph.go"},
		{"bare relative from the root", root, "internal/resolver/graph.go", "internal/resolver/graph.go"},
		{"absolute", sub, filepath.Join(root, "internal", "resolver", "graph.go"), "internal/resolver/graph.go"},
		{"relative to a subdirectory", sub, "resolver/graph.go", "internal/resolver/graph.go"},
		{"climbing but staying inside", sub, "../go.mod", "go.mod"},
		{"a file that does not exist", root, "no/such/file.go", "no/such/file.go"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := memory.NormaliseSearchPath(root, c.cwd, c.path)
			if err != nil {
				t.Fatalf("normalise %q from %s: %v", c.path, c.cwd, err)
			}
			if got != c.want {
				t.Errorf("normalise %q from %s = %q, want %q", c.path, c.cwd, got, c.want)
			}
		})
	}

	for _, c := range []struct{ name, cwd, path string }{
		{"absolute outside", root, filepath.Join(filepath.Dir(root), "elsewhere", "x.go")},
		{"climbing out", root, "../x.go"},
		{"sibling sharing a prefix", root, filepath.Join(root+"-other", "x.go")},
		{"the root itself", sub, ".."},
	} {
		t.Run("rejects "+c.name, func(t *testing.T) {
			got, err := memory.NormaliseSearchPath(root, c.cwd, c.path)
			if !errors.Is(err, memory.ErrOutsideProject) {
				t.Errorf("normalise %q from %s = %q, %v; want ErrOutsideProject", c.path, c.cwd, got, err)
			}
			// A caller that drops the error must not be left holding a path.
			if got != "" {
				t.Errorf("normalise %q from %s = %q alongside the error, want no path", c.path, c.cwd, got)
			}
		})
	}
}

// TestSearchFindsTheSameNoteForEverySpellingOfAPath: the normalised forms
// all land on the stored anchor, so which directory a caller ran from does
// not change the answer.
func TestSearchFindsTheSameNoteForEverySpellingOfAPath(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "pins-shas", note("pins-shas", "  - path: internal/resolver/graph.go\n"))
	sub := filepath.Join(s.ProjectRoot, "internal")

	for _, spelled := range []struct{ cwd, path string }{
		{s.ProjectRoot, "./internal/resolver/graph.go"},
		{sub, filepath.Join(s.ProjectRoot, "internal", "resolver", "graph.go")},
		{sub, "resolver/graph.go"},
	} {
		rel, err := memory.NormaliseSearchPath(s.ProjectRoot, spelled.cwd, spelled.path)
		if err != nil {
			t.Fatalf("normalise %q: %v", spelled.path, err)
		}
		got := search(t, s, memory.Query{Files: []string{rel}})
		if !reflect.DeepEqual(names(got), []string{"pins-shas"}) {
			t.Errorf("%q from %s found %v, want pins-shas", spelled.path, spelled.cwd, names(got))
		}
	}
}

// TestSearchRejectsAnEmptyQuery: with no file and no word there is nothing
// to rank against, and returning every note would read as a match.
func TestSearchRejectsAnEmptyQuery(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "pins-shas", note("pins-shas", "  - path: internal/resolver/graph.go\n"))

	for _, q := range []memory.Query{
		{},
		{Limit: 3},
		{Words: []string{"", " -- ", "?!"}},
		{Files: []string{""}},
	} {
		if _, err := memory.Search(memory.NewFileCorpus(s), q, io.Discard); !errors.Is(err, memory.ErrEmptyQuery) {
			t.Errorf("search %+v: err = %v, want ErrEmptyQuery", q, err)
		}
	}
}

// TestSearchMatchingNothingIsAnEmptyListAndNoError: no match is an answer,
// not a failure.
func TestSearchMatchingNothingIsAnEmptyListAndNoError(t *testing.T) {
	s := project(t, nil)
	writeNote(t, s, "pins-shas", note("pins-shas", "  - path: internal/resolver/graph.go\n"))

	got, err := memory.Search(memory.NewFileCorpus(s), memory.Query{
		Files: []string{"internal/other.go"},
		Words: []string{"zebra"},
	}, io.Discard)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("results = %+v, want none", got)
	}
}

// TestSearchHonoursTheLimit: an explicit limit caps the list, and an unset
// one falls back to DefaultSearchLimit.
func TestSearchHonoursTheLimit(t *testing.T) {
	s := project(t, nil)
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("note-%d", i)
		writeNote(t, s, name, searchNote(name, "The throttle.", "", "Body."))
	}

	if got := search(t, s, memory.Query{Words: []string{"throttle"}, Limit: 3}); !reflect.DeepEqual(names(got), []string{"note-0", "note-1", "note-2"}) {
		t.Errorf("limit 3 = %v", names(got))
	}
	if got := search(t, s, memory.Query{Words: []string{"throttle"}}); len(got) != memory.DefaultSearchLimit {
		t.Errorf("unset limit returned %d, want %d", len(got), memory.DefaultSearchLimit)
	}
	if got := search(t, s, memory.Query{Words: []string{"throttle"}, Limit: 20}); len(got) != 8 {
		t.Errorf("limit above the match count returned %d, want all 8", len(got))
	}
}

// TestSearchLabelsAStaleNoteWithoutDemotingIt: staleness says the anchored
// code moved, not that the note is less relevant. A stale note keeps its
// rank and says so, the same verdict `show` would print.
func TestSearchLabelsAStaleNoteWithoutDemotingIt(t *testing.T) {
	s := project(t, map[string]string{
		"internal/resolver/graph.go": "package resolver\n",
		"internal/lockfile/types.go": "package lockfile\n",
		"internal/sync/sync.go":      "package sync\n",
	})
	writeNote(t, s, "alpha-changed", searchNote("alpha-changed", "The throttle.", "  - path: internal/resolver/graph.go\n", "Body."))
	writeNote(t, s, "bravo-deleted", searchNote("bravo-deleted", "The throttle.", "  - path: internal/sync/sync.go\n", "Body."))
	writeNote(t, s, "charlie-fresh", searchNote("charlie-fresh", "The throttle.", "  - path: internal/lockfile/types.go\n", "Body."))
	for _, name := range []string{"alpha-changed", "bravo-deleted", "charlie-fresh"} {
		if _, err := s.Stamp(loadOne(t, s, name)); err != nil {
			t.Fatalf("stamp %s: %v", name, err)
		}
	}
	write(t, filepath.Join(s.ProjectRoot, "internal/resolver/graph.go"), "package resolver // changed\n")
	if err := os.Remove(filepath.Join(s.ProjectRoot, "internal/sync/sync.go")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	got := search(t, s, memory.Query{Words: []string{"throttle"}})
	if want := []string{"alpha-changed", "bravo-deleted", "charlie-fresh"}; !reflect.DeepEqual(names(got), want) {
		t.Fatalf("order = %v, want %v: a stale note moved", names(got), want)
	}
	for _, r := range got {
		want := r.Name != "charlie-fresh"
		if r.Stale != want {
			t.Errorf("%s stale = %v, want %v", r.Name, r.Stale, want)
		}
		if audit := s.AuditNote(loadOne(t, s, r.Name)); audit.Stale() != r.Stale {
			t.Errorf("%s stale = %v, but audit says %v", r.Name, r.Stale, audit.Stale())
		}
	}
}

// BenchmarkSearch measures one search over a 2,000-note store, file scan
// and parse included, which is what every invocation pays.
func BenchmarkSearch(b *testing.B) {
	root := b.TempDir()
	s := memory.New(root, "")
	if err := s.Scaffold(); err != nil {
		b.Fatalf("scaffold: %v", err)
	}
	words := []string{"lock", "resolver", "throttle", "cache", "render", "stack", "anchor", "review"}
	for i := 0; i < 2000; i++ {
		name := fmt.Sprintf("note-%04d", i)
		w := words[i%len(words)]
		anchor := fmt.Sprintf("  - path: internal/pkg%02d/file%d.go\n", i%50, i%7)
		if i%10 == 0 {
			anchor = fmt.Sprintf("  - path: internal/pkg%02d/*.go\n", i%50)
		}
		body := strings.Repeat(fmt.Sprintf("The %s path depends on %s being settled first. ", w, words[(i+3)%len(words)]), 20)
		raw := searchNote(name, "How the "+w+" behaves under load.", anchor, body)
		if err := os.WriteFile(filepath.Join(s.NotesPath(), name+memory.NoteExt), []byte(raw), 0o644); err != nil {
			b.Fatalf("write: %v", err)
		}
	}
	corpus := memory.NewFileCorpus(s)
	q := memory.Query{
		Files: []string{"internal/pkg07/file3.go"},
		Words: []string{"throttle", "cache"},
		Limit: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := memory.Search(corpus, q, io.Discard)
		if err != nil {
			b.Fatalf("search: %v", err)
		}
		if len(got) == 0 {
			b.Fatal("search found nothing in a store built to match")
		}
	}
}
