// schemagen renders the human-facing schema docs from the Go struct
// definitions in internal/definitions, internal/config, and
// internal/lockfile. The structs are the canonical schema; this tool keeps
// the documentation in lockstep with them.
//
// Run via:
//
//	go generate ./...
//
// or directly:
//
//	go run ./tools/schemagen
//
// Output:
//   - definitions/SCHEMA.md         — toolkit-side definitions schema
//   - definitions/CONFIG-SCHEMA.md  — consumer-side config + lockfile schema
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	cfg "github.com/pedromvgomes/agentic-toolkit/internal/config"
	defs "github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	lock "github.com/pedromvgomes/agentic-toolkit/internal/lockfile"
)

// categoryDoc carries the hand-written prose for a category alongside the
// struct sample we reflect over for the field tables.
type categoryDoc struct {
	Category    defs.Category
	Title       string
	PathPattern string
	FileShape   string // "Markdown + YAML frontmatter" or "YAML manifest"
	Intro       string
	Sample      defs.Definition
	Example     string
	Notes       string // optional trailing notes; rendered after the example
}

var categories = []categoryDoc{
	{
		Category:    defs.CategorySkill,
		Title:       "Skills",
		PathPattern: "definitions/skills/<name>/SKILL.md",
		FileShape:   "Markdown + YAML frontmatter",
		Intro:       "A skill is a reusable, self-contained piece of agent capability — a prose body that an adapter renders into the platform's native skill format. Skills live in their own subdirectory so they can ship bundled resources (templates, allowlists, scripts) alongside SKILL.md.",
		Sample:      &defs.Skill{},
		Example: "---\nname: my-skill\ndescription: Short description shown in tooling.\n---\n\n# Skill body\n\nMarkdown content rendered into the platform skill format.\n",
	},
	{
		Category:    defs.CategoryRule,
		Title:       "Rules",
		PathPattern: "definitions/rules/<name>.md",
		FileShape:   "Markdown + YAML frontmatter",
		Intro:       "A rule is a guidance document that platforms attach to the agent's context — either always-on or scoped to matching files. Canonical scoping uses doublestar globs in the `paths` field; adapters translate to per-platform syntax.",
		Sample:      &defs.Rule{},
		Example: "---\ndescription: Use 4-space indentation in TSX files.\npaths:\n  - \"**/*.tsx\"\n---\n\nWhen editing TSX, prefer 4-space indentation.\n",
	},
	{
		Category:    defs.CategoryInstruction,
		Title:       "Instructions",
		PathPattern: "definitions/instructions/<name>.md",
		FileShape:   "Markdown + YAML frontmatter",
		Intro:       "An instruction is prose concatenated into the platform's top-level instruction file (CLAUDE.md, AGENTS.md, copilot-instructions.md) inside an `agtk:start`/`agtk:end` managed region. Instructions carry no scoping or extensions — they are global by design.",
		Sample:      &defs.Instruction{},
		Example: "---\ndescription: Standing instruction concatenated into AGENTS.md.\n---\n\nAlways commit with a Co-Authored-By trailer.\n",
	},
	{
		Category:    defs.CategoryAgent,
		Title:       "Agents (subagents)",
		PathPattern: "definitions/agents/<name>.md",
		FileShape:   "Markdown + YAML frontmatter",
		Intro:       "An agent is a named subagent the parent agent can delegate to. Canonical fields cover the common ground across Claude/Cursor/OpenCode; platform-specific knobs (memory, isolation, sampling, etc.) sit under `extensions`.",
		Sample:      &defs.Agent{},
		Example: "---\ndescription: Code-review subagent.\nmodel: sonnet\ntools: [Read, Grep, Glob]\ncolor: blue\nextensions:\n  claude:\n    permission_mode: plan\n---\n\nYou are a careful code reviewer.\n",
		Notes: "**Tools** uses the Claude tool-name vocabulary (Read, Grep, Bash, Edit, Write, Agent, …). Adapters map to other platforms' tool names where possible and warn-and-skip on unmapped names.",
	},
	{
		Category:    defs.CategoryCommand,
		Title:       "Commands (slash commands)",
		PathPattern: "definitions/commands/<name>.md  (nesting allowed: definitions/commands/<group>/<name>.md)",
		FileShape:   "Markdown + YAML frontmatter",
		Intro:       "A command is a reusable prompt template invoked via slash syntax. Nested directories produce namespaced names: `commands/git/commit.md` is referenced as `commands/git/commit` in cross-references and translated to `/git:commit` (Claude) or `/git/commit` (OpenCode).",
		Sample:      &defs.Command{},
		Example: "---\ndescription: Run the project linter.\nargument_hint: \"[paths...]\"\ntools: [Bash]\n---\n\nRun lint on $ARGUMENTS.\n",
		Notes: "**Body interpolation portability:**\n\n- `$ARGUMENTS` — portable across Claude and OpenCode; Cursor appends free text after the command name.\n- `$1`, `$2` (positional) — OpenCode-only.\n- `` !`cmd` `` (shell) and `@filename` (file injection) — OpenCode-only.\n\nThe parser does not validate body content — these are notes for portable authoring.\n\n**Cursor caveat:** Cursor commands carry no frontmatter at all. The Cursor adapter strips frontmatter and writes the body verbatim, dropping `description`, `tools`, `model`, and `argument_hint` with a doctor-time warning.",
	},
	{
		Category:    defs.CategoryHook,
		Title:       "Hooks",
		PathPattern: "definitions/hooks/<name>.yaml",
		FileShape:   "YAML manifest (no body)",
		Intro:       "A hook attaches a handler to a lifecycle event. Canonical handler types are `command` (shell) and `prompt` (LLM call) — the universal pair across declarative-hook platforms. Claude-specific handler kinds (http, mcp_tool, agent) live under `extensions.claude`.",
		Sample:      &defs.Hook{},
		Example: "name: log-tools\ndescription: Log every tool invocation.\nevent: PreToolUse\nmatcher: \"Bash|Edit|Write\"\nhandler:\n  type: command\n  command: \"echo $TOOL >> /tmp/agtk-tools.log\"\nfail_closed: false\ntimeout: 2000\n",
		Notes: "**Event vocabulary.** The parser does not validate event names; adapters do at render time and skip-with-warn for events they don't support. Three rough sets:\n\n- **Portable** (Claude ∩ Cursor, normalized to Claude's CamelCase): `SessionStart`, `SessionEnd`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `SubagentStart`, `SubagentStop`, `Stop`, `PreCompact`, `UserPromptSubmit`.\n- **Claude-only** (examples): `PostToolBatch`, `Notification`, `TaskCreated`, `PostCompact`, `PermissionRequest`, `PermissionDenied`, `StopFailure`, `InstructionsLoaded`, `ConfigChange`, `FileChanged`, `WorktreeCreate`, `WorktreeRemove`.\n- **Cursor-only** (examples): `beforeShellExecution`, `afterShellExecution`, `beforeReadFile`, `afterFileEdit`, `afterAgentResponse`, `afterAgentThought`.\n\n**OpenCode** does not support declarative hooks; OpenCode adapter generates a TS plugin shim from each hook definition (future slice).",
	},
	{
		Category:    defs.CategoryMCP,
		Title:       "MCP servers",
		PathPattern: "definitions/mcp/<name>.yaml",
		FileShape:   "YAML manifest (no body)",
		Intro:       "An MCP server definition declares one Model Context Protocol server. Canonical transports are `stdio`, `http`, and `sse`; transport-specific fields are mutually exclusive (parser-enforced). Adapters merge the selected MCP definitions into each platform's native config (`.mcp.json`, `.cursor/mcp.json`, `.vscode/mcp.json`, `opencode.json`).",
		Sample:      &defs.MCPServer{},
		Example: "name: filesystem\ndescription: Filesystem MCP server.\ntransport: stdio\ncommand: \"${HOME}/bin/mcp-fs\"\nargs: [\"--root\", \"${WORKDIR:-/tmp}\"]\nenv:\n  LOG_LEVEL: info\n",
		Notes: "**Server scope** (local/project/user) is decided by the consumer at sync time via `.agentic-toolkit/config.yaml`, not by the definition. A single MCP definition can be installed at any scope.\n\n**Variable expansion.** Field values may use `${VAR}` and `${VAR:-default}` (canonical, matches Claude). Adapters translate to per-platform syntax (Cursor `${env:VAR}`, Copilot `${input:VAR}`/`${env:VAR}`).",
	},
}

