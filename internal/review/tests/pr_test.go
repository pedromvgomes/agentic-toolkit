package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

func TestARemoteURLYieldsTheOwnerAndRepositoryGitHubAddresses(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want string
	}{
		{"git@github.com:pedromvgomes/agentic-toolkit.git", "pedromvgomes/agentic-toolkit"},
		{"git@github.com:pedromvgomes/agentic-toolkit", "pedromvgomes/agentic-toolkit"},
		{"https://github.com/pedromvgomes/agentic-toolkit.git", "pedromvgomes/agentic-toolkit"},
		{"https://github.com/pedromvgomes/agentic-toolkit", "pedromvgomes/agentic-toolkit"},
		{"https://github.com/pedromvgomes/agentic-toolkit/", "pedromvgomes/agentic-toolkit"},
		{"ssh://git@github.com/pedromvgomes/agentic-toolkit.git", "pedromvgomes/agentic-toolkit"},
		// A repository whose own name ends in .git is addressed by that name.
		{"https://github.com/owner/dotfiles.git.git", "owner/dotfiles.git"},
	} {
		r := newRepo(t)
		r.git("remote", "add", "origin", tc.url)
		slug, err := review.RemoteSlug(r.dir, "origin")
		if err != nil {
			t.Errorf("%s: %v", tc.url, err)
			continue
		}
		if slug.String() != tc.want {
			t.Errorf("%s resolved to %s, want %s", tc.url, slug, tc.want)
		}
	}
}

// A host that nests repositories is not one a review can be addressed at, and
// taking the last two segments would post to a repository nobody named.
func TestARemoteURLThatIsNotAnOwnerAndRepositoryIsRefused(t *testing.T) {
	for _, url := range []string{
		"https://gitlab.example.com/group/subgroup/project.git",
		"/srv/git/bare-repo.git",
		"",
	} {
		r := newRepo(t)
		if url != "" {
			r.git("remote", "add", "origin", url)
		}
		if _, err := review.RemoteSlug(r.dir, "origin"); err == nil {
			t.Errorf("%q was accepted as a repository a review can be posted to", url)
		}
	}
}

// A commit reaching git travels as a command argument, and a value that is
// only ever hexadecimal cannot be an option or a revision expression whatever
// else is true of it.
func TestAHeadThatIsNotACommitIDIsRefusedBeforeGitIsRun(t *testing.T) {
	r := newRepo(t)
	for _, sha := range []string{"main", "--upload-pack=touch /tmp/x", "HEAD~1", "abc", ""} {
		if _, err := review.FetchPullRequest(r.dir, 7, sha); err == nil {
			t.Errorf("%q was accepted as a commit id", sha)
		}
		if err := review.FetchCommit(r.dir, sha); err == nil {
			t.Errorf("%q was accepted as a commit id", sha)
		}
	}
}

func TestAPullRequestNumberBelowOneIsRefused(t *testing.T) {
	r := newRepo(t)
	sha := strings.Repeat("a", 40)
	for _, n := range []int{0, -1} {
		if _, err := review.FetchPullRequest(r.dir, n, sha); err == nil {
			t.Errorf("%d was accepted as a pull request number", n)
		}
	}
}

// A commit the repository already holds is not fetched: a checkout that has
// the base needs no network to review against it.
func TestACommitAlreadyPresentIsNotFetched(t *testing.T) {
	r := newRepo(t)
	r.write("a.go", "package a\n")
	sha := r.commit("one")
	// No remote is configured, so a fetch would fail loudly rather than
	// silently succeeding and proving nothing.
	if err := review.FetchCommit(r.dir, sha); err != nil {
		t.Fatalf("a commit already in the repository was fetched anyway: %v", err)
	}
}

// The added lines are what decide where an inline comment may go, so the
// parse is held to a patch carrying every shape a real one does: an addition,
// a removal, a hunk that does both, and a second file.
func TestAddedLinesReadsThePostImageOfEveryHunk(t *testing.T) {
	patch := strings.Join([]string{
		"diff --git a/one.go b/one.go",
		"index 111..222 100644",
		"--- a/one.go",
		"+++ b/one.go",
		"@@ -3,0 +4,2 @@ func f() {",
		"+\tadded four",
		"+\tadded five",
		"@@ -10,2 +12,1 @@",
		"-\tgone",
		"-\talso gone",
		"+\treplacement on twelve",
		"diff --git a/two.go b/two.go",
		"--- a/two.go",
		"+++ b/two.go",
		"@@ -1 +1 @@",
		"-\told",
		"+\tnew on one",
		"diff --git a/gone.go b/gone.go",
		"--- a/gone.go",
		"+++ /dev/null",
		"@@ -1,2 +0,0 @@",
		"-\tremoved",
		"-\talso removed",
		"",
	}, "\n")

	added := review.AddedLines(patch)
	want := map[string][]int{
		"one.go": {4, 5, 12},
		"two.go": {1},
	}
	for path, lines := range want {
		got := added[path]
		if len(got) != len(lines) {
			t.Errorf("%s: %v lines are addable, want %v", path, keys(got), lines)
			continue
		}
		for _, line := range lines {
			if !got[line] {
				t.Errorf("%s:%d is not addable, and it is a line this patch adds", path, line)
			}
		}
	}
	if _, ok := added["gone.go"]; ok {
		t.Error("a deleted file offers a line an inline comment could be attached to")
	}
}

// A removed line advances the pre-image and not the post-image. Counting it
// would shift every line after it in the same hunk, so a comment would land
// on code the finding does not describe — and GitHub would accept it.
func TestAddedLinesDoesNotCountRemovedLinesAgainstThePostImage(t *testing.T) {
	patch := strings.Join([]string{
		"diff --git a/x.go b/x.go",
		"--- a/x.go",
		"+++ b/x.go",
		"@@ -5,3 +5,3 @@",
		" \tcontext on five",
		"-\tgone from six",
		"+\tadded on six",
		" \tcontext on seven",
		"",
	}, "\n")
	added := review.AddedLines(patch)["x.go"]
	if !added[6] {
		t.Errorf("line 6 is not addable; the addable lines are %v", keys(added))
	}
	if len(added) != 1 {
		t.Errorf("addable lines are %v, want only 6: context lines are not added lines", keys(added))
	}
}

// The line numbers come from git rather than from this package's idea of a
// diff, which is the half a hand-written patch cannot check.
func TestAddedLinesAtAgreesWithGit(t *testing.T) {
	r := newRepo(t)
	r.write("f.go", "package f\n\nfunc a() {}\nfunc b() {}\n")
	base := r.commit("base")
	r.write("f.go", "package f\n\nfunc a() {}\nfunc inserted() {}\nfunc b() {}\n")
	head := r.commit("head")

	added, err := review.AddedLinesAt(r.dir, base, head)
	if err != nil {
		t.Fatal(err)
	}
	lines := added["f.go"]
	if !lines[4] {
		t.Errorf("line 4 was inserted and is not addable; addable: %v", keys(lines))
	}
	if lines[3] || lines[5] {
		t.Errorf("an unchanged line is addable; addable: %v", keys(lines))
	}
}

func keys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
