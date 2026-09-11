package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/reviewrun"
)

func line(n int) *int { return &n }

// The JSON carries both the id and the fingerprint because they answer
// different questions: the id is what the judge was asked about in this run
// and means nothing outside it, and the fingerprint is what identifies the
// finding on a later review of the same change.
func TestReviewJSONCarriesTheFingerprintAlongsideTheRunLocalID(t *testing.T) {
	f := reviewrun.Finding{
		ID: "f1", Reviewer: "security", Path: "a.go",
		StartLine: line(3), EndLine: line(5),
		Category: "security:injection", Severity: reviewrun.SeverityRed,
		Confidence: "high", Issue: "unparameterised query", Evidence: "db.Query(x)",
		Suggestion: "parameterise it", Corroboration: 2,
		Verdict: &reviewrun.Verdict{Verdict: reviewrun.VerdictUpheld},
	}
	out := reviewJSON(&reviewrun.Review{
		Available: true, Panel: "deep", Manifest: "m", Range: "main...HEAD",
		Findings: []reviewrun.Finding{f},
		Good:     []string{"clear naming"},
		Reports: []reviewrun.RunReport{
			{Label: "security", Role: reviewrun.RoleReviewer, Provider: "claudecode",
				Model: "opus", Report: reviewrun.Answered([]reviewrun.Finding{f}), CostUSD: 0.5},
			{Label: "correctness", Role: reviewrun.RoleReviewer, Provider: "claudecode",
				Report: reviewrun.Unavailable("timed out")},
		},
		Skipped:            []reviewrun.Skipped{{Path: "AGENTS.md", Reason: reviewrun.SkipInstructionFile}},
		Conventions:        []string{"CLAUDE.md"},
		DiscardedIDs:       []string{"f99"},
		DroppedByValidator: 1,
		CostUSD:            0.5,
	})

	if out.Version != jsonVersion {
		t.Errorf("version is %d, want %d", out.Version, jsonVersion)
	}
	if len(out.Findings) != 1 {
		t.Fatalf("want one finding, got %d", len(out.Findings))
	}
	got := out.Findings[0]
	if got.ID != "f1" {
		t.Errorf("id is %q", got.ID)
	}
	if got.Fingerprint != f.Fingerprint() {
		t.Errorf("fingerprint is %q, want %q", got.Fingerprint, f.Fingerprint())
	}
	if got.Evidence != "db.Query(x)" {
		t.Errorf("the evidence was altered: %q", got.Evidence)
	}
	if *got.StartLine != 3 || *got.EndLine != 5 {
		t.Errorf("the lines were altered: %v-%v", got.StartLine, got.EndLine)
	}
	if got.Verdict != reviewrun.VerdictUpheld {
		t.Errorf("the verdict is %q", got.Verdict)
	}
	if got.Corroboration != 2 {
		t.Errorf("corroboration is %d", got.Corroboration)
	}

	if !out.Partial {
		t.Error("a review with a failed run does not report itself as partial in JSON")
	}
	if len(out.Runs) != 2 {
		t.Fatalf("want two runs, got %d", len(out.Runs))
	}
	if out.Runs[1].Available || out.Runs[1].Reason != "timed out" {
		t.Errorf("the failed run is misreported: %+v", out.Runs[1])
	}
	if len(out.Skipped) != 1 || out.Skipped[0].Path != "AGENTS.md" {
		t.Errorf("the filtered paths are missing: %+v", out.Skipped)
	}
	if out.Dropped != 1 || len(out.Discarded) != 1 {
		t.Errorf("the validator drop or the discarded id is missing: %+v", out)
	}
}

// A cross-cutting finding has no line, and the JSON says null rather than
// zero — line 0 does not exist and would be posted as a real position.
func TestReviewJSONEmitsNullForAFindingWithNoLine(t *testing.T) {
	out := reviewJSON(&reviewrun.Review{
		Available: true,
		Findings:  []reviewrun.Finding{{ID: "f1", Path: "a.go", Evidence: "x", Severity: reviewrun.SeverityAmber}},
	})
	raw, err := json.Marshal(out.Findings[0])
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["start_line"] != nil {
		t.Errorf("a finding with no line emitted start_line %v", decoded["start_line"])
	}
}

