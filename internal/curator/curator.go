// Package curator runs the memory store's curator: the one operation in agtk
// that invokes a model.
//
// It is the only package that constructs an agentic-driver Driver. Every other
// memory operation — index, anchor, audit, lint, show, stats, candidates — must
// stay reachable without one, because that is the property hooks and CI depend
// on, and it is checkable by grep.
//
// The curator has no agent definition. A scripted run refuses to load settings
// files, which is what closes apiKeyHelper and also what puts a consumer's
// rendered .claude/agents/ out of reach — so the prompt below is embedded and
// handed to the provider as a roster, and the tool grant is constructed here
// rather than instructed in prose. See docs/adr/0004.
package curator

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	agentic "github.com/pedromvgomes/agentic-driver"
	"github.com/pedromvgomes/agentic-driver/claudecode"
	"github.com/pedromvgomes/agentic-driver/codex"
)

// prompt is the curator's instructions, and their single home. It ships with
// the binary rather than with the lockfile-pinned definitions, which is the
// one cost of the curator not being a definition.
//
//go:embed prompt.md
var prompt string

// AgentName is what the run delegates by. It is the roster key and the name
// the prompt refers to itself as.
const AgentName = "memory-curator"

// defaultTimeout bounds a curation run. Generous: the curator reads the store,
// verifies pointers against real files and writes notes, and a run killed
// half-way leaves promoted notes beside candidates it never deleted.
const defaultTimeout = 20 * time.Minute

// Providers are the names `memory.agent` accepts, in the order help lists them.
var Providers = []string{"claudecode", "codex"}

// ErrNoProvider means the repo has not named one. There is deliberately no
// default: this is the only memory operation that spends money and reaches
// outside the machine, so an unconfigured repo gets a refusal rather than a
// guess about which CLI it meant.
//
// It carries the whole message rather than being wrapped with a hint at the
// point of return, because the top-level renderer prints every level of a
// wrap chain — and a hint wrapped around a sentinel is read out twice.
var ErrNoProvider = fmt.Errorf(
	"memory: no curation provider configured; set `memory.agent` in the entry manifest to one of %s",
	strings.Join(Providers, ", "))

// Prompt is the curator's embedded instructions.
//
// Exported so a test can assert the embed is populated and still says the
// things that live nowhere else now that the curator is not a definition. An
// empty embed would produce a roster entry with no instructions, and a run
// that answers instead of refusing.
func Prompt() string { return prompt }

// AllowedTools is the grant a curation run is given.
func AllowedTools(agtk, notesDir, candidatesDir string) []string {
	return allowedTools(agtk, notesDir, candidatesDir, grantScope{})
}

// PermissionMode is how a curation run answers permission prompts.
func PermissionMode() string { return permissionMode }

// CheckProvider reports whether name resolves to a provider, without building
// a driver or touching PATH.
func CheckProvider(name string) error {
	_, err := newProvider(name)
	return err
}

// Ready is what a run would need, answered without starting one.
type Ready struct {
	// Provider is the descriptor's stable ID, e.g. "claude-code".
	Provider string
	// Binary is the executable a run would execute.
	Binary string
	// Tools is the grant the run would be given.
	Tools []string
	// Mode is the permission mode the run would use.
	Mode string
}

// Check resolves everything a curation run depends on and starts nothing.
//
// Curation is the only operation here that spends money and writes notes, so
// confirming it is configured must not require performing it. Without this the
// only way to find out whether `memory.agent` resolves is to run the curator
// and watch what happens, which is a costly way to read a config file.
func Check(opts Options) (Ready, error) {
	provider, err := newProvider(opts.Provider)
	if err != nil {
		return Ready{}, err
	}

	driverOpts := []agentic.Option{agentic.WithWorkDir(opts.WorkDir)}
	if opts.Binary != "" {
		driverOpts = append(driverOpts, agentic.WithBinary(opts.Binary))
	}
	driver, err := agentic.New(provider, driverOpts...)
	if err != nil {
		return Ready{}, err
	}
	// Ready is the difference between "configured" and "runnable": the driver
	// constructs happily around a CLI that is not installed, because a
	// provider that vendors its binary needs Install reachable first.
	if err := driver.Ready(); err != nil {
		return Ready{}, err
	}
	// Confinement is resolved here rather than only in Run, because a grant the
	// provider cannot express is exactly the misconfiguration this command
	// exists to surface. Reporting a tool list codex has no vocabulary for is a
	// green light for a run that cannot start.
	b, err := confine(provider, allowedTools(opts.AgtkPath, opts.NotesDir, opts.CandidatesDir, opts.scope()), opts.DryRun)
	if err != nil {
		return Ready{}, err
	}
	return Ready{
		Provider: driver.Descriptor().ID,
		Binary:   driver.Binary(),
		Tools:    b.tools,
		Mode:     b.mode,
	}, nil
}

