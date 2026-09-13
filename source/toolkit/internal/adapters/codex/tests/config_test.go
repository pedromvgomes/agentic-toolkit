package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/codex"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// TestConfig_MCPStdioAndHTTP: transport picks the table's shape, and the
// Codex extension keys land under their Codex spellings — http_headers
// for static headers, default_tools_approval_mode for the approval
// override — rather than the canonical field names.
func TestConfig_MCPStdioAndHTTP(t *testing.T) {
	tmp := t.TempDir()

	plan := makePlan([]resolver.PlannedDefinition{
		pdMCP("ctx", definitions.TransportStdio, &definitions.MCPServer{
			Command: "npx",
			Args:    []string{"-y", "@upstash/context7-mcp"},
			Env:     map[string]string{"TOKEN": "abc"},
		}, &definitions.CodexMCPExt{
			EnabledTools:      []string{"search"},
			ApprovalMode:      "prompt",
			StartupTimeoutSec: 15,
			Required:          true,
		}, "default"),
		pdMCP("figma", definitions.TransportHTTP, &definitions.MCPServer{
			URL:     "https://mcp.figma.com/mcp",
			Headers: map[string]string{"X-Region": "us-east-1"},
		}, &definitions.CodexMCPExt{BearerTokenEnvVar: "FIGMA_TOKEN"}, "default"),
	}, "default")

	renderCodex(t, plan, tmp)
	cfg := mustReadTOML(t, configPath(tmp))
	servers := subTable(t, cfg, "mcp_servers")

	ctx := subTable(t, servers, "ctx")
	wantKeys(t, ctx, map[string]any{
		"command":                     "npx",
		"default_tools_approval_mode": "prompt",
		"required":                    true,
		"startup_timeout_sec":         float64(15),
	})
	if _, ok := ctx["url"]; ok {
		t.Errorf("stdio server got a url key: %v", ctx)
	}

	figma := subTable(t, servers, "figma")
	wantKeys(t, figma, map[string]any{
		"url":                  "https://mcp.figma.com/mcp",
		"bearer_token_env_var": "FIGMA_TOKEN",
	})
	if _, ok := figma["http_headers"]; !ok {
		t.Errorf("http server missing http_headers: %v", figma)
	}
	if _, ok := figma["command"]; ok {
		t.Errorf("http server got a command key: %v", figma)
	}
}

// TestConfig_MCPArgsOverride: a server that is told which client it
// serves needs a different argv per platform, and splitting it into two
// definitions would rename it — the definition's name is the name the
// server is addressed by. The override replaces the canonical argv
// rather than extending it, so the flag it exists to change carries one
// value and not two.
func TestConfig_MCPArgsOverride(t *testing.T) {
	tmp := t.TempDir()

	plan := makePlan([]resolver.PlannedDefinition{
		pdMCP("serena", definitions.TransportStdio, &definitions.MCPServer{
			Command: "serena",
			Args:    []string{"start-mcp-server", "--context", "claude-code"},
		}, &definitions.CodexMCPExt{
			Args: []string{"start-mcp-server", "--context", "codex"},
		}, "default"),
		pdMCP("plain", definitions.TransportStdio, &definitions.MCPServer{
			Command: "plain",
			Args:    []string{"--shared"},
		}, nil, "default"),
	}, "default")

	renderCodex(t, plan, tmp)
	servers := subTable(t, mustReadTOML(t, configPath(tmp)), "mcp_servers")

	got := subTable(t, servers, "serena")["args"].([]any)
	want := []string{"start-mcp-server", "--context", "codex"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("args[%d] = %v, want %q", i, got[i], want[i])
		}
	}

	// A server with no override keeps the canonical argv.
	if plainArgs := subTable(t, servers, "plain")["args"].([]any); len(plainArgs) != 1 || plainArgs[0] != "--shared" {
		t.Errorf("a server without an override lost its canonical args: %v", plainArgs)
	}
}

