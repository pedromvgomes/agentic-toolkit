package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// nameRe is the shape of a note name: kebab-case, which keeps the name, the
// filename stem and the [[link]] target identical.
var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidateAnchorPath rejects paths and patterns the store cannot honour.
//
// `**` is the important one: filepath.Glob expands it as a single
// directory level, so `internal/**/*.go` would quietly anchor to one level
// and the note would look fully anchored while never noticing a file added
// deeper down — the exact failure the glob was written to catch.
//
// Anchors are also confined to the project: an absolute path, or one that
// climbs out with `..`, would record hashes — and, for a glob, the file
// names themselves — from outside the repo into a committed note.
func ValidateAnchorPath(path string) error {
	if strings.Contains(path, "**") {
		return fmt.Errorf("anchor %q: `**` is not supported — anchor one directory level, or list the files", path)
	}
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return fmt.Errorf("anchor %q: must be relative to the project root", path)
	}
	if cleaned := filepath.ToSlash(filepath.Clean(path)); cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("anchor %q: must stay inside the project root", path)
	}
	return nil
}

// Issue is one structural problem found by Lint.
type Issue struct {
	// File is the offending path, or the index when Note is empty.
	File    string
	Note    string
	Message string
}

// Lint validates the store's structure and the freshness of the generated
// index. It deliberately says nothing about whether a note is still TRUE:
// that is staleness, and failing CI on it would turn every rename in an
// unrelated PR red, which is how a memory store becomes the thing people
// route around.
func (s *Store) Lint(notes []*Note, parseErrs []error) []Issue {
	if !s.Exists() {
		// A repo that has not adopted memory is not a broken store: a hook
		// or CI job running lint there must pass. But a configured root that
		// does not exist is a typo, and staying green there would assert the
		// index is current for a store nobody is looking at.
		if s.RootIsExplicit {
			return []Issue{{
				File:    s.Root,
				Message: "configured memory root does not exist — check `memory.root` in the entry manifest",
			}}
		}
		return nil
	}

	var issues []Issue
	for _, err := range parseErrs {
		issues = append(issues, Issue{Message: err.Error()})
	}

	seen := map[string]string{}
	for _, n := range notes {
		issues = append(issues, s.lintNote(n)...)
		if n.Name == "" {
			// Already reported as a missing name; keying on "" would stack a
			// bogus duplicate on every further unnamed note.
			continue
		}
		if prev, dup := seen[n.Name]; dup {
			issues = append(issues, Issue{
				File: n.File, Note: n.Name,
				Message: fmt.Sprintf("duplicate name, also declared by %s", filepath.Base(prev)),
			})
			continue
		}
		seen[n.Name] = n.File
	}

	issues = append(issues, s.lintLayout()...)

	current, err := s.IndexCurrent(notes)
	switch {
	case err != nil:
		issues = append(issues, Issue{File: s.IndexPath(), Message: err.Error()})
	case !current:
		issues = append(issues, Issue{
			File:    s.IndexPath(),
			Message: "index is out of date — run `agtk memory index`",
		})
	}

	sort.SliceStable(issues, func(i, j int) bool { return issues[i].File < issues[j].File })
	return issues
}

// lintLayout flags note files parked in subdirectories, which LoadNotes
// does not walk: they would be silently absent from the index, from audit
// and from `show`.
func (s *Store) lintLayout() []Issue {
	entries, err := os.ReadDir(s.NotesPath())
	if err != nil {
		return nil
	}
	var issues []Issue
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		issues = append(issues, Issue{
			File:    filepath.Join(s.NotesPath(), e.Name()),
			Message: "notes/ is flat; notes in a subdirectory are never indexed or read",
		})
	}
	return issues
}

func (s *Store) lintNote(n *Note) []Issue {
	var issues []Issue
	add := func(format string, args ...any) {
		issues = append(issues, Issue{File: n.File, Note: n.Name, Message: fmt.Sprintf(format, args...)})
	}

	switch {
	case n.Name == "":
		add("missing `name`")
	case !nameRe.MatchString(n.Name):
		add("name %q is not kebab-case", n.Name)
	case n.Name != n.Stem():
		add("name %q does not match filename stem %q", n.Name, n.Stem())
	}

	if !n.Kind.Valid() {
		add("kind %q is not one of %s", n.Kind, joinKinds())
	}
	if !n.Confidence.Valid() {
		add("confidence %q is not one of %s", n.Confidence, joinConfidences())
	}

	switch {
	case strings.TrimSpace(n.Description) == "":
		add("missing `description`")
	case strings.Contains(strings.TrimSpace(n.Description), "\n"):
		add("description must be a single line")
	}

	if strings.TrimSpace(n.Body) == "" {
		add("note has no body")
	}

	if len(n.Anchors) == 0 {
		add("note has no anchors — every claim must carry a pointer")
	}
	for i, a := range n.Anchors {
		if err := ValidateAnchorPath(a.Path); err != nil {
			add("anchors[%d]: %v", i, err)
			continue
		}
		switch {
		case strings.TrimSpace(a.Path) == "":
			add("anchors[%d]: missing `path`", i)
		case a.IsGlob():
			if a.Blob != "" {
				add("anchors[%d] (%s): glob anchors carry `matches`, not `blob`", i, a.Path)
			}
			if len(a.Matches) == 0 {
				add("anchors[%d] (%s): %s", i, a.Path, s.unstampedHint(a.Path, true))
			}
		default:
			if len(a.Matches) > 0 {
				add("anchors[%d] (%s): `matches` is only for glob anchors", i, a.Path)
			}
			if a.Blob == "" {
				add("anchors[%d] (%s): %s", i, a.Path, s.unstampedHint(a.Path, false))
			}
		}
	}
	return issues
}

// unstampedHint distinguishes "nobody has run anchor yet" from "the
// anchored path is gone" — prescribing `agtk memory anchor` for the second
// sends the reader in a loop, because stamping cannot resurrect the file.
func (s *Store) unstampedHint(path string, glob bool) string {
	if glob {
		// globFiles, not filepath.Glob: stamping skips directories, so a
		// pattern matching only directories is "matched" to Glob and
		// "unmatched" to anchor — the advice loop this function exists to
		// avoid. A hard failure is surfaced for the same reason: `anchor`
		// would fail on it too.
		files, err := s.globFiles(path)
		switch {
		case err != nil:
			return err.Error()
		case len(files) == 0:
			return "matches no files — fix the pattern or drop the anchor"
		}
		return "unstamped — run `agtk memory anchor`"
	}
	info, err := os.Stat(s.abs(path))
	switch {
	case os.IsNotExist(err):
		return "anchored file no longer exists — fix the path or drop the anchor"
	case err == nil && info.IsDir():
		// `anchor` skips directories, so prescribing it here would send the
		// reader round a loop that can never go green.
		return "anchors a directory — name a file, or use a glob like " + path + "/*.go"
	case err == nil && !info.Mode().IsRegular():
		return "is not a regular file — fix the path or drop the anchor"
	}
	return "unstamped — run `agtk memory anchor`"
}

func joinKinds() string {
	parts := make([]string, 0, len(AllKinds))
	for _, k := range AllKinds {
		parts = append(parts, string(k))
	}
	return strings.Join(parts, ", ")
}

func joinConfidences() string {
	parts := make([]string, 0, len(AllConfidences))
	for _, c := range AllConfidences {
		parts = append(parts, string(c))
	}
	return strings.Join(parts, ", ")
}
