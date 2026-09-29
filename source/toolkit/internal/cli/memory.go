package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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
			"stamp anchors, report stale notes, lint the store, search and read notes, and\n" +
			"report whether the store is paying for itself.\n" +
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
		newMemorySearchCmd(env),
		newMemoryStatsCmd(env),
		newMemoryHitsCmd(env),
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
	m, err := stack.ParseEntryManifestFile(memoryManifestPath(env))
	switch {
	case err == nil:
		root = m.MemoryRoot()
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
	store := memory.New(memoryProjectRoot(env), root)
	// Every memory command comes through here, so a store left at the old
	// default is reported by whichever one the repo runs first rather than
	// by the one that happens to scaffold over it.
	if err := store.CheckLegacyRoot(); err != nil {
		return nil, err
	}
	return store, nil
}

// loadStoreNotes is the shared prologue: locate the store, parse its notes.
// Parse errors are returned separately so each caller decides for itself
// whether an unreadable note is a warning or a failure: index and audit still
// produce their normal output for the notes that did parse, then fail the
// run with unreadableNotesErr's full list; lint folds them into its own
// issue list; stats leaves them as a warning only, because its exit code
// means "the store itself could not be read" and must not be conflated with
// "one note's frontmatter is bad".
//
// warn prints "skipping unreadable note" for each one as it is found. A
// caller that goes on to report the same errors in full — index and audit,
// through unreadableNotesErr — passes false, so the operator sees the list
// once instead of once per note and then again in full.
func loadStoreNotes(env *Env, warn bool) (*memory.Store, []*memory.Note, []error, error) {
	store, err := memoryStore(env)
	if err != nil {
		return nil, nil, nil, err
	}
	notes, parseErrs := store.LoadNotes()
	if warn {
		// A note that silently drops out of the index is exactly the kind
		// of quiet loss the store must not have.
		for _, e := range parseErrs {
			fmt.Fprintf(env.Stderr, "warning: skipping unreadable note: %v\n", e)
		}
	}
	return store, notes, parseErrs, nil
}

