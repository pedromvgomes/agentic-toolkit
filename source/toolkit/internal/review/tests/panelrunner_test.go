package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// mixed is a manifest that reviews locally with one provider and its pull
// requests with another, judge included. It is the shape a panel's own judge
// exists for: the reviewers already differ per context, and the judge runs in
// every review, so without an override the choice is made once for both.
const mixed = `
version: 1
reviewers:
  unified:     {provider: claudecode, model: sonnet, prompt: builtin:unified}
  correctness: {provider: codex,      model: gpt-5,  prompt: builtin:correctness}
judge:     {provider: claudecode, model: opus,   prompt: builtin:judge}
validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}
panels:
  local: {reviewers: [unified]}
  gpt:
    reviewers: [correctness]
    judge:     {provider: codex, model: gpt-5, prompt: builtin:judge}
    validator: {provider: codex, model: gpt-5, prompt: builtin:validator}
defaults:
  worktree: local
  pr:       gpt
`

func TestAPanelsOwnJudgeAndValidatorOverrideTheManifests(t *testing.T) {
	m := mustParse(t, mixed)

	if got := m.EffectiveJudge("gpt"); got.Provider != "codex" {
		t.Errorf("gpt judge provider = %q, want codex", got.Provider)
	}
	if got := m.EffectiveValidator("gpt"); got.Provider != "codex" {
		t.Errorf("gpt validator provider = %q, want codex", got.Provider)
	}
}

// A panel that declares neither uses the manifest's, so overriding on one
// panel is never the price of declaring them on every panel.
func TestAPanelThatDeclaresNoneUsesTheManifests(t *testing.T) {
	m := mustParse(t, mixed)

	if got := m.EffectiveJudge("local"); got.Provider != "claudecode" {
		t.Errorf("local judge provider = %q, want claudecode", got.Provider)
	}
	if got := m.EffectiveValidator("local"); got.Provider != "claudecode" {
		t.Errorf("local validator provider = %q, want claudecode", got.Provider)
	}
}

// A name that is not a panel resolves the manifest's own rather than nothing.
// Every caller reaches this with a panel the selection produced, so a nil here
// would be a review with no judge reported as a manifest that declares none.
func TestAnUnknownPanelResolvesTheManifestsRunners(t *testing.T) {
	m := mustParse(t, mixed)

	if got := m.EffectiveJudge("no-such-panel"); got == nil || got.Provider != "claudecode" {
		t.Errorf("judge for an unknown panel = %#v, want the manifest's", got)
	}
}

// A panel's own runners are held to the same prompt validation the top-level
// ones are. A panel judge naming a prompt that does not ship would otherwise
// start with no instructions and answer anyway.
func TestAPanelJudgeWithAMisspelledPromptIsRefused(t *testing.T) {
	err := refuse(t, strings.Replace(mixed,
		"judge:     {provider: codex, model: gpt-5, prompt: builtin:judge}",
		"judge:     {provider: codex, model: gpt-5, prompt: builtin:jugde}", 1))

	if !review.IsKind(err, review.ErrInvalidPrompt) {
		t.Fatalf("kind = %v, want invalid_prompt", err)
	}
	if !strings.Contains(err.Error(), "panels.gpt.judge") {
		t.Errorf("error = %q, want it to name panels.gpt.judge", err)
	}
}

// A capability a panel's own runner cannot express fails at spawn time, which
// reads as an outage rather than as a manifest to fix. Checking only the
// manifest's would leave exactly the panel that overrode them unchecked.
func TestAPanelJudgeWithAnUnknownProviderIsRefused(t *testing.T) {
	m := mustParse(t, strings.Replace(mixed,
		"judge:     {provider: codex, model: gpt-5, prompt: builtin:judge}",
		"judge:     {provider: gpt4all, model: x, prompt: builtin:judge}", 1))

	err := review.CheckCapabilities("manifest.yaml", m)
	if err == nil {
		t.Fatal("CheckCapabilities accepted a panel judge with an unknown provider")
	}
	if !strings.Contains(err.Error(), "panels.gpt.judge") {
		t.Errorf("error = %q, want it to name panels.gpt.judge", err)
	}
}

// A context that posts always validates, so a panel it could run needs a
// validator from somewhere. Every panel is checked rather than the context's
// default alone: an escalation raises to another panel and --panel names any
// of them, so a panel resolving none is a review that cannot post, discovered
// when the rule that raised to it fires.
func TestAPostingContextRefusesAPanelThatResolvesNoValidator(t *testing.T) {
	err := refuse(t, strings.Replace(mixed,
		"validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}\n", "", 1))

	if !review.IsKind(err, review.ErrMissingRequired) {
		t.Fatalf("kind = %v, want missing_required", err)
	}
	// `gpt` declares its own, so `local` is the one with nothing to fall back
	// to, and naming it is the difference between a manifest a person can fix
	// and one they have to bisect.
	if !strings.Contains(err.Error(), "panels.local.validator") {
		t.Errorf("error = %q, want it to name panels.local.validator", err)
	}
}

// A manifest with no top-level validator is legal when every panel brings its
// own: the requirement is that a validator resolves, not where it is written.
func TestEveryPanelDeclaringItsOwnValidatorNeedsNoTopLevelOne(t *testing.T) {
	src := strings.Replace(mixed,
		"validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}\n", "", 1)
	src = strings.Replace(src,
		"  local: {reviewers: [unified]}",
		"  local:\n    reviewers: [unified]\n    validator: {provider: claudecode, model: sonnet, prompt: builtin:validator}", 1)

	m := mustParse(t, src)

	if got := m.EffectiveValidator("local"); got == nil || got.Provider != "claudecode" {
		t.Errorf("local validator = %#v, want the panel's own", got)
	}
	if got := m.EffectiveValidator("gpt"); got == nil || got.Provider != "codex" {
		t.Errorf("gpt validator = %#v, want the panel's own", got)
	}
}
