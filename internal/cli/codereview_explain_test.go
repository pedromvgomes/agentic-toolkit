package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// explainPR resolves the pull request the way `run --pr` does and answers
// under the rules its base ref declares.
//
// The panel is the assertion that separates the two targets: the built-in
// default starts the worktree context at `quick` and the pr context at
// `standard`, so a run that had explained the local change instead would say
// `quick` and would say it convincingly.
func TestExplainPRAnswersForThePullRequestNotTheWorkingTree(t *testing.T) {
	work, baseSHA, headSHA := prRepo(t)
	doer := stubDoer{
		"/repos/acme/widgets/installation":    `{"id": 99}`,
		"/app/installations/99/access_tokens": `{"token": "ghs_x", "expires_at": "2999-01-01T00:00:00Z"}`,
		"/repos/acme/widgets/pulls/7": fmt.Sprintf(
			`{"number": 7, "state": "open", "base": {"sha": %q, "ref": "main"}, "head": {"sha": %q, "ref": "feature/x"}}`,
			baseSHA, headSHA),
	}

	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())

	err := runCodeReviewExplain(cmd, env, reviewTarget{pr: 7, context: "worktree"}, false,
		clientSeam{dir: registration(t), doer: doer})
	if err != nil {
		t.Fatalf("explain --pr: %v", err)
	}

	got := out.String()
	for _, want := range []string{"context: pr", "panel:   standard", headSHA} {
		if !strings.Contains(got, want) {
			t.Errorf("explain --pr did not report %q:\n%s", want, got)
		}
	}
	// Nothing is spent: the subcommand reads a manifest and profiles a change,
	// and a model that ran would be a review charged for by a command whose
	// whole point is answering before one is.
	if strings.Contains(got, "could not reach a verdict") {
		t.Errorf("explain ran a review:\n%s", got)
	}
}

// --pr names the change to review, so a flag naming a different one is a
// contradiction to refuse rather than a silent precedence rule.
func TestExplainPRRefusesAFlagThatNamesAnotherChange(t *testing.T) {
	work, _, _ := prRepo(t)
	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())

	err := runCodeReviewExplain(cmd, env, reviewTarget{pr: 7, base: "main", context: "worktree"}, false,
		clientSeam{dir: registration(t)})
	if err == nil {
		t.Fatal("explain accepted --pr together with --base")
	}
	if !strings.Contains(err.Error(), "--base") {
		t.Errorf("error = %q, want it to name --base", err)
	}
}

// A pull request number that is not one is refused before anything is read.
func TestExplainPRRefusesANumberThatIsNotAPullRequest(t *testing.T) {
	work, _, _ := prRepo(t)
	var out bytes.Buffer
	env := &Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: io.Discard, WorkDir: work}
	cmd := NewRootCmd(env)
	cmd.SetContext(context.Background())

	err := runCodeReviewExplain(cmd, env, reviewTarget{pr: -1, context: "worktree"}, false, clientSeam{})
	if err == nil {
		t.Fatal("explain accepted a negative pull request number")
	}
}
