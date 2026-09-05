package tests

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/curator"
)

// The curator has no agent definition, so the embedded prompt is the only copy
// of its content policy. An empty embed would produce a run with a roster
// entry and no instructions, which answers rather than refusing.
func TestThePromptIsEmbedded(t *testing.T) {
	if strings.TrimSpace(curator.Prompt()) == "" {
		t.Fatal("the embedded curator prompt is empty")
	}
}

// The prompt carries rules that live nowhere else now that the curator is not
// a definition: the single-writer rule, the cost bar, and the instruction
// never to compute a hash.
func TestThePromptStatesTheRulesItIsTheOnlyHomeFor(t *testing.T) {
	prompt := curator.Prompt()

	for _, want := range []string{
		"notes/",
		"candidates/",
		"agtk memory anchor",
		"agtk memory index",
		"confidence",
		"suspect",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt never mentions %q", want)
		}
	}
}

// There is deliberately no default provider: this is the only memory
// operation that spends money, so an unconfigured repo is refused rather than
// guessed at — and the refusal has to say what to set.
func TestAnUnconfiguredProviderIsRefusedWithInstructions(t *testing.T) {
	err := curator.CheckProvider("")
	if !errors.Is(err, curator.ErrNoProvider) {
		t.Fatalf("error = %v, want ErrNoProvider", err)
	}
	for _, want := range []string{"memory.agent", "claudecode", "codex"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal never mentions %q: %v", want, err)
		}
	}
}

// A name the driver has no provider for is a gap to fill in the driver, so the
// refusal names the ones that exist rather than falling back to one.
func TestAnUnknownProviderNamesTheOnesThatExist(t *testing.T) {
	err := curator.CheckProvider("claude")
	if err == nil {
		t.Fatal("an unknown provider was accepted")
	}
	if !strings.Contains(err.Error(), "claudecode") {
		t.Errorf("the refusal does not name the real providers: %v", err)
	}
}

func TestEveryAdvertisedProviderResolves(t *testing.T) {
	for _, name := range curator.Providers {
		if err := curator.CheckProvider(name); err != nil {
			t.Errorf("advertised provider %q does not resolve: %v", name, err)
		}
	}
}

// A mode that waives prompting outranks the tool grant rather than combining
// with it, which would leave the grant as decoration and hollow out the
// enforcement claim the curator's design rests on.
func TestTheRunDoesNotWaivePermissionsWholesale(t *testing.T) {
	if mode := curator.PermissionMode(); mode == "bypassPermissions" || mode == "dontAsk" {
		t.Fatalf("permission mode %q voids the constructed tool grant", mode)
	}
}

// The grant is what makes the single-writer rule enforcement rather than
// instruction, so it has to reach the two commands that write to the store and
// stop well short of a blanket shell.
func TestTheToolGrantReachesTheStoreWritersAndNoFurther(t *testing.T) {
	granted := curator.AllowedTools()

	for _, want := range []string{"Bash(agtk memory anchor*)", "Bash(agtk memory index*)"} {
		if !slices.Contains(granted, want) {
			t.Errorf("the curator cannot %s, so it cannot stamp or regenerate", want)
		}
	}
	for _, forbidden := range []string{"Bash", "Bash(*)", "*"} {
		if slices.Contains(granted, forbidden) {
			t.Errorf("the grant includes %q, which is every command", forbidden)
		}
	}
}
