package resolver

import (
	"sort"
)

// srcKey identifies a source by (URL, Ref). Distinct refs of the same URL
// are distinct lockfile entries.
type srcKey struct{ URL, Ref string }

// sourceTable accumulates sources keyed by (URL, Ref). The first add
// wins on Kind: subsequent adds with the same key are no-ops, so a
// SourceStack pin cannot be downgraded by a later SourceDefinition with
// the same key.
type sourceTable struct {
	byKey map[srcKey]PlannedSource
	order []srcKey // insertion order, used for SourceStack ordering
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
	t.order = append(t.order, k)
	return true
}

// orderedSources returns the table in deterministic order:
//
//   - SourceStack entries first, in insertion (visit) order.
//   - SourceDefinition entries next, sorted by (URL, Ref).
func (t *traversalState) orderedSources() []PlannedSource {
	out := make([]PlannedSource, 0, len(t.sources.byKey))
	emitted := make(map[srcKey]bool, len(t.sources.byKey))

	for _, k := range t.sources.order {
		s := t.sources.byKey[k]
		if s.Kind != SourceStack {
			continue
		}
		out = append(out, s)
		emitted[k] = true
	}

	rest := make([]PlannedSource, 0, len(t.sources.byKey))
	for k, s := range t.sources.byKey {
		if emitted[k] {
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
