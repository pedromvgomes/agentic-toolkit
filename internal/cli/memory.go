package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// The memory command group is deliberately model-free. Curation and
// exploration ship as agent definitions; everything here is deterministic
// so it is safe on the path of hooks and CI. Only `anchor` writes notes.
func newMemoryCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage the repo-resident memory store",
		Long: "Deterministic operations on " + memory.DefaultRoot + ": regenerate the index,\n" +
			"stamp anchors, report stale notes, lint the store, read a note, and report\n" +
			"whether the store is paying for itself.\n" +
			"\n" +
			"Staleness is never stored. It is recomputed from working-tree content on\n" +
			"every run, so `audit` writes nothing and is safe to call from a hook.",
		// Cobra rejects an unknown subcommand only at the ROOT: a non-root
		// parent takes it as an argument, prints help and exits 0. An agent
		// told to run a subcommand this binary does not have — a misspelling,
		// or a definition newer than the installed `agtk` — would read that
		// help text as the command's output and report success.
		//
		// NoArgs alone does not close it, because a command with no Run is
		// not Runnable and cobra returns ErrHelp before it validates args. The
		// RunE is what makes the validation reachable; bare `agtk memory`
		// still prints help and exits 0.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		newMemoryIndexCmd(env),
		newMemoryAnchorCmd(env),
		newMemoryAuditCmd(env),
		newMemoryLintCmd(env),
		newMemoryShowCmd(env),
		newMemoryStatsCmd(env),
		newMemoryCandidatesCmd(env),
		newMemoryCurateCmd(env),
	)
	return cmd
}

// errMemoryStale and errMemoryLint flip the exit code after the command has
// already printed its own report; Execute suppresses the generic prefix for
// both, the way it does for status drift.
var (
	errMemoryStale = errors.New("memory: stale notes")
	errMemoryLint  = errors.New("memory: store has issues")
	// errMemoryCurate flips the exit code after the curator's own report has
	// been printed. The CLI ran fine; the curator declared the turn a
	// failure, and that verdict is in the report rather than in this error.
	errMemoryCurate = errors.New("memory: curation reported a failure")
)

// memoryProjectRoot is what anchor paths are relative to. It mirrors
// lockfilePath: next to the entry manifest normally, and the apply
// directory in --source mode, where the source tree is someone else's repo
// and must stay untouched.
func memoryProjectRoot(env *Env) string {
	if env.SourceDir != "" {
		return env.WorkDir
	}
	return stackDir(env)
}

// memoryManifestPath is the manifest whose `memory:` block applies.
//
// In --source mode the store belongs to the consumer being applied to, not
// to the source tree, so the source's `memory:` must not decide where the
// consumer commits its notes — the same rule the resolver enforces for
// stacks reached through extends:.
func memoryManifestPath(env *Env) string {
	if env.SourceDir != "" {
		return filepath.Join(env.WorkDir, ConfigFileName)
	}
	return configFilePath(env)
}

// memoryStore locates the store. It reads `memory:` from the entry manifest
// only — never from the extends graph — and does so by parsing that one
// file rather than resolving the stack, so the memory commands need no
// cache, no network and no lockfile.
func memoryStore(env *Env) (*memory.Store, error) {
	root := ""
	st, err := stack.ParseFile(memoryManifestPath(env))
	switch {
	case err == nil:
		root = st.MemoryRoot()
	case errors.Is(err, fs.ErrNotExist):
		// No manifest: the store still works, at its default location.
	default:
		// All these commands want from the manifest is `memory.root`. A
		// broken `extends:` ref elsewhere in it is a real problem, but not
		// this command's, and failing here would turn a memory hook red for
		// a reason that has nothing to do with the store.
		//
		// Re-read that one field rather than falling back to the default:
		// silently using a different store than the repo configured would
		// make lint green over an empty directory, which is worse than the
		// error we are tolerating.
		//
		// If even that read fails the YAML is malformed, so the store's
		// location is unknown. Guessing the default there would recreate the
		// bug this tolerance was added to fix — quietly operating on the
		// wrong store — so refuse instead.
		recovered, ok := stack.MemoryRootFromFile(memoryManifestPath(env))
		if !ok {
			// renderTopLevelError formats the ParseError structurally and
			// drops anything wrapped around it, so the reason this is fatal
			// for a memory command has to be said separately.
			fmt.Fprintln(env.Stderr, "the manifest could not be read at all, so `memory.root` is unknown;")
			fmt.Fprintln(env.Stderr, "refusing rather than guessing which store to operate on.")
			return nil, err
		}
		root = recovered
		fmt.Fprintf(env.Stderr, "warning: %v\n         reading only `memory.root` from it\n", err)
	}
	if err := memory.ValidateRoot(root); err != nil {
		return nil, err
	}
	return memory.New(memoryProjectRoot(env), root), nil
}

