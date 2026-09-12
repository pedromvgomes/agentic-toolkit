package tests

import (
	"fmt"
	"io/fs"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourceref"
)

// fakeProvider serves predetermined fs.FS + ResolvedRef pairs keyed by
// (URL, Ref). Use the explicit Ref form to differentiate refs of the
// same URL; an empty Ref entry serves as the default-branch resolution
// (and the resolver records its returned Ref in the lockfile).
type fakeProvider struct {
	entries map[fakeKey]fakeEntry
}

type fakeKey struct{ URL, Ref string }

type fakeEntry struct {
	FS  fs.FS
	Ref string // resolved ref (echoes input when input is non-empty)
	SHA string
	Err error
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{entries: map[fakeKey]fakeEntry{}}
}

// register adds an entry. ref is the consumer-facing ref the resolver
// will look up by (use "" to register a default-branch entry). Each entry
// gets a SHA = "<URL>@<resolvedRef>" by default for stable assertions.
func (p *fakeProvider) register(url, ref string, fsys fs.FS) *fakeProvider {
	resolved := ref
	if resolved == "" {
		resolved = "main"
	}
	p.entries[fakeKey{URL: url, Ref: ref}] = fakeEntry{
		FS:  fsys,
		Ref: resolved,
		SHA: fmt.Sprintf("sha:%s@%s", url, resolved),
	}
	return p
}

func (p *fakeProvider) Provide(s sourceref.Source) (fs.FS, resolver.ResolvedRef, error) {
	e, ok := p.entries[fakeKey{URL: s.URL, Ref: s.Ref}]
	if !ok {
		return nil, resolver.ResolvedRef{}, fmt.Errorf("fakeProvider: no entry for %q@%q", s.URL, s.Ref)
	}
	if e.Err != nil {
		return nil, resolver.ResolvedRef{}, e.Err
	}
	return e.FS, resolver.ResolvedRef{Ref: e.Ref, SHA: e.SHA}, nil
}

// makeMapFS is a thin wrapper to keep test setup terse.
func makeMapFS(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for p, body := range files {
		out[p] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// ===== reusable file bodies =====

func validSkillBody(description string) string {
	return "---\ndescription: " + description + "\n---\n\nbody\n"
}

func validAgentBody(description string) string {
	return "---\ndescription: " + description + "\n---\n\nbody\n"
}

func validRuleBody(description string) string {
	return "---\ndescription: " + description + "\nalways: true\n---\n\nbody\n"
}

func validInstructionBody(description string) string {
	return "---\ndescription: " + description + "\n---\n\nbody\n"
}

// stackBody renders a stack manifest from the given category-keyed entry
// lists. extends entries go under `extends:`. Use empty values to omit
// fields.
func stackBody(extends []string, entries map[string][]string) string {
	out := ""
	if len(extends) > 0 {
		out += "extends:\n"
		for _, e := range extends {
			out += "  - " + e + "\n"
		}
	}
	for _, key := range []string{"skills", "agents", "rules", "instructions", "commands", "hooks", "mcp", "settings"} {
		list, ok := entries[key]
		if !ok || len(list) == 0 {
			continue
		}
		out += key + ":\n"
		for _, e := range list {
			out += "  - " + e + "\n"
		}
	}
	return out
}
