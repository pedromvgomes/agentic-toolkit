package tests

import (
	"testing"

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