// TestConfig_MCPSSEReportedNotRendered: Codex speaks stdio and
// streamable http only. An sse definition is skipped and said out loud,
// rather than written as a url Codex would never connect to.
func TestConfig_MCPSSEReportedNotRendered(t *testing.T) {
	tmp := t.TempDir()
	var out bytes.Buffer

	plan := makePlan([]resolver.PlannedDefinition{
		pdMCP("streamy", definitions.TransportSSE, &definitions.MCPServer{
			URL: "https://example.test/sse",
		}, nil, "default"),
	}, "default")

	if err := codex.Render(plan, codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, Stdout: &out,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	if _, err := os.Stat(configPath(tmp)); !os.IsNotExist(err) {
		t.Errorf("config.toml written for an sse-only plan: %v", err)
	}
	if !strings.Contains(out.String(), `mcp "streamy" not rendered`) {
		t.Errorf("skip not reported: %q", out.String())
	}
}

// TestConfig_HooksEnableTheFeature: a rendered hook is inert unless
// Codex's hooks feature is on, so the adapter sets it alongside the hook
// tables rather than leaving it to the consumer.
func TestConfig_HooksEnableTheFeature(t *testing.T) {
	tmp := t.TempDir()

	plan := makePlan([]resolver.PlannedDefinition{
		pdHook("guard", "PreToolUse", "Bash", definitions.HookHandler{
			Type:    definitions.HandlerCommand,
			Command: "./guard.sh",
		}, 30, &definitions.CodexHookExt{Async: true}, "default"),
	}, "default")

	renderCodex(t, plan, tmp)
	cfg := mustReadTOML(t, configPath(tmp))

	if got := subTable(t, cfg, "features")["hooks"]; got != true {
		t.Errorf("features.hooks not enabled: %v", got)
	}

	events := subTable(t, cfg, "hooks")
	blocks, ok := events["PreToolUse"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("PreToolUse not an array-of-tables: %#v", events["PreToolUse"])
	}
	block := blocks[0].(map[string]any)
	if block["matcher"] != "Bash" {
		t.Errorf("matcher: %v", block["matcher"])
	}
	handlers, ok := block["hooks"].([]any)
	if !ok || len(handlers) != 1 {
		t.Fatalf("handler array missing: %#v", block["hooks"])
	}
	wantKeys(t, handlers[0].(map[string]any), map[string]any{
		"type":    "command",
		"command": "./guard.sh",
		"timeout": int64(30),
		"async":   true,
	})
}

// TestConfig_PromptHookReportedNotRendered: Codex parses a prompt
// handler but never runs it. Writing one would leave a hook in the
// config that silently never fires, so it is skipped and reported —
// and not an error, since the same hook renders fine on Claude.
func TestConfig_PromptHookReportedNotRendered(t *testing.T) {
	tmp := t.TempDir()
	var out bytes.Buffer

	plan := makePlan([]resolver.PlannedDefinition{
		pdHook("ask", "Stop", "", definitions.HookHandler{
			Type:   definitions.HandlerPrompt,
			Prompt: "summarize the session",
		}, 0, nil, "default"),
		pdHook("guard", "Stop", "", definitions.HookHandler{
			Type:    definitions.HandlerCommand,
			Command: "./guard.sh",
		}, 0, nil, "default"),
	}, "default")

	if err := codex.Render(plan, codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, Stdout: &out,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.String(), `hook "ask" not rendered`) {
		t.Errorf("skip not reported: %q", out.String())
	}

	cfg := mustReadTOML(t, configPath(tmp))
	blocks := subTable(t, cfg, "hooks")["Stop"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("want only the command hook, got %d blocks: %#v", len(blocks), blocks)
	}
	handler := blocks[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if handler["command"] != "./guard.sh" {
		t.Errorf("wrong hook survived: %v", handler)
	}
}

// TestConfig_SettingsMergedVerbatim: a setting fragment's top-level keys
// become top-level config keys unconverted, and last-stack-wins resolves
// two stacks touching the same key.
func TestConfig_SettingsMergedVerbatim(t *testing.T) {
	tmp := t.TempDir()

	plan := makePlan([]resolver.PlannedDefinition{
		pdSetting("base", map[string]any{"model": "gpt-5", "approval_policy": "on-request"}, "base"),
		pdSetting("override", map[string]any{"model": "gpt-5-codex"}, "app"),
	}, "base", "app")

	renderCodex(t, plan, tmp)
	cfg := mustReadTOML(t, configPath(tmp))
	wantKeys(t, cfg, map[string]any{
		"model":           "gpt-5-codex",
		"approval_policy": "on-request",
	})
}

// TestConfig_AllThreeCategoriesTogether: mcp, hooks and settings share
// one file, and the managed list names every path agtk claimed —
// including the nested feature flag.
func TestConfig_AllThreeCategoriesTogether(t *testing.T) {
	tmp := t.TempDir()

	renderCodex(t, mixedConfigPlan(), tmp)
	cfg := mustReadTOML(t, configPath(tmp))

	for _, key := range []string{"mcp_servers", "hooks", "features", "model"} {
		if _, ok := cfg[key]; !ok {
			t.Errorf("config missing %q: %v", key, mapKeys(cfg))
		}
	}
	got := managedList(t, cfg)
	want := []string{"features.hooks", "hooks", "mcp_servers", "model"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("managed list = %v, want %v", got, want)
	}
}

// TestConfig_MergesIntoHandAuthoredFile: keys agtk never claimed survive
// a render untouched, including a sibling key inside the same [features]
// table the hooks flag lives in.
func TestConfig_MergesIntoHandAuthoredFile(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, tmp, `
sandbox_mode = "workspace-write"

[features]
web_search = true

[mcp_servers.hand_authored]
command = "mine"
`)

	renderCodex(t, mixedConfigPlan(), tmp)
	cfg := mustReadTOML(t, configPath(tmp))

	if cfg["sandbox_mode"] != "workspace-write" {
		t.Errorf("hand-authored top-level key lost: %v", cfg["sandbox_mode"])
	}
	features := subTable(t, cfg, "features")
	if features["web_search"] != true {
		t.Errorf("sibling key inside [features] lost: %v", features)
	}
	if features["hooks"] != true {
		t.Errorf("hooks flag not set: %v", features)
	}
	// mcp_servers is agtk's whole table: a hand-authored server inside it
	// is replaced, the way a hand-authored `hooks` block would be.
	servers := subTable(t, cfg, "mcp_servers")
	if _, ok := servers["hand_authored"]; ok {
		t.Errorf("hand-authored server inside a managed table survived: %v", servers)
	}
}

// TestConfig_ReleasesKeysItNoLongerOwns: a re-render with nothing to say
// drops exactly what the previous render claimed — the nested feature
// flag included, without taking the [features] table's other keys with
// it — and leaves everything else alone.
func TestConfig_ReleasesKeysItNoLongerOwns(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, tmp, "sandbox_mode = \"read-only\"\n\n[features]\nweb_search = true\n")

	renderCodex(t, mixedConfigPlan(), tmp)
	renderCodex(t, makePlan([]resolver.PlannedDefinition{
		pdInstruction("only-instruction", "i", "i body", "default"),
	}, "default"), tmp)

	cfg := mustReadTOML(t, configPath(tmp))
	for _, gone := range []string{"mcp_servers", "hooks", "model"} {
		if _, ok := cfg[gone]; ok {
			t.Errorf("%q still present after the definition went away: %v", gone, mapKeys(cfg))
		}
	}
	if cfg["sandbox_mode"] != "read-only" {
		t.Errorf("unmanaged key lost: %v", cfg["sandbox_mode"])
	}
	features := subTable(t, cfg, "features")
	if features["web_search"] != true {
		t.Errorf("unmanaged sibling lost with the flag: %v", features)
	}
	if _, ok := features["hooks"]; ok {
		t.Errorf("hooks flag outlived the hooks: %v", features)
	}
	if _, ok := cfg["_meta"]; ok {
		t.Errorf("ownership marker left behind with nothing owned: %v", cfg["_meta"])
	}
}

