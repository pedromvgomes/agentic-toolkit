package tests

import (
	"context"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/prwait"
)

const referralComment = "<!-- lydite:results -->\n" +
	"### 🟡 Needs attention\n" +
	"\n" +
	"referral needs a human — comment `/lydite clear` to resolve\n" +
	"\n" +
	"<details>\n" +
	"<summary>🟡 <b>referral</b> — 1 referred, 1 passed</summary>\n" +
	"\n" +
	"| | check | result |\n" +
	"|---|---|---|\n" +
	"| 🟡 | nosec | `#nosec` added in a.go |\n" +
	"| ✅ | deleted-tests | none |\n" +
	"</details>\n" +
	"<details>\n" +
	"<summary>✅ <b>scan</b> — 5 passed</summary>\n" +
	"\n" +
	"| ✅ | gosec | clean |\n" +
	"| ◽ | sbom | informational |\n" +
	"</details>\n" +
	"\n" +
	"<sub><code>0.5.0 · base 1b7c664629c0</code></sub>\n"

const redComment = "<!-- lydite:results -->\n" +
	"### ❌ Failing\n" +
	"<details>\n" +
	"<summary>❌ <b>test</b> — 1 failed, 5 passed</summary>\n" +
	"\n" +
	"| ❌ | go test | TestX failed |\n" +
	"| ✅ | race | passed |\n" +
	"</details>\n"

func TestParseLyditeReadsTheHeadingMarkAndTheReferredRows(t *testing.T) {
	got := prwait.ParseLydite(referralComment)
	if got.Heading != "🟡 Needs attention" || got.Verdict != "referral" {
		t.Errorf("heading %q, verdict %q", got.Heading, got.Verdict)
	}
	want := []prwait.LyditeRow{{Section: "referral", Mark: "🟡", Text: "nosec | `#nosec` added in a.go"}}
	if len(got.Rows) != 1 || got.Rows[0] != want[0] {
		t.Errorf("rows = %+v, want %+v", got.Rows, want)
	}
}

func TestParseLyditeReadsARedHeadingAndItsFailedRows(t *testing.T) {
	got := prwait.ParseLydite(redComment)
	if got.Verdict != "red" || len(got.Rows) != 1 || got.Rows[0].Section != "test" || got.Rows[0].Mark != "❌" {
		t.Errorf("parsed %+v", got)
	}
}

func TestParseLyditeCallsAnUnmarkedHeadingUnknown(t *testing.T) {
	got := prwait.ParseLydite("<!-- lydite:results -->\n### Something else\n")
	if got.Verdict != "unknown" || got.Rows == nil {
		t.Errorf("parsed %+v, want verdict unknown and rows never nil", got)
	}
	green := prwait.ParseLydite("<!-- lydite:results -->\n### ✅ All green\n")
	if green.Verdict != "green" {
		t.Errorf("green heading parsed as %q", green.Verdict)
	}
}

// redHead is a head whose failing lydite status asks for the comment's detail.
func redHead(comments ...githubapp.IssueComment) *fakeReader {
	return &fakeReader{states: []state{{
		pr:       open(headA),
		statuses: []githubapp.CommitStatus{status("lydite/test", "failure")},
		comments: comments,
	}}}
}

func waitFor(reader *fakeReader, app string) prwait.Result {
	return prwait.Wait(context.Background(), reader, prwait.Options{
		Number: 7, Poll: time.Minute, Cap: time.Hour, Clock: newClock(), LyditeApp: app,
	})
}

func TestAMarkerCommentFromAnyOtherBotGivesNoDetail(t *testing.T) {
	for _, author := range []string{"someapp[bot]", "github-actions[bot]", "lydite", "lydite[bot]x", "xlydite[bot]"} {
		res := waitFor(redHead(githubapp.IssueComment{ID: 1, Author: author, Body: redComment}), "")
		if res.Verdict != prwait.ActionNeeded {
			t.Fatalf("%s: verdict %q, want action-needed", author, res.Verdict)
		}
		if res.Lydite != nil {
			t.Errorf("a marker comment by %q was read as lydite's: %+v", author, res.Lydite)
		}
	}
}

func TestAMarkerCommentFromTheLyditeAppsBotGivesDetailInAnyCase(t *testing.T) {
	for _, author := range []string{"lydite[bot]", "Lydite[Bot]"} {
		res := waitFor(redHead(githubapp.IssueComment{ID: 1, Author: author, Body: redComment}), "")
		if res.Lydite == nil || res.Lydite.Verdict != "red" {
			t.Errorf("%s: lydite = %+v, want the red comment", author, res.Lydite)
		}
	}
}

func TestOnlyTheConfiguredLyditeAppsBotCounts(t *testing.T) {
	if res := waitFor(redHead(githubapp.IssueComment{ID: 1, Author: "lydite[bot]", Body: redComment}), "other"); res.Lydite != nil {
		t.Errorf("lydite[bot] was read with the App set to other: %+v", res.Lydite)
	}
	if res := waitFor(redHead(githubapp.IssueComment{ID: 1, Author: "other[bot]", Body: redComment}), "other"); res.Lydite == nil || res.Lydite.Verdict != "red" {
		t.Errorf("other[bot] was not read with the App set to other: %+v", res.Lydite)
	}
}

func TestALaterForgedMarkerCommentDoesNotDisplaceTheRealOne(t *testing.T) {
	genuine := githubapp.IssueComment{ID: 1, Author: "lydite[bot]", Body: redComment, UpdatedAt: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)}
	forged := githubapp.IssueComment{ID: 2, Author: "github-actions[bot]", Body: "<!-- lydite:results -->\n### ✅ All green\n", UpdatedAt: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)}
	res := waitFor(redHead(genuine, forged), "")
	if res.Lydite == nil || res.Lydite.Verdict != "red" || !res.Lydite.UpdatedAt.Equal(genuine.UpdatedAt) {
		t.Errorf("lydite = %+v, want the genuine red comment", res.Lydite)
	}
}
