package tests

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/cli"
	"github.com/pedromvgomes/agentic-toolkit/internal/cloudinit"
)

// renderedCommand is where a checkout made by newCheckout gets its one
// rendered file.
const renderedCommand = ".claude/commands/hello.md"

// identityOnly is a configured session that changes nothing but user.name,
// so the render is all a test has to look at.
var identityOnly = map[string]string{cloudinit.EnvUser: "Ada Lovelace"}

// renderSetup isolates git as fakeHome does and makes every render a test
// starts run the real agtk CLI from this test binary.
func renderSetup(t *testing.T) string {
	t.Helper()
	home := fakeHome(t)
	t.Setenv(actAsAgtk, "1")
	return home
}

// newCheckout creates root/name as a git work tree whose entry manifest
// renders one command from a stack of its own, writes its lockfile with
// `agtk lock`, and returns its path. Nothing is committed.
func newCheckout(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	writeFile(t, filepath.Join(dir, cloudinit.ManifestFileName), "stacks:\n  - ./stacks/s.yaml\n")
	writeFile(t, filepath.Join(dir, "stacks", "s.yaml"), "commands:\n  - hello\n")
	writeFile(t, filepath.Join(dir, "definitions", "commands", "hello.md"), "---\ndescription: Say hello\n---\n\nSay hello.\n")
	gitIn(t, dir, "init", "-q")
	agtkIn(t, dir, "lock")
	return dir
}

// newCommittedCheckout is newCheckout with everything committed, which is
// what makes a checkout one Render considers.
func newCommittedCheckout(t *testing.T, root, name string) string {
	t.Helper()
	dir := newCheckout(t, root, name)
	commitAll(t, dir)
	return dir
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "checkout")
}

// agtkIn runs this test binary as agtk in dir, failing on error.
func agtkIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), actAsAgtk+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("agtk %s in %s: %v: %s", strings.Join(args, " "), dir, err, out)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rendered(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, renderedCommand))
	return err == nil
}