// TestConfig_NotWrittenWhenNothingToSay: a plan with no mcp, hook or
// setting leaves config.toml alone entirely rather than creating an
// empty one.
func TestConfig_NotWrittenWhenNothingToSay(t *testing.T) {
	tmp := t.TempDir()
	renderCodex(t, simpleProjectPlan(), tmp)
	if _, err := os.Stat(configPath(tmp)); !os.IsNotExist(err) {
		t.Errorf("config.toml created with nothing to put in it: %v", err)
	}
}

// TestConfig_DryRunTouchesNothing: the dry run announces config.toml as
// a target without creating it.
func TestConfig_DryRunTouchesNothing(t *testing.T) {
	tmp := t.TempDir()
	var out bytes.Buffer

	if err := codex.Render(mixedConfigPlan(), codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, DryRun: true, Stdout: &out,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.String(), "would update") || !strings.Contains(out.String(), "config.toml") {
		t.Errorf("config.toml not announced: %q", out.String())
	}
	if _, err := os.Stat(configPath(tmp)); !os.IsNotExist(err) {
		t.Errorf("dry run wrote config.toml: %v", err)
	}
}

// ===== helpers =====

func mixedConfigPlan() *resolver.Plan {
	return makePlan([]resolver.PlannedDefinition{
		pdMCP("ctx", definitions.TransportStdio, &definitions.MCPServer{Command: "npx"}, nil, "default"),
		pdHook("guard", "PreToolUse", "Bash", definitions.HookHandler{
			Type: definitions.HandlerCommand, Command: "./guard.sh",
		}, 0, nil, "default"),
		pdSetting("base", map[string]any{"model": "gpt-5"}, "default"),
	}, "default")
}