// unreadableNotesErr names every note loadStoreNotes could not parse, so a
// caller that fails on parse errors reports which files and why rather than
// only a bare non-zero exit.
func unreadableNotesErr(parseErrs []error) error {
	msgs := make([]string, len(parseErrs))
	for i, e := range parseErrs {
		msgs[i] = e.Error()
	}
	return fmt.Errorf("%s unreadable:\n%s", plural(len(parseErrs), "note"), strings.Join(msgs, "\n"))
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
			store, notes, parseErrs, err := loadStoreNotes(env, false)
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
				if err := writeJSON(env, memoryIndexJSON{
					Version: jsonVersion,
					Path:    relToWork(env, store.IndexPath()),
					Notes:   len(notes),
					Changed: changed,
				}); err != nil {
					return err
				}
			} else {
				state := "unchanged"
				if changed {
					state = "rewritten"
				}
				fmt.Fprintf(env.Stdout, "%s: %s (%s)\n", relToWork(env, store.IndexPath()), plural(len(notes), "note"), state)
			}
			// The index is now written without the notes that failed to
			// parse; report that as a failure rather than let the run
			// look clean while the index silently narrowed.
			if len(parseErrs) > 0 {
				return unreadableNotesErr(parseErrs)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

// ===== anchor =====

func newMemoryAnchorCmd(env *Env) *cobra.Command {
	var (
		jsonOut bool
		all     bool
	)
	cmd := &cobra.Command{
		Use:   "anchor [name...]",
		Short: "Stamp current blob hashes into the notes you name",
		Long: "Records each anchored file's git blob hash into the note, and expands glob\n" +
			"anchors to the files they currently match. Stamps the notes you name; --all\n" +
			"stamps every note in the store.\n" +
			"\n" +
			"This is the only command that writes to notes/. It exists so nothing has to\n" +
			"produce a hash by hand: a wrong anchor is worse than no note, because it\n" +
			"short-circuits the check a reader would otherwise have done.\n" +
			"\n" +
			"Naming notes is required rather than optional because stamping does not only\n" +
			"record hashes — it clears the staleness signal, which is the one thing that\n" +
			"tells the next reader nobody has checked a claim. Stamping the whole store\n" +
			"marks notes fresh that nobody looked at, and no later audit will flag them\n" +
			"again.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && !all {
				return errors.New("name the notes to stamp, or pass --all to stamp every note in the store;\n" +
					"stamping clears the staleness signal, so doing it to notes you have not checked\n" +
					"marks them fresh on nobody's say-so")
			}
			if len(args) > 0 && all {
				return errors.New("--all stamps every note; naming notes as well says two different things")
			}

			store, notes, parseErrs, err := loadStoreNotes(env, true)
			if err != nil {
				return err
			}
			selected, failed := selectNotes(store, notes, parseErrs, args)

			results := make([]memory.StampResult, 0, len(selected))
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
	cmd.Flags().BoolVar(&all, "all", false, "stamp every note in the store")
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
			store, notes, parseErrs, err := loadStoreNotes(env, false)
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
			// An unreadable note never reaches Audit, so it cannot show up
			// as stale; report it as its own failure or the note's absence
			// from the report reads as "fresh" instead of "unchecked".
			if len(parseErrs) > 0 {
				return unreadableNotesErr(parseErrs)
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
			"memory index` would generate. It also reports each candidate in candidates/\n" +
			"that cannot be parsed, with the file and the error, since such a candidate\n" +
			"is otherwise dropped from the backlog in silence.\n" +
			"\n" +
			"It says nothing about whether a note is still TRUE — that is `audit`. Failing\n" +
			"CI on staleness would turn every rename in an unrelated PR red, and the path\n" +
			"of least resistance would become deleting the note.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, notes, parseErrs, err := loadStoreNotes(env, true)
			if err != nil {
				return err
			}
			issues := store.Lint(notes, parseErrs)
			// Unreadable candidates are reported here and not by Lint, which
			// the curator runs on its own work and cannot act on them.
			issues = append(issues, store.LintCandidates()...)

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
			store, notes, _, err := loadStoreNotes(env, true)
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

// ===== search =====

// memorySearchJSON is `memory search --json`'s output. Results is never null,
// so a script iterates it without a nil check and a query that matches nothing
// is an empty list. Each entry carries what a caller needs to decide whether to
// open the note — no timestamps and no hit counts, since a search records and
// reports neither.
type memorySearchJSON struct {
	Version int                      `json:"version"`
	Results []memorySearchResultJSON `json:"results"`
}

type memorySearchResultJSON struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Confidence  string `json:"confidence"`
	Description string `json:"description"`
	// Anchors are the note's own anchor paths that cover a queried file; never
	// null, and empty for a note that matched on words alone.
	Anchors []string `json:"anchors"`
	Score   int      `json:"score"`
	Stale   bool     `json:"stale"`
	Show    string   `json:"show"`
}

func newMemorySearchCmd(env *Env) *cobra.Command {
	var (
		jsonOut bool
		files   []string
		limit   int
	)
	cmd := &cobra.Command{
		Use:   "search [--files a,b] [words...]",
		Short: "Rank notes against files and words, without recording a read",
		Long: "Lists the notes that anchor the files you name and the notes that mention the\n" +
			"words you give, best match first. A note anchoring a named file outranks any\n" +
			"note matching on words alone.\n" +
			"\n" +
			"Reads the notes and nothing else: no model is invoked, no hit is recorded and\n" +
			"nothing under the store is written, so it is safe on the path of a hook. Open\n" +
			"a result with the `agtk memory show` command it prints — that read is the one\n" +
			"that counts toward `stats`.\n" +
			"\n" +
			"--files takes paths relative to the working directory, or absolute, as a\n" +
			"comma-separated list or repeated; a path outside the project root is refused.\n" +
			"A `stale` label marks a note whose anchored content has changed; it does not\n" +
			"move the note's rank.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit <= 0 {
				return fmt.Errorf("--limit must be at least 1, got %d", limit)
			}
			if len(files) == 0 && len(args) == 0 {
				return memory.ErrEmptyQuery
			}

			store, err := memoryStore(env)
			if err != nil {
				return err
			}
			projectRoot := memoryProjectRoot(env)
			query := memory.Query{Words: args, Limit: limit}
			for _, f := range files {
				if f == "" {
					continue
				}
				rel, err := memory.NormaliseSearchPath(projectRoot, env.WorkDir, f)
				if err != nil {
					return err
				}
				query.Files = append(query.Files, rel)
			}

			results, err := memory.Search(memory.NewFileCorpus(store), query, env.Stderr)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(env, memorySearchJSON{Version: jsonVersion, Results: searchJSONResults(results)})
			}
			printSearchResults(env, results)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	cmd.Flags().StringSliceVar(&files, "files", nil, "files to find anchoring notes for, comma-separated or repeated")
	cmd.Flags().IntVar(&limit, "limit", memory.DefaultSearchLimit, "return at most this many notes")
	return cmd
}

func searchJSONResults(results []memory.Result) []memorySearchResultJSON {
	out := make([]memorySearchResultJSON, 0, len(results))
	for _, r := range results {
		anchors := r.Anchors
		if anchors == nil {
			anchors = []string{}
		}
		out = append(out, memorySearchResultJSON{
			Name:        r.Name,
			Kind:        string(r.Kind),
			Confidence:  string(r.Confidence),
			Description: r.Description,
			Anchors:     anchors,
			Score:       r.Score,
			Stale:       r.Stale,
			Show:        r.Show,
		})
	}
	return out
}

func printSearchResults(env *Env, results []memory.Result) {
	if len(results) == 0 {
		fmt.Fprintln(env.Stdout, "no matching notes")
		return
	}
	for _, r := range results {
		fmt.Fprintf(env.Stdout, "%s\n", r.Name)
		line := fmt.Sprintf("kind: %s   confidence: %s   score: %d", r.Kind, r.Confidence, r.Score)
		if r.Stale {
			line += "   stale"
		}
		fmt.Fprintf(env.Stdout, "  %s\n", line)
		fmt.Fprintf(env.Stdout, "  %s\n", r.Description)
		if len(r.Anchors) > 0 {
			fmt.Fprintf(env.Stdout, "  anchors: %s\n", strings.Join(r.Anchors, ", "))
		}
		fmt.Fprintf(env.Stdout, "  show: %s\n", r.Show)
	}
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
			store, notes, _, err := loadStoreNotes(env, true)
			if err != nil {
				return err
			}
			st, err := store.Stats(notes)
			if err != nil {
				return err
			}
			for _, e := range st.UnreadableHits {
				fmt.Fprintf(env.Stderr, "warning: skipping unreadable hit record: %v\n", e)
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
	// Both halves of the ledger, adjacent on purpose: a hit rate with nothing
	// to compare it against says whether notes get read, not whether reading
	// them was worth what the index cost to carry.
	if st.IndexBytes == 0 {
		fmt.Fprintln(env.Stdout, "index:       not generated — run `agtk memory index`")
	} else {
		fmt.Fprintf(env.Stdout, "index:       %s (~%s) — the tax, loaded per delegation\n",
			humanBytes(st.IndexBytes), plural(approxTokens(st.IndexBytes), "token"))
	}
	// "this checkout" is not hedging. With no shard and no compacted file,
	// every hit comes from the gitignored log, so the rate describes one
	// working copy's usage and a fresh clone reports zero; a reader who takes
	// it for a property of the store draws the opposite conclusion from the
	// same number. Once a committed record exists the rate is the store's,
	// and only the reads not yet folded are this checkout's alone.
	shared := st.Shards > 0 || st.Compacted
	switch {
	case st.Hits == 0 && !shared:
		fmt.Fprintf(env.Stdout, "hits:        none recorded in this checkout (%s is gitignored)\n", memory.HitsFile)
	case st.Hits == 0:
		fmt.Fprintln(env.Stdout, "hits:        none recorded")
	case !shared:
		fmt.Fprintf(env.Stdout, "hits:        %s over %d of %d notes (%.0f%% hit rate, this checkout only)\n",
			plural(st.Hits, "read"), st.NotesHit, st.Notes, st.HitRate*100)
	default:
		fmt.Fprintf(env.Stdout, "hits:        %s over %d of %d notes (%.0f%% hit rate)\n",
			plural(st.Hits, "read"), st.NotesHit, st.Notes, st.HitRate*100)
	}
	if st.Hits > 0 {
		fmt.Fprintf(env.Stdout, "  window:    %s .. %s\n",
			st.FirstHit.Format(time.RFC3339), st.LastHit.Format(time.RFC3339))
	}
	if shared && st.LocalHits > 0 {
		fmt.Fprintf(env.Stdout, "  unfolded:  %s in %s, this checkout only — `agtk memory hits fold` shares them\n",
			plural(st.LocalHits, "read"), memory.HitsFile)
	}
	if len(st.Cold) > 0 {
		fmt.Fprintf(env.Stdout, "cold:        %d of %d notes never read\n", len(st.Cold), st.Notes)
		// n reads can warm at most n notes, so below one read per note a
		// non-empty cold list is guaranteed whatever the notes are worth. The
		// list still prints — withholding it would send a reader to --json to
		// misread the raw field instead — but a caveat it cannot act on is
		// better than a prune list it can.
		if st.Hits < st.Notes {
			fmt.Fprintf(env.Stdout, "             (%s cannot warm more than %d of %d notes — not yet a prune signal)\n",
				plural(st.Hits, "read"), st.Hits, st.Notes)
		}
		for _, name := range st.Cold {
			fmt.Fprintf(env.Stdout, "             %s\n", name)
		}
	}
}

// ===== hits =====

// errMemoryUnfolded flips `hits fold --check`'s exit code when the local log
// holds reads no shard carries yet.
var errMemoryUnfolded = errors.New("memory: the local hits log holds unfolded reads; run `agtk memory hits fold`")

func newMemoryHitsCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hits",
		Short: "Share the hit record beyond this checkout",
		Long: "`agtk memory show` records each read in the gitignored " + memory.HitsFile + ", which\n" +
			"no other checkout sees. `fold` moves those reads into a committed shard under\n" +
			memory.HitsDir + "/, so `stats` reports them from any clone of the branch.",
		// The same guard as `agtk memory`: without a RunE, cobra takes an
		// unknown subcommand as an argument, prints help and exits 0.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newMemoryHitsFoldCmd(env))
	return cmd
}

// memoryHitsFoldJSON is `memory hits fold --json`'s output. Hits and Notes
// count the reads in the local log and the distinct notes they are of —
// folded, or under --check waiting to be. Shard is the file written, and is
// absent under --check and when there was nothing to fold.
type memoryHitsFoldJSON struct {
	Version int    `json:"version"`
	Check   bool   `json:"check"`
	Log     string `json:"log"`
	Hits    int    `json:"hits"`
	Notes   int    `json:"notes"`
	Shard   string `json:"shard,omitempty"`
}

func newMemoryHitsFoldCmd(env *Env) *cobra.Command {
	var (
		jsonOut bool
		check   bool
	)
	cmd := &cobra.Command{
		Use:   "fold",
		Short: "Move the local hits log into a committed shard",
		Long: "Writes the reads in " + memory.HitsFile + " to a shard of their own,\n" +
			memory.HitsDir + "/<date>-<branch>-<suffix>.json, holding each note's read count and\n" +
			"first and last read, then empties the log. The shard is written first, so a\n" +
			"fold that cannot write it leaves the log as it was.\n" +
			"\n" +
			"Every fold writes a file no other fold names, so branches that both fold\n" +
			"merge without a conflict. Nothing is committed: the shard is left in the\n" +
			"working tree for your own commit.\n" +
			"\n" +
			"An empty or missing log writes nothing. --check writes nothing either, and\n" +
			"exits non-zero when the log holds reads not yet folded.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := memoryStore(env)
			if err != nil {
				return err
			}
			log := relToWork(env, store.HitsPath())
			if check {
				hits, err := store.Hits()
				if err != nil {
					return err
				}
				notes := len(memory.TallyHits(hits))
				if jsonOut {
					if err := writeJSON(env, memoryHitsFoldJSON{
						Version: jsonVersion, Check: true, Log: log, Hits: len(hits), Notes: notes,
					}); err != nil {
						return err
					}
				} else if len(hits) == 0 {
					fmt.Fprintf(env.Stdout, "%s: nothing to fold\n", log)
				} else {
					fmt.Fprintf(env.Stdout, "%s: %s over %s not yet folded\n", log, plural(len(hits), "read"), plural(notes, "note"))
				}
				if len(hits) > 0 {
					return errMemoryUnfolded
				}
				return nil
			}

			fold, err := store.FoldHits(memory.FoldOptions{
				Now:    time.Now(),
				Branch: currentBranch(store.ProjectRoot),
			})
			if err != nil {
				return err
			}
			if jsonOut {
				out := memoryHitsFoldJSON{Version: jsonVersion, Log: log, Hits: fold.Hits, Notes: fold.Notes}
				if fold.Shard != "" {
					out.Shard = relToWork(env, fold.Shard)
				}
				return writeJSON(env, out)
			}
			if fold.Shard == "" {
				fmt.Fprintf(env.Stdout, "%s: nothing to fold\n", log)
				return nil
			}
			fmt.Fprintf(env.Stdout, "folded %s over %s into %s; %s emptied\n",
				plural(fold.Hits, "read"), plural(fold.Notes, "note"), relToWork(env, fold.Shard), log)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	cmd.Flags().BoolVar(&check, "check", false, "write nothing; exit non-zero when the log holds unfolded reads")
	return cmd
}

// currentBranch is the branch checked out at dir, or empty on a detached
// HEAD, outside a repository, or without git — each of which names its
// shard with memory.BranchSlug's fallback rather than failing the fold.
func currentBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--quiet", "--short", "HEAD").Output() // #nosec G204 -- fixed argv; dir is the store's project root
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// humanBytes formats a size the way the tax is worth reading — two
// significant figures, not an exact count nobody compares.
func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f kB", float64(n)/1024)
}

// approxTokens estimates what the index costs to carry, at the four-bytes-per
// token rule of thumb. It is deliberately labelled `~` wherever it is printed:
// the honest number is tokens, and no exact one is available without a
// tokenizer for whichever model reads the store.
func approxTokens(n int64) int {
	return int((n + 3) / 4)
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
		dryRun  bool
		timeout time.Duration
		limit   int
	)
	cmd := &cobra.Command{
		Use:   "curate [note...]",
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
			"Naming notes scopes the run to them and to the candidates targeting them,\n" +
			"and narrows the stamping grant to those names — so a scoped run cannot clear\n" +
			"the staleness signal on a note it was not asked to check.\n" +
			"\n" +
			"--dry-run reports what the curator would do and writes nothing. The grant it\n" +
			"runs under has no writing tools at all, so this is a property of the run\n" +
			"rather than a promise the model keeps.\n" +
			"\n" +
			"A candidate whose frontmatter does not parse is listed at the end and fails\n" +
			"the run, since the curator cannot repair it; `agtk memory lint` names the error.\n" +
			"\n" +
			"Names its provider through `memory.agent` in the entry manifest. There is no\n" +
			"default: this is the only memory command that costs anything.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if limit > 0 && len(args) > 0 {
				return errors.New("--limit narrows the backlog by count; naming notes already narrows it by name")
			}
			if limit > 0 && stale {
				return errors.New("--limit narrows the backlog by count; --stale already narrows it to the stale list")
			}

			store, err := memoryStore(env)
			if err != nil {
				return err
			}
			provider, err := memoryAgent(env)
			if err != nil {
				return err
			}

			if err := knownNotes(env, store, args); err != nil {
				return err
			}

			if check {
				ready, err := curator.Check(curator.Options{
					Provider:      provider,
					WorkDir:       store.ProjectRoot,
					NotesDir:      store.NotesPath(),
					CandidatesDir: store.CandidatesPath(),
					StoreRoot:     store.Root,
					AgtkPath:      selfPath(env),
					DryRun:        dryRun,
					Notes:         args,
					Limit:         limit,
				})
				if err != nil {
					return err
				}
				return reportCurateCheck(env, jsonOut, ready)
			}

			res, err := curator.Run(cmd.Context(), curator.Options{
				Provider: provider,
				// The curator resolves the store the way every other command
				// does — by running `agtk` — so it has to start where agtk
				// would have.
				WorkDir: store.ProjectRoot,
				// Scope the curator's write and deletion grants, so it can
				// author notes and clear the backlog and nothing else.
				NotesDir:      store.NotesPath(),
				CandidatesDir: store.CandidatesPath(),
				StoreRoot:     store.Root,
				// The running binary, not whatever PATH resolves: a consumer
				// installs agtk separately from the lockfile-pinned
				// definitions, so the agtk on PATH can be older than this one
				// and lack `memory` entirely.
				AgtkPath: selfPath(env),
				Stale:    stale,
				DryRun:   dryRun,
				Notes:    args,
				Timeout:  timeout,
				Limit:    limit,
			})
			return reportCurateResult(env, jsonOut, stale, res, err)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	cmd.Flags().BoolVar(&stale, "stale", false, "sweep stale notes instead of the candidate backlog")
	cmd.Flags().BoolVar(&check, "check", false, "report what a run would use and start nothing")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what the curator would do, under a grant with no writing tools")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "bound the curation run (default 20m)")
	cmd.Flags().IntVar(&limit, "limit", 0, "point the non-stale backlog job at the oldest N staged candidates by id, "+
		"instead of the whole backlog; only shapes the job description — the model's tool grants are not "+
		"narrowed, so a run that goes beyond N is possible and is not itself an error")
	return cmd
}

