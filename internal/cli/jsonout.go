package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
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
	Version      int            `json:"version"`
	Notes        int            `json:"notes"`
	ByKind       map[string]int `json:"by_kind"`
	ByConfidence map[string]int `json:"by_confidence"`
	Anchors      int            `json:"anchors"`
	AnchoredFile int            `json:"anchored_files"`
	Stale        int            `json:"stale"`
	Candidates   int            `json:"candidates"`
	Hits         int            `json:"hits"`
	NotesHit     int            `json:"notes_hit"`
	HitRate      float64        `json:"hit_rate"`
	FirstHit     string         `json:"first_hit,omitempty"`
	LastHit      string         `json:"last_hit,omitempty"`
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

func statsJSON(st memory.Stats) memoryStatsJSON {
	out := memoryStatsJSON{
		Version:      jsonVersion,
		Notes:        st.Notes,
		ByKind:       map[string]int{},
		ByConfidence: map[string]int{},
		Anchors:      st.Anchors,
		AnchoredFile: st.AnchoredFile,
		Stale:        st.Stale,
		Candidates:   st.Candidates,
		Hits:         st.Hits,
		NotesHit:     st.NotesHit,
		HitRate:      st.HitRate,
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
