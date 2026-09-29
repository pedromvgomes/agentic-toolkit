package memory

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// DefaultSearchLimit is how many results Search returns when a Query leaves
// Limit unset.
const DefaultSearchLimit = 5

// Weights for where a query word appears in a note. A note's name is the
// closest thing it has to a title, and its description is what the Index
// routes on, so both count for more than a mention in the body.
const (
	nameWeight        = 3
	descriptionWeight = 2
	bodyWeight        = 1
)

// TermCountCap bounds how many occurrences of one query word count within
// one field of one note. Without it a long note that repeats a word scores
// above a short note that is about that word, and the ranking rewards
// length rather than relevance.
const TermCountCap = 3

// ShowCommand is the command a Result names to open its note.
const ShowCommand = "agtk memory show"

// ErrEmptyQuery is returned by Search for a query that names no file and no
// word. It is a usage error: there is nothing to rank against.
var ErrEmptyQuery = errors.New("search needs at least one file or one word")

// ErrOutsideProject is wrapped by NormaliseSearchPath for a path that does
// not sit inside the project root. Anchors never point outside it, so such a
// path could only ever match nothing.
var ErrOutsideProject = errors.New("path is outside the project root")

// Corpus is where Search reads notes from. The file scan over a store is
// the one implementation; the seam is here so a derived index or another
// ranker's input can sit behind the same contract without Search changing.
type Corpus interface {
	// Notes returns every note, plus one error per note that could not be
	// read. A note that fails does not stop the rest being returned.
	Notes() ([]*Note, []error)
	// Stale reports whether n's anchored content has diverged from what was
	// stamped, by the same audit `agtk memory show` reports.
	Stale(n *Note) bool
}

// FileCorpus is a Corpus that reads a store's notes/ directory on every
// call. Nothing is cached between searches, so a result always reflects the
// notes on disk.
type FileCorpus struct {
	store *Store
}

// NewFileCorpus returns the Corpus that scans s's notes/ directory.
func NewFileCorpus(s *Store) *FileCorpus {
	return &FileCorpus{store: s}
}

// Notes is Store.LoadNotes.
func (c *FileCorpus) Notes() ([]*Note, []error) { return c.store.LoadNotes() }

// Stale is Store.AuditNote's verdict.
func (c *FileCorpus) Stale(n *Note) bool { return c.store.AuditNote(n).Stale() }

// Query is what Search ranks notes against.
type Query struct {
	// Files are slash-separated paths relative to the project root, the form
	// anchors are stored in. NormaliseSearchPath produces it from whatever a
	// caller was given.
	Files []string
	// Words are free text. Each is split into terms the same way a note is,
	// so `lock-resolution` and `lock resolution` are the same query.
	Words []string
	// Limit caps the number of results. Zero or negative means
	// DefaultSearchLimit.
	Limit int
}

// Result is one note Search ranked, with what a caller needs to decide
// whether to open it.
type Result struct {
	Name        string
	Kind        Kind
	Confidence  Confidence
	Description string
	// Anchors are the note's own anchor paths — globs as authored — that
	// cover at least one queried file, in the order the note lists them.
	Anchors []string
	// Score orders results within one search. It is not comparable across
	// searches: the weight of a file match depends on the query's words.
	Score int
	// Stale labels a note whose anchored content has diverged. It is a label
	// only; a stale note ranks exactly where a fresh one with the same score
	// would.
	Stale bool
	// Show is the command that opens the note. Opening it that way records
	// a Hit; Search records none.
	Show string
}

