package tests

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// The cloud stack exists to keep a cloud session's commits and pull requests
// free of authoring footers. That depends on the `attribution` key reaching
// Claude's settings.json with every switch off, and on it staying out of
// Codex's config.toml, where an unknown top-level key becomes a stray table.

func TestTheCloudStackTurnsEveryAttributionOffInClaudeSettings(t *testing.T) {
	apply := renderStack(t, "cloud")

	var settings map[string]any
	raw := readFile(t, filepath.Join(apply, ".claude/settings.json"))
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		t.Fatalf("settings.json is not JSON: %v\n%s", err, raw)
	}
	attribution, ok := settings["attribution"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json has no attribution object:\n%s", raw)
	}
	for _, key := range []string{"commit", "pr", "sessionUrl"} {
		if got, ok := attribution[key]; !ok || got != false {
			t.Errorf("attribution.%s = %v (present=%v), want false:\n%s", key, got, ok, raw)
		}
	}
}

func TestTheCloudStacksAttributionSettingDoesNotReachCodex(t *testing.T) {
	work, cache := cloudAndSharedWorkdir(t)

	if _, stderr, err := runCLI(t, work, "render", "--cache", cache); err != nil {
		t.Fatalf("render: %v\nstderr:\n%s", err, stderr)
	}

	// The un-narrowed fragment in the same run proves Codex's config exists
	// and is being written, so the absence below is the platform narrowing
	// and not a missing file.
	cfg := readFile(t, filepath.Join(work, ".codex/config.toml"))
	if !strings.Contains(cfg, "gpt-5-codex") {
		t.Fatalf("codex config.toml was not rendered from the shared fragment:\n%s", cfg)
	}
	if strings.Contains(cfg, "attribution") || strings.Contains(cfg, "sessionUrl") {
		t.Errorf("a claude-only attribution setting reached config.toml:\n%s", cfg)
	}

	claude := readFile(t, filepath.Join(work, ".claude/settings.json"))
	if !strings.Contains(claude, `"attribution"`) {
		t.Errorf("claude: attribution did not reach settings.json:\n%s", claude)
	}
}

// The default stack does not bundle the no-attribution setting, so a consumer
// adopts it only by naming the cloud stack.
func TestTheDefaultStackCarriesNoAttributionSetting(t *testing.T) {
	apply := renderStack(t, "default")

	settings := readFile(t, filepath.Join(apply, ".claude/settings.json"))
	if strings.Contains(settings, "attribution") {
		t.Errorf("the default stack's settings.json carries an attribution key:\n%s", settings)
	}
}

// cloudAndSharedWorkdir stages the real cloud stack and its setting beside a
// fixture stack carrying a setting both platforms read, as a consumer opted
// into both platforms.
func cloudAndSharedWorkdir(t *testing.T) (work, cache string) {
	t.Helper()

	repo := repoRoot(t)
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "stacks/cloud.yaml"), readFile(t, filepath.Join(repo, "stacks/cloud.yaml")))
	writeFile(t, filepath.Join(src, "definitions/settings/no-attribution.yaml"),
		readFile(t, filepath.Join(repo, "definitions/settings/no-attribution.yaml")))
	writeFile(t, filepath.Join(src, "stacks/shared.yaml"), "description: A setting both platforms read.\nsettings:\n  - base\n")
	writeFile(t, filepath.Join(src, "definitions/settings/base.yaml"),
		"description: Baseline settings fragment.\nvalue:\n  model: gpt-5-codex\n")

	url, sha := fixtureRepoFromDir(t, src)
	work = t.TempDir()
	cache = t.TempDir()
	writeFile(t, filepath.Join(work, ".agentic-toolkit.yaml"),
		"stacks:\n  - "+url+"/stacks/cloud.yaml@main\n  - "+url+"/stacks/shared.yaml@main\n"+
			"platforms:\n  - claude\n  - codex\n")
	writeLockfile(t, filepath.Join(work, ".agentic-toolkit.lock.yaml"), url, "main", sha)
	return work, cache
}
