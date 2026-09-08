package tests

import (
	"context"
	"encoding/json"
	"testing"
)

// repliedThreadNode renders one thread with its replies, as the approval query
// asks for them.
func repliedThreadNode(path string, resolved bool, comments string, moreComments bool) string {
	return `{"path":"` + path + `","isResolved":` + boolText(resolved) +
		`,"isOutdated":false,"comments":{"pageInfo":{"hasNextPage":` + boolText(moreComments) +
		`},"nodes":[` + comments + `]}}`
}

// commentNode renders one comment on a thread.
func commentNode(body string, byViewer bool, association string) string {
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return `{"body":` + string(encoded) + `,"viewerDidAuthor":` + boolText(byViewer) +
		`,"authorAssociation":"` + association + `"}`
}

// A finding is cleared by a reply, and write access is read per comment: a
// thread agtk opened carries replies from the change's author and from a
// maintainer, and only one of those clears anything.
func TestAThreadCarriesItsRepliesAndWhatEachAuthorMayDo(t *testing.T) {
	c, net := client(t, append(auth(far()), query(threadsPage(
		repliedThreadNode("a.go", true,
			commentNode("the finding", true, "NONE")+","+
				commentNode("I disagree", false, "CONTRIBUTOR")+","+
				commentNode("agtk: false positive — guarded upstream", false, "OWNER"),
			false),
		false, "")))...)

	threads, err := c.ReadAnsweredThreads(context.Background(), 7)
	if err != nil {
		t.Fatalf("read the threads: %v", err)
	}
	net.done()

	if len(threads) != 1 {
		t.Fatalf("read %d threads, want 1", len(threads))
	}
	thread := threads[0]
	if thread.Body != "the finding" || !thread.ByViewer {
		t.Errorf("the root comment read back as %q by-viewer=%v", thread.Body, thread.ByViewer)
	}
	if len(thread.Replies) != 2 {
		t.Fatalf("read %d replies, want the two comments under the root: %+v", len(thread.Replies), thread.Replies)
	}
	if thread.Replies[0].CanWrite() {
		t.Error("a CONTRIBUTOR reply reports write access")
	}
	if !thread.Replies[1].CanWrite() {
		t.Error("an OWNER reply reports no write access")
	}
	if thread.Truncated {
		t.Error("a thread read whole reports itself truncated")
	}
}

// A marking past the cut is a marking approval did not read. Saying so is what
// lets it refuse on what it cannot see rather than grant on what it did not
// read.
func TestAThreadWithMoreRepliesThanOnePageSaysSo(t *testing.T) {
	c, net := client(t, append(auth(far()), query(threadsPage(
		repliedThreadNode("a.go", true, commentNode("the finding", true, "NONE"), true),
		false, "")))...)

	threads, err := c.ReadAnsweredThreads(context.Background(), 7)
	if err != nil {
		t.Fatalf("read the threads: %v", err)
	}
	net.done()
	if len(threads) != 1 || !threads[0].Truncated {
		t.Errorf("a thread with unread replies does not report itself truncated: %+v", threads)
	}
}

// A review is bound to a commit, and its body is where the review marker
// lives. Both have to come back, and a review somebody else posted has to come
// back marked as theirs.
func TestSubmittedReviewsCarryTheirCommitAndBody(t *testing.T) {
	page := `{"data":{"repository":{"pullRequest":{"reviews":{` +
		`"pageInfo":{"hasNextPage":false,"endCursor":""},"nodes":[` +
		`{"body":"by the app","commit":{"oid":"abc"},"viewerDidAuthor":true},` +
		`{"body":"by a person","commit":{"oid":"abc"},"viewerDidAuthor":false}` +
		`]}}}}}`
	c, net := client(t, append(auth(far()), query(page))...)

	reviews, err := c.ReadSubmittedReviews(context.Background(), 7)
	if err != nil {
		t.Fatalf("read the reviews: %v", err)
	}
	net.done()

	if len(reviews) != 2 {
		t.Fatalf("read %d reviews, want 2", len(reviews))
	}
	if reviews[0].Body != "by the app" || reviews[0].CommitSHA != "abc" || !reviews[0].ByViewer {
		t.Errorf("this installation's review read back as %+v", reviews[0])
	}
	if reviews[1].ByViewer {
		t.Error("a review somebody else posted reads back as this installation's")
	}
}