func main() {
	root, err := moduleRoot()
	if err != nil {
		fail(err)
	}
	for _, doc := range []struct {
		path string
		gen  func() ([]byte, error)
	}{
		{filepath.Join(root, "definitions", "SCHEMA.md"), render},
		{filepath.Join(root, "definitions", "CONFIG-SCHEMA.md"), renderConfig},
	} {
		out, err := doc.gen()
		if err != nil {
			fail(err)
		}
		if err := os.MkdirAll(filepath.Dir(doc.path), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(doc.path, out, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("schemagen: wrote %s (%d bytes)\n", doc.path, len(out))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "schemagen:", err)
	os.Exit(1)
}

// moduleRoot walks up from the current working directory until it finds a
// go.mod file and returns that directory.
func moduleRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", cwd)
		}
		dir = parent
	}
}

// render produces the full SCHEMA.md byte stream.
func render() ([]byte, error) {
	var b bytes.Buffer

	fmt.Fprintln(&b, "# agentic-toolkit definition schema")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "<!-- DO NOT EDIT — generated by tools/schemagen from internal/definitions struct definitions. Run `go generate ./...` to refresh. -->")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Every asset in `definitions/` parses into one of seven category structs. The struct definitions in `internal/definitions/types.go` are the canonical schema; this document is regenerated from them.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Layout overview")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Category | File path | Shape |")
	fmt.Fprintln(&b, "|----------|-----------|-------|")
	for _, c := range categories {
		fmt.Fprintf(&b, "| %s | `%s` | %s |\n", c.Category, c.PathPattern, c.FileShape)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Common fields")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Every definition embeds the same `Common` base. Category is determined by the file's directory under `definitions/`; there is no `type:` field in YAML. `platforms` is an allowlist — omit it when the definition works on every platform; only set it to *narrow* (e.g. a Claude-only skill that depends on Claude features). When set, every populated `extensions.<platform>` block must reference a listed platform.")
	fmt.Fprintln(&b)
	commonDoc := docForType(reflect.TypeOf(defs.Common{}))
	writeFieldTable(&b, commonDoc.Fields)
	fmt.Fprintln(&b)

	for _, c := range categories {
		if err := renderCategory(&b, c); err != nil {
			return nil, err
		}
	}

	renderPresetSection(&b)

	return b.Bytes(), nil
}

