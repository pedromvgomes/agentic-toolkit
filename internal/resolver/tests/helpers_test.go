package tests

import (
	"fmt"
	"io/fs"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
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
	// Err, if set, is returned instead of a successful Provide.
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

// fail makes Provide return an error for (url, ref).
func (p *fakeProvider) fail(url, ref string, err error) *fakeProvider {
	p.entries[fakeKey{URL: url, Ref: ref}] = fakeEntry{Err: err}
	return p
}

func (p *fakeProvider) Provide(s config.Source) (fs.FS, resolver.ResolvedRef, error) {
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

// validSkill returns a SKILL.md body whose name matches the given name.
// Use as "definitions/skills/<name>/SKILL.md" inside a primary or as
// "SKILL.md" inside an external bundle (frontmatter does not need to
// declare name explicitly — the parser derives it).
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

func validHookBody(description string) string {
	return "description: " + description + "\nevent: PreToolUse\nhandler:\n  type: command\n  command: \"echo\"\n"
}

func validSettingBody(description string) string {
	return "description: " + description + "\nvalue:\n  permissions:\n    deny:\n      - \"Bash(rm -rf:*)\"\n"
}

func validPresetBody(description string, defs ...string) string {
	out := "description: " + description + "\ndefinitions:\n"
	for _, d := range defs {
		out += "  - " + d + "\n"
	}
	return out
}
