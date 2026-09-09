// Package handoff decides which handoff documents a session may act on.
//
// A handoff drives implement-handoff, which dispatches subagents holding
// Write, Edit and Bash. The document therefore chooses this session's tasks,
// its file boundaries and the commands it runs, and the only thing standing
// between that and whoever wrote a branch is this package.
//
// The rule is that a handoff is written locally and never committed, so one
// git tracks arrived with a branch rather than from a session on this machine.
// Tracked-ness alone is not the test: git tracks paths, and a committed
// symlink at handoff/ makes the path handoff/x.md untracked while its content
// is entirely branch-authored. Structure decides, per ADR 0014 and the
// principle ADR 0007 states — a symlink is refused, never followed.
package handoff

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Dir is the directory a handoff lives in, relative to the worktree root.
const Dir = "handoff"

// Document is one handoff a session may act on.
type Document struct {
	// Path is the absolute path, with every segment a real directory or file.
	Path string
}

// Refused is one candidate this package will not hand over, and why.
//
// Reported rather than dropped, for the reason reviewrun reports a skipped
// path: a document silently withheld and a directory holding nothing produce
// the same empty list, and only one of them means there is no work waiting.
type Refused struct {
	Path   string
	Reason string
}

// Reasons a candidate is refused.
const (
	RefusedTracked      = "git tracks it: a handoff is written locally, so a committed one arrived with a branch"
	RefusedSymlink      = "symlink: following it leaves the handoff directory"
	RefusedIrregular    = "not a regular file"
	RefusedSymlinkedDir = "the handoff directory is a symlink: everything under it resolves somewhere this worktree does not control"
	RefusedNestedRepo   = "the handoff directory is its own git repository: this worktree's index says nothing about what is inside it"
	RefusedCaseAlias    = "no directory is named `handoff` exactly: a name reachable only by case-folding is one git's index spells differently"
)

// List reports the handoffs waiting in root, and what it refused.
//
// A refusal of the directory itself returns no documents and one Refused
// naming it, because nothing under a symlinked handoff/ can be trusted
// individually — the link decides what every entry resolves to.
func List(root string) ([]Document, []Refused, error) {
	if root == "" {
		return nil, nil, fmt.Errorf("no worktree root given")
	}
	dir := filepath.Join(root, Dir)

	// The directory has to be named `handoff` exactly, as the filesystem
	// spells it, and that is established by reading the parent rather than by
	// joining a constant. On a case-insensitive filesystem — macOS by default,
	// which is where this is developed — a committed `Handoff/` answers to the
	// path `handoff/`, while git's index is case-sensitive and holds
	// `Handoff/task.md`. Asking about `handoff/task.md` then finds no entry
	// and the document reads as untracked: a branch-authored handoff, advertised
	// as local work.
	if !hasExactly(root, Dir) {
		if _, err := os.Lstat(dir); err == nil {
			return nil, []Refused{{Path: dir, Reason: RefusedCaseAlias}}, nil
		}
		return nil, nil, nil
	}

	// Lstat, not Stat: Stat follows the link and reports the target, which is
	// the question this is asking about.
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, []Refused{{Path: dir, Reason: RefusedSymlinkedDir}}, nil
	}
	if !info.IsDir() {
		return nil, []Refused{{Path: dir, Reason: RefusedIrregular}}, nil
	}

	// A gitlink is refused for the reason a review root refuses one: it is a
	// second repository this one does not contain. A committed submodule at
	// handoff/ holds real regular files in a real directory, while the outer
	// index carries only the gitlink — so ls-files reports every path inside
	// it as untracked, and every branch-authored document in it reads as
	// locally written.
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return nil, []Refused{{Path: dir, Reason: RefusedNestedRepo}}, nil
	}

	// Entries are read from the real directory, so a link anywhere above
	// handoff/ is resolved once here rather than being trusted per entry.
	// Containment then needs no separate test: os.ReadDir yields base names,
	// which carry no separator, and a name that is itself a link is refused
	// below rather than resolved.
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve %s: %w", dir, err)
	}
	dir = realDir

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var (
		docs    []Document
		refused []Refused
	)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		path := filepath.Join(dir, name)

		// Depth one throughout: handoff/done/ is consumed work and never a
		// candidate, and a directory named *.md is not a document.
		fi, err := os.Lstat(path)
		if err != nil {
			refused = append(refused, Refused{Path: path, Reason: RefusedIrregular})
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			refused = append(refused, Refused{Path: path, Reason: RefusedSymlink})
			continue
		}
		if !fi.Mode().IsRegular() {
			refused = append(refused, Refused{Path: path, Reason: RefusedIrregular})
			continue
		}
		if tracked(root, path) {
			refused = append(refused, Refused{Path: path, Reason: RefusedTracked})
			continue
		}
		docs = append(docs, Document{Path: path})
	}

	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	sort.Slice(refused, func(i, j int) bool { return refused[i].Path < refused[j].Path })
	return docs, refused, nil
}

// tracked reports whether git has this path in the index.
//
// Only a clean exit means tracked, and only exit 1 means untracked: that is
// the status `--error-unmatch` uses to say "no such path in the index". Every
// other status is git declining to answer — 128 is what a directory that is
// not a repository returns — and an unanswered question is read as tracked.
//
// Failing that way round is the whole point. The answer decides whether a
// document chooses what subagents holding Write and Bash are told to do, so
// the cost of being wrong is a handoff somebody has to re-create, against a
// branch choosing this session's commands.
func tracked(root, path string) bool {
	cmd := exec.Command("git", "ls-files", "--error-unmatch", "--", path)
	cmd.Dir = root
	err := cmd.Run()
	if err == nil {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false
	}
	return true
}

// hasExactly reports whether dir holds an entry named exactly name.
//
// os.Lstat answers a case-insensitive filesystem's question, not this one:
// it resolves `handoff` to a directory called `Handoff` and reports success.
// Reading the parent is the only way to learn how the name is actually
// spelled, and the spelling is what git's index is keyed by.
func hasExactly(dir, name string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}
