package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// jsonVersion is the schema version emitted by every --json output of
// the agtk CLI. Increment when shapes change in a non-additive way.
const jsonVersion = 1

// writeJSON pretty-prints v to env.Stdout with a trailing newline.
func writeJSON(env *Env, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	if _, err := fmt.Fprintln(env.Stdout, string(raw)); err != nil {
		return err
	}
	return nil
}

// ===== plan =====

type planJSON struct {
	Version     int          `json:"version"`
	Sources     []sourceJSON `json:"sources"`
	Definitions []defJSON    `json:"definitions"`
	Diagnostics []diagJSON   `json:"diagnostics"`
}

type sourceJSON struct {
	URL  string `json:"url"`
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
	Kind string `json:"kind"`
}

type defJSON struct {
	Category  string `json:"category"`
	Name      string `json:"name"`
	Stack     string `json:"stack"`
	SourceURL string `json:"source_url"`
	SourceRef string `json:"source_ref"`
	EntryPath string `json:"entry_path"`
}

type diagJSON struct {
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Category  string `json:"category,omitempty"`
	Name      string `json:"name,omitempty"`
	SourceURL string `json:"source_url,omitempty"`
	StackName string `json:"stack_name,omitempty"`
}

func planToJSON(p *resolver.Plan) planJSON {
	out := planJSON{Version: jsonVersion}
	for _, s := range p.Sources {
		out.Sources = append(out.Sources, sourceJSON{
			URL: s.URL, Ref: s.Ref, SHA: s.SHA, Kind: s.Kind.String(),
		})
	}
	for _, d := range p.Definitions {
		out.Definitions = append(out.Definitions, defJSON{
			Category:  string(d.Category),
			Name:      d.Name,
			Stack:     d.StackName,
			SourceURL: d.SourceURL,
			SourceRef: d.SourceRef,
			EntryPath: d.EntryPath,
		})
	}
	for _, d := range p.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, diagJSON{
			Kind:      d.Kind.String(),
			Message:   d.Message,
			Category:  string(d.Category),
			Name:      d.Name,
			SourceURL: d.SourceURL,
			StackName: d.StackName,
		})
	}
	return out
}

// ===== lock =====

type lockJSON struct {
	Version  int            `json:"version"`
	Action   string         `json:"action"` // wrote | unchanged | drift
	Path     string         `json:"path"`
	Lockfile *lockfileJSONT `json:"lockfile,omitempty"`
	Drift    string         `json:"drift,omitempty"`
}

type lockfileJSONT struct {
	Version int          `json:"version"`
	Sources []sourceJSON `json:"sources"`
}

func lockfileJSON(lf *lockfile.Lockfile) *lockfileJSONT {
	out := &lockfileJSONT{Version: lf.Version}
	for _, s := range lf.Sources {
		out.Sources = append(out.Sources, sourceJSON{
			URL: s.URL, Ref: s.Ref, SHA: s.SHA,
		})
	}
	return out
}

func writeLockJSON(env *Env, v lockJSON) error {
	return writeJSON(env, v)
}

// ===== status =====

type statusJSON struct {
	Version int       `json:"version"`
	Clean   bool      `json:"clean"`
	Drift   driftJSON `json:"drift"`
}

type driftJSON struct {
	ConfigVsLockfile []string `json:"config_vs_lockfile"`
	LockfileVsCache  []string `json:"lockfile_vs_cache"`
	Render           []string `json:"render"`
}

// ===== memory =====

type memoryIndexJSON struct {
	Version int    `json:"version"`
	Path    string `json:"path"`
	Notes   int    `json:"notes"`
	Changed bool   `json:"changed"`
}

type memoryCurateCheckJSON struct {
	Version  int      `json:"version"`
	Provider string   `json:"provider"`
	Binary   string   `json:"binary"`
	Mode     string   `json:"mode"`
	Tools    []string `json:"tools"`
}

type memoryCurateJSON struct {
	Version int  `json:"version"`
	Stale   bool `json:"stale"`
	// Failed is the curator's own verdict on its turn, not an error from
	// running it: the report is populated either way and carries the reason.
	Failed  bool    `json:"failed"`
	Model   string  `json:"model,omitempty"`
	CostUSD float64 `json:"cost_usd,omitempty"`
	Report  string  `json:"report"`
}

