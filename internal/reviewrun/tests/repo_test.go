package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo is a throwaway git repository a profile can be built from.
//
// Real git, rather than fixtures: the profile's whole job is reading what git
// reports, and a fixture would be this package's idea of that rather than
// git's. Renames, binary detection and untracked listing are exactly the
// places the two would differ.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-b", "main")
	r.git("config", "user.email", "test@example.invalid")
	r.git("config", "user.name", "Test")
	// A contributor whose global config signs commits would otherwise need a
	// signing key present for these fixtures to commit at all.
	r.git("config", "commit.gpgsign", "false")
	r.git("config", "tag.gpgsign", "false")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (r *repo) write(path, body string) {
	r.t.Helper()
	full := filepath.Join(r.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) mkdir(path string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Join(r.dir, filepath.FromSlash(path)), 0o750); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-m", msg, "--allow-empty")
	return strings.TrimSpace(r.git("rev-parse", "HEAD"))
}