// renderPresetSection documents the Preset shape. Presets sit alongside —
// not inside — the seven category enum, so they have their own section
// and a hand-written intro rather than going through categoryDoc.
func renderPresetSection(b *bytes.Buffer) {
	fmt.Fprintln(b, "## Presets")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "**Path:** `definitions/presets/<name>.yaml`  ")
	fmt.Fprintln(b, "**Shape:** YAML manifest (no body)")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "A preset is a named bundle of definition references — toolkit-side metadata that consumers select by name in their config. Presets are not renderable themselves; the resolver expands each preset's `definitions` list against the available sources. Presets are not in the Category enum and do not embed `Common`.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "### Frontmatter fields")
	fmt.Fprintln(b)
	presetDoc := docForType(reflect.TypeOf(defs.Preset{}))
	writeFieldTable(b, presetDoc.Fields)
	fmt.Fprintln(b)
	fmt.Fprintln(b, "### Reference grammar")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Each entry in `definitions` is one of:")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "- **Local**: `<plural-dir>/<name>` — e.g. `skills/challenge`, `commands/git/commit`.")
	fmt.Fprintln(b, "- **External**: `<plural-dir>::<url>[@<ref>]` — e.g. `skills::github.com/anthropics/skills/skills/skill-creator@main`. The optional `<ref>` is anything git accepts (branch, tag, sha); the parser does not classify it — that is the resolver's job.")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "### Example")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "```yaml")
	fmt.Fprintln(b, "description: Default toolkit bundle.")
	fmt.Fprintln(b, "definitions:")
	fmt.Fprintln(b, "  - skills/challenge")
	fmt.Fprintln(b, "  - rules/bare-repos")
	fmt.Fprintln(b, "  - skills::github.com/anthropics/skills/skills/skill-creator@main")
	fmt.Fprintln(b, "```")
	fmt.Fprintln(b)
}

