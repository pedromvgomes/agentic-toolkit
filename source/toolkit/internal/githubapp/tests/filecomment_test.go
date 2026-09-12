package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// commentsPath is where a comment against a whole file is posted.
const commentsPath = "/repos/acme/widgets/pulls/7/comments"

// subject_type is not a field on a review's draft comments, so a finding that
// hangs off a whole file is its own request after the review.
func TestAFileCommentIsItsOwnRequestAddressedAtTheFile(t *testing.T) {
	c, net := client(t, append(auth(far()), exchange{
		method: http.MethodPost, path: commentsPath, status: 201,
		body: `{"id": 5, "html_url": "https://github.test/c/5"}`,
	})...)

	posted, err := c.CreateFileComment(context.Background(), 7, githubapp.FileComment{
		CommitID: "abc", Path: "a.go", Body: "**RED — correctness**",
	})
	if err != nil {
		t.Fatalf("post the comment: %v", err)
	}
	net.done()
	if posted.HTMLURL != "https://github.test/c/5" {
		t.Errorf("the posted comment read back as %+v", posted)
	}

	sent := net.bodies[len(net.bodies)-1]
	for _, want := range []string{`"subject_type":"file"`, `"path":"a.go"`, `"commit_id":"abc"`} {
		if !strings.Contains(sent, want) {
			t.Errorf("the request does not carry %s:\n%s", want, sent)
		}
	}
	if strings.Contains(sent, `"line"`) {
		t.Errorf("a comment against a whole file names a line:\n%s", sent)
	}
}
