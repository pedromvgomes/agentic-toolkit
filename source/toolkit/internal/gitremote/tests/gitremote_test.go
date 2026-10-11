package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/gitremote"
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
		slug, err := gitremote.RemoteSlug(r.dir, "origin")
		if err != nil {
			t.Errorf("%s: %v", tc.url, err)
			continue
		}
		if slug.String() != tc.want {
			t.Errorf("%s resolved to %s, want %s", tc.url, slug, tc.want)
		}
	}
}

// A host that nests repositories is not one a repository can be addressed at,
// and taking the last two segments would address a repository nobody named.
//
// The slug is interpolated into the API path, so a name carrying `?`, `#` or
// an encoded slash re-points the request at a resource the remote does not
// name — `a?x=y/b` turns the rest of the path into a query and `owner/repo#x`
// truncates it. Those are refused here rather than reshaping a request.
func TestARemoteURLThatIsNotAnOwnerAndRepositoryIsRefused(t *testing.T) {
	for _, url := range []string{
		"https://gitlab.example.com/group/subgroup/project.git",
		"/srv/git/bare-repo.git",
		"git@github.com:a?x=y/b",
		"git@github.com:acme/widgets#x",
		"https://github.com/a%2Fb/c",
		"https://github.com/acme/wid gets",
		"https://github.com/-acme/widgets",
		"",
	} {
		r := newRepo(t)
		if url != "" {
			r.git("remote", "add", "origin", url)
		}
		if _, err := gitremote.RemoteSlug(r.dir, "origin"); err == nil {
			t.Errorf("%q was accepted as an owner/repository address", url)
		}
	}
}

func TestAnEmptyRemoteNameReadsTheDefaultRemote(t *testing.T) {
	r := newRepo(t)
	r.git("remote", "add", gitremote.DefaultRemote, "https://github.com/acme/widgets")
	slug, err := gitremote.RemoteSlug(r.dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if slug.String() != "acme/widgets" {
		t.Errorf("resolved to %s, want acme/widgets", slug)
	}
}

// A remote that is not configured makes git exit non-zero without a word on
// stderr, so the exit status is all the error can carry.
func TestAMissingRemoteIsReportedWithGitsExitStatus(t *testing.T) {
	r := newRepo(t)
	_, err := gitremote.RemoteSlug(r.dir, "origin")
	if err == nil {
		t.Fatal("a repository with no origin was accepted")
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("the exit status is missing from %q", err)
	}
}

// When git does say why it failed, that message is what the error carries.
func TestAnUnreadableConfigIsReportedWithGitsOwnMessage(t *testing.T) {
	r := newRepo(t)
	if err := os.WriteFile(filepath.Join(r.dir, ".git", "config"), []byte("[remote\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := gitremote.RemoteSlug(r.dir, "origin")
	if err == nil {
		t.Fatal("an unreadable config was accepted")
	}
	if !strings.Contains(err.Error(), "bad config") || strings.Contains(err.Error(), "exit status") {
		t.Errorf("git's message is missing from %q", err)
	}
}
