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

// credentialSurface are the packages a credential passes through: the one that
// holds it, and the one that builds what it is spent on. Both are walked
// whole, so a guard keeps covering a package as files are added to it.
var credentialSurface = []string{
	"source/toolkit/internal/githubapp",
	"source/toolkit/internal/reviewpost",
	"source/toolkit/internal/reviewapprove",
}

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
	for _, dir := range credentialSurface {
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

// registrationWriter is the one file in the credential surface that writes to
// disk. It persists the App registration, which is the point of `initialize`;
// everything around it handles the minted token, which must never be written.
const registrationWriter = "source/toolkit/internal/githubapp/credential.go"

// A token that reached disk would outlive the command that minted it, and the
// whole point of minting per run is that nothing persists.
//
// The whole credential surface is walked rather than the one file that mints
// tokens. A guard that reads a single file names a property of that file: a
// write added in any sibling — the file that holds the API calls the token is
// spent on, most obviously — satisfies it while making its claim false.
func TestNoInstallationTokenIsWrittenAnywhere(t *testing.T) {
	repo := repoRoot(t)
	banned := []string{"os.WriteFile", "os.Create", "os.OpenFile"}

	var offenders []string
	for _, dir := range credentialSurface {
		err := filepath.Walk(filepath.Join(repo, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel := filepath.ToSlash(mustRel(t, repo, path))
			if rel == registrationWriter {
				return nil
			}
			body, readErr := os.ReadFile(path) // #nosec G304 -- a .go file inside this repository
			if readErr != nil {
				return readErr
			}
			for lineNo, line := range strings.Split(string(body), "\n") {
				for _, call := range banned {
					if strings.Contains(line, call) {
						offenders = append(offenders, rel+":"+strconv.Itoa(lineNo+1)+" calls "+call)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("the credential surface writes to disk outside %s; an installation token that reached disk would outlive the run that minted it: %v",
			registrationWriter, offenders)
	}
}

// approvalPackage owns the approval event and the one call that sends it, as a
// path from the repo root.
const approvalPackage = "source/toolkit/internal/reviewapprove"

// approvalImportPath is the same package as an import path.
//
// Spelled out rather than built from approvalPackage: the module root is
// source/toolkit, so the import path is not the module path joined to the
// repo-relative path, and deriving one from the other yields a package that
// does not exist. Nothing reports that — `go list -deps` simply never emits it,
// and the test below passes without testing anything.
const approvalImportPath = "github.com/pedromvgomes/agentic-toolkit/internal/reviewapprove"

// Approval is a GitHub review with event APPROVE, and one package names it.
//
// The guarantee is that the code does not exist where it must not, rather than
// that a prompt was told not to write it. This is the weaker of the two checks
// that make it so — it holds only until somebody spells the event differently —
// and it is here because a stray literal is the cheap mistake the import graph
// would not notice.
func TestOnlyOnePackageNamesTheApprovalEvent(t *testing.T) {
	repo := repoRoot(t)
	var offenders []string
	err := filepath.Walk(filepath.Join(repo, "source/toolkit/internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel := filepath.ToSlash(mustRel(t, repo, path))
		if rel == "source/toolkit/internal/cli/tests/credential_surface_test.go" || strings.HasPrefix(rel, approvalPackage+"/") {
			return nil
		}
		body, readErr := os.ReadFile(path) // #nosec G304 -- a .go file inside this repository
		if readErr != nil {
			return readErr
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
		t.Errorf("the approval event is named outside %s: %v", approvalPackage, offenders)
	}
}

// A review run must not be able to approve the code it just reviewed. That is
// the hazard GitHub blocks GITHUB_TOKEN approvals to prevent, and ADR 0006
// makes it a property of the import graph.
//
// The load-bearing half of the pair. A literal ban holds only while the event
// is spelt one way; an import that does not exist cannot be reached however it
// is spelt, and adding one is a deliberate act rather than a forgotten
// deletion.
func TestNoReviewPathCanReachTheApproval(t *testing.T) {
	// A target that does not resolve would make every comparison below fail to
	// match, so the test would pass while enforcing nothing. Resolve it first.
	if _, err := exec.Command("go", "list", approvalImportPath).Output(); err != nil {
		t.Fatalf("go list %s: %v — the approval package this test guards does not resolve, so the guard is empty", approvalImportPath, err)
	}
	for _, pkg := range []string{
		"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun",
		"github.com/pedromvgomes/agentic-toolkit/internal/reviewpost",
		"github.com/pedromvgomes/agentic-toolkit/internal/review",
		"github.com/pedromvgomes/agentic-toolkit/internal/curator",
	} {
		out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg).Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		for dep := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
			if dep == approvalImportPath {
				t.Errorf("%s depends on %s, so a review run can reach the code that approves it", pkg, approvalImportPath)
			}
		}
	}
}

// writeEndpoints are the API paths that would change what is in the
// repository, as they appear in a request path.
//
// Matched as path fragments rather than as words. "merge" is a word this
// codebase uses constantly — a merge base is where a change is measured from —
// and a guard that fired on it would be turned off within a week.
var writeEndpoints = []string{
	"/contents/",
	"/git/refs",
	"/git/blobs",
	"/git/trees",
	"/git/commits",
	"/git/tags",
	"/merges",
	"/merge",
}

// The App holds `contents: write` only so that its approvals count.
//
// GitHub weighs a review by whether its author can push, and drops one from an
// author who cannot out of the set it decides from — so without the grant an
// approval reads as APPROVED and satisfies nothing. ADR 0009.
//
// The grant is wider than the use, and this is what keeps the difference
// honest. Removing the permission breaks approval silently; removing this
// guard breaks nothing visibly, which is why the guard rather than a convention
// carries the claim that the permission is held and never spent.
func TestNothingInTheBinaryWritesToARepository(t *testing.T) {
	repo := repoRoot(t)
	var offenders []string
	err := filepath.Walk(filepath.Join(repo, "source/toolkit/internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel := filepath.ToSlash(mustRel(t, repo, path))
		if rel == "source/toolkit/internal/cli/tests/credential_surface_test.go" {
			return nil
		}
		body, readErr := os.ReadFile(path) // #nosec G304 -- a .go file inside this repository
		if readErr != nil {
			return readErr
		}
		for lineNo, line := range strings.Split(string(body), "\n") {
			for _, endpoint := range writeEndpoints {
				if strings.Contains(line, `"`+endpoint) || strings.Contains(line, endpoint+`"`) {
					offenders = append(offenders, rel+":"+strconv.Itoa(lineNo+1)+" names "+endpoint)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("the binary names an endpoint that writes to a repository; the App holds contents: write only so that its approvals count: %v", offenders)
	}
}
