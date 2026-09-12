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
	granted := curator.AllowedTools("/opt/agtk", "/repo/.agents/memory/notes", "/repo/.agents/memory/candidates")

	for _, want := range []string{"Bash(/opt/agtk memory anchor*)", "Bash(/opt/agtk memory index*)"} {
		if !slices.Contains(granted, want) {
			t.Errorf("the curator cannot %s, so it cannot stamp or regenerate", want)
		}
	}
	for _, forbidden := range []string{"Bash", "Bash(*)", "*", "Bash(rm *)"} {
		if slices.Contains(granted, forbidden) {
			t.Errorf("the grant includes %q, which is far more than clearing the backlog", forbidden)
		}
	}
}

// Clearing the backlog and retracting a note are the only things the curator
// deletes, and each names its own directory. An unscoped `rm` would hand the
// one agent with a constructed grant the ability to remove anything in the
// repo — the guarantee this list exists to make, given away in its last line.
func TestTheDeletionGrantsReachOnlyTheStore(t *testing.T) {
	granted := curator.AllowedTools("/opt/agtk", "/repo/.agents/memory/notes", "/repo/.agents/memory/candidates")

	var deletions []string
	for _, tool := range granted {
		if strings.HasPrefix(tool, "Bash(rm") {
			deletions = append(deletions, tool)
		}
	}
	want := []string{
		"Bash(rm /repo/.agents/memory/candidates/*)",
		"Bash(rm /repo/.agents/memory/notes/*)",
	}
	if len(deletions) != len(want) {
		t.Fatalf("deletion grants = %v, want %v", deletions, want)
	}
	for _, w := range want {
		if !slices.Contains(deletions, w) {
			t.Errorf("deletion grants = %v, missing %q", deletions, w)
		}
	}
}

// The store is configurable, so a grant built around a hard-coded path would
// be wrong in exactly the repos that moved their store.
func TestTheDeletionGrantsFollowTheConfiguredStore(t *testing.T) {
	granted := curator.AllowedTools("/opt/agtk", "/elsewhere/docs/memory/notes", "/elsewhere/docs/memory/candidates")

	for _, tool := range granted {
		if !strings.HasPrefix(tool, "Bash(rm") {
			continue
		}
		if !strings.Contains(tool, "/elsewhere/docs/memory/") {
			t.Errorf("deletion grant %q ignores the configured store", tool)
		}
	}
}

// A consumer installs agtk separately from the lockfile-pinned definitions, so
// the agtk on PATH can be older than the build that started the run — old
// enough to lack `memory` entirely. A grant naming the bare name would let the
// curator verify everything and record none of it.
func TestTheGrantNamesTheRunningBinaryNotThePathName(t *testing.T) {
	granted := curator.AllowedTools("/opt/agtk-2.0", "/repo/notes", "/repo/candidates")

	for _, tool := range granted {
		if strings.HasPrefix(tool, "Bash(agtk ") {
			t.Errorf("grant %q names the bare `agtk`, which PATH may resolve to an older build", tool)
		}
	}
	if !slices.Contains(granted, "Bash(/opt/agtk-2.0 memory anchor*)") {
		t.Errorf("the grant does not let the curator stamp with the binary it was given: %v", granted)
	}
}

// Stamping does not only record hashes — it clears the staleness signal, which
// is the one thing that tells the next reader nobody has checked a claim. Bare
// `agtk memory anchor` stamps every note in the store, so a curator using it
// marks notes fresh that it never looked at, and no later audit flags them.
func TestThePromptForbidsStampingTheWholeStore(t *testing.T) {
	prompt := curator.Prompt()

	if !strings.Contains(prompt, "anchor <name>") {
		t.Error("the prompt does not tell the curator to stamp notes by name")
	}
	if !strings.Contains(prompt, "--all") {
		t.Error("the prompt does not warn against the whole-store escape hatch")
	}
}

// The trigger for a glob anchor is quantification, not multiplicity. A claim
// that says every, only or no X in a directory is falsified by a file that
// does not exist yet, and a per-file anchor can never notice one appearing —
// so the note stays green while the invariant it protects is gone.
func TestThePromptGlobsOnQuantificationRatherThanFileCount(t *testing.T) {
	prompt := curator.Prompt()

	if !strings.Contains(prompt, "quantif") {
		t.Error("the prompt does not name quantification as the glob trigger")
	}
	// The failure this rule exists to prevent is silent, so the prompt has to
	// say what goes wrong rather than only what to do.
	if !strings.Contains(prompt, "does not exist yet") {
		t.Error("the prompt does not say why a quantified claim needs a glob")
	}
	// Anchoring everything by directory trades a missed falsification for
	// staleness churn on every unrelated edit, which trains a reader to ignore
	// the one signal the scheme exists to preserve.
	if !strings.Contains(prompt, "is the default") {
		t.Error("the prompt does not keep per-file anchoring as the default")
	}
}
