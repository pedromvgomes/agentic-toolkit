package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// scopeRepo is a branch with history a re-review has to reason about: a base,
// a reviewed head, a later head on top of it, a rewritten head that is not a
// descendant of the reviewed one, and a head that merged a newer base in.
type scopeRepo struct {
	dir                              string
	base, reviewed, later, rewritten string
	newerBase, merged                string
}

func buildScopeRepo(t *testing.T) scopeRepo {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(name, body, msg string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		run("add", "-A")
		run("commit", "-q", "-m", msg)
		return run("rev-parse", "HEAD")
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")

	var r scopeRepo
	r.dir = dir
	r.base = commit("a.go", "package a\n", "base")
	run("checkout", "-q", "-b", "feature")
	r.reviewed = commit("b.go", "package a\n\nfunc b() {}\n", "reviewed")
	r.later = commit("b.go", "package a\n\nfunc b() { _ = 1 }\n", "fix")

	run("checkout", "-q", "-b", "rewritten", r.base)
	r.rewritten = commit("b.go", "package a\n\nfunc b() { _ = 2 }\n", "rewritten")

	run("checkout", "-q", "main")
	r.newerBase = commit("c.go", "package a\n", "newer base")
	run("checkout", "-q", "feature")
	run("merge", "-q", "--no-edit", "main")
	r.merged = run("rev-parse", "HEAD")
	return r
}

// completeReviewOf is a review this installation posted that reached a verdict
// on head.
func completeReviewOf(head string) githubapp.SubmittedReview {
	marker := reviewrun.ReviewMarker{Head: head, Complete: true}
	return githubapp.SubmittedReview{CommitSHA: head, Body: marker.Render(), ByViewer: true}
}

// A re-review reads on from the last complete review only when every condition
// that keeps the earlier review a description of this change holds. Anything
// else reads the whole change, and says why.
func TestAPullRequestReReviewNarrowsOnlyWhenItIsSafe(t *testing.T) {
	r := buildScopeRepo(t)
	target := func(head, baseSHA string) *pullRequestTarget {
		return &pullRequestTarget{
			pr:        githubapp.PullRequest{Number: 7, HeadSHA: head, BaseSHA: baseSHA},
			mergeBase: r.base,
		}
	}
	incomplete := completeReviewOf(r.reviewed)
	incomplete.Body = reviewrun.ReviewMarker{Head: r.reviewed, Complete: false}.Render()
	foreign := completeReviewOf(r.reviewed)
	foreign.ByViewer = false

	cases := []struct {
		name      string
		target    *pullRequestTarget
		reviews   []githubapp.SubmittedReview
		full      bool
		wantSince string
		wantWhy   string
	}{
		{
			name:      "a push on top of a reviewed head reads what changed since it",
			target:    target(r.later, r.base),
			reviews:   []githubapp.SubmittedReview{completeReviewOf(r.reviewed)},
			wantSince: r.reviewed,
		},
		{
			name:    "--full reads the whole change",
			target:  target(r.later, r.base),
			reviews: []githubapp.SubmittedReview{completeReviewOf(r.reviewed)},
			full:    true,
			wantWhy: "--full",
		},
		{
			name:    "no earlier review reads the whole change",
			target:  target(r.later, r.base),
			wantWhy: "no earlier review",
		},
		{
			name:    "a review that reached no verdict is not read on from",
			target:  target(r.later, r.base),
			reviews: []githubapp.SubmittedReview{incomplete},
			wantWhy: "no earlier review",
		},
		{
			name:    "a review somebody else posted is not read on from",
			target:  target(r.later, r.base),
			reviews: []githubapp.SubmittedReview{foreign},
			wantWhy: "no earlier review",
		},
		{
			name:    "a rewritten branch reads the whole change",
			target:  target(r.rewritten, r.base),
			reviews: []githubapp.SubmittedReview{completeReviewOf(r.reviewed)},
			wantWhy: "rewritten",
		},
		{
			name:    "a branch that merged the base in reads the whole change",
			target:  &pullRequestTarget{pr: githubapp.PullRequest{Number: 7, HeadSHA: r.merged, BaseSHA: r.newerBase}, mergeBase: r.newerBase},
			reviews: []githubapp.SubmittedReview{completeReviewOf(r.reviewed)},
			wantWhy: "merged in",
		},
		{
			name:    "a forced re-review of the reviewed head reads the whole change",
			target:  target(r.reviewed, r.base),
			reviews: []githubapp.SubmittedReview{completeReviewOf(r.reviewed)},
			wantWhy: "of this head",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chooseScope(r.dir, tc.target, tc.reviews, nil, tc.full)
			if got.since != tc.wantSince {
				t.Fatalf("since = %q, want %q (reason %q)", got.since, tc.wantSince, got.reason)
			}
			if tc.wantWhy != "" && !strings.Contains(got.reason, tc.wantWhy) {
				t.Errorf("reason = %q, want it to say %q", got.reason, tc.wantWhy)
			}
			if tc.wantSince != "" && got.reason != "" {
				t.Errorf("a narrowed review carries a reason for reading the whole change: %q", got.reason)
			}
		})
	}
}

// A newer complete review is what a re-review reads on from, not the first.
func TestAPullRequestReReviewReadsOnFromTheNewestCompleteReview(t *testing.T) {
	r := buildScopeRepo(t)
	target := &pullRequestTarget{
		pr:        githubapp.PullRequest{Number: 7, HeadSHA: r.later, BaseSHA: r.base},
		mergeBase: r.base,
	}
	older := completeReviewOf(r.base)
	got := chooseScope(r.dir, target, []githubapp.SubmittedReview{older, completeReviewOf(r.reviewed)}, nil, false)
	if got.since != r.reviewed {
		t.Fatalf("read on from %q, want the newest complete review %q", got.since, r.reviewed)
	}
}
