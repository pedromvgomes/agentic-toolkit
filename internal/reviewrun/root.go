package reviewrun

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// instructionNames are the paths never written into the review root.
//
// A coding-agent CLI reads these as instructions before it ever sees a prompt,
// by walking upward from where it runs. Claude Code can be told to load no
// settings at all; codex cannot, and takes AGENTS.override.md as ranking above
// the repository's own AGENTS.md. Filtering at write time rather than deleting
// afterwards means there is no window in which the file exists at all.
//
// A denylist rots — a vendor that adds a discovery filename opens the hole
// again silently — which is why the review workdir sits behind it. See ADR
// 0007.
var instructionNames = []string{
	"AGENTS.md",
	"AGENTS.override.md",
	"TEAM_GUIDE.md",
	".agents.md",
	"CLAUDE.md",
}

// instructionDirs are the directories never written into the review root.
// Codex takes a project-level .codex/config.toml into its configuration
// precedence, and .claude/ carries settings and agent definitions.
var instructionDirs = []string{
	".codex",
	".claude",
}

// modeSymlink and modeGitlink are the tree entry modes the review root refuses.
//
// Both make the root's defining property false. A symlink resolves wherever
// its target says, which is a path out of the copy and into the operator's
// real filesystem, and a gitlink names a second repository the copy does not
// contain.
const (
	modeSymlink = "120000"
	modeGitlink = "160000"
	modeExec    = "100755"
)

// treeEntry is one row of `git ls-tree -r -z`.
type treeEntry struct {
	Mode string
	Type string
	SHA  string
	Path string
}

// Skipped is one path the review root does not contain, and why.
//
// Reported rather than dropped silently: each one is a file the reviewers
// cannot see, which is a gap in coverage, and a review that quietly omits
// files reads exactly like a review that looked at everything.
type Skipped struct {
	Path   string
	Reason string
}

// Reasons a path is absent from the review root.
const (
	SkipInstructionFile = "instruction file: a CLI would read it as instructions before the prompt"
	SkipSymlink         = "symlink: following it leaves the review root"
	SkipGitlink         = "gitlink: a submodule is a second repository the copy does not hold"
	SkipUnsafePath      = "unsafe path: it does not stay inside the review root"
)

// parseLsTree reads `git ls-tree -r -z` output.
//
// Each record is `<mode> SP <type> SP <sha> TAB <path>`, NUL-terminated. NUL
// framing is what makes a record boundary impossible to forge from a path: a
// filename may contain a newline, and under a PR review the filenames come
// from a branch somebody else wrote.
//
// A record that does not have the shape is skipped rather than ending the
// walk, because the next boundary is still known. Refusing the whole tree on
// one malformed row would let a single crafted entry deny the review.
func parseLsTree(out []byte) []treeEntry {
	var entries []treeEntry
	for _, rec := range bytes.Split(out, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		tab := bytes.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		fields := strings.Fields(string(rec[:tab]))
		if len(fields) != 3 {
			continue
		}
		path := string(rec[tab+1:])
		if path == "" {
			continue
		}
		entries = append(entries, treeEntry{
			Mode: fields[0],
			Type: fields[1],
			SHA:  fields[2],
			Path: path,
		})
	}
	return entries
}

// safeRelPath reports whether a tree path can be joined onto the review root
// without leaving it.
//
// Git does not put `..` or an absolute path in a tree entry, so this closes a
// hole that should not exist. It is checked anyway because the cost of being
// wrong is a write outside the temporary directory, onto paths named by the
// branch under review.
func safeRelPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return false
	}
	if strings.ContainsRune(p, 0) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	// A backslash is a separator on Windows, so a path that is one component
	// to git may be several on disk.
	return !strings.ContainsRune(p, '\\')
}

// isInstruction reports whether a tree path is one the review root withholds.
func isInstruction(p string) bool {
	base := gitBase(p)
	for _, name := range instructionNames {
		if base == name {
			return true
		}
	}
	for _, dir := range instructionDirs {
		if p == dir || strings.HasPrefix(p, dir+"/") || strings.Contains(p, "/"+dir+"/") {
			return true
		}
	}
	return false
}

