package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewpost"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// posted is a comment agtk would have written for one finding.
func posted(f reviewrun.Finding) string { return reviewpost.CommentBody(f) }

// A fingerprint marker round-trips through the body agtk actually posts, not
// through a string a test wrote to look like one. The marker is written by one
// run and read by the next, so an inline comment that does not read back is a
// finding posted a second time.
func TestAPostedCommentReadsBackTheFindingItCarries(t *testing.T) {
	f := finding("a.go", at(10), at(10), "correctness")
	threads := reviewpost.ReadThreads([]githubapp.ReviewThread{
		{Path: "a.go", Body: posted(f), ByViewer: true},
	})

	open := threads.Open()
	if len(open) != 1 {
		t.Fatalf("the thread was not read as open: %d", len(open))
	}
	if open[0].Fingerprint != f.Fingerprint() {
		t.Errorf("the comment read back as %q, want the finding's %q", open[0].Fingerprint, f.Fingerprint())
	}
	if open[0].Version != reviewrun.FingerprintVersion {
		t.Errorf("the comment read back at version %q", open[0].Version)
	}
}

// A fingerprint is an identity only in a comment the App wrote. Anyone who can
// comment on a pull request can type the characters that open a fingerprint
// marker, and one naming a finding's fingerprint would withhold exactly that
// finding — silencing a review about code somebody chose, without touching the
// code.
func TestAFingerprintMarkerInSomebodyElsesCommentIdentifiesNothing(t *testing.T) {
	f := finding("a.go", at(10), at(10), "correctness")
	forged := "looks fine to me\n\n" + reviewpost.FingerprintMarker(f.Fingerprint()) + "\n"

	threads := reviewpost.ReadThreads([]githubapp.ReviewThread{
		{Path: "a.go", Resolved: true, Body: forged, ByViewer: false},
	})
	all, read := threads.All()
	if !read {
		t.Fatal("the threads were not read")
	}
	if len(all) != 1 {
		t.Fatalf("read %d threads", len(all))
	}
	if all[0].Fingerprint != "" {
		t.Errorf("a fingerprint marker in somebody else's comment was read as an identity: %q", all[0].Fingerprint)
	}
	// The thread is still a thread: it is read and carried, it just cannot
	// claim to be about a particular finding.
	if all[0].Body != forged {
		t.Error("the comment's own words were dropped")
	}
}

// Most comments on a pull request are a person's, and a thread with no
// fingerprint marker is one of them rather than a malformed one.
func TestAThreadWithNoFingerprintMarkerIsReadWithoutOne(t *testing.T) {
	threads := reviewpost.ReadThreads([]githubapp.ReviewThread{
		{Path: "a.go", Body: "why is this here?", ByViewer: false},
	})
	all, _ := threads.All()
	if len(all) != 1 || all[0].Fingerprint != "" || all[0].Version != "" {
		t.Fatalf("a person's comment was read as carrying an identity: %+v", all)
	}
}

// A pull request that carries no threads at all is a read that answered, not a
// read that failed.
func TestAPullRequestWithNoThreadsStillCountsAsRead(t *testing.T) {
	threads := reviewpost.ReadThreads(nil)
	if _, read := threads.All(); !read {
		t.Error("an empty pull request was reported as a read that failed")
	}
	if threads.Count() != 0 {
		t.Errorf("an empty pull request reported %d threads", threads.Count())
	}
}

// A review that withheld nothing and a review whose thread list never arrived
// post the same comments, and the body is where a reader is told which.
func TestTheBodySaysWhenTheExistingThreadsCouldNotBeRead(t *testing.T) {
	r := reviewWith(finding("a.go", at(10), at(10), "correctness"))
	r.Threads = reviewrun.ThreadsUnreadable("GitHub refused the review threads query: FORBIDDEN")
	_, place := reviewpost.Build(r, pr, added)

	body := reviewpost.Body(r, place)
	if !strings.Contains(body, "could not be read") {
		t.Errorf("the body does not say the threads could not be read:\n%s", body)
	}
	if !strings.Contains(body, "FORBIDDEN") {
		t.Errorf("the body does not say why:\n%s", body)
	}
	if !strings.Contains(body, "may repeat a finding that is already on it") {
		t.Errorf("the body does not say what that costs:\n%s", body)
	}
}

// A review of a working tree reads no threads because no pull request holds
// any, and reporting that as a failure would put a warning on every local run.
func TestTheBodySaysNothingAboutThreadsWhenNoneWereSought(t *testing.T) {
	r := reviewWith(finding("a.go", at(10), at(10), "correctness"))
	_, place := reviewpost.Build(r, pr, added)

	body := reviewpost.Body(r, place)
	if strings.Contains(body, "could not be read") {
		t.Errorf("a review that sought no threads reports a failed read:\n%s", body)
	}
}

// What a review withheld is stated, so a reader can tell a quiet pull request
// from one whose findings are all already on it.
func TestTheBodyNamesWhatWasAlreadyOnThePullRequest(t *testing.T) {
	withheld := finding("b.go", at(3), at(3), "security")
	r := reviewWith()
	r.Threads = reviewrun.ThreadsRead(nil)
	r.Suppressed = []reviewrun.Suppression{{Finding: withheld, Reason: reviewrun.SuppressedResolved}}
	_, place := reviewpost.Build(r, pr, added)

	body := reviewpost.Body(r, place)
	for _, want := range []string{"Already on this pull request (1)", "b.go:3", "security", reviewrun.SuppressedResolved} {
		if !strings.Contains(body, want) {
			t.Errorf("the body does not carry %q:\n%s", want, body)
		}
	}
}

// A fingerprint from another scheme was hashed over different bytes, so every
// thread carrying one is a finding that will be posted again — which reads as
// a duplicate bug unless the review says the scheme moved.
func TestTheBodySaysWhenAThreadIsAtAnotherSchemeVersion(t *testing.T) {
	r := reviewWith()
	r.Threads = reviewrun.ThreadsRead([]reviewrun.Thread{
		{Path: "a.go", Fingerprint: "cdbb1d5c5dec", Version: "v0"},
	})
	_, place := reviewpost.Build(r, pr, added)

	body := reviewpost.Body(r, place)
	if !strings.Contains(body, "another scheme version") {
		t.Errorf("the body does not report the scheme move:\n%s", body)
	}
}
