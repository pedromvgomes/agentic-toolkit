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
	"fmt"
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
func AllowedTools(agtk, candidatesDir string) []string {
	return allowedTools(agtk, candidatesDir, grantScope{})
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
	return Ready{
		Provider: driver.Descriptor().ID,
		Binary:   driver.Binary(),
		Tools:    allowedTools(opts.AgtkPath, opts.CandidatesDir, opts.scope()),
		Mode:     permissionMode,
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
	// asking the model nicely: a run told to hold back but handed Write and
	// Edit is one refusal away from editing the store.
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
// The deletion grant is scoped to the candidates directory rather than left as
// a bare `rm`. Clearing the backlog is the only thing the curator deletes, and
// a grant that reads `rm *` would let the one agent with a constructed grant
// remove anything in the repo — which is the guarantee this list exists to
// make, given away in its last line. The directory is a parameter because
// `memory.root` is configurable, so there is no path to hard-code.
func allowedTools(agtk, candidatesDir string, opts grantScope) []string {
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

	tools = append(tools, "Write", "Edit", "Bash("+agtk+" memory index*)")
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

// permissionMode lets the run act on its grant without a prompt nobody is
// there to answer.
//
// Emphatically not a mode that waives prompting altogether: those outrank
// AllowedTools rather than combining with it, which would leave the grant
// above as decoration and hollow out the enforcement claim in ADR 0004.
const permissionMode = "acceptEdits"

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

	res, err := driver.Run(ctx, agentic.Request{
		Prompt: task(opts),
		Agents: map[string]agentic.Agent{
			AgentName: {Description: agentDescription, Prompt: prompt},
		},
		AllowedTools:   allowedTools(opts.AgtkPath, opts.CandidatesDir, opts.scope()),
		PermissionMode: permissionMode,
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

// task is the instruction the run itself receives. The curator's own content
// policy lives in the roster entry; this says which of its two jobs to do, and
// which binary to do it with.
//
// The path is spelled out because the prompt speaks of `agtk` generically while
// the grant permits exactly one executable. A curator that reached for the bare
// name would be denied by its own grant, and — worse — the `agtk` on PATH may
// predate the memory subsystem entirely, so the reach would fail even if it
// were allowed.
func task(opts Options) string {
	agtk := opts.AgtkPath
	if agtk == "" {
		agtk = "agtk"
	}
	preamble := "Delegate to the " + AgentName + " agent. Use `" + agtk +
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
		// The grant already withholds every writing tool, so this is not what
		// makes the run safe. It is what stops the curator spending its budget
		// discovering that one tool call at a time, and reporting a refusal
		// where a preview was asked for.
		job += "This is a dry run: report what you would promote, merge, reject or " +
			"re-stamp, and why, but write nothing. You have no writing tools. " +
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
