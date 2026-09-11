package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/stack"
)

func TestEffectivePlatforms_DefaultsToClaudeOnly(t *testing.T) {
	s, err := stack.ParseBytes("t.yaml", []byte("description: no platforms set\n"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got := s.EffectivePlatforms()
	if len(got) != 1 || got[0] != definitions.PlatformClaude {
		t.Errorf("EffectivePlatforms() = %v, want [claude]", got)
	}
}

func TestEffectivePlatforms_HonoursExplicitList(t *testing.T) {
	body := "description: opts in\nplatforms:\n  - claude\n  - codex\n"
	s, err := stack.ParseBytes("t.yaml", []byte(body))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got := s.EffectivePlatforms()
	if len(got) != 2 || got[0] != definitions.PlatformClaude || got[1] != definitions.PlatformCodex {
		t.Errorf("EffectivePlatforms() = %v, want [claude codex]", got)
	}
}

func TestParseBytes_UnknownPlatform_Rejected(t *testing.T) {
	body := "description: typo\nplatforms:\n  - cluade\n"
	_, err := stack.ParseBytes("t.yaml", []byte(body))
	if err == nil {
		t.Fatal("expected error for unknown platform")
	}
	if !stack.IsKind(err, stack.ErrUnknownPlatform) {
		t.Errorf("error kind != unknown_platform: %v", err)
	}
}

func TestParseBytes_PlatformsIsNotTreatedAsLegacy(t *testing.T) {
	// platforms: was a v1-only top-level field and is intercepted by the
	// legacy-config detector pre-decode; it must not be flagged now that
	// it has real v2 semantics.
	body := "description: v2 platforms\nplatforms:\n  - codex\n"
	_, err := stack.ParseBytes("t.yaml", []byte(body))
	if err != nil {
		t.Fatalf("platforms: must parse as a v2 field, got: %v", err)
	}
}
