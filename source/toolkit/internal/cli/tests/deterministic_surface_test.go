package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// driverModule is the import path prefix a model call arrives through.
const driverModule = "github.com/pedromvgomes/agentic-driver"

// driverSeams are the files outside the model-invoking packages that may name
// the driver.
//
// Both of them only ever ASK a provider what it can express — they type-assert
// its interfaces and call its argument builders, so that a manifest naming a
// provider that cannot do what a run needs is refused before any process
// starts. Neither constructs a driver, which is what
// TestADriverIsConstructedOnlyWhereAModelIsInvoked holds them to.
//
// An allowlist rather than a rule, because "does not construct" is not
// something an import can express: the second test is the real guarantee and
// this one is what keeps the surface small enough for it to be readable.
var driverSeams = map[string]bool{
	"source/toolkit/internal/provider/provider.go": true,
	"source/toolkit/internal/review/capability.go": true,
}

// The deterministic commands must stay reachable without a driver ever being
// constructed. That is the property hooks and CI depend on: they call `stats`,
// `audit` and `lint` on every session and every build, and `code-review
// explain` decides which panel a change would get — and a model call on any of
// those paths would trade reproducibility for auth, cost and rate limits.
//
// ADR 0002 says the property is "checkable by grep". This makes it checked —
// by imports rather than by text, so a file that merely names the module in a
// comment or an error string does not read as a violation.
func TestTheDriverIsReachedThroughNamedSeamsOnly(t *testing.T) {
	repo := repoRoot(t)

	var offenders []string
	fset := token.NewFileSet()
	err := filepath.Walk(filepath.Join(repo, "source/toolkit/internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel := filepath.ToSlash(mustRel(t, repo, path))
		if isModelInvokingPackage(rel) || driverSeams[rel] {
			return nil
		}
		// Tests are not in the binary, so what they import cannot decide
		// whether a deterministic command runs without a driver — and a test
		// that could not name the driver could not build the fake provider it
		// takes to assert how a real one is confined.
		//
		// The construction guard below still covers them: a test that built
		// something able to spawn a CLI would be caught there, which is the
		// half that would actually cost a run.
		if strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if strings.HasPrefix(strings.Trim(imp.Path.Value, `"`), driverModule) {
				offenders = append(offenders, rel)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("the driver is imported outside %v and the named seams: %v",
			modelInvokingPackages, offenders)
	}
}

// modelInvokingPackages are the packages that may reach a model.
//
// Two, and they are the two operations that spend money: curating the memory
// store and running a review panel. Everything else — indexing, anchoring,
// auditing, linting, profiling a change, choosing a panel — stays reachable
// without a driver, which is what lets a hook call it on every session.
//
// Prefixes rather than filenames, because a package that invokes a model is
// allowed to be more than one file. The construction guard below is what keeps
// the permission meaningful.
var modelInvokingPackages = []string{
	"source/toolkit/internal/curator/",
	"source/toolkit/internal/reviewrun/",
}

// isModelInvokingPackage reports whether a file belongs to one of them.
func isModelInvokingPackage(rel string) bool {
	for _, prefix := range modelInvokingPackages {
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

// Constructing a driver is the act that leads to a process, so it is the act
// worth confining rather than the import that permits it. A seam may ask a
// provider what it can express; only a package that exists to spend money may
// build something that runs one.
func TestADriverIsConstructedOnlyWhereAModelIsInvoked(t *testing.T) {
	repo := repoRoot(t)

	var offenders []string
	fset := token.NewFileSet()
	err := filepath.Walk(filepath.Join(repo, "source/toolkit/internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel := filepath.ToSlash(mustRel(t, repo, path))
		if isModelInvokingPackage(rel) {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		local := driverLocalName(f)
		if local == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "New" {
				return true
			}
			if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == local {
				offenders = append(offenders, rel)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("a driver is constructed outside %v: %v", modelInvokingPackages, offenders)
	}
}

// driverLocalName returns the name the driver's root package is bound to in
// this file, or "" when the file does not import it.
func driverLocalName(f *ast.File) string {
	for _, imp := range f.Imports {
		if strings.Trim(imp.Path.Value, `"`) != driverModule {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "agentic"
	}
	return ""
}

// The store package is what every deterministic command is built on, so its
// dependency graph is where the property would break first and least visibly.
func TestTheStorePackageCannotReachADriver(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}",
		"github.com/pedromvgomes/agentic-toolkit/internal/memory").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for dep := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(dep, driverModule) {
			t.Errorf("source/toolkit/internal/memory depends on %s", dep)
		}
	}
}

// Every deterministic subcommand has to run to completion with no provider
// configured and no agent CLI installed. `curate` is the only one that may
// refuse, and it must refuse by saying how to fix it rather than by failing
// obscurely.
func TestTheDeterministicSubcommandsRunWithNoProviderConfigured(t *testing.T) {
	work := memoryProject(t, "skills: []\n")

	for _, args := range [][]string{
		{"memory", "index"},
		{"memory", "anchor", "--all"},
		{"memory", "audit"},
		{"memory", "lint"},
		{"memory", "stats"},
		{"memory", "candidates"},
	} {
		if _, stderr, err := runCLI(t, work, args...); err != nil {
			t.Errorf("%s needs a provider it should not need: %v\n%s", strings.Join(args, " "), err, stderr)
		}
	}

	_, _, err := runCLI(t, work, "memory", "curate")
	if err == nil {
		t.Fatal("curate ran without a provider configured")
	}
	if !strings.Contains(err.Error(), "memory.agent") {
		t.Errorf("curate's refusal does not say how to fix it: %v", err)
	}
}

func mustRel(t *testing.T, base, path string) string {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	return rel
}

// `code-review explain` decides which panel a change would get, and `panels`
// lists what it could have got instead. Both must do that on a machine with no
// provider configured and no agent CLI installed.
//
// It is the property the whole deterministic surface exists for: the answer to
// "why is this review deeper than I expected" has to be available before
// paying for the review that would tell you. `run` is the subcommand that
// spends, and it is the only one here that may need a CLI.
func TestCodeReviewExplainRunsWithNoProviderInstalled(t *testing.T) {
	work := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "user.name", "Test"},
		// A contributor whose global config signs commits would otherwise
		// need a signing key present for this fixture to commit at all.
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "one"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// PATH holds git and nothing else, so neither provider CLI can be found.
	// A command that needed one would fail here rather than quietly resolving
	// the operator's own installation and passing on a machine that has it.
	t.Setenv("PATH", gitOnlyPath(t))
	stdout, stderr, err := runCLI(t, work, "code-review", "explain", "--base", "HEAD")
	if err != nil {
		t.Fatalf("explain needs a provider it should not need: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "panel:") {
		t.Errorf("explain produced no panel decision:\n%s", stdout)
	}

	// The structured form and the panel listing are read by a caller that
	// is about to decide whether to spend, so they must be as free as the
	// prose is.
	for _, args := range [][]string{
		{"code-review", "explain", "--base", "HEAD", "--json"},
		{"code-review", "panels", "--base", "HEAD"},
		{"code-review", "panels", "--base", "HEAD", "--json"},
		{"code-review", "panels", "--base", "HEAD", "--context", "pr"},
	} {
		if _, stderr, err := runCLI(t, work, args...); err != nil {
			t.Errorf("%s needs a provider it should not need: %v\n%s", strings.Join(args, " "), err, stderr)
		}
	}
}

// `signals` is a table in the binary and must not need anything at all.
func TestCodeReviewSignalsRunsWithNoProviderInstalled(t *testing.T) {
	work := t.TempDir()
	t.Setenv("PATH", gitOnlyPath(t))
	if _, stderr, err := runCLI(t, work, "code-review", "signals"); err != nil {
		t.Fatalf("signals needs a provider it should not need: %v\n%s", err, stderr)
	}
}

// gitOnlyPath is a PATH holding git and nothing else.
//
// Emptying PATH outright would prove too much: git is a dependency the
// deterministic surface is entitled to, and a test that removed it would fail
// for a reason that has nothing to do with providers.
func gitOnlyPath(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git is not on PATH: %v", err)
	}
	dir := t.TempDir()
	if err := os.Symlink(git, filepath.Join(dir, "git")); err != nil {
		t.Fatalf("link git into an isolated PATH: %v", err)
	}
	return dir
}