type memoryCandidatesJSON struct {
	Version int    `json:"version"`
	Path    string `json:"path"`
	// Staged counts every file in the directory, readable or not, the same
	// way `stats` counts them. A curator comparing it against len(candidates)
	// can tell "the backlog is empty" from "I could not read three of them".
	Staged int `json:"staged"`
	// Candidates is never null: a curator reading this iterates it, and an
	// empty backlog is the ordinary case rather than an absent field.
	Candidates []memoryCandidateJSON `json:"candidates"`
	// Unreadable is never null for the same reason. A candidate that does not
	// parse is still a finding somebody staged, and dropping it silently is
	// the loss this command exists to surface.
	Unreadable []string `json:"unreadable"`
}

func unreadableJSONEntries(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

type memoryCandidateJSON struct {
	Name  string   `json:"name"`
	File  string   `json:"file"`
	About string   `json:"about"`
	Saw   []string `json:"saw,omitempty"`
	// Targets and Verdict travel together: a verdict is a statement about the
	// note named here, never about the finding itself.
	Targets string   `json:"targets,omitempty"`
	Verdict string   `json:"verdict,omitempty"`
	Body    string   `json:"body"`
	Issues  []string `json:"issues,omitempty"`
}

func candidateJSONEntries(env *Env, candidates []*memory.Candidate) []memoryCandidateJSON {
	out := make([]memoryCandidateJSON, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, memoryCandidateJSON{
			Name:    c.Stem(),
			File:    relToWork(env, c.File),
			About:   c.About,
			Saw:     c.Saw,
			Targets: c.Targets,
			Verdict: string(c.Verdict),
			Body:    strings.TrimSpace(c.Body),
			Issues:  c.CandidateIssues(),
		})
	}
	return out
}

type memoryAnchorJSON struct {
	Version int                    `json:"version"`
	Notes   []memoryAnchorNoteJSON `json:"notes"`
}

type memoryAnchorNoteJSON struct {
	Name    string                `json:"name"`
	Changed bool                  `json:"changed"`
	Anchors []memoryAnchorRowJSON `json:"anchors"`
	Missing []string              `json:"missing,omitempty"`
}

type memoryAnchorRowJSON struct {
	Path string `json:"path"`
	Glob bool   `json:"glob"`
	Blob string `json:"blob,omitempty"`
	// Missing marks an anchor whose path resolved to nothing; Blob and
	// Matches then carry the values kept from the previous stamp.
	Missing bool `json:"missing,omitempty"`
	Matches int  `json:"matches,omitempty"`
}

type memoryAuditJSON struct {
	Version int                   `json:"version"`
	Notes   int                   `json:"notes"`
	Stale   []memoryAuditNoteJSON `json:"stale"`
}

type memoryAuditNoteJSON struct {
	Name   string            `json:"name"`
	Drifts []memoryDriftJSON `json:"drifts"`
}

type memoryDriftJSON struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Pattern string `json:"pattern,omitempty"`
	// Was and Now are blob hashes and nothing else; a reason an anchor
	// could not be evaluated goes in Detail, so consumers never have to
	// tell a hash from an error message.
	Was    string `json:"was,omitempty"`
	Now    string `json:"now,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type memoryLintJSON struct {
	Version int               `json:"version"`
	Notes   int               `json:"notes"`
	Issues  []memoryIssueJSON `json:"issues"`
}

type memoryIssueJSON struct {
	File    string `json:"file,omitempty"`
	Note    string `json:"note,omitempty"`
	Message string `json:"message"`
}

type memoryShowJSON struct {
	Version     int      `json:"version"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Confidence  string   `json:"confidence"`
	Description string   `json:"description"`
	Stale       bool     `json:"stale"`
	Anchors     []string `json:"anchors"`
	Body        string   `json:"body"`
}

type memoryStatsJSON struct {
	Version int `json:"version"`
	// Root and ProjectRoot are reported so an agent does not have to
	// re-derive store resolution from the manifest: `memory.root` set in a
	// stack reached through extends: is deliberately ignored, so a grep over
	// YAML gets a different answer than agtk does.
	//
	// Root is the store; ProjectRoot is what anchor paths resolve against.
	// They are normally different directories — `memory.root: .` is the case
	// that collapses them — so an anchor resolves against ProjectRoot either
	// way. --source changes where each is derived from, not whether they
	// differ. Both go through relToWork, which falls back to an absolute
	// path when the target is not below WorkDir.
	Root         string         `json:"root"`
	ProjectRoot  string         `json:"project_root"`
	Notes        int            `json:"notes"`
	ByKind       map[string]int `json:"by_kind"`
	ByConfidence map[string]int `json:"by_confidence"`
	Anchors      int            `json:"anchors"`
	AnchoredFile int            `json:"anchored_files"`
	Stale        int            `json:"stale"`
	Candidates   int            `json:"candidates"`
	// IndexBytes is the tax: what an explorer loads before it has decided any
	// note is relevant. Reported beside HitRate so a consumer reading this
	// has both halves of the ledger without a second call.
	IndexBytes int64 `json:"index_bytes"`
	Hits       int   `json:"hits"`
	NotesHit   int   `json:"notes_hit"`
	// HitRate covers this checkout alone. The hits log is gitignored, so a
	// fresh clone reports zero reads over a store that is heavily used
	// elsewhere, and a consumer that treats this as a property of the store
	// reads that as evidence to prune.
	HitRate float64 `json:"hit_rate"`
	// Cold names the notes with no recorded hit, sorted. It is the actionable
	// form of a low HitRate, and carries the same per-checkout caveat.
	Cold     []string `json:"cold"`
	FirstHit string   `json:"first_hit,omitempty"`
	LastHit  string   `json:"last_hit,omitempty"`
}