// gitBase is filepath.Base for a git path, which uses forward slashes on
// every platform.
func gitBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// classify splits a tree into the entries the review root writes and the ones
// it refuses.
func classify(entries []treeEntry) (write []treeEntry, skipped []Skipped) {
	for _, e := range entries {
		switch {
		case e.Mode == modeSymlink:
			skipped = append(skipped, Skipped{e.Path, SkipSymlink})
		case e.Mode == modeGitlink:
			skipped = append(skipped, Skipped{e.Path, SkipGitlink})
		case !safeRelPath(e.Path):
			skipped = append(skipped, Skipped{e.Path, SkipUnsafePath})
		case isInstruction(e.Path):
			skipped = append(skipped, Skipped{e.Path, SkipInstructionFile})
		case e.Type != "blob":
			// A tree row of any other type is not a file to write.
			continue
		default:
			write = append(write, e)
		}
	}
	return write, skipped
}

// writeBlobs materialises entries under dir, reading every blob in one
// `git cat-file --batch`.
//
// The batch is keyed by object id, not by path. Object ids are hex, so nothing
// a branch author chose reaches the request framing — which is what lets this
// use the newline-delimited form safely, where a path-keyed batch would need
// NUL framing to survive a filename containing a newline.
func writeBlobs(repoDir, dir string, entries []treeEntry) error {
	if len(entries) == 0 {
		return nil
	}
	var stdin bytes.Buffer
	for _, e := range entries {
		fmt.Fprintf(&stdin, "%s\n", e.SHA)
	}
	cmd := exec.Command("git", "cat-file", "--batch") // #nosec G204 -- fixed argv; object ids travel on stdin
	cmd.Dir = repoDir
	cmd.Stdin = &stdin
	cmd.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("read the tree's blobs: %w", err)
	}
	return spreadBatch(out, dir, entries)
}

// spreadBatch writes one `git cat-file --batch` response out as files.
//
// Records are `<sha> <type> <size>\n<contents>\n`, in the order asked for. The
// walk is positional, so a record that cannot be read ends it: unlike a head
// scan, a misaligned body here would write one file's contents under another
// file's name, and a reviewer reading that would report findings against code
// that does not exist.
func spreadBatch(out []byte, dir string, entries []treeEntry) error {
	rest := out
	for _, e := range entries {
		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			return fmt.Errorf("the tree's blobs ended before %s", e.Path)
		}
		header := string(rest[:nl])
		rest = rest[nl+1:]

		fields := strings.Fields(header)
		if len(fields) != 3 {
			return fmt.Errorf("git could not read the object for %s: %s", e.Path, header)
		}
		size, err := strconv.Atoi(fields[2])
		// Bounded before it is used as a length: a negative or oversized
		// count would slice past the response, and the count is read from a
		// stream rather than computed here.
		if err != nil || size < 0 || size > len(rest) {
			return fmt.Errorf("the object for %s does not declare a readable size: %s", e.Path, header)
		}
		body := rest[:size]
		rest = rest[size:]
		if len(rest) > 0 && rest[0] == '\n' {
			rest = rest[1:]
		}
		if err := writeFile(dir, e, body); err != nil {
			return err
		}
	}
	return nil
}

// writeFile puts one blob at its path under dir, creating parents.
func writeFile(dir string, e treeEntry, body []byte) error {
	// Checked again at the point of use rather than trusted from classify:
	// this is the call that actually touches the filesystem, and the path it
	// is handed was chosen by the branch under review.
	if !safeRelPath(e.Path) {
		return fmt.Errorf("refusing to write %q: it does not stay inside the review root", e.Path)
	}
	full := filepath.Join(dir, filepath.FromSlash(e.Path))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return err
	}
	perm := os.FileMode(0o600)
	if e.Mode == modeExec {
		perm = 0o700
	}
	return os.WriteFile(full, body, perm)
}
