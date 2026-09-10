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
