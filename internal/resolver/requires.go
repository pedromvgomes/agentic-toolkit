package resolver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// maxRequirementRounds bounds the fixpoint below. Requirements are
// transitive — a definition pulled in may declare its own — so the pass
// repeats until nothing new appears. The bound is a backstop against a
// pathological graph, not the termination argument: the overlay only ever
// grows and is bounded by the catalog, so a normal graph settles in a round
// or two.
const maxRequirementRounds = 16

// pullInRequirements adds every definition that a resolved definition
// declares it needs, and says so.
//
// `requires:` is a claim by a definition's author that it does not work
// alone — a skill that dispatches to a subagent, an instruction that names
// one. Without this, a stack could list the skill and not the agent, and
// nothing would notice until the consumer's session dispatched to something
// that was never installed. That is not hypothetical: it is how
// `wrap-session` shipped broken to every consumer.
//
// Pulling in rather than merely reporting mirrors what the resolver already
// does for sources: an entry that needs a source the stack does not name
// gets it, locked like any other, with an informational diagnostic. A
// requirement is the same relationship one level down.
//
// Nothing here is fatal. A requirement that cannot be resolved is somebody
// else's metadata problem — often in a definition the consumer does not own
// — and hard-failing their sync over it would be worse than rendering
// without it, which is what happens today anyway.
func (s *traversalState) pullInRequirements() {
	// Requirements already reported, so a definition required by three
	// others is announced once rather than three times.
	seen := map[defKey]bool{}

	for round := 0; round < maxRequirementRounds; round++ {
		added := 0
		for _, w := range s.overlaySnapshot() {
			for _, req := range w.Definition.GetCommon().Requires {
				cat, name, ok := parseRequirement(req)
				if !ok {
					s.reportRequirement(DiagUnresolvedRequirement, w, req,
						fmt.Sprintf("%q is not in 'category/name' form", req))
					continue
				}
				key := defKey{Category: cat, Name: name}
				if _, exists := s.overlay[key]; exists {
					continue
				}
				if seen[key] {
					continue
				}
				seen[key] = true

				pulled, err := s.resolveBare(
					stack.EntryRef{Kind: stack.RefBare, Name: name, Raw: req},
					cat, w.root, w.ctx,
				)
				if err != nil {
					s.reportRequirement(DiagUnresolvedRequirement, w, req, err.Error())
					continue
				}
				pulled.root, pulled.ctx = w.root, w.ctx
				s.overlay[key] = *pulled
				added++
				s.reportRequirement(DiagPulledRequirement, w, req, "")
			}
		}
		if added == 0 {
			return
		}
	}
}

// overlaySnapshot is the overlay in a stable order, so a round's pulls do
// not depend on map iteration and two runs of the same stack produce the
// same diagnostics in the same sequence.
func (s *traversalState) overlaySnapshot() []walkedDef {
	out := make([]walkedDef, 0, len(s.overlay))
	for _, w := range s.overlay {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// reportRequirement records one diagnostic about a requirement of w.
func (s *traversalState) reportRequirement(kind DiagnosticKind, w walkedDef, req, detail string) {
	msg := fmt.Sprintf("%s/%s requires %s, which no stack lists; pulling it in",
		w.Category.CategoryDir(), w.Name, req)
	if kind == DiagUnresolvedRequirement {
		msg = fmt.Sprintf("%s/%s requires %s, which could not be resolved: %s",
			w.Category.CategoryDir(), w.Name, req, detail)
	}
	s.diags = append(s.diags, Diagnostic{
		Kind:      kind,
		Message:   msg,
		Category:  w.Category,
		Name:      w.Name,
		SourceURL: w.SourceURL,
		StackName: w.StackName,
	})
}

// parseRequirement splits a "category/name" cross-reference.
//
// The left half is the category's DIRECTORY name — "skills", not "skill" —
// because that is the form every `requires:` in the catalog is written in
// and the form the schema documents. Matching against CategoryDir rather
// than trimming an "s" also keeps `mcp`, which is not a plural, correct.
func parseRequirement(req string) (definitions.Category, string, bool) {
	dir, name, ok := strings.Cut(strings.TrimSpace(req), "/")
	if !ok || dir == "" || name == "" {
		return "", "", false
	}
	for _, cat := range definitions.AllCategories {
		if cat.CategoryDir() == dir {
			return cat, name, true
		}
	}
	return "", "", false
}
