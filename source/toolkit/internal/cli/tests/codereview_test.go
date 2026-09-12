package tests

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// runGit runs one git command in dir, failing the test if it does not succeed.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// writeIn writes one file at a repo-relative path inside dir.
func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), body)
}

// gitProject builds a throwaway repository with one commit on main and the
// named files left uncommitted, which is the working-tree change a review
// would look at.
func gitProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) { runGit(t, dir, args...) }
	write := func(name, body string) { writeIn(t, dir, name, body) }

	run("init", "-b", "main")
	run("config", "user.email", "test@example.invalid")
	run("config", "user.name", "Test")
	// A contributor whose global config signs commits would otherwise need a
	// signing key present for this fixture to commit at all.
	run("config", "commit.gpgsign", "false")
	write("README.md", "# base\n")
	run("add", "-A")
	run("commit", "-m", "base")

	for name, body := range files {
		write(name, body)
	}
	return dir
}

// Deciding which panel a change gets must not need a model, a provider or an
// agent CLI. It is the answer to "why is this review deeper than I expected",
// and it has to be available before paying for the review that would tell you.
func TestCodeReviewExplainNeedsNoProvider(t *testing.T) {
	work := gitProject(t, map[string]string{
		"internal/auth/token.go": "package auth\n\nfunc Verify() bool { return true }\n",
	})

	stdout, stderr, err := runCLI(t, work, "code-review", "explain", "--base", "main")
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, stderr)
	}

	for _, want := range []string{
		"manifest: built-in default",
		"context: worktree",
		"default: quick",
		"panel:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("explain output missing %q:\n%s", want, stdout)
		}
	}
}

// A repo that believes it wrote a manifest and is being reviewed by the
// built-in one needs to be told: the two produce entirely different panels,
// and nothing else in the output would say which was read.
func TestCodeReviewExplainNamesWhichManifestItRead(t *testing.T) {
	manifest := `version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  only: {reviewers: [correctness]}
defaults: {worktree: only, pr: only}
`
	work := gitProject(t, map[string]string{
		".agentic-toolkit/code-review/manifest.yaml": manifest,
		"main.go": "package main\n",
	})

	stdout, stderr, err := runCLI(t, work, "code-review", "explain", "--base", "main")
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, stderr)
	}
	if strings.Contains(stdout, "built-in default") {
		t.Errorf("explain read the built-in default over the repo's own manifest:\n%s", stdout)
	}
	if !strings.Contains(stdout, "panel:   only") {
		t.Errorf("explain did not run the repo's own panel:\n%s", stdout)
	}
}

// A manifest that cannot staff a review is refused before anything is spent,
// and the refusal names the field.
func TestCodeReviewExplainRefusesABrokenManifest(t *testing.T) {
	work := gitProject(t, map[string]string{
		".agentic-toolkit/code-review/manifest.yaml": `version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  only: {reviewers: [typo]}
defaults: {worktree: only, pr: only}
`,
	})

	_, _, err := runCLI(t, work, "code-review", "explain", "--base", "main")
	if err == nil {
		t.Fatal("explain accepted a panel naming a reviewer that does not exist")
	}
	if !strings.Contains(err.Error(), "panels.only.reviewers[0]") {
		t.Errorf("refusal does not name the field: %v", err)
	}
}

// The vocabulary is closed and ships with the binary, so listing it needs
// neither a repository nor a manifest.
func TestCodeReviewSignalsListsTheVocabulary(t *testing.T) {
	stdout, stderr, err := runCLI(t, t.TempDir(), "code-review", "signals")
	if err != nil {
		t.Fatalf("signals: %v\n%s", err, stderr)
	}
	for _, want := range []string{"auth", "concurrency", "fix-revert", "shared-kernel"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("signals output missing %q:\n%s", want, stdout)
		}
	}
	// Each name carries the one-line account of what it says about a change,
	// so the list answers "which of these do I want" rather than only "what
	// may I write".
	if !strings.Contains(stdout, "middleware that gates requests") {
		t.Errorf("signals output has names without descriptions:\n%s", stdout)
	}
}

// Cobra rejects an unknown subcommand only at the root: a non-root parent
// takes it as an argument, prints help and exits 0. An agent told to run a
// subcommand this binary does not have would read that help as the command's
// output and report success.
func TestAnUnknownCodeReviewSubcommandFails(t *testing.T) {
	if _, _, err := runCLI(t, t.TempDir(), "code-review", "expalin"); err == nil {
		t.Error("an unknown subcommand printed help and exited 0")
	}
}

