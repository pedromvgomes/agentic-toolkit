package tests

import (
	"fmt"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourceref"
	"github.com/pedromvgomes/agentic-toolkit/internal/stack"
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

func validCommandBody(description string) string {
	return "---\ndescription: " + description + "\n---\n\nbody\n"
}

func validHookBody(name string) string {
	return "name: " + name + "\ndescription: A hook.\nevent: PreToolUse\nhandler:\n  type: command\n  command: \"true\"\n"
}

func validMCPBody(name string) string {
	return "name: " + name + "\ndescription: An MCP server.\ntransport: stdio\ncommand: mcp-server\n"
}

// validSettingBody sets one top-level key, so two settings definitions can be
// made to contend for it.
func validSettingBody(name, model string) string {
	return "name: " + name + "\ndescription: A setting.\nvalue:\n  model: " + model + "\n"
}

// entryBody renders an entry manifest composing the given stacks. extra is
// appended verbatim for the fields a test sets on top (root:, context:,
// memory:).
func entryBody(stacks []string, extra string) string {
	if len(stacks) == 0 {
		return "stacks: []\n" + extra
	}
	out := "stacks:\n"
	for _, s := range stacks {
		out += "  - " + s + "\n"
	}
	return out + extra
}

// parseEntry reads the entry manifest out of an in-memory FS, which is where
// tests keep it; the CLI reads the same bytes off disk.
func parseEntry(t *testing.T, fsys fs.FS, pathInFS string) *stack.EntryManifest {
	t.Helper()
	raw, err := fs.ReadFile(fsys, pathInFS)
	if err != nil {
		t.Fatalf("read %s: %v", pathInFS, err)
	}
	m, err := stack.ParseEntryManifestBytes(pathInFS, raw)
	if err != nil {
		t.Fatalf("parse entry manifest: %v", err)
	}
	return m
}

// resolveEntry parses the entry manifest at the conventional path in entryFS
// and resolves it against provider.
func resolveEntry(t *testing.T, entryFS fs.FS, provider *fakeProvider) (*resolver.Plan, error) {
	t.Helper()
	return resolver.Resolve(parseEntry(t, entryFS, entryManifestPath), entryFS, entryManifestPath, provider)
}

// entryManifestPath is where every fixture keeps its entry manifest.
const entryManifestPath = ".agentic-toolkit.yaml"

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
