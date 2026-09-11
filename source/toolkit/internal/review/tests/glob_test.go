package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
)

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// `**` spans any number of segments, including none. A rule written
		// `**/auth/**` has to catch the package at the repo root as well as
		// one buried six directories down, or it is a protection that holds
		// only for the layouts its author happened to picture.
		{"**/auth/**", "internal/auth/token.go", true},
		{"**/auth/**", "auth/token.go", true},
		{"**/auth/**", "a/b/c/auth/d/e/token.go", true},
		{"**/auth/**", "internal/auth", true},
		{"**/auth/**", "internal/authz/token.go", false},
		{"**/auth/**", "internal/resolver/token.go", false},

		{"**/migrations/**", "db/migrations/0001_init.sql", true},
		{"internal/resolver/**", "internal/resolver/graph.go", true},
		{"internal/resolver/**", "internal/stack/graph.go", false},

		// `*` stays inside one segment, so a pattern for top-level YAML does
		// not quietly reach into subdirectories.
		{"*.tf", "main.tf", true},
		{"*.tf", "infra/main.tf", false},
		{"**/*.tf", "infra/main.tf", true},
		{"**/*.tf", "main.tf", true},
		{".github/workflows/*", ".github/workflows/ci.yml", true},
		{".github/workflows/*", ".github/workflows/nested/ci.yml", false},

		{"?ain.go", "main.go", true},
		{"?ain.go", "domain.go", false},

		{"go.sum", "go.sum", true},
		{"go.sum", "vendor/go.sum", false},
	}

	for _, tc := range cases {
		if got := review.MatchGlob(tc.pattern, tc.path); got != tc.want {
			t.Errorf("MatchGlob(%q, %q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
		}
	}
}

func TestMatchAnyGlob(t *testing.T) {
	patterns := []string{"**/auth/**", "**/migrations/**"}

	if !review.MatchAnyGlob(patterns, "db/migrations/1.sql") {
		t.Error("a path matching the second pattern should match the list")
	}
	if review.MatchAnyGlob(patterns, "internal/resolver/graph.go") {
		t.Error("a path matching no pattern should not match the list")
	}
	if review.MatchAnyGlob(nil, "anything") {
		t.Error("an empty list matches nothing")
	}
}