// A review that could not reach a verdict must say so in JSON too, and must
// not emit an empty findings list that a consumer would read as clean.
func TestReviewJSONReportsAnUnavailableReview(t *testing.T) {
	out := reviewJSON(&reviewrun.Review{
		Panel: "deep", Reason: "no reviewer answered, so nothing looked at this change",
	})
	if out.Available {
		t.Error("an unavailable review reports itself as available in JSON")
	}
	if out.Reason == "" {
		t.Error("an unavailable review gives no reason in JSON")
	}
}

// An empty list rather than null, so a consumer iterating it does not have to
// special-case the clean review.
func TestReviewJSONEmitsEmptyListsRatherThanNull(t *testing.T) {
	raw, err := json.Marshal(reviewJSON(&reviewrun.Review{Available: true}))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"findings", "runs"} {
		if decoded[key] == nil {
			t.Errorf("%q is null rather than an empty list", key)
		}
	}
}

// explainFixture is a manifest and a selection over it, built directly so the
// JSON rendering is tested without a repository or a diff.
func explainFixture(t *testing.T) (*review.Manifest, *review.Selection) {
	t.Helper()
	m, err := review.ParseBytes("manifest.yaml", []byte(`version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  quick: {reviewers: [correctness]}
  deep:  {reviewers: [correctness], quorum: 3, validate: true}
defaults: {worktree: quick, pr: quick}
escalate:
  - to: deep
    any:
      - signals: {in: [concurrency]}
`))
	if err != nil {
		t.Fatal(err)
	}
	return m, &review.Selection{
		Context: review.ContextWorktree, Default: "quick", Panel: "deep", Validates: true,
		Skipped: []review.SkippedRule{{Index: 0, To: "deep", Reason: "could not be read"}},
	}
}