// Typing the command with no flags is the common case, so the ref a change is
// measured against when nobody names one has to be exercised.
func TestCodeReviewExplainDetectsTheBaseWhenNoneIsNamed(t *testing.T) {
	work := gitProject(t, map[string]string{"added.go": "package main\n"})

	stdout, stderr, err := runCLI(t, work, "code-review", "explain")
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, stderr)
	}
	if !strings.Contains(stdout, "range:    main...working tree") {
		t.Errorf("explain did not detect main as the base:\n%s", stdout)
	}
}

// A repo matching none of the conventions gets a refusal naming what was
// tried, rather than a change measured against a base nobody chose.
func TestCodeReviewExplainRefusesWhenNoBaseCanBeDetected(t *testing.T) {
	work := gitProject(t, map[string]string{"added.go": "package main\n"})
	runGit(t, work, "branch", "-m", "main", "trunk")

	_, _, err := runCLI(t, work, "code-review", "explain")
	if err == nil {
		t.Fatal("explain measured the change against a base it never found")
	}
	for _, want := range []string{"no base branch found", "origin/HEAD", "--base"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to mention %q", err, want)
		}
	}
}

// A review that posts reads its rules from the base ref. Everything on the
// branch under review is written by its author, so a manifest read from the
// working tree would let a change name the reviewers that judge it.
func TestAPostingContextReadsTheManifestFromTheBaseRef(t *testing.T) {
	onBase := `version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  trusted: {reviewers: [correctness]}
defaults: {worktree: trusted, pr: trusted}
`
	work := gitProject(t, nil)
	writeIn(t, work, ".agentic-toolkit/code-review/manifest.yaml", onBase)
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-m", "declare the review")

	// The branch rewrites the rules it will be judged by.
	writeIn(t, work, ".agentic-toolkit/code-review/manifest.yaml",
		strings.ReplaceAll(onBase, "trusted", "rewritten"))

	pr, _, err := runCLI(t, work, "code-review", "explain", "--context", "pr", "--base", "HEAD")
	if err != nil {
		t.Fatalf("explain --context pr: %v", err)
	}
	if strings.Contains(pr, "rewritten") {
		t.Errorf("a posting review read the manifest from the branch under review:\n%s", pr)
	}
	if !strings.Contains(pr, "panel:   trusted") {
		t.Errorf("a posting review did not use the base ref's panel:\n%s", pr)
	}

	// A local review of the working tree is the author reviewing their own
	// change, so it reads what they have written.
	worktree, _, err := runCLI(t, work, "code-review", "explain", "--base", "HEAD")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if !strings.Contains(worktree, "panel:   rewritten") {
		t.Errorf("a worktree review should read the working tree:\n%s", worktree)
	}
}

// namedPanels is a manifest whose panel names are its own. The built-in
// default calls its panels quick, standard and deep; a repo is free to call
// them anything, and a caller that had memorised the built-in names would be
// wrong in exactly the repos that cared enough to write a manifest.
const namedPanels = `version: 1
reviewers:
  correctness: {provider: claudecode, prompt: builtin:correctness}
  security:    {provider: claudecode, prompt: builtin:security}
judge:     {provider: claudecode, prompt: builtin:judge}
validator: {provider: claudecode, prompt: builtin:validator}
panels:
  thorough:
    description: Both reviewers, twice.
    reviewers: [correctness, security]
    quorum: 2
  fast:
    description: One reviewer, once.
    reviewers: [correctness]
  careful:
    reviewers: [correctness, security]
defaults: {worktree: fast, pr: careful}
escalate:
  - to: thorough
    all:
      - touches: {matches: ["**/auth/**"]}
`

// panelsOut is the shape `panels --json` emits, as a test reads it.
type panelsOut struct {
	Version  int    `json:"version"`
	Manifest string `json:"manifest"`
	Context  string `json:"context"`
	Panels   []struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Reviewers   []string `json:"reviewers"`
		Quorum      int      `json:"quorum"`
		Runs        int      `json:"runs"`
		DefaultFor  []string `json:"default_for"`
	} `json:"panels"`
}

// panelsJSON runs `panels --json` and decodes it.
func panelsJSON(t *testing.T, work string, args ...string) panelsOut {
	t.Helper()
	stdout, stderr, err := runCLI(t, work, append([]string{"code-review", "panels", "--json"}, args...)...)
	if err != nil {
		t.Fatalf("panels --json: %v\n%s", err, stderr)
	}
	var got panelsOut
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("panels --json is not valid JSON: %v\n%s", err, stdout)
	}
	return got
}

