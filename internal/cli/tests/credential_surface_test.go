package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// credentialPackage is where the GitHub App key and the installation tokens
// minted from it live. Nothing else in the binary holds either.
const credentialPackage = "github.com/pedromvgomes/agentic-toolkit/internal/githubapp"

// The App installation token reaches every repository the App is installed on,
// and a review run is driven by a model reading a diff somebody else wrote.
// Handing that process the credential widens a grant across an entire account.
//
// ADR 0006 says the credential never enters a model's process. This is what
// makes that a property rather than a sentence: the package that invokes a
// model cannot reach the package that holds the credential, so passing the
// token would require adding an import rather than forgetting to remove one.
func TestTheModelInvokingPackagesCannotReachTheCredential(t *testing.T) {
	for _, pkg := range []string{
		"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun",
		"github.com/pedromvgomes/agentic-toolkit/internal/curator",
		"github.com/pedromvgomes/agentic-toolkit/internal/review",
	} {
		out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for dep := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			if dep == credentialPackage {
				t.Errorf("%s depends on %s, so a run that reads someone else's diff can reach this machine's GitHub App key", pkg, credentialPackage)
			}
		}
	}
}

// A reviewer inherits the operator's environment: agentic.Request carries a
// prompt, a model and a workdir, and nothing that would strip a variable off
// the child.
//
// So a token placed in this process's environment is a token every reviewer
// reads. Holding it in memory and passing it as an argument is what keeps it
// out of theirs, and os.Setenv is the one call that would undo that silently.
func TestTheCredentialIsNeverPutIntoTheProcessEnvironment(t *testing.T) {
	repo := repoRoot(t)
	var offenders []string
	fset := token.NewFileSet()
	for _, dir := range []string{"internal/githubapp", "internal/reviewpost"} {
		err := filepath.Walk(filepath.Join(repo, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if parseErr != nil {
				return parseErr
			}
			rel := filepath.ToSlash(mustRel(t, repo, path))
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "os" {
					return true
				}
				if sel.Sel.Name == "Setenv" || sel.Sel.Name == "Environ" {
					offenders = append(offenders, rel+" calls os."+sel.Sel.Name)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("the credential packages touch the process environment, which every reviewer inherits: %v", offenders)
	}
}

// A token that reached disk would outlive the command that minted it, and the
// whole point of minting per run is that nothing persists.
func TestNoInstallationTokenIsWrittenAnywhere(t *testing.T) {
	repo := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(repo, "internal/githubapp/client.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"os.WriteFile", "os.Create", "os.OpenFile"} {
		if strings.Contains(string(body), banned) {
			t.Errorf("internal/githubapp/client.go calls %s; an installation token that reached disk would outlive the run that minted it", banned)
		}
	}
}

// Approval is a GitHub review with event APPROVE, and no code path from a
// review run may reach one. The guarantee is that the code does not exist,
// rather than that a prompt was told not to.
func TestNothingInTheBinaryCanPostAnApproval(t *testing.T) {
	repo := repoRoot(t)
	var offenders []string
	err := filepath.Walk(filepath.Join(repo, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		body, readErr := os.ReadFile(path) // #nosec G304 -- a .go file inside this repository
		if readErr != nil {
			return readErr
		}
		rel := filepath.ToSlash(mustRel(t, repo, path))
		if rel == "internal/cli/tests/credential_surface_test.go" {
			return nil
		}
		for lineNo, line := range strings.Split(string(body), "\n") {
			if strings.Contains(line, `"APPROVE"`) {
				offenders = append(offenders, rel+":"+strconv.Itoa(lineNo+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("the binary can name the approval event: %v", offenders)
	}
}
