package reviewrun

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	agentic "github.com/pedromvgomes/agentic-driver"
)

// An outage: the run could not be carried out at all.
func TestAnErrorFromTheDriverIsAnOutage(t *testing.T) {
	raw, report := classify(agentic.Result{}, errors.New("no such binary"), "security")
	if raw != nil || report.Available {
		t.Fatalf("an outage produced an answer: %v %+v", raw, report)
	}
	if !strings.Contains(report.Reason, "could not be run") || !strings.Contains(report.Reason, "security") {
		t.Errorf("the outage does not name the run or the cause: %q", report.Reason)
	}
}

// A run that was made and did not answer: an unmet schema constraint, a
// sandbox refusal, or the CLI declaring its own failure. Indistinguishable
// here, and Text carries whatever account exists.
func TestARunThatDeclaredItsOwnFailureCarriesItsAccount(t *testing.T) {
	res := agentic.Result{IsError: true, Text: "the sandbox denied reading /etc"}
	raw, report := classify(res, nil, "security")
	if raw != nil || report.Available {
		t.Fatalf("a declared failure produced an answer: %v %+v", raw, report)
	}
	if !strings.Contains(report.Reason, "sandbox denied") {
		t.Errorf("the report drops the run's own account: %q", report.Reason)
	}
}

// The driver's contract says this cannot happen. It is branched on anyway,
// because the alternative reading is "found nothing" — the one failure a
// review must never make silently.
func TestARunThatAnsweredNeitherWayIsUnavailableRatherThanClean(t *testing.T) {
	raw, report := classify(agentic.Result{}, nil, "correctness")
	if raw != nil || report.Available {
		t.Fatalf("a contentless result was read as an answer: %v %+v", raw, report)
	}
	if !strings.Contains(report.Reason, "cannot happen") {
		t.Errorf("the defensive branch does not say what it is: %q", report.Reason)
	}
	if !strings.Contains(report.Reason, "clean review") {
		t.Errorf("the defensive branch does not say what it refuses to conclude: %q", report.Reason)
	}
}

func TestAStructuredAnswerIsAvailable(t *testing.T) {
	res := agentic.Result{Structured: json.RawMessage(`{"findings":[]}`)}
	raw, report := classify(res, nil, "correctness")
	if !report.Available {
		t.Fatalf("a structured answer was read as a failure: %+v", report)
	}
	if string(raw) != `{"findings":[]}` {
		t.Errorf("the answer was altered: %s", raw)
	}
}

func TestAccountTrimsAndSubstitutesForSilence(t *testing.T) {
	if got := account("   "); !strings.Contains(got, "said nothing") {
		t.Errorf("a silent failure renders as %q", got)
	}
	long := strings.Repeat("x", accountLimit+50)
	got := account(long)
	if len(got) > accountLimit+len("…") {
		t.Errorf("a long account was not trimmed: %d characters", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a trimmed account does not say it was trimmed: %q", got[len(got)-10:])
	}
	if got := account("  boom  "); got != "boom" {
		t.Errorf("account did not trim: %q", got)
	}
}

func TestDecodeFindingsReadsEveryField(t *testing.T) {
	raw := json.RawMessage(`{"findings":[{
      "path":"a.go","start_line":3,"end_line":5,"category":"correctness",
      "severity":"RED","confidence":"high","issue":"boom","evidence":"x := 1",
      "suggestion":"do not"}]}`)

	got, err := decodeFindings(raw, "correctness")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want one finding, got %d", len(got))
	}
	f := got[0]
	if f.Reviewer != "correctness" || f.Path != "a.go" || *f.StartLine != 3 || *f.EndLine != 5 ||
		f.Severity != SeverityRed || f.Issue != "boom" || f.Evidence != "x := 1" || f.Suggestion != "do not" {
		t.Errorf("decoded as %+v", f)
	}
}

// A cross-cutting claim has no line, and null is how the schema says so.
func TestDecodeFindingsAcceptsANullLine(t *testing.T) {
	raw := json.RawMessage(`{"findings":[{"path":"a.go","start_line":null,"end_line":null,
      "category":"design","severity":"AMBER","confidence":"low","issue":"x","evidence":"y"}]}`)
	got, err := decodeFindings(raw, "unified")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].HasLine() {
		t.Fatalf("a null line produced %+v", got)
	}
}

// A finding with no quote has no identity, and an empty fingerprint would
// collide with every other finding that also failed to quote.
func TestDecodeFindingsDropsAFindingWithNoEvidence(t *testing.T) {
	raw := json.RawMessage(`{"findings":[
      {"path":"a.go","start_line":1,"end_line":1,"category":"c","severity":"RED","confidence":"high","issue":"x","evidence":"  "},
      {"path":"b.go","start_line":1,"end_line":1,"category":"c","severity":"RED","confidence":"high","issue":"y","evidence":"z"}]}`)
	got, err := decodeFindings(raw, "correctness")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "b.go" {
		t.Fatalf("an unquotable finding survived: %+v", got)
	}
}

// A severity off the ladder becomes AMBER rather than being dropped or read as
// worst: the claim is still a claim, and its rank is the part that is unknown.
func TestDecodeFindingsNormalisesAnUnknownSeverity(t *testing.T) {
	raw := json.RawMessage(`{"findings":[{"path":"a.go","start_line":1,"end_line":1,
      "category":"c","severity":"CRITICAL","confidence":"high","issue":"x","evidence":"y"}]}`)
	got, err := decodeFindings(raw, "correctness")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Severity != SeverityAmber {
		t.Fatalf("an unknown severity decoded as %+v", got)
	}
}

func TestDecodeFindingsRefusesMalformedJSON(t *testing.T) {
	if _, err := decodeFindings(json.RawMessage(`{"findings":`), "correctness"); err == nil {
		t.Fatal("malformed JSON decoded without complaint")
	}
}

// The schemas are the single definition of what a run answers with, so they
// have to be JSON before anything is sent.
func TestEverySchemaIsValidJSON(t *testing.T) {
	for name, schema := range map[string]json.RawMessage{
		"finding": findingSchema, "validator": validatorSchema, "judge": judgeSchema,
	} {
		var v any
		if err := json.Unmarshal(schema, &v); err != nil {
			t.Errorf("the %s schema is not valid JSON: %v", name, err)
		}
	}
}

// The judge answers with ids and never with a path, a line or a quote. See
// ADR 0008: a pass that can rewrite a quote can break a finding's identity.
func TestTheJudgeSchemaCannotCarryEvidence(t *testing.T) {
	var schema struct {
		Properties struct {
			Findings struct {
				Items struct {
					Properties map[string]any `json:"properties"`
				} `json:"items"`
			} `json:"findings"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(judgeSchema, &schema); err != nil {
		t.Fatal(err)
	}
	props := schema.Properties.Findings.Items.Properties
	for _, forbidden := range []string{"evidence", "path", "file", "start_line", "end_line", "line"} {
		if _, present := props[forbidden]; present {
			t.Errorf("the judge schema lets the judge re-emit %q", forbidden)
		}
	}
	if _, present := props["id"]; !present {
		t.Error("the judge schema has no id to answer with")
	}
}

// A CLI's own error message is exactly where a non-ASCII path or a localised
// string turns up, and a byte-sliced truncation leaves invalid UTF-8 in the
// reason a person reads and in the --json output.
func TestAccountTrimsOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("é", accountLimit)
	got := account(long)
	if !utf8.ValidString(got) {
		t.Errorf("account() produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a trimmed account does not say it was trimmed: %q", got)
	}
}