// A repo with no manifest is reviewed by the built-in default, and the listing
// says so: a repo that believes it wrote a manifest and is reading the
// default's panels has nothing else in the output to tell it.
func TestCodeReviewPanelsListsTheBuiltInDefaultAndSaysSo(t *testing.T) {
	work := gitProject(t, map[string]string{"main.go": "package main\n"})

	stdout, stderr, err := runCLI(t, work, "code-review", "panels", "--base", "main")
	if err != nil {
		t.Fatalf("panels: %v\n%s", err, stderr)
	}
	for _, want := range []string{
		"manifest: built-in default",
		"quick (1 run)",
		"standard (2 runs)",
		"deep (3 reviewers × quorum 2 = 6 runs)",
		"default for worktree",
		"default for pr",
		// The description is the one line that says what a panel is for,
		// which is what a person choosing between panels chooses on.
		"The pre-push pass",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("panels output missing %q:\n%s", want, stdout)
		}
	}

	got := panelsJSON(t, work, "--base", "main")
	if got.Version != 1 {
		t.Errorf("version is %d", got.Version)
	}
	if !strings.Contains(got.Manifest, "built-in default") {
		t.Errorf("the JSON does not say the built-in default was read: %q", got.Manifest)
	}
}

// The names `panels` emits are exactly the names --panel accepts. That is the
// contract a caller offering a choice of depth depends on, and the two
// commands drifting apart is the failure it would not see until a user did.
func TestCodeReviewPanelsEmitsTheNamesThatPanelAccepts(t *testing.T) {
	work := gitProject(t, map[string]string{
		".agentic-toolkit/code-review/manifest.yaml": namedPanels,
		"main.go": "package main\n",
	})

	got := panelsJSON(t, work, "--base", "main")
	names := make([]string, 0, len(got.Panels))
	for _, p := range got.Panels {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "careful,fast,thorough" {
		t.Fatalf("panels emitted %v, want the manifest's own names", names)
	}

	for _, name := range names {
		stdout, stderr, err := runCLI(t, work, "code-review", "explain", "--base", "main", "--panel", name)
		if err != nil {
			t.Errorf("--panel %s was refused though panels listed it: %v\n%s", name, err, stderr)
			continue
		}
		if !strings.Contains(stdout, "panel:   "+name+" ") {
			t.Errorf("--panel %s did not run that panel:\n%s", name, stdout)
		}
	}

	_, _, err := runCLI(t, work, "code-review", "explain", "--base", "main", "--panel", "quick")
	if err == nil {
		t.Fatal("--panel accepted a name panels did not emit")
	}
	for _, name := range names {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal does not name %q from the declared set: %v", name, err)
		}
	}
}

// Shallowest first, by name among equals. Cost is the order escalations
// climb, and a listing in a YAML map's order would change meaning with a
// reordering that changes nothing else.
func TestCodeReviewPanelsListsShallowestFirst(t *testing.T) {
	work := gitProject(t, map[string]string{
		".agentic-toolkit/code-review/manifest.yaml": namedPanels,
		"main.go": "package main\n",
	})

	got := panelsJSON(t, work, "--base", "main")
	if len(got.Panels) != 3 {
		t.Fatalf("want three panels, got %+v", got.Panels)
	}
	if got.Panels[0].Name != "fast" || got.Panels[1].Name != "careful" || got.Panels[2].Name != "thorough" {
		t.Errorf("panels are not listed shallowest first: %s, %s, %s",
			got.Panels[0].Name, got.Panels[1].Name, got.Panels[2].Name)
	}
	thorough := got.Panels[2]
	if thorough.Runs != 4 || thorough.Quorum != 2 || len(thorough.Reviewers) != 2 {
		t.Errorf("thorough's cost is misreported: %+v", thorough)
	}
	if thorough.Description != "Both reviewers, twice." {
		t.Errorf("the description was altered: %q", thorough.Description)
	}
	// A panel with no description is listed by name and cost, and the field
	// is present and empty rather than missing.
	careful := got.Panels[1]
	if careful.Description != "" || careful.DefaultFor == nil || careful.DefaultFor[0] != "pr" {
		t.Errorf("careful is misreported: %+v", careful)
	}
	// Nobody's default is an empty list, never null: a consumer iterating
	// it must not have to special-case the panel no context starts from.
	if thorough.DefaultFor == nil || len(thorough.DefaultFor) != 0 {
		t.Errorf("thorough is nobody's default, yet default_for is %#v", thorough.DefaultFor)
	}
}

