package cli

import (
	"encoding/json"
	"fmt"

	"github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
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
	Preset    string `json:"preset"`
	SourceURL string `json:"source_url"`
	SourceRef string `json:"source_ref"`
	EntryPath string `json:"entry_path"`
}

type diagJSON struct {
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	Category   string `json:"category,omitempty"`
	Name       string `json:"name,omitempty"`
	SourceURL  string `json:"source_url,omitempty"`
	PresetName string `json:"preset_name,omitempty"`
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
			Preset:    d.PresetName,
			SourceURL: d.SourceURL,
			SourceRef: d.SourceRef,
			EntryPath: d.EntryPath,
		})
	}
	for _, d := range p.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, diagJSON{
			Kind:       d.Kind.String(),
			Message:    d.Message,
			Category:   string(d.Category),
			Name:       d.Name,
			SourceURL:  d.SourceURL,
			PresetName: d.PresetName,
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