func renderCodex(t *testing.T, plan *resolver.Plan, projectRoot string) {
	t.Helper()
	if err := codex.Render(plan, codex.Options{
		Scope:       codex.ScopeProject,
		ProjectRoot: projectRoot,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
}

func configPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".codex", "config.toml")
}

func writeConfigFile(t *testing.T, projectRoot, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(projectRoot, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath(projectRoot), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := toml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse %s: %v\n%s", path, err, raw)
	}
	return m
}

func subTable(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	sub, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is not a table: %#v", key, m[key])
	}
	return sub
}

func wantKeys(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%q = %#v, want %#v", k, got[k], v)
		}
	}
}

func managedList(t *testing.T, cfg map[string]any) []string {
	t.Helper()
	agtk := subTable(t, subTable(t, cfg, "_meta"), "agtk")
	raw, ok := agtk["managed"].([]any)
	if !ok {
		t.Fatalf("managed list missing: %#v", agtk)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

// TestConfig_SettingFragmentCannotDisableHooks: a fragment naming
// `features` would replace the table the hooks flag lives in, leaving
// every rendered hook in the file and none of them running. The hook
// definitions own the flag, so the fragment is dropped instead — the
// same precedence they already have over a fragment naming `hooks`.
func TestConfig_SettingFragmentCannotDisableHooks(t *testing.T) {
	tmp := t.TempDir()

	plan := makePlan([]resolver.PlannedDefinition{
		pdHook("guard", "PreToolUse", "Bash", definitions.HookHandler{
			Type: definitions.HandlerCommand, Command: "./guard.sh",
		}, 0, nil, "default"),
		pdSetting("theirs", map[string]any{
			"features": map[string]any{"web_search": true},
			"hooks":    map[string]any{"Stop": "nonsense"},
		}, "default"),
	}, "default")

	renderCodex(t, plan, tmp)
	cfg := mustReadTOML(t, configPath(tmp))

	if got := subTable(t, cfg, "features")["hooks"]; got != true {
		t.Errorf("a setting fragment disabled the hooks it rendered alongside: features = %v", cfg["features"])
	}
	if _, ok := subTable(t, cfg, "hooks")["PreToolUse"]; !ok {
		t.Errorf("the fragment replaced the hook tables: %v", cfg["hooks"])
	}
	// Only what agtk actually wrote is released on a later render.
	got := managedList(t, cfg)
	want := []string{"features.hooks", "hooks"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("managed list = %v, want %v", got, want)
	}
}

// TestConfig_SettingFragmentKeepsUnrelatedLookalikeKeys: the overlap
// check is segment-wise, so a fragment key that merely shares a prefix
// with a claimed path is not mistaken for it.
func TestConfig_SettingFragmentKeepsUnrelatedLookalikeKeys(t *testing.T) {
	tmp := t.TempDir()

	plan := makePlan([]resolver.PlannedDefinition{
		pdHook("guard", "PreToolUse", "", definitions.HookHandler{
			Type: definitions.HandlerCommand, Command: "./guard.sh",
		}, 0, nil, "default"),
		pdSetting("theirs", map[string]any{"feature_flags": map[string]any{"beta": true}}, "default"),
	}, "default")

	renderCodex(t, plan, tmp)
	cfg := mustReadTOML(t, configPath(tmp))

	if subTable(t, cfg, "feature_flags")["beta"] != true {
		t.Errorf("a key sharing a prefix with features.hooks was dropped: %v", mapKeys(cfg))
	}
	if subTable(t, cfg, "features")["hooks"] != true {
		t.Errorf("hooks flag lost: %v", cfg["features"])
	}
}

// TestConfig_DryRunSaysNothingWhenEverythingIsSkipped: a plan whose
// every mcp and hook is one Codex cannot express collects nothing, so
// the render it previews would leave config.toml alone. The dry run
// says so, and reports the skips that led there.
func TestConfig_DryRunSaysNothingWhenEverythingIsSkipped(t *testing.T) {
	tmp := t.TempDir()
	var out bytes.Buffer

	plan := makePlan([]resolver.PlannedDefinition{
		pdMCP("streamy", definitions.TransportSSE, &definitions.MCPServer{
			URL: "https://example.test/sse",
		}, nil, "default"),
		pdHook("ask", "Stop", "", definitions.HookHandler{
			Type: definitions.HandlerPrompt, Prompt: "summarize",
		}, 0, nil, "default"),
	}, "default")

	if err := codex.Render(plan, codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, DryRun: true, Stdout: &out,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Contains(out.String(), "config.toml") && strings.Contains(out.String(), "would update") {
		t.Errorf("dry run announced a write the render would not make: %q", out.String())
	}
	for _, want := range []string{`mcp "streamy" not rendered`, `hook "ask" not rendered`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry run did not report %q: %q", want, out.String())
		}
	}
}

// TestConfig_DryRunAnnouncesClearingManagedKeys: a plan with no mcp,
// hook or setting left still reclaims what a previous render claimed,
// so the preview says so rather than going quiet about a file it is
// about to rewrite.
func TestConfig_DryRunAnnouncesClearingManagedKeys(t *testing.T) {
	tmp := t.TempDir()
	var out bytes.Buffer

	// Hand-authored content is what keeps the file alive once agtk's
	// keys go; without it the render removes the file instead, which is
	// TestConfig_DryRunAnnouncesTheRemoval's case.
	writeConfigFile(t, tmp, "sandbox_mode = \"workspace-write\"\n")
	renderCodex(t, mixedConfigPlan(), tmp)

	if err := codex.Render(simpleProjectPlan(), codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, DryRun: true, Stdout: &out,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.String(), "clearing managed keys") {
		t.Errorf("dry run silent about the keys the render would reclaim: %q", out.String())
	}

	// And the real render does exactly what the preview said.
	renderCodex(t, simpleProjectPlan(), tmp)
	cfg := mustReadTOML(t, configPath(tmp))
	if _, ok := cfg["mcp_servers"]; ok {
		t.Errorf("managed keys not reclaimed: %v", mapKeys(cfg))
	}
}

// TestConfig_DryRunSurfacesAnUnreadableConfig: Options.DryRun promises
// that errors depending on filesystem state are still surfaced. A
// config.toml that will not parse is one, and a preview that reported
// success for it would be previewing a render that cannot run.
func TestConfig_DryRunSurfacesAnUnreadableConfig(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, tmp, "this is [ not valid TOML\n")

	var out bytes.Buffer
	err := codex.Render(mixedConfigPlan(), codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, DryRun: true, Stdout: &out,
	})
	if err == nil {
		t.Fatalf("dry run reported success for a config the render refuses: %q", out.String())
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should name the parse failure: %v", err)
	}

	// And the real render fails the same way, rather than the two
	// disagreeing about the same file.
	if err := codex.Render(mixedConfigPlan(), codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp,
	}); err == nil {
		t.Error("real render accepted a config the dry run refused")
	}
}

