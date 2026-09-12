package tests

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-driver/agentictest"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
)

// The terminal `result` line of a run. Every provider streams, so a fake's
// stdout is NDJSON and a whole turn fits on one line; the fields are trimmed to
// the ones the provider reads.
const curatedEnvelope = `{"type":"result","subtype":"success","is_error":false,"session_id":"s1","num_turns":4,"total_cost_usd":0.42,"result":"Promoted: lockfile-pins-shas-not-tags\nRejected: 20260905-where-render-lives — re-derivable\nStore: 9 notes, 0 stale","modelUsage":{"claude-opus-5[1m]":{"canonicalModel":"claude-opus-5","inputTokens":12,"cacheReadInputTokens":9000}}}`

// A failing turn. The CLI reporting its own failure is a verdict, not an
// outage: the report is populated and carries the explanation.
const refusedEnvelope = `{"type":"result","subtype":"success","is_error":true,"session_id":"s2","result":"could not reach the store"}`

func run(t *testing.T, stdout string, opts curator.Options) (*agentictest.Fake, curator.Result, error) {
	t.Helper()

	fake := (&agentictest.Fake{Stdout: stdout}).Build(t)
	opts.Binary = fake.Path()
	if opts.Provider == "" {
		opts.Provider = "claudecode"
	}
	if opts.WorkDir == "" {
		opts.WorkDir = t.TempDir()
	}
	res, err := curator.Run(t.Context(), opts)
	return fake, res, err
}

func TestARunReturnsTheCuratorsReport(t *testing.T) {
	_, res, err := run(t, curatedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Text, "Promoted: lockfile-pins-shas-not-tags") {
		t.Errorf("Text = %q, want the curator's report", res.Text)
	}
	if res.IsError {
		t.Error("a successful turn was reported as a failure")
	}
	if res.CostUSD != 0.42 {
		t.Errorf("CostUSD = %v, want what the provider reported", res.CostUSD)
	}
	if res.Model == "" {
		t.Error("Model is empty; the run's cost is meaningless without the model it was charged for")
	}
}

// The CLI declaring the turn a failure is a verdict from the provider, not an
// error from running it. Discarding the report as an outage loses the only
// explanation there is.
func TestACuratorsOwnFailureCarriesItsReport(t *testing.T) {
	_, res, err := run(t, refusedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run turned a verdict into an error: %v", err)
	}
	if !res.IsError {
		t.Error("a failed turn was reported as a success")
	}
	if !strings.Contains(res.Text, "could not reach the store") {
		t.Errorf("Text = %q, want the explanation kept", res.Text)
	}
}

// The grant is constructed here and passed on the command line, which is what
// makes the single-writer rule enforcement rather than instruction. If it
// stopped reaching the child, nothing else would notice.
func TestTheGrantAndTheRosterReachTheChild(t *testing.T) {
	fake, _, err := run(t, curatedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	argv := strings.Join(fake.Recorded(t).Args, "\x00")
	for _, want := range []string{
		"--agents",
		curator.AgentName,
		"--allowedTools",
		"Bash(agtk memory anchor*)",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("the child was not given %q", want)
		}
	}

	// No mode is passed at all. Every mode the CLI accepts either waives
	// prompting for something the grant is meant to decide, or asks a question
	// no one is present to answer; leaving the flag off keeps the constructed
	// grant as the whole of the run's permission.
	if strings.Contains(argv, "--permission-mode") {
		t.Errorf("the child was given a permission mode, which outranks the grant: %q", argv)
	}

	// The measure that closes apiKeyHelper. A run that loaded settings files
	// would also be a run whose grant a settings file could widen.
	if !strings.Contains(argv, "--setting-sources") {
		t.Error("the run did not refuse to load settings sources")
	}
}