// Search ranks the corpus's notes against q and returns at most q.Limit
// results: which notes anchor the files q names, and which mention the words
// it names. It calls no model, builds and reads no index, and writes nothing
// — no Hit, no INDEX.md, no note — so it is safe on the path of a hook, and
// running it twice over the same tree returns the same list in the same
// order.
//
// A query that matches nothing returns an empty, non-nil list and a nil
// error; one that names no file and no word returns ErrEmptyQuery.
//
// Ranking:
//   - A note with an anchor covering a queried file ranks above every note
//     that matches on words alone, and one covering more of the queried
//     files ranks above one covering fewer. A concrete anchor covers the
//     path it names; a glob anchor covers what path.Match says it matches,
//     which is one directory level. A `**` anchor covers nothing (ADR 0005).
//   - Words score by field: a term in the name weighs nameWeight, in the
//     description descriptionWeight, in the body bodyWeight, each count
//     capped at TermCountCap per term per field.
//   - Ties break by name, ascending, so the order is total.
//
// Staleness is computed only for the notes returned, and never moves a
// note's rank.
//
// Every note the corpus could not read is reported on warn, one line each,
// and the search continues over the rest. A nil warn reports on os.Stderr,
// so an unreadable note is never dropped without a word.
func Search(c Corpus, q Query, warn io.Writer) ([]Result, error) {
	files := queryFiles(q.Files)
	terms := queryTerms(q.Words)
	if len(files) == 0 && len(terms) == 0 {
		return nil, ErrEmptyQuery
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if warn == nil {
		warn = os.Stderr
	}

	notes, errs := c.Notes()
	for _, e := range errs {
		fmt.Fprintf(warn, "warning: skipping unreadable note: %v\n", e)
	}

	// One file match outweighs the highest score words alone can reach, so
	// the file signal wins whatever the words say.
	fileWeight := len(terms)*TermCountCap*(nameWeight+descriptionWeight+bodyWeight) + 1

	type ranked struct {
		note    *Note
		anchors []string
		score   int
	}
	var hits []ranked
	for _, n := range notes {
		anchors, covered := coveringAnchors(n, files)
		score := covered*fileWeight + wordScore(n, terms)
		if score == 0 {
			continue
		}
		hits = append(hits, ranked{note: n, anchors: anchors, score: score})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].note.Name < hits[j].note.Name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}

	results := make([]Result, 0, len(hits))
	for _, h := range hits {
		results = append(results, Result{
			Name:        h.note.Name,
			Kind:        h.note.Kind,
			Confidence:  h.note.Confidence,
			Description: h.note.Description,
			Anchors:     h.anchors,
			Score:       h.score,
			Stale:       c.Stale(h.note),
			Show:        ShowCommand + " " + h.note.Name,
		})
	}
	return results, nil
}

// NormaliseSearchPath turns a path a caller typed into the project-relative,
// slash-separated form anchors are stored in. An absolute path is taken as
// is; any other path is relative to cwd. `./a`, `/project/a` and `a` typed
// from the project root therefore all come out as `a`.
//
// It is string work only: nothing is stat'ed and no symlink is resolved, so
// a path that does not exist normalises like one that does. A path that
// lands outside projectRoot, or on projectRoot itself, is an error wrapping
// ErrOutsideProject.
func NormaliseSearchPath(projectRoot, cwd, p string) (string, error) {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, abs)
	}
	rel, err := filepath.Rel(filepath.Clean(projectRoot), filepath.Clean(abs))
	if err != nil {
		return "", fmt.Errorf("%s: %w", p, ErrOutsideProject)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s: %w", p, ErrOutsideProject)
	}
	return rel, nil
}

// tokenize splits s into lower-cased terms at every rune that is neither a
// letter nor a digit. Notes and queries go through the same split, which is
// what lets a query word match the name `pins-shas` or the path fragment
// `graph.go` in a body.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// queryFiles cleans the queried paths and drops empty and repeated ones, so
// naming a file twice does not count it twice.
func queryFiles(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, f := range in {
		if f == "" {
			continue
		}
		f = path.Clean(filepath.ToSlash(f))
		if seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// queryTerms is every distinct term in words. Repeating a word in the query
// does not multiply its weight.
func queryTerms(words []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range words {
		for _, t := range tokenize(w) {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// coveringAnchors returns n's anchors that cover at least one of files, and
// how many of files are covered by any anchor.
func coveringAnchors(n *Note, files []string) ([]string, int) {
	if len(files) == 0 {
		return nil, 0
	}
	var anchors []string
	coveredFiles := map[string]bool{}
	for _, a := range n.Anchors {
		hit := false
		for _, f := range files {
			if anchorCovers(a, f) {
				hit = true
				coveredFiles[f] = true
			}
		}
		if hit {
			anchors = append(anchors, a.Path)
		}
	}
	return anchors, len(coveredFiles)
}

// anchorCovers reports whether a names file. Both are project-relative and
// slash-separated. An anchor the store cannot honour — `**`, absolute, or
// climbing out of the project — covers nothing.
func anchorCovers(a Anchor, file string) bool {
	if ValidateAnchorPath(a.Path) != nil {
		return false
	}
	if !a.IsGlob() {
		return path.Clean(a.Path) == file
	}
	ok, err := path.Match(a.Path, file)
	return err == nil && ok
}

// wordScore is n's weighted, capped term count over terms.
func wordScore(n *Note, terms []string) int {
	if len(terms) == 0 {
		return 0
	}
	want := make(map[string]int, len(terms))
	for i, t := range terms {
		want[t] = i
	}
	score := 0
	for _, field := range []struct {
		text   string
		weight int
	}{
		{n.Name, nameWeight},
		{n.Description, descriptionWeight},
		{n.Body, bodyWeight},
	} {
		counts := make([]int, len(terms))
		for _, tok := range tokenize(field.text) {
			if i, ok := want[tok]; ok && counts[i] < TermCountCap {
				counts[i]++
			}
		}
		for _, c := range counts {
			score += c * field.weight
		}
	}
	return score
}