// A caller offering choices for a PR review must offer the panels that PR
// will be judged by, and those are declared at the base ref: a branch must
// not name the reviewers that judge it.
func TestCodeReviewPanelsReadsTheBaseRefForAPostingContext(t *testing.T) {
	work := gitProject(t, nil)
	writeIn(t, work, ".agentic-toolkit/code-review/manifest.yaml", namedPanels)
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-m", "declare the review")

	// The branch renames the panels it will be judged by.
	writeIn(t, work, ".agentic-toolkit/code-review/manifest.yaml",
		strings.ReplaceAll(namedPanels, "thorough", "renamed"))

	pr := panelsJSON(t, work, "--context", "pr", "--base", "HEAD")
	for _, p := range pr.Panels {
		if p.Name == "renamed" {
			t.Errorf("a posting context listed a panel the branch declared:\n%+v", pr.Panels)
		}
	}
	if pr.Context != "pr" || !strings.HasSuffix(pr.Manifest, ".agentic-toolkit/code-review/manifest.yaml") || strings.Contains(pr.Manifest, "built-in") {
		t.Errorf("a posting context does not name the base ref's manifest: %+v", pr)
	}

	worktree := panelsJSON(t, work, "--base", "HEAD")
	found := false
	for _, p := range worktree.Panels {
		found = found || p.Name == "renamed"
	}
	if !found {
		t.Errorf("a worktree listing should read the working tree:\n%+v", worktree.Panels)
	}
}

func TestCodeReviewPanelsRefusesAManifestThatDoesNotParse(t *testing.T) {
	work := gitProject(t, map[string]string{
		".agentic-toolkit/code-review/manifest.yaml": "version: 1\npanels: [not, a, map]\n",
	})

	_, _, err := runCLI(t, work, "code-review", "panels", "--base", "main")
	if err == nil {
		t.Fatal("panels listed something from a manifest that does not parse")
	}
	if !strings.Contains(err.Error(), "manifest.yaml") {
		t.Errorf("the refusal does not name the manifest: %v", err)
	}
}

// A base ref that does not resolve is a refusal, never a listing under the
// built-in default: a typo in --base would otherwise offer a PR the wrong
// panels with nothing in the output saying so.
func TestCodeReviewPanelsRefusesABaseRefThatDoesNotResolve(t *testing.T) {
	work := gitProject(t, map[string]string{"main.go": "package main\n"})

	for _, ctx := range []string{"worktree", "pr"} {
		stdout, _, err := runCLI(t, work, "code-review", "panels", "--context", ctx, "--base", "no-such-ref")
		if err == nil {
			t.Errorf("--context %s listed panels against a base that does not exist:\n%s", ctx, stdout)
			continue
		}
		if !strings.Contains(err.Error(), "no-such-ref") {
			t.Errorf("--context %s: the refusal does not name the ref: %v", ctx, err)
		}
	}
}

func TestCodeReviewPanelsRefusesAnUnknownContext(t *testing.T) {
	work := gitProject(t, map[string]string{"main.go": "package main\n"})
	_, _, err := runCLI(t, work, "code-review", "panels", "--base", "main", "--context", "nope")
	if err == nil {
		t.Fatal("an unknown context was accepted")
	}
	if !strings.Contains(err.Error(), "worktree") {
		t.Errorf("the refusal does not list the contexts: %v", err)
	}
}

// explainOut is the shape `explain --json` emits, as a test reads it.
type explainOut struct {
	Version  int    `json:"version"`
	Manifest string `json:"manifest"`
	Range    string `json:"range"`
	Change   struct {
		Files     int      `json:"files"`
		Lines     int      `json:"lines"`
		Languages []string `json:"languages"`
	} `json:"change"`
	Context string `json:"context"`
	Default struct {
		Name string `json:"name"`
		Runs int    `json:"runs"`
	} `json:"default"`
	Fired []struct {
		Index      int    `json:"index"`
		To         string `json:"to"`
		Combinator string `json:"combinator"`
		Conditions []struct {
			Condition string `json:"condition"`
			Held      bool   `json:"held"`
		} `json:"conditions"`
	} `json:"fired"`
	Skipped []struct {
		Index  int    `json:"index"`
		To     string `json:"to"`
		Reason string `json:"reason"`
	} `json:"skipped"`
	Panel struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Runs        int    `json:"runs"`
	} `json:"panel"`
	Overridden       bool   `json:"overridden"`
	Validates        bool   `json:"validates"`
	ValidationReason string `json:"validation_reason"`
}