// Options configure one curation run.
type Options struct {
	// Provider is the value of `memory.agent`.
	Provider string
	// WorkDir is where the child runs, so relative store and anchor paths
	// mean what they mean to agtk.
	WorkDir string
	// Stale asks for a sweep of stale notes rather than the candidate
	// backlog.
	Stale bool
	// DryRun asks the curator to report what it would do and write nothing.
	// It is enforced by withholding every writing tool from the grant, not by
	// asking the model nicely: a run told to hold back but handed the note
	// write grant is one refusal away from editing the store.
	DryRun bool
	// Notes scopes the run to the named notes. It narrows the stamping grant
	// to exactly those names, so a scoped run cannot clear the staleness
	// signal on a note it was not asked to check.
	Notes []string
	// Timeout overrides the default bound.
	Timeout time.Duration
	// Binary pins the executable instead of resolving the provider's name on
	// PATH, so nothing PATH resolves and no repointed symlink can stand in
	// for the CLI that was chosen.
	Binary string
	// CandidatesDir is the staging directory the run may clear. It scopes the
	// deletion grant, so it is required for a run and not merely cosmetic.
	CandidatesDir string
	// NotesDir is the directory the run may author notes in. It scopes the
	// write grant, so it is required for a run and not merely cosmetic.
	NotesDir string
	// AgtkPath is the agtk the curator shells back into to stamp anchors and
	// regenerate the index.
	//
	// It must be the running binary's own path, not the name `agtk`. Consumers
	// install the binary separately from the lockfile-pinned definitions, so
	// whatever PATH resolves may be older than the build that started this run
	// — old enough not to have `memory` at all, in which case the curator's
	// stamping commands fail and it finishes having verified everything and
	// recorded nothing.
	AgtkPath string
}

// scope is the narrowing this run's grant gets.
func (o Options) scope() grantScope {
	return grantScope{dryRun: o.DryRun, notes: o.Notes}
}

// Result is what a run produced.
type Result struct {
	// Text is the curator's report.
	Text string
	// IsError reports that the CLI declared the turn a failure. The report is
	// still populated and carries the explanation.
	IsError bool
	// Model and CostUSD are what the provider reported, and are zero when it
	// reported nothing.
	Model   string
	CostUSD float64
}

