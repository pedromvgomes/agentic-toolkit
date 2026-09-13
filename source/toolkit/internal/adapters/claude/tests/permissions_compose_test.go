package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/claude"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// renderPermissions renders the given settings definitions and returns
// settings.json's three permission lists.
//
// stackOrder is passed through to makePlan, because which contribution is
// "later" is what a last-wins merge would have resolved on — so a test that
// did not control it could pass for the wrong reason.
func renderPermissions(t *testing.T, defs []resolver.PlannedDefinition, stackOrder ...string) (allow, deny, ask []string) {
	t.Helper()
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")

	if err := claude.Render(makePlan(defs, stackOrder...), claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: scopeRoot, ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(scopeRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
			Ask   []string `json:"ask"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("settings.json: %v\n%s", err, raw)
	}
	return settings.Permissions.Allow, settings.Permissions.Deny, settings.Permissions.Ask
}

// renderPermissionsErr renders and returns the error, for the malformed cases.
func renderPermissionsErr(t *testing.T, defs []resolver.PlannedDefinition, stackOrder ...string) error {
	t.Helper()
	tmp := t.TempDir()
	return claude.Render(makePlan(defs, stackOrder...), claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: filepath.Join(tmp, ".claude"), ProjectRoot: tmp,
	})
}

// The grant a stack ships for the definitions it ships has to survive being
// layered under another stack that also pre-approves something. Under a
// last-wins merge on the whole `permissions` key it does not: the later
// stack's contribution replaces the earlier one's entire allow list, and
// nothing anywhere reports that it happened.
func TestPermissionsFromEveryStackSurviveTheMerge(t *testing.T) {
	allow, _, _ := renderPermissions(t, []resolver.PlannedDefinition{
		pdSetting("memory-permissions", "memory grants", map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(agtk memory stats*)"}},
		}, "memory"),
		pdSetting("skill-permissions", "skill grants", map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(gh pr view *)"}},
		}, "default"),
	}, "memory", "default")

	for _, want := range []string{"Bash(agtk memory stats*)", "Bash(gh pr view *)"} {
		if !hasGrant(allow, want) {
			t.Errorf("%q did not survive the merge; allow = %v", want, allow)
		}
	}
}

// The extended stack is visited first, so a last-wins merge resolves in
// favour of the extending stack. That is the direction the regression ran:
// `default.yaml` extending a memory stack would silently drop the memory
// stack's grants, never the other way round.
func TestTheExtendingStackDoesNotEraseWhatItExtends(t *testing.T) {
	allow, _, _ := renderPermissions(t, []resolver.PlannedDefinition{
		pdSetting("extended", "shipped by the stack being extended", map[string]any{
			"permissions": map[string]any{"allow": []any{"Read(**/.memory/INDEX.md)"}},
		}, "memory"),
		pdSetting("extending", "shipped by the stack doing the extending", map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(gh pr view *)"}},
		}, "default"),
	}, "memory", "default")

	if !hasGrant(allow, "Read(**/.memory/INDEX.md)") {
		t.Errorf("the extended stack's grant was erased by the extending stack; allow = %v", allow)
	}
}

// `deny` and `ask` compose on the same terms as `allow`. A stack that
// tightens what an agent may do must not have that tightening dropped by a
// stack layered over it, which is the more dangerous direction of the same
// bug.
func TestDenyAndAskComposeAlongsideAllow(t *testing.T) {
	allow, deny, ask := renderPermissions(t, []resolver.PlannedDefinition{
		pdSetting("a", "tightens", map[string]any{
			"permissions": map[string]any{
				"deny": []any{"Bash(rm -rf *)"},
				"ask":  []any{"Bash(git push*)"},
			},
		}, "memory"),
		pdSetting("b", "grants", map[string]any{
			"permissions": map[string]any{
				"allow": []any{"Bash(gh pr view *)"},
				"deny":  []any{"Read(./.env)"},
			},
		}, "default"),
	}, "memory", "default")

	if !hasGrant(deny, "Bash(rm -rf *)") || !hasGrant(deny, "Read(./.env)") {
		t.Errorf("deny did not compose: %v", deny)
	}
	if !hasGrant(ask, "Bash(git push*)") {
		t.Errorf("ask was dropped: %v", ask)
	}
	if !hasGrant(allow, "Bash(gh pr view *)") {
		t.Errorf("allow was dropped: %v", allow)
	}
}

// Two stacks pre-approving the same thing is not an error and must not
// produce the rule twice — a settings file listing a grant twice is the kind
// of thing a reader stops trusting.
func TestAGrantTwoStacksBothShipAppearsOnce(t *testing.T) {
	allow, _, _ := renderPermissions(t, []resolver.PlannedDefinition{
		pdSetting("a", "one", map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(gh pr view *)"}},
		}, "memory"),
		pdSetting("b", "two", map[string]any{
			"permissions": map[string]any{"allow": []any{"Bash(gh pr view *)"}},
		}, "default"),
	}, "memory", "default")

	n := 0
	for _, a := range allow {
		if a == "Bash(gh pr view *)" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the shared grant appears %d times, want 1: %v", n, allow)
	}
}

// Composition is confined to `permissions`. Every other key stays last-wins,
// which is what lets a consumer take back a default the way feature-flow-model
// documents.
func TestKeysOtherThanPermissionsStayLastWins(t *testing.T) {
	tmp := t.TempDir()
	scopeRoot := filepath.Join(tmp, ".claude")
	if err := claude.Render(makePlan([]resolver.PlannedDefinition{
		pdSetting("a", "one", map[string]any{"model": "sonnet"}, "memory"),
		pdSetting("b", "two", map[string]any{"model": "opus"}, "default"),
	}, "memory", "default"), claude.Options{
		Scope: claude.ScopeProject, ScopeRoot: scopeRoot, ProjectRoot: tmp,
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(scopeRoot, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.Model != "opus" {
		t.Errorf("model = %q, want the later stack's %q", settings.Model, "opus")
	}
}

// Composing silently past a member that is not the list its name promises
// would drop whatever the other definitions put there — the failure the whole
// change exists to prevent. The error names the definition that wrote it,
// because the author of a shared stack is not the person running the render.
func TestAMalformedPermissionsMemberIsRefusedAndNamed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value map[string]any
		want  string
	}{
		{
			name:  "allow is not a list",
			value: map[string]any{"permissions": map[string]any{"allow": "Bash(gh pr view *)"}},
			want:  "permissions.allow",
		},
		{
			name:  "permissions is not a mapping",
			value: map[string]any{"permissions": []any{"Bash(gh pr view *)"}},
			want:  "`permissions`",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := renderPermissionsErr(t, []resolver.PlannedDefinition{
				pdSetting("good", "well-formed", map[string]any{
					"permissions": map[string]any{"allow": []any{"Bash(agtk memory stats*)"}},
				}, "memory"),
				pdSetting("malformed", "broken", tc.value, "default"),
			}, "memory", "default")
			if err == nil {
				t.Fatal("a malformed permissions member rendered without complaint")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not name %s: %v", tc.want, err)
			}
			if !strings.Contains(err.Error(), "malformed") {
				t.Errorf("error does not name the definition that wrote it: %v", err)
			}
		})
	}
}
