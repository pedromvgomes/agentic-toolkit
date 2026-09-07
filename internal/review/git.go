package review

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// git runs one git command in dir and returns its stdout.
//
// Every argument list here is built from constants and from refs the invoker
// named, and each ends with `--` before any path, because under a PR review
// the paths come from a branch somebody else wrote.
func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...) // #nosec G204 -- arguments are built here, never interpolated from a diff
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// DiffFile is one file's line counts in a change, before any judgement about
// whether it is worth reviewing.
type DiffFile struct {
	// Path is the file's name at the head of the range.
	Path string
	// OldPath is set when the file was renamed, and is its name at the base.
	OldPath string
	// Added and Removed are the line counts git reported. A pure rename
	// reports zero of each: the mechanical move is not a changed line.
	Added   int
	Removed int
	// Binary reports that git counted no lines because it could not.
	Binary bool
}

// Renamed reports whether the file moved.
func (f DiffFile) Renamed() bool { return f.OldPath != "" }

// Changed is the file's contribution to a change's line count.
func (f DiffFile) Changed() int { return f.Added + f.Removed }

// rangeArg renders a base and head as the argument git diff takes.
//
// An empty head means the working tree, which is what a pre-push review looks
// at: the change as it stands, including what has not been committed.
//
// With a head, the range is `base...head` — the two-dot form would count every
// commit that landed on the base since the branch left it, which is other
// people's work wearing this change's name.
func rangeArg(base, head string) string {
	if head == "" {
		return base
	}
	return base + "..." + head
}

// DiffRange returns the per-file line counts between base and head.
func DiffRange(dir, base, head string) ([]DiffFile, error) {
	out, err := git(dir, "diff", "-M", "-z", "--numstat", rangeArg(base, head), "--")
	if err != nil {
		return nil, err
	}
	return parseNumstat(out)
}

// parseNumstat reads `git diff -z --numstat` output.
//
// The NUL-separated form is the only one a rename can be read out of without
// guessing: the human-readable form renders `old => new` inside the path
// field, and a filename containing that arrow is indistinguishable from a
// move. Here a rename is simply two path records instead of one.
func parseNumstat(out []byte) ([]DiffFile, error) {
	records := strings.Split(string(out), "\x00")
	var files []DiffFile

	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\t")
		if len(fields) < 3 {
			return nil, fmt.Errorf("numstat record %q has %d fields, want 3", rec, len(fields))
		}

		f := DiffFile{}
		added, removed := fields[0], fields[1]
		if added == "-" || removed == "-" {
			f.Binary = true
		} else {
			var err error
			if f.Added, err = strconv.Atoi(added); err != nil {
				return nil, fmt.Errorf("numstat record %q: added count: %w", rec, err)
			}
			if f.Removed, err = strconv.Atoi(removed); err != nil {
				return nil, fmt.Errorf("numstat record %q: removed count: %w", rec, err)
			}
		}

		// An empty path field means the next two records are the old and new
		// names of a rename.
		if fields[2] == "" {
			if i+2 >= len(records) {
				return nil, fmt.Errorf("numstat record %q announces a rename with no paths after it", rec)
			}
			f.OldPath = records[i+1]
			f.Path = records[i+2]
			i += 2
		} else {
			f.Path = fields[2]
		}
		files = append(files, f)
	}
	return files, nil
}