func anchorJSONNotes(results []memory.StampResult) []memoryAnchorNoteJSON {
	out := make([]memoryAnchorNoteJSON, 0, len(results))
	for _, r := range results {
		rows := make([]memoryAnchorRowJSON, 0, len(r.Anchors))
		for _, a := range r.Anchors {
			rows = append(rows, memoryAnchorRowJSON{
				Path: a.Path, Glob: a.IsGlob, Blob: a.Blob, Missing: a.Missing, Matches: a.Matches,
			})
		}
		out = append(out, memoryAnchorNoteJSON{Name: r.Name, Changed: r.Changed, Anchors: rows, Missing: r.Missing})
	}
	return out
}

func auditJSONNotes(audits []memory.NoteAudit) []memoryAuditNoteJSON {
	out := make([]memoryAuditNoteJSON, 0, len(audits))
	for _, a := range audits {
		drifts := make([]memoryDriftJSON, 0, len(a.Drifts))
		for _, d := range a.Drifts {
			drifts = append(drifts, memoryDriftJSON{
				Kind: string(d.Kind), Path: d.Path, Pattern: d.Pattern,
				Was: d.Was, Now: d.Now, Detail: d.Detail,
			})
		}
		out = append(out, memoryAuditNoteJSON{Name: a.Name, Drifts: drifts})
	}
	return out
}

func lintJSONIssues(env *Env, issues []memory.Issue) []memoryIssueJSON {
	out := make([]memoryIssueJSON, 0, len(issues))
	for _, i := range issues {
		file := ""
		if i.File != "" {
			file = relToWork(env, i.File)
		}
		out = append(out, memoryIssueJSON{File: file, Note: i.Note, Message: i.Message})
	}
	return out
}

func statsJSON(env *Env, store *memory.Store, st memory.Stats) memoryStatsJSON {
	out := memoryStatsJSON{
		Version:      jsonVersion,
		Root:         relToWork(env, store.Root),
		ProjectRoot:  relToWork(env, store.ProjectRoot),
		Notes:        st.Notes,
		ByKind:       map[string]int{},
		ByConfidence: map[string]int{},
		Anchors:      st.Anchors,
		AnchoredFile: st.AnchoredFile,
		Stale:        st.Stale,
		Candidates:   st.Candidates,
		IndexBytes:   st.IndexBytes,
		Hits:         st.Hits,
		NotesHit:     st.NotesHit,
		HitRate:      st.HitRate,
		Cold:         st.Cold,
	}
	if out.Cold == nil {
		// A JSON consumer branching on this must not have to distinguish null
		// from empty to learn that every note has been read.
		out.Cold = []string{}
	}
	for k, n := range st.ByKind {
		out.ByKind[string(k)] = n
	}
	for c, n := range st.ByConfidence {
		out.ByConfidence[string(c)] = n
	}
	if !st.FirstHit.IsZero() {
		out.FirstHit = st.FirstHit.Format(time.RFC3339)
		out.LastHit = st.LastHit.Format(time.RFC3339)
	}
	return out
}

// ===== code review =====

type reviewOutJSON struct {
	Version     int             `json:"version"`
	Manifest    string          `json:"manifest"`
	Range       string          `json:"range"`
	Panel       string          `json:"panel"`
	Available   bool            `json:"available"`
	Reason      string          `json:"reason,omitempty"`
	Partial     bool            `json:"partial"`
	Findings    []findingJSON   `json:"findings"`
	Good        []string        `json:"good,omitempty"`
	Runs        []runReportJSON `json:"runs"`
	Skipped     []skippedJSON   `json:"skipped,omitempty"`
	Conventions []string        `json:"conventions,omitempty"`
	Discarded   []string        `json:"discarded_judge_ids,omitempty"`
	Reattached  []string        `json:"reattached_injection_ids,omitempty"`
	Dropped     int             `json:"dropped_by_validator"`
	CostUSD     float64         `json:"cost_usd"`
}

