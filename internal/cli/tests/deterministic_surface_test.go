package tests

import (
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

// The deterministic memory commands must stay reachable without a driver ever
// being constructed. That is the property hooks and CI depend on: they call
// `stats`, `audit` and `lint` on every session and every build, and a model
// call on that path would trade reproducibility for auth, cost and rate
// limits.
//
// ADR 0002 says the property is "checkable by grep". This makes it checked —
// by imports rather than by text, so a file that merely names the module in a
// comment or an error string does not read as a violation.
func TestOnlyTheCuratorPackageConstructsADriver(t *testing.T) {
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
		t.Errorf("the driver is imported outside internal/curator: %v", offenders)
	}
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