// renderConfig produces CONFIG-SCHEMA.md, the consumer-facing schema for
// .agentic-toolkit/config.yaml and .agentic-toolkit/lock.yaml.
func renderConfig() ([]byte, error) {
	var b bytes.Buffer

	fmt.Fprintln(&b, "# agentic-toolkit consumer config schema")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "<!-- DO NOT EDIT — generated by tools/schemagen from internal/config and internal/lockfile struct definitions. Run `go generate ./...` to refresh. -->")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "A consumer repo opts into the toolkit by committing two files under `.agentic-toolkit/`:")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- `config.yaml` — declares the toolkit source(s), target platforms, and preset bundles to render. Hand-edited.")
	fmt.Fprintln(&b, "- `lock.yaml` — pinned record of what the resolver actually fetched. Resolver-written; commit it.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## ConsumerConfig")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "**Path:** `.agentic-toolkit/config.yaml`")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Fields")
	fmt.Fprintln(&b)
	writeFieldTable(&b, docForType(reflect.TypeOf(cfg.ConsumerConfig{})).Fields)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "### `Source`")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "`source` and each entry in `externals` deserialise into the same `Source` struct. Two YAML forms are accepted:")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "- **Shorthand**: `<url>[@<ref>]` — e.g. `github.com/owner/repo@main`. Empty `<ref>` (or no `@`) means the resolver chooses the default branch.")
	fmt.Fprintln(&b, "- **Mapping**: `{ url: <url>, ref: <ref> }`.")
	fmt.Fprintln(&b)
	writeFieldTable(&b, docForType(reflect.TypeOf(cfg.Source{})).Fields)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "### `presets` semantics")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Presets are applied in declared order. If two entries reference the same definition, the later one wins. Slice-1 only resolves preset names against the **primary** source — external presets are not supported yet. The parser validates name format only; existence is the resolver's responsibility.")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "### Example")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "```yaml")
	fmt.Fprintln(&b, "source: github.com/pedromvgomes/agentic-toolkit@main")
	fmt.Fprintln(&b, "platforms:")
	fmt.Fprintln(&b, "  - claude")
	fmt.Fprintln(&b, "  - cursor")
	fmt.Fprintln(&b, "externals:")
	fmt.Fprintln(&b, "  - github.com/anthropics/skills@main")
	fmt.Fprintln(&b, "presets:")
	fmt.Fprintln(&b, "  - default")
	fmt.Fprintln(&b, "  - bare-repos")
	fmt.Fprintln(&b, "```")
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Lockfile")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "**Path:** `.agentic-toolkit/lock.yaml`")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "The resolver writes the lockfile after a successful sync. It pins every source the run touched (primary + declared externals + sources implied by external preset refs) so subsequent runs can reproduce the exact fetch graph. The current schema version is **%d**.\n", lock.Version)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "### Top-level fields")
	fmt.Fprintln(&b)
	writeFieldTable(&b, docForType(reflect.TypeOf(lock.Lockfile{})).Fields)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "### `sources` entry (`ResolvedSource`)")
	fmt.Fprintln(&b)
	writeFieldTable(&b, docForType(reflect.TypeOf(lock.ResolvedSource{})).Fields)
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "### Example")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "```yaml")
	fmt.Fprintln(&b, "version: 1")
	fmt.Fprintln(&b, "sources:")
	fmt.Fprintln(&b, "  - url: github.com/pedromvgomes/agentic-toolkit")
	fmt.Fprintln(&b, "    ref: main")
	fmt.Fprintln(&b, "    sha: 0123456789abcdef0123456789abcdef01234567")
	fmt.Fprintln(&b, "  - url: github.com/anthropics/skills")
	fmt.Fprintln(&b, "    ref: main")
	fmt.Fprintln(&b, "    sha: fedcba9876543210fedcba9876543210fedcba98")
	fmt.Fprintln(&b, "```")
	fmt.Fprintln(&b)

	return b.Bytes(), nil
}