// fakeAgtk writes a shell script standing in for agtk and returns its path.
func fakeAgtk(t *testing.T, body string) string {
	t.Helper()
	requireTool(t, "sh")
	path := filepath.Join(t.TempDir(), "agtk")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// runRender runs cloudinit.Run with Render set and returns stdout and
// stderr. opts supplies everything else; Getenv defaults to identityOnly and
// Dir to a directory outside any checkout.
func runRender(t *testing.T, opts cloudinit.Options) (stdout, stderr string, err error) {
	t.Helper()
	if opts.Getenv == nil {
		opts.Getenv = env(identityOnly)
	}
	if opts.Dir == "" {
		opts.Dir = t.TempDir()
	}
	opts.Render = true
	var errBuf bytes.Buffer
	opts.Stderr = &errBuf
	stdout, err = runWith(t, opts)
	return stdout, errBuf.String(), err
}

func TestTheCheckoutFileNamesAreTheOnesTheCLIReads(t *testing.T) {
	if cloudinit.ManifestFileName != cli.ConfigFileName {
		t.Errorf("ManifestFileName = %q, want the CLI's %q", cloudinit.ManifestFileName, cli.ConfigFileName)
	}
	if cloudinit.LockFileName != cli.LockFileName {
		t.Errorf("LockFileName = %q, want the CLI's %q", cloudinit.LockFileName, cli.LockFileName)
	}
}

func TestRenderWithNothingSetRendersNothingAndPrintsOnlyTheNothingToDoLine(t *testing.T) {
	renderSetup(t)
	root := t.TempDir()
	dir := newCommittedCheckout(t, root, "repo")

	stdout, stderr, err := runRender(t, cloudinit.Options{
		Getenv:     env(nil),
		RenderRoot: root,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stdout != cloudinit.NothingToDo+"\n" {
		t.Errorf("stdout = %q, want exactly %q", stdout, cloudinit.NothingToDo+"\n")
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want it empty", stderr)
	}
	if rendered(dir) {
		t.Error("an unconfigured session rendered a checkout")
	}
}

func TestAConfiguredRunWithoutRenderRendersNothing(t *testing.T) {
	renderSetup(t)
	root := t.TempDir()
	dir := newCommittedCheckout(t, root, "repo")

	out, err := runWith(t, cloudinit.Options{
		Getenv:     env(identityOnly),
		Dir:        t.TempDir(),
		RenderRoot: root,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rendered(dir) {
		t.Error("a run without Render rendered a checkout")
	}
	if strings.Contains(out, "render") {
		t.Errorf("stdout reports a render that was not asked for: %q", out)
	}
}

// One checkout's failure is reported and the next still renders: the
// failing one sorts first, so a run that stopped at it would render nothing.
func TestOneFailingCheckoutIsReportedAndTheOthersStillRender(t *testing.T) {
	renderSetup(t)
	root := t.TempDir()
	broken := newCommittedCheckout(t, root, "a-broken")
	writeFile(t, filepath.Join(broken, renderedCommand), "mine\n")
	good := newCommittedCheckout(t, root, "b-good")

	stdout, stderr, err := runRender(t, cloudinit.Options{RenderRoot: root})

	const wantErr = "cloud init: render failed in 1 of 2 checkouts"
	if err == nil || err.Error() != wantErr {
		t.Fatalf("Run = %v, want %q", err, wantErr)
	}
	if !rendered(good) {
		t.Error("the checkout after the failing one was not rendered")
	}
	if !strings.Contains(stdout, "agtk cloud init: rendered "+good+"\n") {
		t.Errorf("stdout does not report %s as rendered:\n%s", good, stdout)
	}
	if strings.Contains(stdout, broken) {
		t.Errorf("stdout names the failing checkout:\n%s", stdout)
	}
	lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("stderr carries %d lines, want one for the one failure:\n%s", len(lines), stderr)
	}
	prefix := "agtk cloud init: render failed in " + broken + ": agtk render: exit status 1: "
	if !strings.HasPrefix(lines[0], prefix) || !strings.Contains(lines[0], "exists and is not tracked by agtk") {
		t.Errorf("stderr = %q, want a line starting %q with render's own reason", lines[0], prefix)
	}
	if data, _ := os.ReadFile(filepath.Join(broken, renderedCommand)); string(data) != "mine\n" {
		t.Errorf("the file render refused to overwrite was changed: %q", data)
	}
}

// A resumed session runs the hook again, over everything the first run
// rendered.
func TestASecondRenderOverRenderedOutputSucceeds(t *testing.T) {
	renderSetup(t)
	root := t.TempDir()
	dir := newCommittedCheckout(t, root, "repo")
	want := "agtk cloud init: user.name set to \"Ada Lovelace\" globally\n" +
		"agtk cloud init: rendered " + dir + "\n"

	for i := 1; i <= 2; i++ {
		stdout, stderr, err := runRender(t, cloudinit.Options{RenderRoot: root})
		if err != nil {
			t.Fatalf("run %d: %v; stderr=%q", i, err, stderr)
		}
		if stdout != want {
			t.Errorf("run %d stdout =\n%s\nwant\n%s", i, stdout, want)
		}
		if !rendered(dir) {
			t.Errorf("run %d left %s unrendered", i, renderedCommand)
		}
	}
}

// Every directory here would render, or fail visibly, if it were attempted:
// each has the manifest or the lockfile render reads, and lacks only what
// makes it a committed checkout.
func TestDirectoriesThatAreNotCommittedCheckoutsAreSkipped(t *testing.T) {
	renderSetup(t)
	root := t.TempDir()

	noManifest := newCommittedCheckout(t, root, "no-manifest")
	gitIn(t, noManifest, "rm", "-q", cloudinit.ManifestFileName)
	commitAll(t, noManifest)

	uncommitted := newCheckout(t, root, "uncommitted-lockfile")

	notARepo := newCheckout(t, root, "not-a-repo")
	if err := os.RemoveAll(filepath.Join(notARepo, ".git")); err != nil {
		t.Fatal(err)
	}

	manifestDir := newCommittedCheckout(t, root, "manifest-is-a-directory")
	gitIn(t, manifestDir, "rm", "-q", cloudinit.ManifestFileName)
	commitAll(t, manifestDir)
	writeFile(t, filepath.Join(manifestDir, cloudinit.ManifestFileName, "x"), "")

	writeFile(t, filepath.Join(root, "a-file"), "")
	good := newCommittedCheckout(t, root, "z-good")

	stdout, stderr, err := runRender(t, cloudinit.Options{RenderRoot: root})
	if err != nil {
		t.Fatalf("Run: %v; stderr=%q", err, stderr)
	}
	want := "agtk cloud init: user.name set to \"Ada Lovelace\" globally\n" +
		"agtk cloud init: rendered " + good + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	for _, dir := range []string{noManifest, uncommitted, notARepo, manifestDir} {
		if rendered(dir) {
			t.Errorf("%s was rendered", dir)
		}
	}
}

// A directory inside another work tree sees that tree's HEAD, so its lookup
// of the committed lockfile would find the enclosing checkout's.
func TestADirectoryInsideAnotherWorkTreeIsSkipped(t *testing.T) {
	renderSetup(t)
	root := newCommittedCheckout(t, t.TempDir(), "outer")
	inner := newCheckout(t, root, "inner")
	if err := os.RemoveAll(filepath.Join(inner, ".git")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, root)

	stdout, stderr, err := runRender(t, cloudinit.Options{RenderRoot: root})
	if err != nil {
		t.Fatalf("Run: %v; stderr=%q", err, stderr)
	}
	if want := "agtk cloud init: no checkout under " + root + " to render\n"; !strings.HasSuffix(stdout, want) {
		t.Errorf("stdout =\n%s\nwant it to end %q", stdout, want)
	}
	if rendered(inner) {
		t.Error("a directory inside another work tree was rendered")
	}
}

func TestAMissingRenderRootHasNothingToRender(t *testing.T) {
	fakeHome(t)
	root := filepath.Join(t.TempDir(), "missing")

	stdout, _, err := runRender(t, cloudinit.Options{RenderRoot: root})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := "agtk cloud init: no checkout under " + root + " to render\n"; !strings.HasSuffix(stdout, want) {
		t.Errorf("stdout =\n%s\nwant it to end %q", stdout, want)
	}
}

func TestARenderRootThatCannotBeListedFails(t *testing.T) {
	fakeHome(t)
	root := filepath.Join(t.TempDir(), "a-file")
	writeFile(t, root, "")

	_, _, err := runRender(t, cloudinit.Options{RenderRoot: root})
	if err == nil || !strings.HasPrefix(err.Error(), "cloud init: list "+root+": ") {
		t.Fatalf("Run = %v, want a failure to list %s", err, root)
	}
}

// A run whose identity or key fails stops there: the render comes after
// both.
func TestAFailedSigningKeyRendersNothing(t *testing.T) {
	renderSetup(t)
	root := t.TempDir()
	dir := newCommittedCheckout(t, root, "repo")

	_, _, err := runRender(t, cloudinit.Options{
		Getenv:     env(map[string]string{cloudinit.EnvSigningKey: "not*base64"}),
		RenderRoot: root,
	})
	if err == nil || !strings.Contains(err.Error(), "is not valid base64") {
		t.Fatalf("Run = %v, want the signing key's failure", err)
	}
	if rendered(dir) {
		t.Error("a run whose signing key failed went on to render")
	}
}

// The given binary runs `render` and nothing else, from inside the checkout:
// `sync` would resolve refs over the network.
func TestTheExecutableRunsRenderFromInsideEachCheckout(t *testing.T) {
	renderSetup(t)
	root := t.TempDir()
	first := newCommittedCheckout(t, root, "first")
	second := newCommittedCheckout(t, root, "second")
	log := filepath.Join(t.TempDir(), "log")
	agtk := fakeAgtk(t, `echo "$PWD $#:$*" >> '`+log+`'`)

	_, stderr, err := runRender(t, cloudinit.Options{RenderRoot: root, Executable: agtk})
	if err != nil {
		t.Fatalf("Run: %v; stderr=%q", err, stderr)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := gitIn(t, first, "rev-parse", "--show-toplevel") + " 1:render\n" +
		gitIn(t, second, "rev-parse", "--show-toplevel") + " 1:render\n"
	if string(data) != want {
		t.Errorf("invocations =\n%s\nwant\n%s", data, want)
	}
}

func TestAMultiLineRenderFailureIsReportedOnOneLine(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	dir := newCommittedCheckout(t, root, "repo")
	agtk := fakeAgtk(t, "printf 'first\\n\\n  second  \\n' >&2; exit 3")

	_, stderr, err := runRender(t, cloudinit.Options{RenderRoot: root, Executable: agtk})
	if err == nil {
		t.Fatal("Run succeeded over a failing render")
	}
	want := "agtk cloud init: render failed in " + dir + ": agtk render: exit status 3: first; second\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestASilentRenderFailureCarriesOnlyTheExitStatus(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	dir := newCommittedCheckout(t, root, "repo")
	agtk := fakeAgtk(t, "echo 'stdout is not the report'; exit 2")

	_, stderr, err := runRender(t, cloudinit.Options{RenderRoot: root, Executable: agtk})
	if err == nil {
		t.Fatal("Run succeeded over a failing render")
	}
	if want := "agtk cloud init: render failed in " + dir + ": agtk render: exit status 2\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestARenderThatOutlivesItsTimeoutIsReportedAsSuch(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	dir := newCommittedCheckout(t, root, "repo")
	agtk := fakeAgtk(t, "exec sleep 30")

	start := time.Now()
	_, stderr, err := runRender(t, cloudinit.Options{
		RenderRoot:    root,
		Executable:    agtk,
		RenderTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Run succeeded over a render that never finished")
	}
	if want := "agtk cloud init: render failed in " + dir + ": agtk render did not finish within 200ms\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("Run took %s, want it bounded by the render timeout", elapsed)
	}
}

// With Stderr, RenderTimeout and Executable all zero, a failure is still
// counted, the render gets DefaultRenderTimeout rather than a zero that
// expires at once, and the binary is this one.
func TestZeroRenderOptionsDiscardTheReportAndRenderWithThisBinary(t *testing.T) {
	renderSetup(t)
	root := t.TempDir()
	broken := newCommittedCheckout(t, root, "a-broken")
	writeFile(t, filepath.Join(broken, renderedCommand), "mine\n")
	good := newCommittedCheckout(t, root, "b-good")

	_, err := runWith(t, cloudinit.Options{
		Getenv:     env(identityOnly),
		Dir:        t.TempDir(),
		Render:     true,
		RenderRoot: root,
	})
	if err == nil || err.Error() != "cloud init: render failed in 1 of 2 checkouts" {
		t.Fatalf("Run = %v, want the one failure counted", err)
	}
	if !rendered(good) {
		t.Error("the default timeout and binary did not render the good checkout")
	}
}

// An empty RenderRoot searches DefaultRenderRoot. The stand-in binary does
// nothing, so whatever checkouts that directory holds on this machine are
// only read.
func TestAnEmptyRenderRootSearchesTheDefault(t *testing.T) {
	fakeHome(t)
	log := filepath.Join(t.TempDir(), "log")
	agtk := fakeAgtk(t, `echo "$PWD" >> '`+log+`'`)

	stdout, stderr, err := runRender(t, cloudinit.Options{Executable: agtk})
	if err != nil {
		t.Fatalf("Run: %v; stderr=%q", err, stderr)
	}
	if !strings.Contains(stdout, " "+cloudinit.DefaultRenderRoot) {
		t.Errorf("stdout names no directory under %s:\n%s", cloudinit.DefaultRenderRoot, stdout)
	}
	data, _ := os.ReadFile(log)
	for dir := range strings.FieldsSeq(string(data)) {
		if filepath.Dir(dir) != cloudinit.DefaultRenderRoot {
			t.Errorf("rendered %s, which is not directly under %s", dir, cloudinit.DefaultRenderRoot)
		}
	}
}
