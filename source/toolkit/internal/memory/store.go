package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store is one memory store on disk.
//
// ProjectRoot is what anchor paths are relative to. It is separate from
// Root because `memory.root` is configurable: the store may sit anywhere,
// but a note that says `internal/resolver/graph.go` always means that path
// from the project root, so notes stay portable if the store moves.
type Store struct {
	Root        string
	ProjectRoot string

	// RootIsExplicit records that the root came from `memory.root` rather
	// than the default. Lint treats a missing store differently in each
	// case: not adopted yet, versus configured and not there.
	RootIsExplicit bool
}

// New builds a Store. root may be absolute or relative to projectRoot; an
// empty root means DefaultRoot.
func New(projectRoot, root string) *Store {
	explicit := root != ""
	if root == "" {
		root = DefaultRoot
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(projectRoot, root)
	}
	return &Store{Root: root, ProjectRoot: projectRoot, RootIsExplicit: explicit}
}

// ValidateRoot rejects a configured `memory.root` that would put the store
// outside the repo. The store is meant to be committed and to travel with
// its branch; an absolute or climbing path silently defeats both, the same
// way an escaping anchor path would.
func ValidateRoot(root string) error {
	if root == "" {
		return nil
	}
	if filepath.IsAbs(root) || strings.HasPrefix(root, "/") {
		return fmt.Errorf("memory.root %q: must be relative to the entry manifest", root)
	}
	if cleaned := filepath.ToSlash(filepath.Clean(root)); cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("memory.root %q: must stay inside the repo", root)
	}
	return nil
}

// IndexPath, NotesPath, CandidatesPath and HitsPath locate the store's
// fixed members.
func (s *Store) IndexPath() string      { return filepath.Join(s.Root, IndexFile) }
func (s *Store) NotesPath() string      { return filepath.Join(s.Root, NotesDir) }
func (s *Store) CandidatesPath() string { return filepath.Join(s.Root, CandidatesDir) }
func (s *Store) HitsPath() string       { return filepath.Join(s.Root, HitsFile) }

// Exists reports whether the store directory is present.
func (s *Store) Exists() bool {
	info, err := os.Stat(s.Root)
	return err == nil && info.IsDir()
}

// Scaffold creates the store's directories and the .gitignore that keeps
// local hit telemetry out of commits. Idempotent.
func (s *Store) Scaffold() error {
	for _, dir := range []string{s.Root, s.NotesPath(), s.CandidatesPath()} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	// An empty directory does not survive a clone, and candidates/ has to
	// exist for an explorer to stage into it on a fresh checkout.
	keep := filepath.Join(s.CandidatesPath(), GitkeepFile)
	if _, err := os.Stat(keep); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(keep, nil, 0o644); err != nil { // #nosec G306 -- committed placeholder
			return fmt.Errorf("write %s: %w", keep, err)
		}
	}

	return s.ensureGitignore()
}

// ensureGitignore writes the store's .gitignore if it is not already there,
// creating the store directory as needed.
func (s *Store) ensureGitignore() error {
	if err := os.MkdirAll(s.Root, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", s.Root, err)
	}
	gitignore := filepath.Join(s.Root, GitignoreFile)
	if _, err := os.Stat(gitignore); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(gitignore, []byte(HitsFile+"\n"), 0o644); err != nil { // #nosec G306 -- committed source
			return fmt.Errorf("write %s: %w", gitignore, err)
		}
	}
	return nil
}

// LoadNotes parses every *.md under notes/, sorted by name. Parse failures
// are collected rather than returned as a single error: one malformed note
// must not hide the rest of the store from index, audit or lint.
func (s *Store) LoadNotes() ([]*Note, []error) {
	entries, err := os.ReadDir(s.NotesPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("read %s: %w", s.NotesPath(), err)}
	}

	var (
		notes []*Note
		errs  []error
	)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), NoteExt) {
			continue
		}
		file := filepath.Join(s.NotesPath(), e.Name())
		raw, err := os.ReadFile(file) // #nosec G304 -- reads notes from the store the invoker pointed at
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", file, err))
			continue
		}
		n, err := ParseNote(file, raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		notes = append(notes, n)
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Name < notes[j].Name })
	return notes, errs
}

// Stem is the note's filename without extension — what lint checks the
// `name:` field against.
func (n *Note) Stem() string {
	return strings.TrimSuffix(filepath.Base(n.File), NoteExt)
}

// abs resolves a project-relative anchor path to an absolute one.
func (s *Store) abs(rel string) string {
	return filepath.Join(s.ProjectRoot, filepath.FromSlash(rel))
}

// rel is the inverse of abs: an absolute path as a slash-separated,
// project-relative anchor path.
func (s *Store) rel(abs string) string {
	r, err := filepath.Rel(s.ProjectRoot, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(r)
}