func renderCategory(b *bytes.Buffer, c categoryDoc) error {
	fmt.Fprintf(b, "## %s\n\n", c.Title)
	fmt.Fprintf(b, "**Path:** `%s`  \n", c.PathPattern)
	fmt.Fprintf(b, "**Shape:** %s\n\n", c.FileShape)
	fmt.Fprintf(b, "%s\n\n", c.Intro)

	t := reflect.TypeOf(c.Sample).Elem()
	doc := docForType(t)
	specific := filterOutCommon(doc.Fields)
	if len(specific) > 0 {
		fmt.Fprintln(b, "### Frontmatter fields beyond Common")
		fmt.Fprintln(b)
		writeFieldTable(b, specific)
		fmt.Fprintln(b)
	}

	subs := collectSubStructs(t)
	for _, s := range subs {
		fmt.Fprintf(b, "### %s\n\n", s.Title)
		writeFieldTable(b, s.Fields)
		fmt.Fprintln(b)
	}

	fmt.Fprintln(b, "### Example")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "```yaml")
	fmt.Fprint(b, c.Example)
	if !strings.HasSuffix(c.Example, "\n") {
		fmt.Fprintln(b)
	}
	fmt.Fprintln(b, "```")
	fmt.Fprintln(b)

	if c.Notes != "" {
		fmt.Fprintln(b, "### Notes")
		fmt.Fprintln(b)
		fmt.Fprintln(b, c.Notes)
		fmt.Fprintln(b)
	}

	return nil
}

// fieldDoc captures one field's reflected metadata.
type fieldDoc struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

// structDoc bundles a name + its fields. Used for both the top-level
// category struct (minus Common) and any nested sub-structs surfaced as
// separate sections (extensions, OAuthConfig, HookHandler, …).
type structDoc struct {
	Title  string
	Fields []fieldDoc
}

// docForType reflects t (must be a struct) and returns the field-level docs.
// Embedded structs are flattened into the parent's field list.
func docForType(t reflect.Type) structDoc {
	var fields []fieldDoc
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			child := docForType(f.Type)
			fields = append(fields, child.Fields...)
			continue
		}
		yamlTag := f.Tag.Get("yaml")
		if yamlTag == "-" {
			continue
		}
		name, omitempty := parseYAMLTag(yamlTag, f.Name)
		if name == "" {
			continue
		}
		desc := f.Tag.Get("agtkdoc")
		required, cleanDesc := parseAgtkdoc(desc)
		if !omitempty && desc == "" {
			required = true
		}
		fields = append(fields, fieldDoc{
			Name:        name,
			Type:        renderType(f.Type),
			Required:    required,
			Description: cleanDesc,
		})
	}
	return structDoc{Fields: fields}
}

