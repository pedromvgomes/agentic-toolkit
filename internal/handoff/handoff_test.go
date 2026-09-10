package handoff

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo builds a worktree with a git repository in it. Each test gets its own:
// the package has no shared fixture, and one test's commit is another's
// surprise.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return root
}

func commit(t *testing.T, root, message string) {
	t.Helper()
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", message}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// reasons returns the refusal reason per base name, so an assertion names the
// file rather than an index into a slice.
func reasons(refused []Refused) map[string]string {
	out := map[string]string{}
	for _, r := range refused {
		out[filepath.Base(r.Path)] = r.Reason
	}
	return out
}

func names(docs []Document) []string {
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		out = append(out, filepath.Base(d.Path))
	}
	return out
}

// The ordinary case: a document a session on this machine wrote and never
// committed is the one thing this hands over.
func TestAnUntrackedRegularFileIsHandedOver(t *testing.T) {
	root := repo(t)
	write(t, filepath.Join(root, Dir, "task.md"), "# do the thing\n")

	docs, refused, err := List(root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := names(docs); len(got) != 1 || got[0] != "task.md" {
		t.Errorf("want task.md handed over, got %v (refused %v)", got, reasons(refused))
	}
}

// A committed handoff arrived with a branch, and acting on it lets whoever
// wrote that branch choose what subagents holding Bash are told to do.
func TestATrackedHandoffIsRefused(t *testing.T) {
	root := repo(t)
	write(t, filepath.Join(root, Dir, "task.md"), "# do the thing\n")
	commit(t, root, "commit the handoff")

	docs, refused, err := List(root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("a tracked handoff was handed over: %v", names(docs))
	}
	if got := reasons(refused)["task.md"]; got != RefusedTracked {
		t.Errorf("task.md refused as %q, want the tracked reason", got)
	}
}

// The bypass this package exists to close. git tracks `handoff` and
// `real/task.md`; it does not track the path `handoff/task.md`, so a check
// that asked only about tracked-ness reads a branch-authored document as
// locally written.
func TestACommittedSymlinkAsTheHandoffDirectoryIsRefused(t *testing.T) {
	root := repo(t)
	write(t, filepath.Join(root, "real", "task.md"), "# attacker chosen\n")
	if err := os.Symlink("real", filepath.Join(root, Dir)); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "commit a symlinked handoff directory")

	docs, refused, err := List(root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("a symlinked handoff directory handed over %v", names(docs))
	}
	if len(refused) != 1 || refused[0].Reason != RefusedSymlinkedDir {
		t.Errorf("the directory itself was not refused: %v", reasons(refused))
	}
}

// The same bypass one level down: the directory is real and the document is a
// link into committed content.
func TestASymlinkedHandoffFileIsRefused(t *testing.T) {
	root := repo(t)
	write(t, filepath.Join(root, "real", "task.md"), "# attacker chosen\n")
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real", "task.md"), filepath.Join(root, Dir, "task.md")); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "commit a symlinked handoff file")

	docs, refused, err := List(root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("a symlinked handoff was handed over: %v", names(docs))
	}
	if got := reasons(refused)["task.md"]; got != RefusedSymlink {
		t.Errorf("task.md refused as %q, want the symlink reason", got)
	}
}

// A committed submodule at handoff/ holds real regular files in a real
// directory, and the outer index carries only the gitlink — so ls-files
// reports every path inside it as untracked and each branch-authored document
// reads as locally written. The same class a review root refuses as a gitlink.
func TestANestedRepositoryAtTheHandoffDirectoryIsRefused(t *testing.T) {
	root := repo(t)
	inner := filepath.Join(root, Dir)
	write(t, filepath.Join(inner, "task.md"), "# attacker chosen\n")
	cmd := exec.Command("git", "init", "-q", ".")
	cmd.Dir = inner
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init in %s: %v: %s", inner, err, out)
	}

	docs, refused, err := List(root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("a nested repository handed over %v", names(docs))
	}
	if len(refused) != 1 || refused[0].Reason != RefusedNestedRepo {
		t.Errorf("the nested repository was not refused: %v", reasons(refused))
	}
}