// TestConfig_GoesWithItsLastKey: when everything agtk wrote is released
// and the consumer had nothing of their own in the file, the file goes
// too. A nought-byte config.toml is one Codex still has to parse, and
// one more line in every diff, for no content.
func TestConfig_GoesWithItsLastKey(t *testing.T) {
	tmp := t.TempDir()

	renderCodex(t, mixedConfigPlan(), tmp)
	if _, err := os.Stat(configPath(tmp)); err != nil {
		t.Fatalf("config.toml should exist while agtk has keys in it: %v", err)
	}

	var out bytes.Buffer
	if err := codex.Render(simpleProjectPlan(), codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, Stdout: &out,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, err := os.Stat(configPath(tmp)); !os.IsNotExist(err) {
		t.Errorf("config.toml outlived the last key it held: %v", err)
	}
	if !strings.Contains(out.String(), "removed") {
		t.Errorf("removal not reported: %q", out.String())
	}
}

// TestConfig_DryRunAnnouncesTheRemoval: the preview distinguishes a file
// that will be rewritten without agtk's keys from one that will be
// deleted, because those are different outcomes for whoever reads it.
func TestConfig_DryRunAnnouncesTheRemoval(t *testing.T) {
	tmp := t.TempDir()
	renderCodex(t, mixedConfigPlan(), tmp)

	var out bytes.Buffer
	if err := codex.Render(simpleProjectPlan(), codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, DryRun: true, Stdout: &out,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out.String(), "would remove") {
		t.Errorf("dry run did not announce the removal: %q", out.String())
	}
	if _, err := os.Stat(configPath(tmp)); err != nil {
		t.Errorf("dry run removed the file it was previewing: %v", err)
	}
}

// Every top-level settings key resolves last-stack-wins here, `permissions`
// included. The Claude adapter composes that one instead, and the difference
// is deliberate: `permissions` is Claude Code's vocabulary, a fragment written
// in it declares `platforms: [claude]`, and none reaches this adapter.
//
// Pinned because the two adapters' merges are otherwise the same code twice,
// so the next person to read them will ask which one is wrong.
func TestCodexSettingsResolveLastWinsIncludingPermissions(t *testing.T) {
	tmp := t.TempDir()
	var out bytes.Buffer

	plan := makePlan([]resolver.PlannedDefinition{
		pdSetting("extended", map[string]any{
			"permissions": map[string]any{"allow": []any{"from-the-extended-stack"}},
		}, "memory"),
		pdSetting("extending", map[string]any{
			"permissions": map[string]any{"allow": []any{"from-the-extending-stack"}},
		}, "default"),
	}, "memory", "default")

	if err := codex.Render(plan, codex.Options{
		Scope: codex.ScopeProject, ProjectRoot: tmp, Stdout: &out,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}

	raw, err := os.ReadFile(configPath(tmp))
	if err != nil {
		t.Fatalf("config.toml: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "from-the-extending-stack") {
		t.Errorf("the later stack's permissions did not win:\n%s", body)
	}
	if strings.Contains(body, "from-the-extended-stack") {
		t.Errorf("permissions composed here; this adapter resolves last-wins:\n%s", body)
	}
}
