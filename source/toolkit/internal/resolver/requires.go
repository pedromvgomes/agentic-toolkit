package resolver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
)

// maxRequirementDepth bounds how far a requirement chain is followed. Each
// round resolves the requirements of the definitions the previous round added,
// so this is a limit on the DEPTH of the chain, not on how many definitions
// end up pulled in.
//
// It is a backstop rather than the termination argument: a definition is
// scanned once, and the overlay only grows, so an ordinary graph settles in a
// round or two. Reaching the bound is reported, because a closure that was cut
// short leaves definitions missing and the render would otherwise look
// complete.
const maxRequirementDepth = 16

// pullInRequirements adds every definition that a resolved definition declares
// it needs, and says so.
//
// `requires:` is a claim by a definition's author that it does not work alone —
// a skill that dispatches to a subagent, an instruction that names one. Without
// this, a stack can list the skill and not the agent, and nothing notices until
// a consumer's session delegates to something that was never installed.
//
// Pulling in rather than merely reporting mirrors what the resolver does for
// sources: an entry that needs a source the stack does not name gets it, locked
// like any other, with an informational diagnostic. A requirement is the same
// relationship one level down.
//
// Nothing here is fatal. A requirement that cannot be resolved is a metadata
// problem in a definition the consumer often does not own, and failing their
// whole resolve over it would be a worse outcome than rendering without it.
func (s *traversalState) pullInRequirements() {
	// Each definition's requirements are read once: only what the previous
	// round added can name something nobody has looked at yet. Re-scanning the
	// whole overlay every round would repeat work and repeat diagnostics.
	frontier := s.overlaySnapshot()

	// Keyed by declaring definition plus the raw requirement: a malformed
	// reference is an authoring mistake at one site, so each site says so once
	// however many rounds run.
	reported := map[string]bool{}

	// A requirement one definition cannot resolve may still be satisfied by
	// another that declares it from a source where it exists, so failures are
	// held until the closure settles and only then reported — and only if the
	// definition is genuinely absent. Reporting on the spot would blame the
	// first requirer for something that ends up present.
	var pending []pendingRequirement

	settled := false
	for depth := 0; depth < maxRequirementDepth; depth++ {
		var next []walkedDef
		for _, w := range frontier {
			added, failed := s.pullOne(w, reported)
			next = append(next, added...)
			pending = append(pending, failed...)
		}
		if len(next) == 0 {
			settled = true
			break
		}
		frontier = next
	}

	s.reportUnsatisfied(pending)
	if settled {
		return
	}

	s.diags = append(s.diags, Diagnostic{
		Kind: DiagUnresolvedRequirement,
		Message: fmt.Sprintf(
			"requirement chain deeper than %d; the closure may be incomplete and some definitions absent",
			maxRequirementDepth),
	})
}

// pendingRequirement is a requirement a definition could not resolve, held
// until the closure settles in case another definition resolves it.
type pendingRequirement struct {
	by     walkedDef
	raw    string
	key    defKey
	detail string
}

// pullOne resolves the requirements w declares, returning what it added and
// what it could not resolve.
func (s *traversalState) pullOne(w walkedDef, reported map[string]bool) (added []walkedDef, failed []pendingRequirement) {

	for _, req := range w.Definition.GetCommon().Requires {
		mark := w.Category.CategoryDir() + "/" + w.Name + "\x00" + req
		cat, name, err := parseRequirement(req)
		if err != nil {
			if !reported[mark] {
				reported[mark] = true
				s.reportRequirement(DiagUnresolvedRequirement, w, req, err.Error())
			}
			continue
		}
		// The requirement's own name is only a first guess at the key: a
		// file-shaped definition's `name:` field wins over its filename, so the
		// definition that comes back may be called something else.
		if _, exists := s.overlay[defKey{Category: cat, Name: name}]; exists {
			continue
		}

		pulled, err := s.resolveBare(
			stack.EntryRef{Kind: stack.RefBare, Name: name, Raw: req},
			cat, w.root, w.ctx,
		)
		if err != nil {
			failed = append(failed, pendingRequirement{
				by:     w,
				raw:    req,
				key:    defKey{Category: cat, Name: name},
				detail: err.Error(),
			})
			continue
		}

		key := defKey{Category: pulled.Category, Name: pulled.Name}
		if _, exists := s.overlay[key]; exists {
			continue
		}
		s.overlay[key] = *pulled
		added = append(added, *pulled)
		s.reportRequirement(DiagPulledRequirement, w, req, "")
	}
	return added, failed
}

// reportUnsatisfied announces the requirements nothing resolved, once each.
//
// A requirement two definitions both declare is one missing definition, not
// two problems, so it is reported by the first declarer in overlay order and
// not repeated.
func (s *traversalState) reportUnsatisfied(pending []pendingRequirement) {
	said := map[defKey]bool{}
	for _, p := range pending {
		if _, exists := s.overlay[p.key]; exists {
			continue
		}
		if said[p.key] {
			continue
		}
		said[p.key] = true
		s.reportRequirement(DiagUnresolvedRequirement, p.by, p.raw, p.detail)
	}
}

// overlaySnapshot is the overlay in a stable order, so which definition is
// scanned first does not depend on map iteration and two runs of the same
// stack produce the same diagnostics in the same sequence.
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

// parseRequirement splits a "category/name" cross-reference and validates the
// name as strictly as a stack entry.
//
// The left half is the category's DIRECTORY name — "skills", not "skill" —
// which is the form the schema documents and every `requires:` in the catalog
// is written in. Matching against CategoryDir rather than trimming an "s" also
// keeps `mcp`, which is not a plural, correct.
//
// The name half goes through stack.ParseEntryRef, the same validation a name
// written in a manifest gets. Without it a requirement is a path fragment that
// reaches path.Join, which CLEANS "..": a definition could name
// "skills/../../elsewhere" and read a file outside the convention root
// entirely. A requirement may only name a definition at the canonical
// <root>/<plural>/<name> location, so anything ParseEntryRef reads as a URL or
// a path is refused too.
func parseRequirement(req string) (definitions.Category, string, error) {
	dir, name, ok := strings.Cut(strings.TrimSpace(req), "/")
	if !ok || dir == "" || name == "" {
		return "", "", fmt.Errorf("%q is not in 'category/name' form", req)
	}

	var cat definitions.Category
	for _, c := range definitions.AllCategories {
		if c.CategoryDir() == dir {
			cat = c
			break
		}
	}
	if cat == "" {
		return "", "", fmt.Errorf("%q names no category", dir)
	}

	ref, err := stack.ParseEntryRef(name, cat)
	if err != nil {
		return "", "", err
	}
	if ref.Kind != stack.RefBare {
		return "", "", fmt.Errorf("%q must name a definition, not a path or URL", name)
	}
	return cat, ref.Name, nil
}