func explainJSON(t *testing.T, work string, args ...string) (explainOut, string) {
	t.Helper()
	stdout, stderr, err := runCLI(t, work, append([]string{"code-review", "explain", "--json"}, args...)...)
	if err != nil {
		t.Fatalf("explain --json: %v\n%s", err, stderr)
	}
	var got explainOut
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("explain --json is not valid JSON: %v\n%s", err, stdout)
	}
	return got, stdout
}

// Everything the prose prints, structured: the default, the rule that fired
// and what held, the resulting panel, and why it validates.
func TestCodeReviewExplainJSONCarriesTheDecision(t *testing.T) {
	// A posting context reads the manifest from the base ref, so the
	// manifest is on it and the change is not.
	work := gitProject(t, nil)
	writeIn(t, work, ".agentic-toolkit/code-review/manifest.yaml", namedPanels)
	runGit(t, work, "add", "-A")
	runGit(t, work, "commit", "-m", "declare the review")
	writeIn(t, work, "internal/auth/token.go", "package auth\n")

	got, _ := explainJSON(t, work, "--base", "main", "--context", "pr")
	if got.Version != 1 {
		t.Errorf("version is %d", got.Version)
	}
	if got.Range != "main...working tree" || got.Context != "pr" {
		t.Errorf("range/context misreported: %q %q", got.Range, got.Context)
	}
	if !strings.HasSuffix(got.Manifest, ".agentic-toolkit/code-review/manifest.yaml") {
		t.Errorf("the manifest read is not named: %q", got.Manifest)
	}
	if got.Change.Files != 1 || len(got.Change.Languages) != 1 || got.Change.Languages[0] != "go" {
		t.Errorf("the change is misprofiled: %+v", got.Change)
	}
	if got.Default.Name != "careful" || got.Default.Runs != 2 {
		t.Errorf("the default is misreported: %+v", got.Default)
	}
	if len(got.Fired) != 1 {
		t.Fatalf("want one rule fired, got %+v", got.Fired)
	}
	fired := got.Fired[0]
	if fired.Index != 0 || fired.To != "thorough" || fired.Combinator != "all" {
		t.Errorf("the fired rule is misreported: %+v", fired)
	}
	if len(fired.Conditions) != 1 || !fired.Conditions[0].Held || !strings.Contains(fired.Conditions[0].Condition, "auth") {
		t.Errorf("the condition that held is misreported: %+v", fired.Conditions)
	}
	if got.Panel.Name != "thorough" || got.Panel.Runs != 4 || got.Panel.Description != "Both reviewers, twice." {
		t.Errorf("the resulting panel is misreported: %+v", got.Panel)
	}
	if got.Overridden {
		t.Error("a panel the rules chose reports itself as overridden")
	}
	if !got.Validates || !strings.Contains(got.ValidationReason, "posts") {
		t.Errorf("a posting context does not report validation: %v %q", got.Validates, got.ValidationReason)
	}
}

// A panel named on the command line overrides the rules in both directions,
// and the JSON says so: the rules are still reported, and did not decide.
func TestCodeReviewExplainJSONReportsAnOverride(t *testing.T) {
	work := gitProject(t, map[string]string{
		".agentic-toolkit/code-review/manifest.yaml": namedPanels,
		"internal/auth/token.go":                     "package auth\n",
	})

	got, _ := explainJSON(t, work, "--base", "main", "--panel", "fast")
	if got.Panel.Name != "fast" || !got.Overridden {
		t.Errorf("the override is misreported: %+v overridden=%v", got.Panel, got.Overridden)
	}
	if len(got.Fired) != 1 {
		t.Errorf("an override dropped the rule that fired: %+v", got.Fired)
	}
	if got.Validates || got.ValidationReason != "" {
		t.Errorf("a worktree review of a panel that does not ask reports validation: %v %q",
			got.Validates, got.ValidationReason)
	}
}