// UntrackedFiles lists the files git does not track and .gitignore does not
// exclude.
//
// A working-tree review that skipped them would omit every file the change
// added, which is the half of a change a review most needs to see — and it
// would do so silently, reporting a smaller change rather than an incomplete
// one.
func UntrackedFiles(dir string) ([]string, error) {
	out, err := git(dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// Patch returns the diff text for the named paths, which is what signal
// detection reads: a signal like concurrency is a property of the lines a
// change touched, not of the files it touched.
func Patch(dir, base, head string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	args := append([]string{"diff", "-M", rangeArg(base, head), "--"}, paths...)
	out, err := git(dir, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// MergeBase returns the commit base and head diverged from, which is what a
// range is anchored to.
func MergeBase(dir, base, head string) (string, error) {
	if head == "" {
		head = "HEAD"
	}
	out, err := git(dir, "merge-base", base, head)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// baseCandidates are the refs a change is measured against when nobody named
// one, in the order they are tried.
//
// The remote's own idea of its default branch comes first, because it is the
// only candidate that is a fact rather than a convention. The rest are the
// conventions, and a repo matching none of them gets a refusal naming what was
// tried — guessing at that point would silently measure a change against the
// wrong base and report a size nobody could reproduce.
var baseCandidates = []string{"origin/HEAD", "origin/main", "origin/master", "main", "master"}

// DetectBase finds the ref a change should be measured against.
func DetectBase(dir string) (string, error) {
	for _, ref := range baseCandidates {
		if _, err := git(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			return ref, nil
		}
	}
	return "", fmt.Errorf("no base branch found; tried %s. Name one with --base",
		strings.Join(baseCandidates, ", "))
}

// RepoRoot returns the working tree dir contains.
func RepoRoot(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Hunk is one changed region, addressed in the pre-image — the file as it was
// at the base, which is where a deleted line still exists and still has a
// history.
type Hunk struct {
	// File is the path at the head of the range.
	File string
	// Start and Length address the region in the pre-image. Length is zero
	// for a hunk that only adds, which has no history to have undone.
	Start  int
	Length int
}

// hunkHeaderRE reads `@@ -start,len +start,len @@`. A missing length is one
// line, which is what the unified format's shorthand means.
var hunkHeaderRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+\d+(?:,\d+)? @@`)

// Hunks reads the changed regions out of a unified diff.
func Hunks(patch string) []Hunk {
	var out []Hunk
	file := ""
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			file = strings.TrimPrefix(line, "+++ b/")
			continue
		}
		m := hunkHeaderRE.FindStringSubmatch(line)
		if m == nil || file == "" {
			continue
		}
		start, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		length := 1
		if m[2] != "" {
			if length, err = strconv.Atoi(m[2]); err != nil {
				continue
			}
		}
		out = append(out, Hunk{File: file, Start: start, Length: length})
	}
	return out
}

// LineHistory returns the subjects of the commits that last wrote a region of
// a file, oldest last.
//
// `-L` addresses lines rather than a file, which is the difference between
// asking "was this code a repair" and "has this file ever been repaired". The
// second is true of nearly every file in a mature repo, so a signal built on
// it would fire always and mean nothing.
func LineHistory(dir, rev, file string, start, length int, limit int) ([]string, error) {
	if length < 1 {
		return nil, nil
	}
	spec := fmt.Sprintf("%d,%d:%s", start, start+length-1, file)
	out, err := git(dir, "log", "-L", spec, "--format=%s", "-s", "-n", strconv.Itoa(limit), rev)
	if err != nil {
		return nil, err
	}
	var subjects []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			subjects = append(subjects, line)
		}
	}
	return subjects, nil
}

// FileHeads reads the opening bytes of each path, at rev or — when rev is
// empty — from the working tree.
//
// One process for the whole set. The alternative is `git show` per file, which
// on a change big enough to need this reading is hundreds of spawns to answer
// a question about hundreds of first lines.
func FileHeads(dir, rev string, paths []string, limit int) map[string]string {
	heads := make(map[string]string, len(paths))
	if len(paths) == 0 {
		return heads
	}

	if rev == "" {
		for _, p := range paths {
			body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p))) // #nosec G304 -- reads a file git listed inside the repository
			if err != nil {
				continue
			}
			if len(body) > limit {
				body = body[:limit]
			}
			heads[p] = string(body)
		}
		return heads
	}

	var stdin bytes.Buffer
	for _, p := range paths {
		fmt.Fprintf(&stdin, "%s:%s\n", rev, p)
	}
	cmd := exec.Command("git", "cat-file", "--batch") // #nosec G204 -- fixed argv; the paths travel on stdin
	cmd.Dir = dir
	cmd.Stdin = &stdin
	out, err := cmd.Output()
	if err != nil {
		// Unreadable heads cost the content marker and nothing else: the name
		// patterns and the repo's own .gitattributes still classify.
		return heads
	}

	// Records are `<sha> <type> <size>\n<contents>\n`, in the order asked for.
	// A missing path answers `<spec> missing\n` and consumes no body.
	rest := out
	for _, p := range paths {
		nl := bytes.IndexByte(rest, '\n')
		if nl < 0 {
			break
		}
		header := string(rest[:nl])
		rest = rest[nl+1:]
		fields := strings.Fields(header)
		if len(fields) != 3 {
			// A `missing` or `ambiguous` answer: no body follows.
			continue
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size > len(rest) {
			break
		}
		body := rest[:size]
		if len(body) > limit {
			body = body[:limit]
		}
		heads[p] = string(body)
		// The body is followed by a newline the size does not count.
		rest = rest[size:]
		if len(rest) > 0 && rest[0] == '\n' {
			rest = rest[1:]
		}
	}
	return heads
}

// headBytes is how much of a file is read to look for a generated marker.
// Every convention that defines one puts it in a header, and reading further
// would let a file that merely discusses the phrase exclude itself.
const headBytes = 4096