// loadStoreNotes is the shared prologue: locate the store, parse its notes.
// Parse errors are returned separately because index, audit and stats must
// keep working when one note is malformed; only lint reports them.
func loadStoreNotes(env *Env) (*memory.Store, []*memory.Note, []error, error) {
	store, err := memoryStore(env)
	if err != nil {
		return nil, nil, nil, err
	}
	notes, parseErrs := store.LoadNotes()
	// Every command but lint ignores these, and a note that silently drops
	// out of the index is exactly the kind of quiet loss the store must not
	// have. Warn once, here, for all of them.
	for _, e := range parseErrs {
		fmt.Fprintf(env.Stderr, "warning: skipping unreadable note: %v\n", e)
	}
	return store, notes, parseErrs, nil
}

// ===== index =====

func newMemoryIndexCmd(env *Env) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Regenerate INDEX.md from note frontmatter",
		Long: "Walks notes/*.md and rewrites INDEX.md — name, kind, confidence, description\n" +
			"and anchor paths. Nothing hand-writes the index, so it cannot drift from the\n" +
			"notes and a merge conflict in it is resolved by regenerating.\n" +
			"\n" +
			"Creates the store (notes/, candidates/, .gitignore) when it does not exist.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, notes, _, err := loadStoreNotes(env)
			if err != nil {
				return err
			}
			if err := store.Scaffold(); err != nil {
				return err
			}
			changed, err := store.WriteIndex(notes)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(env, memoryIndexJSON{
					Version: jsonVersion,
					Path:    relToWork(env, store.IndexPath()),
					Notes:   len(notes),
					Changed: changed,
				})
			}
			state := "unchanged"
			if changed {
				state = "rewritten"
			}
			fmt.Fprintf(env.Stdout, "%s: %s (%s)\n", relToWork(env, store.IndexPath()), plural(len(notes), "note"), state)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

// ===== anchor =====