// allowedTools is what the curator may do, constructed here rather than
// instructed in markdown.
//
// This is what makes ADR 0003's single-writer rule enforcement for the curator
// rather than an honour system: it is an argv flag, so no settings file can
// widen it.
//
// Both the write grant and the deletion grant are scoped to a named directory
// rather than left bare. Authoring notes and clearing the backlog are the only
// things the curator writes and deletes, and grants that read `Write` or
// `rm *` would let the one agent with a constructed grant touch anything in
// the repo — which is the guarantee this list exists to make, given away in
// its own entries. Both directories are parameters because `memory.root` is
// configurable, so there is no path to hard-code.
func allowedTools(agtk, notesDir, candidatesDir string, opts grantScope) []string {
	if agtk == "" {
		agtk = "agtk"
	}
	// Reading is the whole grant for a dry run. Write, Edit, anchor, index and
	// the deletion of candidates are exactly the operations that change the
	// store, so a preview that keeps any of them is a preview only for as long
	// as the model chooses to make it one.
	tools := []string{
		"Read",
		"Grep",
		"Glob",
		"Bash(" + agtk + " memory show *)",
		"Bash(" + agtk + " memory candidates*)",
		"Bash(" + agtk + " memory stats*)",
		"Bash(" + agtk + " memory audit*)",
		"Bash(" + agtk + " memory lint*)",
	}
	if opts.dryRun {
		return tools
	}

	// Spelled `Edit(...)`, never `Write(...)`: an Edit rule covers every
	// file-editing tool including Write, while a Write rule is not consulted
	// by the file permission check at all — so a grant written the obvious
	// way names the right path and constrains nothing.
	//
	// An empty directory would compose to a pattern rooted at `/`, which is
	// the widest possible reading of a grant meant to be the narrowest. A
	// caller that names no notes directory gets no write grant at all: the
	// run finishes having promoted nothing, which is visible, rather than the
	// curator holding a licence over the repo nobody meant to give it.
	if notesDir != "" {
		tools = append(tools, "Edit("+editPattern(notesDir)+")")
	}
	tools = append(tools, "Bash("+agtk+" memory index*)")
	tools = append(tools, anchorGrants(agtk, opts.notes)...)

	// The deletion grant is scoped to the candidates directory rather than
	// left as a bare `rm`. Clearing the backlog is the only thing the curator
	// deletes, and a grant that reads `rm *` would let the one agent with a
	// constructed grant remove anything in the repo — which is the guarantee
	// this list exists to make, given away in its last line. The directory is
	// a parameter because `memory.root` is configurable, so there is no path
	// to hard-code.
	//
	// An empty directory would compose to `rm /*`, which is the widest
	// possible reading of a grant meant to be the narrowest. A caller that
	// names no staging directory gets no deletion grant at all: the backlog
	// goes uncleared, which is visible, rather than the curator holding a
	// licence nobody meant to give it.
	if candidatesDir != "" {
		tools = append(tools, "Bash(rm "+candidatesDir+"/*)")
	}
	// Retraction. A note whose claim is simply gone is deleted rather than
	// kept, and the prompt says so — an instruction the grant denies leaves
	// the store asserting something false while the run reports success. Same
	// scoping as the backlog's, and withheld on the same terms when no
	// directory names it.
	if notesDir != "" {
		tools = append(tools, "Bash(rm "+notesDir+"/*)")
	}
	return tools
}

// grantScope is what narrows a run's grant below the full one.
type grantScope struct {
	dryRun bool
	notes  []string
}

// anchorGrants permits stamping.
//
// Stamping is the write that matters most, because `agtk memory anchor` clears
// the one signal saying nobody has checked a claim — so a run scoped to some
// notes must not be able to stamp the others.
//
// A scoped grant names each note exactly, with no trailing wildcard. Note names
// are kebab-case, so a name may be a prefix of another name: a grant reading
// `anchor lockfile-pins*` also permits `anchor lockfile-pins-shas-not-tags`,
// which is a different note the run never checked. The cost of the exact form
// is that a scoped run stamps one note per call, which the prompt says to do.
//
// An unscoped run gets the open grant, since its scope is the store.
func anchorGrants(agtk string, notes []string) []string {
	if len(notes) == 0 {
		return []string{"Bash(" + agtk + " memory anchor*)"}
	}
	grants := make([]string, 0, len(notes))
	for _, n := range notes {
		grants = append(grants, "Bash("+agtk+" memory anchor "+n+")")
	}
	return grants
}

// editPattern renders a directory as the body of an Edit rule matching
// everything beneath it.
//
// An absolute path is doubled at the root — `//tmp/x/**` — because a single
// leading slash is read as relative to the project directory, and a pattern
// that resolves nowhere denies every write including the ones the run exists
// to make.
func editPattern(dir string) string {
	if filepath.IsAbs(dir) {
		return "/" + dir + "/**"
	}
	return dir + "/**"
}

// bound is how one run is confined, in the vocabulary its provider has. The
// two fields are alternatives rather than layers: a provider that takes a
// per-tool allowlist is bounded by the list, and one that does not is bounded
// by a sandbox mode.
type bound struct {
	mode  string
	tools []string
}