type findingJSON struct {
	ID            string `json:"id"`
	Fingerprint   string `json:"fingerprint"`
	Reviewer      string `json:"reviewer"`
	Path          string `json:"path"`
	StartLine     *int   `json:"start_line"`
	EndLine       *int   `json:"end_line"`
	Category      string `json:"category"`
	Severity      string `json:"severity"`
	Confidence    string `json:"confidence,omitempty"`
	Issue         string `json:"issue"`
	Evidence      string `json:"evidence"`
	Suggestion    string `json:"suggestion,omitempty"`
	Corroboration int    `json:"corroboration"`
	Verdict       string `json:"verdict,omitempty"`
}

type runReportJSON struct {
	Label     string  `json:"label"`
	Role      string  `json:"role"`
	Provider  string  `json:"provider"`
	Model     string  `json:"model,omitempty"`
	Available bool    `json:"available"`
	Reason    string  `json:"reason,omitempty"`
	Findings  int     `json:"findings"`
	CostUSD   float64 `json:"cost_usd"`
}

type skippedJSON struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// reviewJSON renders a finished review.
//
// The fingerprint is emitted alongside the id because they answer different
// questions: the id is what the judge was asked about in this run and means
// nothing outside it, and the fingerprint is what identifies the finding on a
// later review of the same change.
func reviewJSON(r *reviewrun.Review) reviewOutJSON {
	out := reviewOutJSON{
		Version:     jsonVersion,
		Manifest:    r.Manifest,
		Range:       r.Range,
		Panel:       r.Panel,
		Available:   r.Available,
		Reason:      r.Reason,
		Partial:     r.Partial(),
		Findings:    []findingJSON{},
		Runs:        []runReportJSON{},
		Good:        r.Good,
		Conventions: r.Conventions,
		Discarded:   r.DiscardedIDs,
		Reattached:  r.ReattachedIDs,
		Dropped:     r.DroppedByValidator,
		CostUSD:     r.CostUSD,
	}
	for _, f := range r.Findings {
		row := findingJSON{
			ID: f.ID, Fingerprint: f.Fingerprint(), Reviewer: f.Reviewer,
			Path: f.Path, StartLine: f.StartLine, EndLine: f.EndLine,
			Category: f.Category, Severity: string(f.Severity), Confidence: f.Confidence,
			Issue: f.Issue, Evidence: f.Evidence, Suggestion: f.Suggestion,
			Corroboration: f.Corroboration,
		}
		if f.Verdict != nil {
			row.Verdict = f.Verdict.Verdict
		}
		out.Findings = append(out.Findings, row)
	}
	for _, run := range r.Reports {
		out.Runs = append(out.Runs, runReportJSON{
			Label: run.Label, Role: run.Role, Provider: run.Provider, Model: run.Model,
			Available: run.Report.Available, Reason: run.Report.Reason,
			Findings: run.Report.Count(), CostUSD: run.CostUSD,
		})
	}
	for _, s := range r.Skipped {
		out.Skipped = append(out.Skipped, skippedJSON{Path: s.Path, Reason: s.Reason})
	}
	return out
}

type reviewPlanJSON struct {
	Version     int              `json:"version"`
	Manifest    string           `json:"manifest"`
	Range       string           `json:"range"`
	Panel       string           `json:"panel"`
	Root        string           `json:"review_root"`
	WorkDir     string           `json:"review_workdir"`
	Conventions []string         `json:"conventions,omitempty"`
	Runs        []plannedRunJSON `json:"runs"`
}

type plannedRunJSON struct {
	Label    string `json:"label"`
	Role     string `json:"role"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	Prompt   string `json:"prompt"`
}

// planRunJSON renders what a review would do, prompts included: a preview that
// withheld them would not be a preview of what gets sent.
func planRunJSON(p *reviewrun.Plan) reviewPlanJSON {
	out := reviewPlanJSON{
		Version:     jsonVersion,
		Manifest:    p.Manifest,
		Range:       p.Range,
		Panel:       p.Panel,
		Root:        p.Material.Root.Code,
		WorkDir:     p.Material.Root.Work,
		Conventions: p.Material.ConventionPaths(),
		Runs:        []plannedRunJSON{},
	}
	for _, r := range p.Runs {
		out.Runs = append(out.Runs, plannedRunJSON{
			Label: r.Label, Role: r.Role, Provider: r.Provider, Model: r.Model, Prompt: r.Prompt,
		})
	}
	return out
}
