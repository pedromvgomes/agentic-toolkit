package reviewrun

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
)

// Root is the code under review, written outside the project directory, and
// the empty directory the children actually run in.
//
// The two are siblings and neither is the other: everything under Code is
// material a run opens by absolute path, and Work is where the process sits so
// that walking upward from it finds no project at all. See ADR 0007.
type Root struct {
	// Code is the review root: a copy of the reviewed tree.
	Code string
	// Work is the review workdir: empty, and in no repository.
	Work string
	// Skipped lists what the tree held and the copy does not.
	Skipped []Skipped
	// Files is how many blobs were written.
	Files int

	parent string
}

// Close removes everything the root occupies.
//
// Called from a defer on every exit path including cancellation: the tree is
// written into the system temporary directory, and a review that was
// interrupted has no later opportunity to tidy up after itself.
func (r *Root) Close() error {
	if r == nil || r.parent == "" {
		return nil
	}
	return os.RemoveAll(r.parent)
}

// resolveTree returns the tree-ish the review root is written from.
//
// A named head is that commit's tree. An empty head means the working tree,
// which is what a pre-push review looks at, and `git stash create` is how a
// dirty tree becomes an object without touching the stash stack — `git stash
// push` would, and the stack is shared with every other worktree and every
// other session on this machine.
//
// A clean tree makes `stash create` print nothing, which is not a failure: it
// means the working tree is HEAD, so HEAD is the answer.
func resolveTree(dir, head string) (string, error) {
	if head != "" {
		return head + "^{tree}", nil
	}
	out, err := git(dir, "stash", "create")
	if err != nil {
		return "", fmt.Errorf("capture the working tree: %w", err)
	}
	if commit := strings.TrimSpace(string(out)); commit != "" {
		return commit + "^{tree}", nil
	}
	return "HEAD^{tree}", nil
}

// BuildRoot writes the reviewed tree to a temporary directory and returns it
// alongside the empty directory the runs will use as their working directory.
//
// Written from `git ls-tree` rather than checked out. A `git worktree` leaves
// a `.git` in the root — the whole repository behind a directory a sandbox has
// no reason to refuse — and registers an entry to be pruned from the
// operator's checkout afterwards. `git archive` reads the reviewed head's own
// `.gitattributes`, so `export-ignore` lets a branch hide files from the
// review and `export-subst` lets it rewrite them. Writing the files ourselves
// means our code is the only thing that decides what lands.
func BuildRoot(dir, head string) (*Root, error) {
	return buildRoot(dir, head, true)
}

// PlanRoot resolves the tree and classifies it without writing a single blob.
//
// A preview needs the root's paths and the list of what the copy would not
// hold, both of which come from the tree listing. Writing the bytes as well
// would put an entire source tree on disk for a run that starts no process and
// reads none of it.
func PlanRoot(dir, head string) (*Root, error) {
	return buildRoot(dir, head, false)
}

func buildRoot(dir, head string, materialise bool) (*Root, error) {
	tree, err := resolveTree(dir, head)
	if err != nil {
		return nil, err
	}

	parent, err := os.MkdirTemp("", "agtk-review-")
	if err != nil {
		return nil, err
	}
	root := &Root{
		parent: parent,
		Code:   filepath.Join(parent, "root"),
		Work:   filepath.Join(parent, "work"),
	}
	for _, d := range []string{root.Code, root.Work} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			_ = root.Close()
			return nil, err
		}
	}

	out, err := git(dir, "ls-tree", "-r", "-z", tree)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("list the reviewed tree: %w", err)
	}
	write, skipped := classifyEntries(parseLsTree(out))
	root.Skipped = skipped
	root.Files = len(write)

	if materialise {
		if err := writeBlobs(dir, root.Code, write); err != nil {
			_ = root.Close()
			return nil, err
		}
	}

	// A working-tree review is not fully described by any tree object:
	// `stash create` records tracked modifications and nothing else, so a file
	// the change adds and has not staged is missing from it. Those are exactly
	// the files a pre-push review most needs to look at.
	if head == "" {
		if err := copyUntracked(dir, root, materialise); err != nil {
			_ = root.Close()
			return nil, err
		}
	}
	return root, nil
}

// copyUntracked adds the reviewable untracked files to the review root.
//
// The same enumeration the change profile uses, so a file the review reports
// on and a file the reviewers can read are one list. Ignored files are not in
// it, and neither is anything that is not a regular file — a symlink is
// refused here for the reason it is refused in the tree.
func copyUntracked(dir string, root *Root, materialise bool) error {
	paths, err := review.UntrackedFiles(dir)
	if err != nil {
		return err
	}
	for _, p := range paths {
		switch {
		case !safeRelPath(p):
			root.Skipped = append(root.Skipped, Skipped{p, SkipUnsafePath})
			continue
		case isInstruction(p):
			root.Skipped = append(root.Skipped, Skipped{p, SkipInstructionFile})
			continue
		}
		src := filepath.Join(dir, filepath.FromSlash(p))
		info, err := os.Lstat(src)
		if err != nil {
			// Named and not dropped, for the reason Skipped exists: a file the
			// reviewers cannot see is a gap in coverage, and a review that
			// quietly omits one reads exactly like a review that looked at
			// everything.
			root.Skipped = append(root.Skipped, Skipped{p, SkipUnreadable})
			continue
		}
		if !info.Mode().IsRegular() {
			root.Skipped = append(root.Skipped, Skipped{p, SkipSymlink})
			continue
		}
		body, err := os.ReadFile(src) // #nosec G304 -- a regular file git listed inside the repository
		if err != nil {
			root.Skipped = append(root.Skipped, Skipped{p, SkipUnreadable})
			continue
		}
		mode := "100644"
		if info.Mode().Perm()&0o100 != 0 {
			mode = modeExec
		}
		if materialise {
			if err := writeFile(root.Code, treeEntry{Mode: mode, Path: p}, body); err != nil {
				return err
			}
		}
		root.Files++
	}
	return nil
}

// git runs one git command in dir and returns its stdout.
//
// GIT_LITERAL_PATHSPECS is forced for the reason internal/review forces it:
// `--` ends option parsing but not pathspec magic, and under a PR review the
// paths come from a branch somebody else wrote.
func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...) // #nosec G204 -- arguments are built here, never interpolated from a diff
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1")
	out, err := cmd.Output()
	if err != nil {
		msg := err.Error()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if s := strings.TrimSpace(string(exitErr.Stderr)); s != "" {
				msg = s
			}
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}