// A count that could not be computed is null, never zero: unavailable is
// never low, and a consumer must not be able to read it as a small count.
func TestExplainJSONEmitsNullForAnUnavailableCount(t *testing.T) {
	m, sel := explainFixture(t)
	set := review.NewSignalSet()
	set.MarkUndetermined(review.SignalConcurrency, "the blame budget ran out")
	p := &review.Profile{
		Signals:          set,
		ReferencingFiles: review.UnavailableCount("no extractor for this language"),
	}

	out := explainJSON("manifest.yaml", "main...working tree", m, p, sel)
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Change struct {
			ReferencingFiles struct {
				Value  *int   `json:"value"`
				Reason string `json:"reason"`
			} `json:"referencing_files"`
			Undetermined []struct {
				Signal string `json:"signal"`
				Reason string `json:"reason"`
			} `json:"undetermined_signals"`
		} `json:"change"`
		Skipped []struct {
			Reason string `json:"reason"`
		} `json:"skipped"`
		ValidationReason string `json:"validation_reason"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Change.ReferencingFiles.Value != nil {
		t.Errorf("an unavailable count was emitted as %d", *decoded.Change.ReferencingFiles.Value)
	}
	if decoded.Change.ReferencingFiles.Reason != "no extractor for this language" {
		t.Errorf("the reason the count is unavailable is missing: %q", decoded.Change.ReferencingFiles.Reason)
	}
	if len(decoded.Change.Undetermined) != 1 || decoded.Change.Undetermined[0].Signal != "concurrency" ||
		decoded.Change.Undetermined[0].Reason != "the blame budget ran out" {
		t.Errorf("the undetermined signal is misreported: %+v", decoded.Change.Undetermined)
	}
	if len(decoded.Skipped) != 1 || decoded.Skipped[0].Reason != "could not be read" {
		t.Errorf("the skipped rule is misreported: %+v", decoded.Skipped)
	}
	if decoded.ValidationReason != "the panel asks for it" {
		t.Errorf("validation reason is %q", decoded.ValidationReason)
	}
}

// The panel listing carries what a caller chooses on: the description, the
// cost broken into its factors, and which contexts start from it.
func TestPanelsJSONCarriesWhatAPanelIsForAndWhatItSpends(t *testing.T) {
	m, _ := explainFixture(t)
	out := panelsJSON("manifest.yaml", review.ContextPR, m)

	if out.Version != jsonVersion || out.Context != "pr" || out.Manifest != "manifest.yaml" {
		t.Errorf("the header is misreported: %+v", out)
	}
	if len(out.Panels) != 2 || out.Panels[0].Name != "quick" || out.Panels[1].Name != "deep" {
		t.Fatalf("panels are not listed shallowest first: %+v", out.Panels)
	}
	deep := out.Panels[1]
	if deep.Quorum != 3 || deep.Runs != 3 || len(deep.Reviewers) != 1 || deep.Reviewers[0] != "correctness" {
		t.Errorf("deep's cost is misreported: %+v", deep)
	}
	if len(deep.DefaultFor) != 0 {
		t.Errorf("deep is nobody's default: %+v", deep.DefaultFor)
	}
	quick := out.Panels[0]
	if len(quick.DefaultFor) != 2 || quick.DefaultFor[0] != "worktree" || quick.DefaultFor[1] != "pr" {
		t.Errorf("quick's defaults are misreported: %+v", quick.DefaultFor)
	}
}

// Panels that spend the same are the same depth, so the listing falls back to
// the name. Without the fallback, equal-cost panels come out in whatever order
// the runtime walked the map, and a caller offering a choice of depth shows a
// different order on every invocation.
func TestPanelsJSONBreaksACostTieByName(t *testing.T) {
	m, err := review.ParseBytes("manifest.yaml", []byte(`version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
  security:    {provider: claudecode, prompt: builtin:security}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  zulu:  {reviewers: [correctness]}
  alpha: {reviewers: [security]}
  mike:  {reviewers: [correctness, security]}
defaults: {worktree: alpha, pr: mike}
`))
	if err != nil {
		t.Fatal(err)
	}

	out := panelsJSON("manifest.yaml", review.ContextWorktree, m)
	got := make([]string, 0, len(out.Panels))
	for _, p := range out.Panels {
		got = append(got, p.Name)
	}
	want := []string{"alpha", "zulu", "mike"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("panels listed %v, want %v — equal cost orders by name, and mike is deeper", got, want)
	}
	if out.Panels[0].Runs != out.Panels[1].Runs {
		t.Fatalf("the fixture no longer ties: %d and %d runs", out.Panels[0].Runs, out.Panels[1].Runs)
	}
}

// A file the change did not really author is named with the reason it does not
// count, so a reader asking why a panel is shallower than expected can see
// what was left out.
func TestExplainJSONNamesTheExcludedFilesAndWhy(t *testing.T) {
	m, sel := explainFixture(t)
	p := &review.Profile{
		Signals:          review.NewSignalSet(),
		ReferencingFiles: review.AvailableCount(0),
		Files: []review.ChangedFile{
			{DiffFile: review.DiffFile{Path: "internal/app/main.go", Added: 3}, Language: review.LangGo},
			{DiffFile: review.DiffFile{Path: "go.sum", Added: 400}, Excluded: review.ExcludedLockfile},
			{DiffFile: review.DiffFile{Path: "assets/logo.png"}, Excluded: review.ExcludedBinary},
		},
	}

	out := explainJSON("manifest.yaml", "main...working tree", m, p, sel)
	if len(out.Change.Excluded) != 2 {
		t.Fatalf("want two excluded files, got %+v", out.Change.Excluded)
	}
	byPath := map[string]string{}
	for _, f := range out.Change.Excluded {
		byPath[f.Path] = f.Reason
	}
	if byPath["go.sum"] != "lockfile" || byPath["assets/logo.png"] != "binary" {
		t.Errorf("the exclusions are misreported: %+v", out.Change.Excluded)
	}
	// The reviewable file is not among them: `excluded` says what was left
	// out, not what the change contains.
	if _, listed := byPath["internal/app/main.go"]; listed {
		t.Errorf("a reviewable file is listed as excluded: %+v", out.Change.Excluded)
	}
}
