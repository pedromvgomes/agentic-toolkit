package resolver

import (
	"sort"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
)

// srcKey identifies a source by (URL, Ref). Distinct refs of the same URL
// are distinct lockfile entries.
type srcKey struct{ URL, Ref string }

// sourceTable accumulates sources keyed by (URL, Ref). The first add
// wins on Kind: subsequent adds with the same key are no-ops, so a source
// declared as Primary cannot later be downgraded to Implicit by a preset
// pointing at the same URL+ref.
type sourceTable struct {
	byKey map[srcKey]PlannedSource
}

func newSourceTable() *sourceTable {
	return &sourceTable{byKey: map[srcKey]PlannedSource{}}
}

// add records a source. Returns true if this call inserted a new entry,
// false if the key already existed (and the existing entry was kept).
func (t *sourceTable) add(url, ref, sha string, kind SourceKind) bool {
	k := srcKey{URL: url, Ref: ref}
	if _, ok := t.byKey[k]; ok {
		return false
	}
	t.byKey[k] = PlannedSource{URL: url, Ref: ref, SHA: sha, Kind: kind}
	return true
}

// ordered returns sources in deterministic order: Primary first, then
// Declared in cfg.Externals order, then Implicit sorted by (URL, Ref).
// cfg's primary and declared externals are always present in the result
// even if no preset entry referenced them — they are explicit consumer
// intent.
func (t *sourceTable) ordered(cfg *config.ConsumerConfig) []PlannedSource {
	out := make([]PlannedSource, 0, len(t.byKey))
	seen := make(map[srcKey]bool, len(t.byKey))

	// Primary first.
	primaryK := srcKey{URL: cfg.Source.URL, Ref: cfg.Source.Ref}
	// The actual stored Ref may differ when cfg.Source.Ref was empty —
	// the provider filled in the default-branch name. We resolved using
	// cfg.Source above, so look up by what the table has under that
	// inserted ref.
	for k, s := range t.byKey {
		if s.Kind == SourcePrimary {
			out = append(out, s)
			seen[k] = true
			break
		}
	}
	_ = primaryK

	// Declared externals in config order.
	for _, ext := range cfg.Externals {
		// Find the table entry whose original key matches this external.
		// The key uses the ref the consumer wrote (possibly empty); the
		// stored PlannedSource carries the resolved ref.
		k := srcKey{URL: ext.URL, Ref: ext.Ref}
		if s, ok := t.byKey[k]; ok && !seen[k] {
			out = append(out, s)
			seen[k] = true
		}
	}

	// Implicit (and any unclaimed) entries sorted by (URL, Ref).
	rest := make([]PlannedSource, 0)
	for k, s := range t.byKey {
		if seen[k] {
			continue
		}
		rest = append(rest, s)
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].URL != rest[j].URL {
			return rest[i].URL < rest[j].URL
		}
		return rest[i].Ref < rest[j].Ref
	})
	out = append(out, rest...)
	return out
}