// collectSubStructs walks the category struct and returns each nested
// sub-struct (extensions blocks, OAuthConfig, HookHandler, …) for separate
// documentation.
func collectSubStructs(t reflect.Type) []structDoc {
	var out []structDoc
	seen := map[string]bool{}
	walkSubStructs(t, "", &out, seen)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

func walkSubStructs(t reflect.Type, prefix string, out *[]structDoc, seen map[string]bool) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		yamlTag := f.Tag.Get("yaml")
		if yamlTag == "-" || strings.HasPrefix(yamlTag, ",") || yamlTag == "" {
			// inline embedded → already covered
			if f.Anonymous {
				continue
			}
		}
		name, _ := parseYAMLTag(yamlTag, f.Name)
		if name == "" {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() != reflect.Struct {
			continue
		}
		// Skip the Common base — already documented at the top.
		if ft.Name() == "Common" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if seen[ft.String()] {
			continue
		}
		seen[ft.String()] = true
		fields := docForType(ft).Fields
		// Skip extension *wrapper* types — their fields are themselves
		// struct pointers, already rendered as their own sub-sections.
		if !isExtensionWrapper(ft) && len(fields) > 0 {
			*out = append(*out, structDoc{
				Title:  fmt.Sprintf("`%s` (%s)", path, ft.Name()),
				Fields: fields,
			})
		}
		walkSubStructs(ft, path, out, seen)
	}
}

// filterOutCommon removes fields that came from the Common base from the
// per-category field list, since Common is documented separately.
func filterOutCommon(fields []fieldDoc) []fieldDoc {
	commonNames := map[string]bool{}
	for _, f := range docForType(reflect.TypeOf(defs.Common{})).Fields {
		commonNames[f.Name] = true
	}
	var out []fieldDoc
	for _, f := range fields {
		if commonNames[f.Name] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func writeFieldTable(b *bytes.Buffer, fields []fieldDoc) {
	if len(fields) == 0 {
		fmt.Fprintln(b, "_(no fields beyond the Common base)_")
		return
	}
	fmt.Fprintln(b, "| Field | Type | Required | Description |")
	fmt.Fprintln(b, "|-------|------|----------|-------------|")
	for _, f := range fields {
		req := "no"
		if f.Required {
			req = "**yes**"
		}
		desc := strings.ReplaceAll(f.Description, "\n", " ")
		desc = strings.ReplaceAll(desc, "|", `\|`)
		fmt.Fprintf(b, "| `%s` | `%s` | %s | %s |\n", f.Name, f.Type, req, desc)
	}
}

// parseYAMLTag returns (name, omitempty). Returns an empty name when the
// field is excluded ("-") or inlined ("inline" without a name).
func parseYAMLTag(tag, fallback string) (string, bool) {
	if tag == "" {
		return fallback, false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	omitempty := false
	for _, p := range parts[1:] {
		switch p {
		case "omitempty":
			omitempty = true
		case "inline":
			return "", false
		}
	}
	return name, omitempty
}

// parseAgtkdoc returns (required, description). The "required;" prefix
// marks a field as required regardless of yaml omitempty (for fields whose
// emptiness is meaningful but still required at the schema level).
func parseAgtkdoc(tag string) (bool, string) {
	if strings.HasPrefix(tag, "required;") {
		return true, strings.TrimSpace(strings.TrimPrefix(tag, "required;"))
	}
	return false, strings.TrimSpace(tag)
}

// isExtensionWrapper reports whether t is a per-category extension
// wrapper (e.g. SkillExtensions, AgentExtensions). The convention is the
// type name ending in "Extensions" and every exported field being a
// pointer-to-struct.
func isExtensionWrapper(t reflect.Type) bool {
	if !strings.HasSuffix(t.Name(), "Extensions") {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Type.Kind() != reflect.Pointer || f.Type.Elem().Kind() != reflect.Struct {
			return false
		}
	}
	return true
}

func renderType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return renderType(t.Elem())
	case reflect.Slice:
		return "[]" + renderType(t.Elem())
	case reflect.Map:
		return "map[" + renderType(t.Key()) + "]" + renderType(t.Elem())
	}
	return t.Name()
}