func newMemoryAnchorCmd(env *Env) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "anchor [name...]",
		Short: "Stamp current blob hashes into note anchors",
		Long: "Records each anchored file's git blob hash into the note, and expands glob\n" +
			"anchors to the files they currently match. With no arguments, stamps every\n" +
			"note; otherwise only the named ones.\n" +
			"\n" +
			"This is the only command that writes to notes/. It exists so nothing has to\n" +
			"produce a hash by hand: a wrong anchor is worse than no note, because it\n" +
			"short-circuits the check a reader would otherwise have done.",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, notes, _, err := loadStoreNotes(env)
			if err != nil {
				return err
			}
			selected, err := selectNotes(notes, args)
			if err != nil {
				return err
			}

			results := make([]memory.StampResult, 0, len(selected))
			var failed []string
			for _, n := range selected {
				res, err := store.Stamp(n)
				if err != nil {
					// Keep going: one unstampable note must not stop the
					// rest of the store from being brought up to date.
					fmt.Fprintf(env.Stderr, "error: %s: %v\n", n.Name, err)
					failed = append(failed, n.Name)
					continue
				}
				results = append(results, res)
			}
			if jsonOut {
				if err := writeJSON(env, memoryAnchorJSON{Version: jsonVersion, Notes: anchorJSONNotes(results)}); err != nil {
					return err
				}
			} else {
				printAnchorReport(env, results, failed)
			}
			if len(failed) > 0 {
				return fmt.Errorf("could not stamp %s", strings.Join(failed, ", "))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

func printAnchorReport(env *Env, results []memory.StampResult, failed []string) {
	changed := 0
	for _, r := range results {
		if r.Changed {
			changed++
		}
	}
	switch {
	case len(results) == 0 && len(failed) == 0:
		fmt.Fprintln(env.Stdout, "no notes in the store")
	case len(failed) > 0:
		// Errors are on stderr; stdout must not read as success in a hook
		// log that shows only stdout. Keyed on failures rather than on an
		// empty result set, because a store with no notes yet is healthy.
		fmt.Fprintf(env.Stdout, "%s could not be stamped\n", plural(len(failed), "note"))
	case changed == 0:
		fmt.Fprintf(env.Stdout, "%s already current\n", plural(len(results), "note"))
	default:
		fmt.Fprintf(env.Stdout, "anchored %s:\n", plural(changed, "note"))
	}
	for _, r := range results {
		if !r.Changed {
			continue
		}
		fmt.Fprintf(env.Stdout, "  %s\n", r.Name)
		for _, a := range r.Anchors {
			switch {
			case a.Missing && a.IsGlob:
				fmt.Fprintf(env.Stdout, "    %-44s -> matches nothing, keeping %s\n",
					a.Path, plural(a.Matches, "previous match"))
			case a.Missing:
				// The hash shown is the one kept from the last stamp, so say
				// so — otherwise it reads exactly like a fresh stamp.
				fmt.Fprintf(env.Stdout, "    %-44s -> missing, keeping previous\n", a.Path)
			case a.IsGlob:
				fmt.Fprintf(env.Stdout, "    %-44s -> %s\n", a.Path, plural(a.Matches, "match"))
			default:
				fmt.Fprintf(env.Stdout, "    %-44s -> %s\n", a.Path, short(a.Blob))
			}
		}
	}
	for _, r := range results {
		for _, m := range r.Missing {
			fmt.Fprintf(env.Stderr, "warning: %s: %q matches nothing\n", r.Name, m)
		}
	}
}

// ===== audit =====

func newMemoryAuditCmd(env *Env) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Report notes whose anchored content has changed",
		Long: "Compares each note's recorded blob hashes against the working tree and lists\n" +
			"what moved: changed files, missing files, and files added to or removed from\n" +
			"a glob anchor.\n" +
			"\n" +
			"Writes nothing — staleness is derived, not stored, so this leaves no diff and\n" +
			"cannot destroy a curator's `confidence:` verdict. Exits non-zero when any note\n" +
			"is stale.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, notes, _, err := loadStoreNotes(env)
			if err != nil {
				return err
			}
			audits := store.Audit(notes)
			stale := staleOnly(audits)

			if jsonOut {
				if err := writeJSON(env, memoryAuditJSON{
					Version: jsonVersion,
					Notes:   len(notes),
					Stale:   auditJSONNotes(stale),
				}); err != nil {
					return err
				}
			} else {
				printAuditReport(env, len(notes), stale)
			}
			if len(stale) > 0 {
				return errMemoryStale
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

func printAuditReport(env *Env, total int, stale []memory.NoteAudit) {
	if len(stale) == 0 {
		fmt.Fprintf(env.Stdout, "all %s fresh\n", plural(total, "note"))
		return
	}
	fmt.Fprintf(env.Stdout, "stale (%d of %d):\n", len(stale), total)
	for _, a := range stale {
		fmt.Fprintf(env.Stdout, "  %s\n", a.Name)
		for _, d := range a.Drifts {
			fmt.Fprintf(env.Stdout, "    %-44s %s\n", d.Path, driftDetail(d))
		}
	}
}

func driftDetail(d memory.Drift) string {
	switch d.Kind {
	case memory.DriftChanged:
		if d.Was == "" {
			return "unstamped -> " + short(d.Now)
		}
		return short(d.Was) + " -> " + short(d.Now)
	case memory.DriftInvalid:
		return d.Detail
	case memory.DriftUnstamped:
		return "unstamped -> " + short(d.Now)
	case memory.DriftMissing:
		if d.Detail != "" {
			return d.Detail
		}
		return "missing"
	case memory.DriftAdded:
		return "added, matches " + d.Pattern
	case memory.DriftRemoved:
		return "removed, matched " + d.Pattern
	}
	return string(d.Kind)
}

// ===== lint =====

func newMemoryLintCmd(env *Env) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Structural check of the store, for CI",
		Long: "Checks that notes parse, that names are kebab-case and match their filenames,\n" +
			"that kind and confidence are in range, that every note has a description, a\n" +
			"body and at least one stamped anchor, and that INDEX.md matches what `agtk\n" +
			"memory index` would generate.\n" +
			"\n" +
			"It says nothing about whether a note is still TRUE — that is `audit`. Failing\n" +
			"CI on staleness would turn every rename in an unrelated PR red, and the path\n" +
			"of least resistance would become deleting the note.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, notes, parseErrs, err := loadStoreNotes(env)
			if err != nil {
				return err
			}
			issues := store.Lint(notes, parseErrs)

			if jsonOut {
				if err := writeJSON(env, memoryLintJSON{
					Version: jsonVersion,
					Notes:   len(notes),
					Issues:  lintJSONIssues(env, issues),
				}); err != nil {
					return err
				}
			} else {
				printLintReport(env, len(notes), issues)
			}
			if len(issues) > 0 {
				return errMemoryLint
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

func printLintReport(env *Env, total int, issues []memory.Issue) {
	if len(issues) == 0 {
		fmt.Fprintf(env.Stdout, "notes: %d ok\nindex: current\n", total)
		return
	}
	fmt.Fprintf(env.Stdout, "notes: %d checked, %s\n", total, plural(len(issues), "issue"))
	for _, i := range issues {
		where := relToWork(env, i.File)
		if i.Note != "" {
			where = i.Note
		}
		if where == "" {
			fmt.Fprintf(env.Stdout, "  %s\n", i.Message)
			continue
		}
		fmt.Fprintf(env.Stdout, "  %s: %s\n", where, i.Message)
	}
}

// ===== show =====

func newMemoryShowCmd(env *Env) *cobra.Command {
	var (
		jsonOut bool
		noHit   bool
	)
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Print a note and record the read",
		Long: "Prints one note with its freshness computed on the spot, and appends a line to\n" +
			"the gitignored " + memory.HitsFile + " so `agtk memory stats` can tell whether\n" +
			"the always-loaded index is being repaid.\n" +
			"\n" +
			"Reading notes through this command rather than opening the files is what makes\n" +
			"that measurement honest: a separate \"record a hit\" step is the kind of\n" +
			"bookkeeping an agent skips, and the denominator would quietly drift.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, notes, _, err := loadStoreNotes(env)
			if err != nil {
				return err
			}
			note := findNote(notes, args[0])
			if note == nil {
				return fmt.Errorf("no note named %q in %s", args[0], relToWork(env, store.NotesPath()))
			}
			audit := store.AuditNote(note)

			if !noHit {
				// Telemetry must never break a read.
				if err := store.RecordHit(note.Name, time.Now()); err != nil {
					fmt.Fprintf(env.Stderr, "warning: record hit: %v\n", err)
				}
			}
			if jsonOut {
				return writeJSON(env, memoryShowJSON{
					Version:     jsonVersion,
					Name:        note.Name,
					Kind:        string(note.Kind),
					Confidence:  string(note.Confidence),
					Description: note.Description,
					Stale:       audit.Stale(),
					Anchors:     anchorPaths(note),
					Body:        strings.TrimSpace(note.Body),
				})
			}
			fmt.Fprintf(env.Stdout, "%s\n---\nkind: %s   confidence: %s   anchors: %d   stale: %s\n---\n%s\n",
				note.Name, note.Kind, note.Confidence, len(note.Anchors), yesNo(audit.Stale()),
				strings.TrimSpace(note.Body))
			if audit.Stale() {
				fmt.Fprintln(env.Stdout)
				for _, d := range audit.Drifts {
					fmt.Fprintf(env.Stdout, "stale: %-40s %s\n", d.Path, driftDetail(d))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	cmd.Flags().BoolVar(&noHit, "no-hit", false, "do not record this read in "+memory.HitsFile)
	return cmd
}

// ===== stats =====

func newMemoryStatsCmd(env *Env) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Report store size, staleness and hit rate",
		Long: "The index is a tax collected on every session; the notes pay out only on a\n" +
			"hit. Hit rate is what says whether the tax is repaid — and if it stays low,\n" +
			"the answer is to prune, never to store more.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, notes, _, err := loadStoreNotes(env)
			if err != nil {
				return err
			}
			st, err := store.Stats(notes)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(env, statsJSON(env, store, st))
			}
			printStats(env, store, st)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

func printStats(env *Env, store *memory.Store, st memory.Stats) {
	fmt.Fprintf(env.Stdout, "root:        %s\n", relToWork(env, store.Root))
	fmt.Fprintf(env.Stdout, "notes:       %d\n", st.Notes)
	for _, k := range memory.AllKinds {
		if n := st.ByKind[k]; n > 0 {
			fmt.Fprintf(env.Stdout, "  %-10s %d\n", string(k)+":", n)
		}
	}
	for _, c := range memory.AllConfidences {
		if n := st.ByConfidence[c]; n > 0 {
			fmt.Fprintf(env.Stdout, "  %-10s %d\n", string(c)+":", n)
		}
	}
	fmt.Fprintf(env.Stdout, "anchors:     %d (%d files)\n", st.Anchors, st.AnchoredFile)
	fmt.Fprintf(env.Stdout, "stale:       %d\n", st.Stale)
	fmt.Fprintf(env.Stdout, "candidates:  %d\n", st.Candidates)
	if st.Hits == 0 {
		fmt.Fprintln(env.Stdout, "hits:        none recorded")
		return
	}
	fmt.Fprintf(env.Stdout, "hits:        %s over %d of %d notes (%.0f%% hit rate)\n",
		plural(st.Hits, "read"), st.NotesHit, st.Notes, st.HitRate*100)
	fmt.Fprintf(env.Stdout, "  window:    %s .. %s\n",
		st.FirstHit.Format(time.RFC3339), st.LastHit.Format(time.RFC3339))
}

// ===== candidates =====

func newMemoryCandidatesCmd(env *Env) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "candidates",
		Short: "List the findings staged for curation",
		Long: "Prints what is waiting in candidates/: what each finding is about, the paths\n" +
			"it came from, and — for a re-check of an existing note — which note it\n" +
			"concerns and what the explorer concluded.\n" +
			"\n" +
			"Structural problems are reported alongside, because a candidate is the one\n" +
			"input to curation nothing else checks: a malformed one becomes a bad note or\n" +
			"a silently dropped finding.\n" +
			"\n" +
			"Reports only. Promoting, merging and rejecting are `agtk memory curate`, and\n" +
			"the difference is that this one never invokes a model.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := memoryStore(env)
			if err != nil {
				return err
			}
			// Parse failures are part of the report rather than a warning on
			// the side. A finding that cannot be read is still a finding that
			// was staged, and reporting only what parsed would say "no
			// candidates staged" over a directory holding three of them —
			// which is the silent loss this command exists to surface.
			candidates, parseErrs := store.LoadCandidates()

			if jsonOut {
				return writeJSON(env, memoryCandidatesJSON{
					Version:    jsonVersion,
					Path:       relToWork(env, store.CandidatesPath()),
					Staged:     len(candidates) + len(parseErrs),
					Candidates: candidateJSONEntries(env, candidates),
					Unreadable: unreadableJSONEntries(parseErrs),
				})
			}
			printCandidatesReport(env, candidates, parseErrs)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

func printCandidatesReport(env *Env, candidates []*memory.Candidate, parseErrs []error) {
	staged := len(candidates) + len(parseErrs)
	if staged == 0 {
		fmt.Fprintln(env.Stdout, "no candidates staged")
		return
	}
	// Counted the same way `agtk memory stats` counts them — every file in
	// the directory — so the two never disagree about the size of the
	// backlog and a hook can quote either.
	fmt.Fprintf(env.Stdout, "%s staged:\n", plural(staged, "candidate"))
	for _, c := range candidates {
		fmt.Fprintf(env.Stdout, "  %s\n", c.Stem())
		if c.About != "" {
			fmt.Fprintf(env.Stdout, "    about:   %s\n", c.About)
		}
		if len(c.Saw) > 0 {
			fmt.Fprintf(env.Stdout, "    saw:     %s\n", strings.Join(c.Saw, ", "))
		}
		if c.Targets != "" {
			fmt.Fprintf(env.Stdout, "    targets: %s (%s)\n", c.Targets, c.Verdict)
		}
		for _, issue := range c.CandidateIssues() {
			fmt.Fprintf(env.Stdout, "    issue:   %s\n", issue)
		}
	}
	// On stdout, not stderr: a hook reads stdout, and a finding nobody can
	// read is exactly what a backlog report must not omit.
	for _, e := range parseErrs {
		fmt.Fprintf(env.Stdout, "  unreadable: %v\n", e)
	}
}

// ===== curate =====

// newMemoryCurateCmd is the one memory subcommand that invokes a model.
//
// Everything above it is deterministic and safe on the path of a hook; this
// one spends money and reaches outside the machine, which is why it is a
// separate command rather than a flag on `audit`. Nothing fires it implicitly.
func newMemoryCurateCmd(env *Env) *cobra.Command {
	var (
		jsonOut bool
		stale   bool
		check   bool
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "curate",
		Short: "Run the curator over staged candidates, or over stale notes",
		Long: "Promotes, merges and rejects the findings in candidates/, then stamps and\n" +
			"regenerates the index. With --stale, sweeps notes whose anchored content has\n" +
			"moved instead.\n" +
			"\n" +
			"The curator runs in its own process with its own context, holding the store\n" +
			"and the backlog and not this session's history. Its tool grant is constructed\n" +
			"here and passed on the command line, so notes/ has one writer by\n" +
			"construction rather than by instruction.\n" +
			"\n" +
			"Names its provider through `memory.agent` in the entry manifest. There is no\n" +
			"default: this is the only memory command that costs anything.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := memoryStore(env)
			if err != nil {
				return err
			}
			provider, err := memoryAgent(env)
			if err != nil {
				return err
			}

			if check {
				ready, err := curator.Check(curator.Options{
					Provider:      provider,
					WorkDir:       store.ProjectRoot,
					CandidatesDir: store.CandidatesPath(),
					AgtkPath:      selfPath(env),
				})
				if err != nil {
					return err
				}
				if jsonOut {
					return writeJSON(env, memoryCurateCheckJSON{
						Version:  jsonVersion,
						Provider: ready.Provider,
						Binary:   ready.Binary,
						Mode:     ready.Mode,
						Tools:    ready.Tools,
					})
				}
				fmt.Fprintf(env.Stdout, "provider:  %s\nbinary:    %s\nmode:      %s\ntools:     %s\n",
					ready.Provider, ready.Binary, ready.Mode, strings.Join(ready.Tools, ", "))
				return nil
			}

			res, err := curator.Run(cmd.Context(), curator.Options{
				Provider: provider,
				// The curator resolves the store the way every other command
				// does — by running `agtk` — so it has to start where agtk
				// would have.
				WorkDir: store.ProjectRoot,
				// Scopes the curator's deletion grant, so it can clear the
				// backlog and nothing else.
				CandidatesDir: store.CandidatesPath(),
				// The running binary, not whatever PATH resolves: a consumer
				// installs agtk separately from the lockfile-pinned
				// definitions, so the agtk on PATH can be older than this one
				// and lack `memory` entirely.
				AgtkPath: selfPath(env),
				Stale:    stale,
				Timeout:  timeout,
			})
			if err != nil {
				return err
			}

			if jsonOut {
				if err := writeJSON(env, memoryCurateJSON{
					Version: jsonVersion,
					Stale:   stale,
					Failed:  res.IsError,
					Model:   res.Model,
					CostUSD: res.CostUSD,
					Report:  res.Text,
				}); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(env.Stdout, res.Text)
			}
			if res.IsError {
				return errMemoryCurate
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	cmd.Flags().BoolVar(&stale, "stale", false, "sweep stale notes instead of the candidate backlog")
	cmd.Flags().BoolVar(&check, "check", false, "report what a run would use and start nothing")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "bound the curation run (default 20m)")
	return cmd
}

// selfPath is this binary's own path, for a child that shells back into agtk.
//
// A failure here is not worth refusing a run over: the grant falls back to the
// bare name, which is what a curator would have used anyway.
func selfPath(env *Env) string {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(env.Stderr, "warning: cannot resolve this binary's path (%v); the curator will use whatever `agtk` is on PATH\n", err)
		return ""
	}
	return exe
}

// memoryAgent reads `memory.agent` from the entry manifest, the same way and
// from the same file memoryStore reads `memory.root`.
func memoryAgent(env *Env) (string, error) {
	st, err := stack.ParseFile(memoryManifestPath(env))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// No manifest is the same state as a manifest naming no
			// provider. Returning empty rather than the sentinel keeps one
			// place — the curator, which knows the provider names — in charge
			// of saying what to do about it.
			return "", nil
		}
		return "", err
	}
	return st.MemoryAgent(), nil
}

// ===== shared helpers =====

// selectNotes filters notes by name, erroring on a name that is not in the
// store rather than silently stamping nothing.
func selectNotes(notes []*memory.Note, names []string) ([]*memory.Note, error) {
	if len(names) == 0 {
		return notes, nil
	}
	out := make([]*memory.Note, 0, len(names))
	for _, name := range names {
		n := findNote(notes, name)
		if n == nil {
			return nil, fmt.Errorf("no note named %q", name)
		}
		out = append(out, n)
	}
	return out, nil
}

func findNote(notes []*memory.Note, name string) *memory.Note {
	name = strings.TrimSuffix(name, memory.NoteExt)
	for _, n := range notes {
		if n.Name == name {
			return n
		}
	}
	return nil
}

func staleOnly(audits []memory.NoteAudit) []memory.NoteAudit {
	var out []memory.NoteAudit
	for _, a := range audits {
		if a.Stale() {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func anchorPaths(n *memory.Note) []string {
	paths := make([]string, 0, len(n.Anchors))
	for _, a := range n.Anchors {
		paths = append(paths, a.Path)
	}
	return paths
}

// relToWork renders an absolute path relative to the working directory when
// that is shorter, so reports stay readable in a worktree layout.
func relToWork(env *Env, path string) string {
	rel, err := filepath.Rel(env.WorkDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

func short(blob string) string {
	if len(blob) > 7 {
		return blob[:7]
	}
	return blob
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	if strings.HasSuffix(word, "h") {
		return fmt.Sprintf("%d %ses", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