// sandboxReadOnly is the mode a provider without a per-tool allowlist is
// confined with.
//
// The spelling is one CLI's vocabulary, which is exactly what this package
// should not know. It is asked for rather than assumed: confine puts it
// through the provider's own PermissionArgs and refuses when that comes back
// with a refusal, so a provider spelling confinement differently produces an
// error naming what it does accept rather than a run that was never bounded.
const sandboxReadOnly = "read-only"

// confine works out how to bound this run on this provider.
//
// It discovers the vocabulary by asking, never by switching on the provider's
// ID: the narrower grant — the per-tool allowlist — is offered first, and a
// provider with no such vocabulary refuses it with ErrInvalidRequest, which is
// the signal to fall back to a sandbox mode.
//
// A run that writes is refused outright on a provider with no allowlist. The
// widest sandbox that would let it write covers the whole workspace, and
// accepting that would make the store's own notes false: they say the grant is
// what confines the curator and that it is scoped to the notes directory. A
// preview writes nothing, so a read-only sandbox expresses it exactly — more
// tightly, in fact, than withholding tools from a list does.
func confine(p agentic.Provider, tools []string, dryRun bool) (bound, error) {
	perm, ok := p.(agentic.Permitter)
	if !ok {
		return bound{}, fmt.Errorf(
			"%s cannot be told what a scripted run may do, so curation cannot be bounded on it",
			p.Descriptor().ID)
	}

	if _, err := perm.PermissionArgs(permissionMode, tools); err == nil {
		return bound{mode: permissionMode, tools: tools}, nil
	} else if !errors.Is(err, agentic.ErrInvalidRequest) {
		return bound{}, err
	}

	if !dryRun {
		return bound{}, fmt.Errorf(
			"%s has no per-tool allowlist, so a curation run that writes notes cannot be confined to the store on it; "+
				"re-run with --dry-run, which is bounded by the %q sandbox, or point memory.agent at a provider that grants tools",
			p.Descriptor().ID, sandboxReadOnly)
	}

	b := bound{mode: sandboxReadOnly}
	if _, err := perm.PermissionArgs(b.mode, nil); err != nil {
		return bound{}, fmt.Errorf("%s has no per-tool allowlist and does not accept the %q sandbox mode: %w",
			p.Descriptor().ID, sandboxReadOnly, err)
	}
	return b, nil
}

// roster is the curator's agent definition, or nil for a provider that cannot
// define one. A nil roster means the policy travels in the prompt instead —
// see task.
func roster(p agentic.Provider) map[string]agentic.Agent {
	if _, ok := p.(agentic.AgentDefiner); !ok {
		return nil
	}
	return map[string]agentic.Agent{
		AgentName: {Description: agentDescription, Prompt: prompt},
	}
}

// permissionMode is empty, which passes no mode and leaves the CLI's own
// default in force. Under a constructed grant that is already the behaviour
// this run wants: every tool in AllowedTools proceeds unprompted, and anything
// outside it is denied outright rather than prompted for, because a
// non-interactive run has nobody to ask.
//
// Emphatically not a mode that waives prompting. Those outrank AllowedTools
// rather than combining with it, which would leave the grant above as
// decoration and hollow out the enforcement claim in ADR 0004. `acceptEdits`
// is exactly such a mode for the half of the grant that matters most: it
// approves every file edit anywhere on disk, so the notes directory named in
// the Edit rule stops being a boundary and becomes a comment.
const permissionMode = ""