// knownNotes rejects a name the store does not hold.
//
// A typo would otherwise scope the run to nothing and cost a full model
// invocation to report that it found nothing to do — and the report would read
// the same as a run that correctly found nothing, which is the reading that
// matters here.
func knownNotes(env *Env, store *memory.Store, names []string) error {
	if len(names) == 0 {
		return nil
	}
	notes, errs := store.LoadNotes()
	if len(errs) > 0 {
		return errs[0]
	}
	known := make(map[string]bool, len(notes))
	for _, n := range notes {
		known[n.Name] = true
	}
	for _, name := range names {
		if !known[name] {
			return fmt.Errorf("memory: no note named %q; `agtk memory stats` lists the store", name)
		}
	}
	return nil
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

// reportCurateCheck prints what a curate run would be given, and starts
// nothing: the provider, the binary, the permission mode, the tool grant and
// the delegation deny list.
func reportCurateCheck(env *Env, jsonOut bool, ready curator.Ready) error {
	if jsonOut {
		return writeJSON(env, memoryCurateCheckJSON{
			Version:         jsonVersion,
			Provider:        ready.Provider,
			Binary:          ready.Binary,
			Mode:            ready.Mode,
			Tools:           ready.Tools,
			DisallowedTools: ready.DisallowedTools,
		})
	}
	fmt.Fprintf(env.Stdout, "provider:  %s\nbinary:    %s\nmode:      %s\ntools:     %s\ndeny:      %s\n",
		ready.Provider, ready.Binary, describeMode(ready.Mode), describeTools(ready.Tools), describeDenyList(ready.DisallowedTools))
	return nil
}

// reportCurateResult prints what a curate run produced and decides what the
// command returns. A verification failure comes back from curator.Run beside
// a populated Result: Text is the curator's own account of a run the store
// then contradicted, so it is printed — or carried in the JSON report — before
// the error that names the mismatch is returned. Every other error returns
// before Text is ever set, so its emptiness is what tells the two apart.
//
// The candidates agtk cleared after verification follow the curator's report,
// one per line, or ride in the JSON report's `cleared` list. They are printed
// because agtk, not the run, removed them: the curator's account does not
// mention them, and a file vanishing from candidates/ with nothing on record
// saying who took it reads as a lost finding.
//
// The candidates that did not parse come last, one per line, or ride in the
// JSON report's `unreadable` list. The curator cannot repair one, so each is
// a finding waiting on a person, and curator.Run fails the run while any
// remain — the error it returns is what makes the command exit non-zero.
//
// The hit shards agtk compacted follow, as one count, then each shard that
// could not be read and was left in place; the JSON report carries them as
// `compactedShards` and `unreadableShards`. Both change files under the store
// that the session commits, and the curator's account mentions neither.
func reportCurateResult(env *Env, jsonOut, stale bool, res curator.Result, runErr error) error {
	if runErr != nil && res.Text == "" {
		return runErr
	}

	if jsonOut {
		cleared := res.Cleared
		if cleared == nil {
			cleared = []string{}
		}
		if err := writeJSON(env, memoryCurateResultJSON{
			memoryCurateJSON: memoryCurateJSON{
				Version: jsonVersion,
				Stale:   stale,
				Failed:  runErr != nil || res.IsError,
				Model:   res.Model,
				CostUSD: res.CostUSD,
				Report:  res.Text,
			},
			Cleared:          cleared,
			Unreadable:       unreadableJSONEntries(res.Unreadable),
			CompactedShards:  len(res.Compaction.Folded),
			UnreadableShards: unreadableJSONEntries(res.Compaction.Skipped),
		}); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(env.Stdout, res.Text)
		for _, id := range res.Cleared {
			fmt.Fprintf(env.Stdout, "cleared: %s (resolved, left in candidates/ by the run)\n", id)
		}
		for _, e := range res.Unreadable {
			fmt.Fprintf(env.Stdout, "unreadable: %v\n", e)
		}
		if n := len(res.Compaction.Folded); n > 0 {
			fmt.Fprintf(env.Stdout, "hits: compacted %d shard(s) into %s\n", n, memory.CompactedHitsFile)
		}
		for _, e := range res.Compaction.Skipped {
			fmt.Fprintf(env.Stdout, "hits: unreadable shard left in place: %v\n", e)
		}
	}
	if runErr != nil {
		return runErr
	}
	if res.IsError {
		return errMemoryCurate
	}
	return nil
}

// memoryCurateResultJSON is `memory curate --json`'s output: memoryCurateJSON's
// fields, flattened into the same object, the candidates agtk cleared and the
// candidates that did not parse. Cleared is never null, so a script iterates it
// without a nil check, and an empty list is the ordinary case of a run that
// deleted what it resolved. Unreadable is never null for the same reason, and
// its entries read exactly as `memory candidates --json` reports them.
// CompactedShards counts the hit shards folded into the compacted hit record,
// and UnreadableShards, never null, names each shard left in place unread.
type memoryCurateResultJSON struct {
	memoryCurateJSON
	Cleared          []string `json:"cleared"`
	Unreadable       []string `json:"unreadable"`
	CompactedShards  int      `json:"compactedShards"`
	UnreadableShards []string `json:"unreadableShards"`
}

// memoryAgent reads `memory.agent` from the entry manifest, the same way and
// from the same file memoryStore reads `memory.root`.
func memoryAgent(env *Env) (string, error) {
	m, err := stack.ParseEntryManifestFile(memoryManifestPath(env))
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
	return m.MemoryAgent(), nil
}

// ===== shared helpers =====

// selectNotes filters notes by name, resolving each one independently so a
// name that fails to resolve does not stop another name in the same call
// from being selected. names with no note in the store are told apart from
// names whose file is there but fails to parse, since only the store, not
// the caller, knows which of the two happened. An empty names selects every
// note, exactly as it always has.
func selectNotes(store *memory.Store, notes []*memory.Note, parseErrs []error, names []string) (selected []*memory.Note, failed []string) {
	if len(names) == 0 {
		return notes, nil
	}
	for _, name := range names {
		l := curator.ResolveNote(store, notes, parseErrs, name)
		switch {
		case l.Note != nil:
			selected = append(selected, l.Note)
		case l.Err != nil:
			failed = append(failed, fmt.Sprintf("%s: note exists but does not parse: %v", name, l.Err))
		default:
			failed = append(failed, fmt.Sprintf("no note named %q", name))
		}
	}
	return selected, failed
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

// describeTools renders the tool grant for a reader. An empty grant is a
// decision, not a gap: a provider with no per-tool vocabulary is confined by
// its sandbox mode instead, and a blank field invites the reading that nothing
// confines the run at all.
func describeTools(tools []string) string {
	if len(tools) == 0 {
		return "none — this provider has no per-tool grant; the mode is the whole confinement"
	}
	return strings.Join(tools, ", ")
}

// describeMode renders the permission mode for a reader. An empty mode is a
// decision, not a gap: passing none leaves the grant as the whole of the run's
// permission, and printing a blank field invites the opposite reading.
func describeMode(mode string) string {
	if mode == "" {
		return "none passed — the grant is the whole permission"
	}
	return mode
}

// describeDenyList renders the delegation deny list for a reader. Empty means
// this provider has no vocabulary for denying a tool at all, which Check
// refuses to report ready for on anything but a dry run — so an empty field
// here means the sandbox mode is what closes delegation instead, not that
// nothing does.
func describeDenyList(tools []string) string {
	if len(tools) == 0 {
		return "none — this provider cannot deny a tool; the mode is what closes delegation instead"
	}
	return strings.Join(tools, ", ")
}