// --stale is its own command rather than a flag on audit, and the two ask for
// different work. A flag that reached the child identically would mean the
// sweep never happened.
func TestTheStaleSweepAsksForDifferentWork(t *testing.T) {
	backlog, _, err := run(t, curatedEnvelope, curator.Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sweep, _, err := run(t, curatedEnvelope, curator.Options{Stale: true})
	if err != nil {
		t.Fatalf("Run --stale: %v", err)
	}

	backlogArgs := strings.Join(backlog.Recorded(t).Args, " ")
	sweepArgs := strings.Join(sweep.Recorded(t).Args, " ")
	if backlogArgs == sweepArgs {
		t.Fatal("--stale sent the child the same instruction as the default run")
	}
	if !strings.Contains(sweepArgs, "audit") {
		t.Errorf("the sweep does not point the curator at the stale list: %q", sweepArgs)
	}
	if !strings.Contains(backlogArgs, "candidates") {
		t.Errorf("the default run does not point the curator at the backlog: %q", backlogArgs)
	}
}

// The curator resolves the store by running agtk, so it has to start where
// agtk would have — the project root, not wherever the invoking shell sat.
func TestTheChildRunsInTheProjectRoot(t *testing.T) {
	dir := t.TempDir()
	fake, _, err := run(t, curatedEnvelope, curator.Options{WorkDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Resolved on both sides: a temp directory reaches the child through a
	// symlink on macOS, so the raw strings differ for the same directory.
	if got, want := resolve(t, fake.Recorded(t).Cwd), resolve(t, dir); got != want {
		t.Errorf("child ran in %q, want the project root %q", got, want)
	}
}

func resolve(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	return real
}

// A provider the repo has not chosen must not reach a process at all.
func TestAnUnconfiguredRunStartsNoProcess(t *testing.T) {
	fake := (&agentictest.Fake{Stdout: curatedEnvelope}).Build(t)

	_, err := curator.Run(t.Context(), curator.Options{
		Binary:  fake.Path(),
		WorkDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("a run with no provider configured succeeded")
	}
	if fake.Ran() {
		t.Error("a run with no provider configured still spawned a process")
	}
}

// The whole reason --check exists is that confirming the configuration must
// not perform it. A check that spawned the CLI would spend money to answer a
// question about a config file.
func TestCheckStartsNothing(t *testing.T) {
	fake := (&agentictest.Fake{Stdout: curatedEnvelope}).Build(t)

	ready, err := curator.Check(curator.Options{
		Provider:      "claudecode",
		Binary:        fake.Path(),
		WorkDir:       t.TempDir(),
		CandidatesDir: "/repo/.agents/memory/candidates",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if fake.Ran() {
		t.Fatal("--check spawned the CLI")
	}
	if ready.Provider == "" || ready.Binary != fake.Path() {
		t.Errorf("Ready = %+v, want the provider and the pinned binary", ready)
	}
	if ready.Mode != curator.PermissionMode() {
		t.Errorf("Mode = %q, want the mode a run would use", ready.Mode)
	}
}

// The check reports the grant a run would be given, which is how a reader
// notices a grant that is wider than they meant. An over-broad `rm` was found
// exactly this way.
func TestCheckReportsTheGrantARunWouldUse(t *testing.T) {
	fake := (&agentictest.Fake{Stdout: curatedEnvelope}).Build(t)

	ready, err := curator.Check(curator.Options{
		Provider:      "claudecode",
		Binary:        fake.Path(),
		WorkDir:       t.TempDir(),
		NotesDir:      "/repo/.agents/memory/notes",
		CandidatesDir: "/repo/.agents/memory/candidates",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	want := curator.AllowedTools("", "/repo/.agents/memory/notes", "/repo/.agents/memory/candidates")
	if len(ready.Tools) != len(want) {
		t.Fatalf("Tools = %v, want the run's grant %v", ready.Tools, want)
	}
	// Every deletion names a directory inside the store. A bare `rm` would let
	// the one agent holding a constructed grant remove anything in the repo.
	for _, tool := range ready.Tools {
		if !strings.HasPrefix(tool, "Bash(rm ") {
			continue
		}
		if !strings.Contains(tool, "/repo/.agents/memory/candidates/") &&
			!strings.Contains(tool, "/repo/.agents/memory/notes/") {
			t.Errorf("check reports an unscoped deletion grant: %q", tool)
		}
	}
}

func TestCheckRefusesAnUnconfiguredProvider(t *testing.T) {
	if _, err := curator.Check(curator.Options{WorkDir: t.TempDir()}); !errors.Is(err, curator.ErrNoProvider) {
		t.Fatalf("error = %v, want ErrNoProvider", err)
	}
}

// "Configured" and "runnable" are different states: the driver constructs
// happily around a CLI that is not installed. A check that only resolved the
// provider would report a run as ready that cannot start.
func TestCheckRefusesABinaryThatCannotRun(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-installed")

	_, err := curator.Check(curator.Options{
		Provider: "claudecode",
		Binary:   missing,
		WorkDir:  t.TempDir(),
	})
	if err == nil {
		t.Fatal("check reported a missing binary as ready")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error does not name the binary it looked for: %v", err)
	}
}

// grant is what a run under opts would be handed, read from the surface that
// reports it rather than from the child's argv — the argv also carries the
// curator's own prompt, which names the very tools a grant assertion is
// looking for.
func grant(t *testing.T, opts curator.Options) []string {
	t.Helper()
	fake := (&agentictest.Fake{Stdout: curatedEnvelope}).Build(t)
	opts.Provider = "claudecode"
	opts.Binary = fake.Path()
	if opts.WorkDir == "" {
		opts.WorkDir = t.TempDir()
	}
	ready, err := curator.Check(opts)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return ready.Tools
}

// A dry run must be safe by construction, not by cooperation. Handing the
// curator Write, Edit and the stamping commands and then asking it in prose to
// hold back leaves the store one misreading away from being edited by a run
// whose whole purpose was to touch nothing.
func TestADryRunIsHandedNoWritingTools(t *testing.T) {
	granted := strings.Join(grant(t, curator.Options{DryRun: true, CandidatesDir: "/repo/candidates"}), "\x00")

	for _, forbidden := range []string{"Write", "Edit", "memory anchor", "memory index", "Bash(rm "} {
		if strings.Contains(granted, forbidden) {
			t.Errorf("a dry run was granted %q", forbidden)
		}
	}
	if !strings.Contains(granted, "Read") || !strings.Contains(granted, "memory candidates") {
		t.Errorf("a dry run cannot read the backlog it is meant to report on: %q", granted)
	}
}

// Stamping is the write that matters most: `agtk memory anchor` clears the one
// signal saying nobody has checked a claim. A run scoped to one note must not
// be able to launder the freshness of any other.
func TestAScopedRunCanOnlyStampTheNotesItNames(t *testing.T) {
	granted := grant(t, curator.Options{Notes: []string{"pins-shas"}, AgtkPath: "/opt/agtk"})

	if !slices.Contains(granted, "Bash(/opt/agtk memory anchor pins-shas)") {
		t.Errorf("the scoped run cannot stamp the note it was given: %v", granted)
	}
	if slices.Contains(granted, "Bash(/opt/agtk memory anchor*)") {
		t.Error("the scoped run kept the open stamping grant, so it can stamp any note")
	}
}

// Note names are kebab-case, so one name can be a prefix of another. A scoped
// grant carrying a trailing wildcard would permit stamping a longer-named note
// the run never looked at — clearing the one signal that says nobody has
// checked that claim, which is the silent failure the scoping exists to stop.
func TestAScopedStampingGrantDoesNotReachPrefixedNames(t *testing.T) {
	granted := grant(t, curator.Options{Notes: []string{"lockfile-pins"}, AgtkPath: "/opt/agtk"})

	for _, tool := range granted {
		if strings.HasPrefix(tool, "Bash(/opt/agtk memory anchor") && strings.HasSuffix(tool, "*)") {
			t.Errorf("scoped stamping grant %q ends in a wildcard, so it reaches lockfile-pins-shas-not-tags", tool)
		}
	}
}

// The child also has to be told which notes are in scope; the narrowed grant
// stops it stamping the others but not reading or rewriting them.
func TestAScopedRunTellsTheChildItsScope(t *testing.T) {
	fake, _, err := run(t, curatedEnvelope, curator.Options{Notes: []string{"pins-shas"}})
	if err != nil {
		t.Fatalf("Run with notes: %v", err)
	}

	if !strings.Contains(strings.Join(fake.Recorded(t).Args, "\x00"), "pins-shas") {
		t.Error("the child was not told which notes are in scope")
	}
}

// An unscoped run's scope is the store, so it keeps the open grant. Without
// this the scoping change would be indistinguishable from one that broke
// stamping outright.
func TestAnUnscopedRunKeepsTheOpenStampingGrant(t *testing.T) {
	if !slices.Contains(grant(t, curator.Options{AgtkPath: "/opt/agtk"}), "Bash(/opt/agtk memory anchor*)") {
		t.Error("an unscoped run lost the open stamping grant")
	}
}

// The stale sweep's first instruction is to run `agtk memory audit --json`. A
// grant that omits audit leaves the sweep denied at its first command, having
// spent a model invocation to get there.
func TestTheStaleSweepMayRunTheAuditItIsToldToRun(t *testing.T) {
	if !slices.Contains(grant(t, curator.Options{Stale: true, AgtkPath: "/opt/agtk"}), "Bash(/opt/agtk memory audit*)") {
		t.Error("the sweep cannot run the audit its own instruction points it at")
	}
}

// The deletion grant is scoped to a named directory, and the write grant has
// to be too. `Write` and `Edit` with no path attached let the one agent in the
// system holding a constructed grant edit any file in the repo — which is the
// guarantee the constructed grant exists to make, given away by the two
// broadest entries in it.
func TestTheWriteGrantIsScopedToTheNotesDirectory(t *testing.T) {
	granted := grant(t, curator.Options{
		NotesDir:      "/repo/.agents/memory/notes",
		CandidatesDir: "/repo/.agents/memory/candidates",
	})
	for _, g := range granted {
		if g == "Write" || g == "Edit" {
			t.Errorf("granted bare %q, which reaches every file in the repo", g)
		}
	}
	joined := strings.Join(granted, "\x00")
	// An Edit rule covers every file-editing tool, Write included. A Write
	// rule is not consulted by the file permission check, so a grant spelled
	// that way names the right directory and constrains nothing.
	if !strings.Contains(joined, "Edit(//repo/.agents/memory/notes/**)") {
		t.Errorf("missing the scoped Edit rule; the curator cannot author the notes it exists to author: %q", joined)
	}
	if strings.Contains(joined, "Write(") {
		t.Errorf("granted a Write(path) rule, which the file permission check ignores: %q", joined)
	}
}

// A single leading slash in a permission pattern reads as relative to the
// project directory, so an absolute store path has to be doubled at the root.
// A pattern that resolves nowhere denies every write, including the ones the
// run exists to make.
func TestAnAbsoluteNotesPathIsDoubledAtTheRoot(t *testing.T) {
	granted := strings.Join(grant(t, curator.Options{
		NotesDir:      "/repo/.agents/memory/notes",
		CandidatesDir: "/repo/.agents/memory/candidates",
	}), "\x00")

	if !strings.Contains(granted, "Edit(//repo/") {
		t.Errorf("absolute notes path was not doubled at the root: %q", granted)
	}
}

// A mode that waives prompting outranks AllowedTools rather than combining
// with it. acceptEdits waives it for exactly the half of the grant that scopes
// where notes may be written, which turns that boundary into a comment.
func TestThePermissionModeDoesNotWaiveTheGrant(t *testing.T) {
	if mode := curator.PermissionMode(); mode != "" {
		t.Errorf("PermissionMode() = %q; a mode that auto-approves edits leaves the scoped Edit rule as decoration", mode)
	}
}

// The store's location is configurable, so there is no notes path to hard-code
// and no safe default to guess. A run that names no notes directory gets no
// write grant at all: it finishes having promoted nothing, which is visible,
// rather than holding a licence over the whole repo that nobody granted it.
func TestNamingNoNotesDirectoryGrantsNoWrite(t *testing.T) {
	granted := strings.Join(grant(t, curator.Options{
		CandidatesDir: "/repo/.agents/memory/candidates",
	}), "\x00")

	if strings.Contains(granted, "Write") || strings.Contains(granted, "Edit") {
		t.Errorf("a run with no notes directory was granted a write tool: %q", granted)
	}
}

// The prompt tells the curator to delete a note whose claim is simply gone,
// and a retraction it is instructed to make but not permitted to make leaves
// the store asserting something false while the run reports success.
func TestTheGrantPermitsTheRetractionThePromptInstructs(t *testing.T) {
	granted := strings.Join(grant(t, curator.Options{
		NotesDir:      "/repo/.agents/memory/notes",
		CandidatesDir: "/repo/.agents/memory/candidates",
	}), "\x00")

	if !strings.Contains(granted, "Bash(rm /repo/.agents/memory/notes/*)") {
		t.Errorf("the curator cannot delete a note it rules now-false: %q", granted)
	}
}

// A dry run previews and writes nothing, so the retraction grant is withheld
// along with every other way of changing the store.
func TestADryRunCannotDeleteNotes(t *testing.T) {
	granted := strings.Join(grant(t, curator.Options{
		DryRun:        true,
		NotesDir:      "/repo/.agents/memory/notes",
		CandidatesDir: "/repo/.agents/memory/candidates",
	}), "\x00")

	if strings.Contains(granted, "rm ") {
		t.Errorf("a dry run was granted a deletion: %q", granted)
	}
}
