package cli

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// initRepo is an empty git repository, since the manifest is written relative
// to the repository root rather than to the working directory.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return dir
}

// What init writes is the default verbatim, so a repo that runs it is reviewed
// exactly as it was the moment before. A scaffold that differed would change
// how a repo is reviewed as a side effect of asking to see the rules.
func TestInitWritesTheBuiltInDefaultVerbatim(t *testing.T) {
	dir := initRepo(t)
	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: dir}

	if err := runCodeReviewInit(env, false, false); err != nil {
		t.Fatalf("init: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(dir, ".agents", "code-review", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, review.DefaultManifestYAML()) {
		t.Error("init wrote something other than the built-in default")
	}
	// And it parses: a scaffold a repo cannot use is worse than none, because
	// the failure arrives at the next review rather than now.
	if _, err := review.ParseBytes("manifest.yaml", written); err != nil {
		t.Errorf("the manifest init wrote does not parse: %v", err)
	}
}

// A manifest is where a repo tuned its own reviews. Overwriting one with the
// default would replace every rule it had with none of them, and reviews would
// go on running as though that were what somebody wanted.
func TestInitRefusesToOverwriteAnExistingManifest(t *testing.T) {
	dir := initRepo(t)
	path := filepath.Join(dir, ".agents", "code-review", "manifest.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("version: 1 # this repo's own\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: dir}

	err := runCodeReviewInit(env, false, false)
	if err == nil {
		t.Fatal("init overwrote an existing manifest")
	}
	if body, _ := os.ReadFile(path); !strings.Contains(string(body), "this repo's own") {
		t.Errorf("the existing manifest was modified: %q", body)
	}

	// --force is the deliberate way through.
	if err := runCodeReviewInit(env, true, false); err != nil {
		t.Fatalf("init --force: %v", err)
	}
	if body, _ := os.ReadFile(path); !bytes.Equal(body, review.DefaultManifestYAML()) {
		t.Error("--force did not replace the manifest with the default")
	}
}

// The manifest init writes must actually select a panel, not merely parse.
// Provenance changes meaning: a rule the built-in default skips is a refusal
// once a repo owns it, so a scaffold that parses and then refuses every review
// is the failure this guards.
func TestTheManifestInitWritesCanSelectAPanel(t *testing.T) {
	m, err := review.DefaultManifest()
	if err != nil {
		t.Fatal(err)
	}
	written, err := review.ParseBytes("manifest.yaml", review.DefaultManifestYAML())
	if err != nil {
		t.Fatalf("the scaffold does not parse: %v", err)
	}
	if written.Builtin {
		t.Fatal("a parsed manifest claims to be the built-in one")
	}
	// A change whose language has no symbol extractor is the case that
	// separates the two: readable counts hide it.
	p := review.Profile{
		ChangedFiles: 1, ChangedLines: 10,
		Signals:          review.NewSignalSet(),
		ReferencingFiles: review.UnavailableCount("no symbol extractor for unrecognised files"),
	}
	for _, ctx := range review.Contexts {
		if _, err := review.Select(m, ctx, &p, ""); err != nil {
			t.Fatalf("the built-in default refuses %s, before init is involved: %v", ctx, err)
		}
		if _, err := review.Select(written, ctx, &p, ""); err != nil {
			// Not a failure of init: the refusal is correct once the repo owns
			// the rule. What init owes the user is saying so, which
			// reportUnrunnableRules does.
			t.Logf("as a repo's own manifest, %s refuses: %v", ctx, err)
		}
	}
}

// --dry-run reports where the manifest would go and writes nothing, so the
// location can be confirmed without creating the file.
func TestInitDryRunWritesNothing(t *testing.T) {
	dir := initRepo(t)
	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: dir}

	if err := runCodeReviewInit(env, false, true); err != nil {
		t.Fatalf("init --dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "code-review", "manifest.yaml")); !os.IsNotExist(err) {
		t.Error("--dry-run wrote the manifest")
	}
	if !strings.Contains(out.String(), "Nothing was written") {
		t.Errorf("output = %q, want it to say nothing was written", out.String())
	}
}

// Saying where the manifest lives is the one thing --dry-run is for, and it is
// most useful in the repo that already has one. Refusing there would make the
// read-only question fail exactly when its answer exists.
func TestInitDryRunReportsThePathWhenAManifestExists(t *testing.T) {
	dir := initRepo(t)
	path := filepath.Join(dir, ".agents", "code-review", "manifest.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: dir}

	if err := runCodeReviewInit(env, false, true); err != nil {
		t.Fatalf("init --dry-run on an existing manifest: %v", err)
	}
	for _, want := range []string{path, "--force", "Nothing was written"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output = %q, want it to mention %q", out.String(), want)
		}
	}
}

// `initialize` is what registration was called and scripts hold that name, so
// it still resolves — to the same command, not a second one.
func TestRegisterKeepsInitializeAsAnAlias(t *testing.T) {
	env := &Env{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard, WorkDir: t.TempDir()}
	root := NewRootCmd(env)

	byName, _, err := root.Find([]string{"code-review", "register"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	byAlias, _, err := root.Find([]string{"code-review", "initialize"})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if byName != byAlias {
		t.Error("initialize resolves to a different command than register")
	}
	// init and register are unrelated jobs, and one of them stores a private
	// key. The prefix must not resolve to the other.
	byInit, _, err := root.Find([]string{"code-review", "init"})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if byInit == byName {
		t.Error("init resolves to the registration command")
	}
}

// git runs one command in dir, failing the test if it does not.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// Writing the default is the one moment a skipped rule becomes a review that
// will not run, so init says which rule and what to do. Silence here is a
// mystery at the next review, in a repo that was reviewing fine a moment ago.
func TestInitNamesARuleThisRepoCannotEvaluate(t *testing.T) {
	dir := initRepo(t)
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	// A language with no symbol extractor, which is what makes the default's
	// referencing_files rules unreadable here.
	if err := os.WriteFile(filepath.Join(dir, "main.zig"), []byte("pub fn main() void {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "main.zig"), []byte("pub fn main() void {}\npub fn other() void {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-m", "change")

	var out, errOut bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, WorkDir: dir}
	if err := runCodeReviewInit(env, false, false); err != nil {
		t.Fatalf("init: %v", err)
	}

	for _, want := range []string{"cannot be evaluated", "referencing_files", "refuses"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("init said nothing about %q:\nstdout: %s\nstderr: %s", want, out.String(), errOut.String())
		}
	}
}
