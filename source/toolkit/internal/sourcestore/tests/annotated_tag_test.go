package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/sourceref"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

// tagFixture adds a tag to the bare repo behind url. When annotated is
// true the tag is a real tag object, so `git ls-remote <tag>` reports the
// tag object's own SHA and the commit only appears on the peeled
// `<tag>^{}` line. Returns the SHA the tag ultimately points at (the
// commit), which is what a consumer pinning that tag must end up with.
func tagFixture(t *testing.T, url, tag string, annotated bool) (commitSHA string) {
	t.Helper()
	bare := strings.TrimPrefix(url, "file://")
	if annotated {
		runGitOK(t, bare,
			"-c", "user.email=t@t",
			"-c", "user.name=t",
			"-c", "tag.gpgsign=false",
			"tag", "-a", tag, "-m", "release "+tag,
		)
	} else {
		runGitOK(t, bare, "tag", tag)
	}
	return strings.TrimSpace(runGitOut(t, bare, "rev-parse", tag+"^{commit}"))
}

// TestLiveProvider_AnnotatedTag_ResolvesToCommit pins the behavior that
// makes a tagged release installable. `git ls-remote` reports an
// annotated tag's own object SHA first and the commit only on the peeled
// `^{}` line; fetching that ref checks out the commit. Resolving to the
// tag object instead means the recorded SHA never equals what the fetch
// produces, and every consumer pinning the tag fails verification.
func TestLiveProvider_AnnotatedTag_ResolvesToCommit(t *testing.T) {
	url, headSHA := fixtureRepo(t, map[string]string{
		"definitions/skills/foo/SKILL.md": "skill foo",
	})
	commitSHA := tagFixture(t, url, "v1.0.0", true)
	if commitSHA != headSHA {
		t.Fatalf("fixture: tag commit %q != HEAD %q", commitSHA, headSHA)
	}

	cache := sourcestore.NewCache(t.TempDir())
	provider := sourcestore.NewLiveProvider(cache)

	fsys, rr, err := provider.Provide(sourceref.Source{URL: url, Ref: "v1.0.0"})
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if rr.SHA != commitSHA {
		t.Errorf("SHA = %q, want the commit %q (an annotated tag must peel to its commit)", rr.SHA, commitSHA)
	}
	if rr.Ref != "v1.0.0" {
		t.Errorf("Ref = %q, want v1.0.0", rr.Ref)
	}
	if got := readFSFile(t, fsys, "definitions/skills/foo/SKILL.md"); got != "skill foo" {
		t.Errorf("file content mismatch: %q", got)
	}
}

// TestLiveProvider_LightweightTag_ResolvesToCommit is the control: a
// lightweight tag points straight at the commit and has no peeled line,
// so it must keep resolving exactly as before.
func TestLiveProvider_LightweightTag_ResolvesToCommit(t *testing.T) {
	url, headSHA := fixtureRepo(t, map[string]string{
		"definitions/skills/foo/SKILL.md": "skill foo",
	})
	tagFixture(t, url, "v1.0.0", false)

	cache := sourcestore.NewCache(t.TempDir())
	provider := sourcestore.NewLiveProvider(cache)

	_, rr, err := provider.Provide(sourceref.Source{URL: url, Ref: "v1.0.0"})
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if rr.SHA != headSHA {
		t.Errorf("SHA = %q, want %q", rr.SHA, headSHA)
	}
}
