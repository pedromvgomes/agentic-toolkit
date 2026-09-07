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

// driverSeams are the files outside internal/curator that may name the driver.
//
// Both of them only ever ASK a provider what it can express — they type-assert
// its interfaces and call its argument builders, so that a manifest naming a
// provider that cannot do what a run needs is refused before any process
// starts. Neither constructs a driver, which is what
// TestOnlyTheCuratorConstructsADriver holds them to.
//
// An allowlist rather than a rule, because "does not construct" is not
// something an import can express: the second test is the real guarantee and
// this one is what keeps the surface small enough for it to be readable.
var driverSeams = map[string]bool{
	"internal/provider/provider.go": true,
	"internal/review/capability.go": true,
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
	err := filepath.Walk(filepath.Join(repo, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel := filepath.ToSlash(mustRel(t, repo, path))
		if strings.HasPrefix(rel, "internal/curator/") || driverSeams[rel] {
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
		t.Errorf("the driver is imported outside internal/curator and the named seams: %v", offenders)
	}
}

// Constructing a driver is the act that leads to a process, so it is the act
// worth confining rather than the import that permits it. A seam may ask a
// provider what it can express; only the curator may build something that
// runs one.
func TestOnlyTheCuratorConstructsADriver(t *testing.T) {
	repo := repoRoot(t)

	var offenders []string
	fset := token.NewFileSet()
	err := filepath.Walk(filepath.Join(repo, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel := filepath.ToSlash(mustRel(t, repo, path))
		if strings.HasPrefix(rel, "internal/curator/") {
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
		t.Errorf("a driver is constructed outside internal/curator: %v", offenders)
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
			t.Errorf("internal/memory depends on %s", dep)
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