// On a case-insensitive filesystem — macOS by default, which is where this is
// developed — a committed `Handoff/` answers to the path `handoff/`, while
// git's index is case-sensitive and holds `Handoff/task.md`. Asking about
// `handoff/task.md` finds no entry, so the document would read as untracked.
func TestACaseAliasedHandoffDirectoryIsRefused(t *testing.T) {
	root := repo(t)
	write(t, filepath.Join(root, "Handoff", "task.md"), "# attacker chosen\n")
	commit(t, root, "commit a capitalised handoff directory")

	if _, err := os.Lstat(filepath.Join(root, Dir)); err != nil {
		t.Skip("this filesystem is case-sensitive, so the alias cannot arise")
	}

	docs, refused, err := List(root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("a case-aliased handoff directory handed over %v", names(docs))
	}
	if len(refused) != 1 || refused[0].Reason != RefusedCaseAlias {
		t.Errorf("the case alias was not refused: %v", reasons(refused))
	}
}

// git's index is case-sensitive and a filesystem need not be, so the on-disk
// spelling and the indexed one can differ in either direction. A checkout of
// `handoff/Task.md` where `task.md` already exists writes the existing file
// and leaves the on-disk name lowercase; the reverse happens just as easily.
// Either way, asking git about the on-disk spelling finds no entry and the
// branch's content reads as locally written.
func TestATrackedNameIsRefusedWhateverCaseItIsOnDisk(t *testing.T) {
	for name, tc := range map[string]struct{ onDisk, indexed string }{
		"lowercase on disk, capitalised in the index": {onDisk: "task.md", indexed: Dir + "/Task.md"},
		"capitalised on disk, lowercase in the index": {onDisk: "Task.md", indexed: Dir + "/task.md"},
		// The directory carries the alias just as easily, and a pathspec is
		// matched case-sensitively too — so restricting the query to
		// `-- handoff` would miss this entirely.
		"the directory is what differs": {onDisk: "task.md", indexed: "Handoff/task.md"},
	} {
		t.Run(name, func(t *testing.T) {
			root := repo(t)
			write(t, filepath.Join(root, "README.md"), "x\n")
			commit(t, root, "init")

			path := filepath.Join(root, Dir, tc.onDisk)
			write(t, path, "# attacker chosen\n")
			blob := run(t, root, "hash-object", "-w", path)
			run(t, root, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+tc.indexed)

			docs, refused, err := List(root)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(docs) != 0 {
				t.Fatalf("a case-variant of a tracked name was handed over: %v", names(docs))
			}
			if got := reasons(refused)[tc.onDisk]; got != RefusedTracked {
				t.Errorf("%s refused as %q, want the tracked reason", tc.onDisk, got)
			}
		})
	}
}

// A git that will not answer refuses everything: the answer decides whether a
// document directs subagents holding Write, Edit and Bash.
func TestAnUnanswerableGitRefusesEveryCandidate(t *testing.T) {
	root := t.TempDir() // not a repository, so ls-files cannot answer
	write(t, filepath.Join(root, Dir, "task.md"), "# do the thing\n")

	docs, refused, err := List(root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("a document was handed over though tracked-ness was unknown: %v", names(docs))
	}
	if got := reasons(refused)["task.md"]; got != RefusedTracked {
		t.Errorf("task.md refused as %q, want the tracked reason", got)
	}
}

// Consumed work is not a candidate, and the walk is depth one, so a directory
// under handoff/ is never descended into.
func TestConsumedHandoffsAreNotCandidates(t *testing.T) {
	root := repo(t)
	write(t, filepath.Join(root, Dir, "done", "old.md"), "# finished\n")
	write(t, filepath.Join(root, Dir, "task.md"), "# current\n")

	docs, _, err := List(root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := names(docs); len(got) != 1 || got[0] != "task.md" {
		t.Errorf("want only task.md, got %v", got)
	}
}

// A regular file named handoff/ is not a directory of documents. Refused and
// named rather than ignored, so the reason a session sees nothing is visible.
func TestAHandoffPathThatIsNotADirectoryIsRefused(t *testing.T) {
	root := repo(t)
	write(t, filepath.Join(root, Dir), "not a directory\n")

	docs, refused, err := List(root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 0 {
		t.Fatalf("a file named handoff handed over %v", names(docs))
	}
	if len(refused) != 1 || refused[0].Reason != RefusedIrregular {
		t.Errorf("a non-directory handoff was not refused: %v", reasons(refused))
	}
}

// A worktree with no handoff directory is the common case and is not an error.
func TestNoHandoffDirectoryIsNotAnError(t *testing.T) {
	root := repo(t)
	docs, refused, err := List(root)
	if err != nil {
		t.Fatalf("a missing handoff directory was an error: %v", err)
	}
	if len(docs) != 0 || len(refused) != 0 {
		t.Errorf("want nothing, got %v / %v", names(docs), reasons(refused))
	}
}

// run executes one git command in root and returns its trimmed stdout.
func run(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}