// Run curates the store and returns the curator's report.
func Run(ctx context.Context, opts Options) (Result, error) {
	provider, err := newProvider(opts.Provider)
	if err != nil {
		return Result{}, err
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	driverOpts := []agentic.Option{
		agentic.WithWorkDir(opts.WorkDir),
		agentic.WithTimeout(timeout),
	}
	if opts.Binary != "" {
		driverOpts = append(driverOpts, agentic.WithBinary(opts.Binary))
	}
	driver, err := agentic.New(provider, driverOpts...)
	if err != nil {
		return Result{}, err
	}
	if err := driver.Ready(); err != nil {
		return Result{}, err
	}

	b, err := confine(provider, allowedTools(opts.AgtkPath, opts.NotesDir, opts.CandidatesDir, opts.scope()), opts.DryRun)
	if err != nil {
		return Result{}, err
	}
	agents := roster(provider)

	res, err := driver.Run(ctx, agentic.Request{
		Prompt:         task(opts, agents != nil),
		Agents:         agents,
		AllowedTools:   b.tools,
		PermissionMode: b.mode,
		WorkDir:        opts.WorkDir,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{
		Text:    strings.TrimSpace(res.Text),
		IsError: res.IsError,
		Model:   res.Model,
		CostUSD: res.Usage.CostUSD,
	}, nil
}

// agentDescription is what the delegating model reads when deciding to use the
// roster entry, so it says when — not what.
const agentDescription = "Promotes, merges and rejects findings staged in the repo's memory store, and re-checks notes whose anchors have moved. The only author of notes."

// task is the instruction the run itself receives. It says which of the two
// jobs to do and which binary to do it with; where the curator's content policy
// travels depends on whether this provider has a roster.
//
// With a roster the policy is the roster entry and this delegates to it. Without
// one, the policy is prepended here instead. It cannot simply be dropped: a run
// given the job and none of the rules would promote candidates without the
// quality bar, the anchoring rule or the single-writer discipline that make it
// curation rather than filing.
//
// The path is spelled out because the prompt speaks of `agtk` generically while
// the grant permits exactly one executable. A curator that reached for the bare
// name would be denied by its own grant, and — worse — the `agtk` on PATH may
// predate the memory subsystem entirely, so the reach would fail even if it
// were allowed.
func task(opts Options, delegates bool) string {
	agtk := opts.AgtkPath
	if agtk == "" {
		agtk = "agtk"
	}
	preamble := "Delegate to the " + AgentName + " agent. "
	if !delegates {
		preamble = prompt + "\n\n---\n\nThose are your instructions. "
	}
	preamble += "Use `" + agtk +
		"` for every agtk command — that exact path, never the bare name `agtk`, which may " +
		"resolve to an older build without the `memory` subcommand and is not in your tool grant. "

	var job string
	if opts.Stale {
		job = "Sweep the memory store's stale notes: run `" + agtk +
			" memory audit --json` for the list, then re-check each stale note's claim against " +
			"the code its pointers name and update, re-stamp or reject it. "
	} else {
		job = "Curate the memory store's staged candidates: run `" + agtk +
			" memory candidates --json` for the backlog. "
	}

	if len(opts.Notes) > 0 {
		job += "Consider only these notes and the candidates targeting them: " +
			strings.Join(opts.Notes, ", ") + ". Leave every other note and candidate alone. "
	}

	if opts.DryRun {
		// The confinement already makes this true, so the sentence is not what
		// makes the run safe. It is what stops the curator spending its budget
		// discovering that one refusal at a time, and reporting a denial where
		// a preview was asked for. Phrased without naming a mechanism because
		// there are two: a withheld tool grant, or a read-only sandbox.
		job += "This is a dry run: report what you would promote, merge, reject or " +
			"re-stamp, and why, but write nothing. Writing is not available to you. " +
			"Do not stamp anchors, regenerate the index or delete candidates. "
	}

	return preamble + job + "Report exactly what the agent reports."
}

// newProvider resolves `memory.agent` to a provider.
//
// A name the driver has no provider for is a gap to fill in the driver, where
// the dialect knowledge is tested, rather than an escape hatch here.
func newProvider(name string) (agentic.Provider, error) {
	switch name {
	case "":
		return nil, ErrNoProvider
	case "claudecode":
		// On PATH, not vendored: curation runs on a developer's machine
		// against the CLI they are already authenticated with.
		return claudecode.NewOnPath()
	case "codex":
		return codex.New(), nil
	default:
		return nil, fmt.Errorf("memory.agent %q is not a provider; use one of %s",
			name, strings.Join(Providers, ", "))
	}
}