// The JSON and the prose are two renderings of one decision, and the panel
// they name for the same change is the same panel.
func TestCodeReviewExplainJSONAgreesWithTheProse(t *testing.T) {
	work := gitProject(t, map[string]string{
		"internal/auth/token.go": "package auth\n",
	})

	prose, stderr, err := runCLI(t, work, "code-review", "explain", "--base", "main")
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, stderr)
	}
	panelLine := ""
	for _, line := range strings.Split(prose, "\n") {
		if strings.HasPrefix(line, "panel:") {
			panelLine = line
		}
	}
	if panelLine == "" {
		t.Fatalf("no panel line in:\n%s", prose)
	}
	proseName := strings.Fields(strings.TrimPrefix(panelLine, "panel:"))[0]

	got, _ := explainJSON(t, work, "--base", "main")
	if got.Panel.Name != proseName {
		t.Errorf("prose chose %q and JSON chose %q for the same change", proseName, got.Panel.Name)
	}
	if !strings.Contains(prose, "manifest: "+got.Manifest) {
		t.Errorf("prose and JSON name different manifests:\n%s\n%q", prose, got.Manifest)
	}
}

// Empty lists rather than null, so a consumer iterating them does not have to
// special-case the change that fired nothing.
func TestCodeReviewExplainJSONEmitsEmptyListsRatherThanNull(t *testing.T) {
	work := gitProject(t, map[string]string{"main.go": "package main\n"})

	_, raw := explainJSON(t, work, "--base", "main")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"fired", "skipped"} {
		if decoded[key] == nil {
			t.Errorf("%q is null rather than an empty list", key)
		}
	}
	change, _ := decoded["change"].(map[string]any)
	for _, key := range []string{"languages", "symbols", "signals", "undetermined_signals", "excluded"} {
		if change[key] == nil {
			t.Errorf("change.%s is null rather than an empty list", key)
		}
	}
	panel, _ := decoded["panel"].(map[string]any)
	for _, key := range []string{"reviewers", "default_for"} {
		if panel[key] == nil {
			t.Errorf("panel.%s is null rather than an empty list", key)
		}
	}
	// A scalar nobody set is present and empty for the same reason: a
	// consumer must not have to tell a missing key apart from a review that
	// validates for no stated reason.
	reason, present := decoded["validation_reason"]
	if !present {
		t.Error("validation_reason is absent rather than an empty string")
	}
	if reason != "" {
		t.Errorf("a worktree review that does not validate gives a reason: %v", reason)
	}
}

// A manifest that parses but names a provider no build can drive is refused
// before anything is listed. Parsing is not the only way a manifest fails, and
// a listing offered under one agtk cannot staff a review under another.
func TestCodeReviewPanelsRefusesAManifestNoProviderCanStaff(t *testing.T) {
	work := gitProject(t, map[string]string{
		".agentic-toolkit/code-review/manifest.yaml": `version: 1
reviewers:
  correctness: {provider: gemini, prompt: builtin:correctness}
judge:     {provider: gemini, prompt: builtin:judge}
validator: {provider: gemini, prompt: builtin:validator}
panels:
  only: {reviewers: [correctness]}
defaults: {worktree: only, pr: only}
`,
		"main.go": "package main\n",
	})

	stdout, _, err := runCLI(t, work, "code-review", "panels", "--base", "main")
	if err == nil {
		t.Fatalf("panels listed a manifest no provider can staff:\n%s", stdout)
	}
	for _, want := range []string{"reviewers.correctness.provider", "gemini"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
}

// A panel whose purpose has always been obvious needs no description, and is
// listed by name and cost alone rather than under an empty line.
func TestCodeReviewPanelsListsAPanelThatHasNoDescription(t *testing.T) {
	work := gitProject(t, map[string]string{
		".agentic-toolkit/code-review/manifest.yaml": namedPanels,
		"main.go": "package main\n",
	})

	stdout, stderr, err := runCLI(t, work, "code-review", "panels", "--base", "main")
	if err != nil {
		t.Fatalf("panels: %v\n%s", err, stderr)
	}

	lines := strings.Split(stdout, "\n")
	described, undescribed := "", ""
	for i, line := range lines {
		if strings.HasPrefix(line, "fast ") && i+1 < len(lines) {
			described = lines[i+1]
		}
		if strings.HasPrefix(line, "careful ") && i+1 < len(lines) {
			undescribed = lines[i+1]
		}
	}
	if strings.TrimSpace(described) != "One reviewer, once." {
		t.Errorf("a described panel does not carry its description: %q", described)
	}
	if strings.TrimSpace(undescribed) != "" {
		t.Errorf("a panel with no description carries a line anyway: %q", undescribed)
	}
	if strings.Contains(stdout, "\n    \n") {
		t.Errorf("an undescribed panel emits a blank indented line:\n%q", stdout)
	}
}
