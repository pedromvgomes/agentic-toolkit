package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

const (
	instructionsBeginMarker = "<!-- BEGIN AGTK MANAGED -->"
	instructionsEndMarker   = "<!-- END AGTK MANAGED -->"
)

// instructionsPath returns the absolute target path for CLAUDE.md.
func instructionsPath(roots scopeRoots) string {
	return filepath.Join(roots.ProjectRoot, "CLAUDE.md")
}

// renderInstructions writes the managed region of CLAUDE.md from the
// instruction definitions in plan. Layout:
//
//   - File doesn't exist + project scope + AGENTS.md exists → seed file
//     with `@<rel>` import then the managed block. AGENTS.md is searched
//     at StackDir first (next to the manifest) and falls back to
//     ProjectRoot. The seeded import is the path relative to
//     ProjectRoot (where CLAUDE.md is written), so the agent resolves
//     it correctly at runtime.
//   - File doesn't exist (any other case) → create with just the
//     managed block.
//   - File exists with markers → replace the region between markers,
//     preserve everything else verbatim.
//   - File exists without markers → append the managed block to the end
//     after a blank line, preserving existing content.
//
// No-op when plan has zero instructions AND no existing managed region
// (avoids creating empty files).
func renderInstructions(plan *resolver.Plan, roots scopeRoots, opts Options) error {
	var instructions []*definitions.Instruction
	for _, d := range plan.Definitions {
		if d.Category != definitions.CategoryInstruction {
			continue
		}
		instructions = append(instructions, d.Definition.(*definitions.Instruction))
	}

	target := instructionsPath(roots)
	existing, existsErr := os.ReadFile(target)
	exists := existsErr == nil

	managedBody := buildInstructionsRegion(instructions)

	if len(instructions) == 0 {
		// Nothing to render. If the file has an existing managed block,
		// strip it (kept tidy on definition removal). Otherwise leave
		// the file (or absence) alone.
		if !exists {
			return nil
		}
		updated, changed := removeManagedRegion(string(existing))
		if !changed {
			return nil
		}
		return os.WriteFile(target, []byte(updated), 0o644)
	}

	var newContent string
	switch {
	case !exists:
		seed := ""
		if roots.Scope == ScopeProject {
			if rel, ok := findAgentsImport(roots); ok {
				seed = "@" + rel + "\n\n"
			}
		}
		newContent = seed + managedBody + "\n"
	default:
		current := string(existing)
		if strings.Contains(current, instructionsBeginMarker) && strings.Contains(current, instructionsEndMarker) {
			newContent = replaceManagedRegion(current, managedBody)
		} else {
			joiner := "\n"
			if !strings.HasSuffix(current, "\n") {
				joiner = "\n\n"
			} else if !strings.HasSuffix(current, "\n\n") {
				joiner = "\n"
			}
			newContent = current + joiner + managedBody + "\n"
		}
	}

	if exists && string(existing) == newContent {
		if opts.Stdout != nil {
			fmt.Fprintf(opts.Stdout, "unchanged %s\n", target)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("claude: mkdir %s: %w", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(newContent), 0o644); err != nil {
		return fmt.Errorf("claude: write %s: %w", target, err)
	}
	if opts.Stdout != nil {
		fmt.Fprintf(opts.Stdout, "wrote %s\n", target)
	}
	return nil
}

// findAgentsImport locates an AGENTS.md to seed the @-import with and
// returns the path relative to ProjectRoot (where CLAUDE.md is being
// written). Search order: StackDir, then ProjectRoot. Returns ok=false
// when neither has an AGENTS.md or the relative path can't be computed.
func findAgentsImport(roots scopeRoots) (string, bool) {
	candidates := []string{roots.StackDir, roots.ProjectRoot}
	seen := map[string]bool{}
	for _, dir := range candidates {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		full := filepath.Join(dir, "AGENTS.md")
		if _, err := os.Stat(full); err != nil {
			continue
		}
		rel, err := filepath.Rel(roots.ProjectRoot, full)
		if err != nil {
			continue
		}
		return filepath.ToSlash(rel), true
	}
	return "", false
}

// buildInstructionsRegion concatenates instruction bodies inside the
// agtk managed markers. Each instruction is separated by a blank line.
// Order matches plan.Definitions (alphabetical by name within
// instruction category).
func buildInstructionsRegion(instructions []*definitions.Instruction) string {
	var b strings.Builder
	b.WriteString(instructionsBeginMarker)
	b.WriteString("\n")
	for i, inst := range instructions {
		if i > 0 {
			b.WriteString("\n")
		}
		body := strings.TrimSpace(inst.Body)
		if body == "" {
			continue
		}
		b.WriteString(body)
		b.WriteString("\n")
	}
	b.WriteString(instructionsEndMarker)
	return b.String()
}

// replaceManagedRegion swaps out the existing managed block (markers
// inclusive) for newRegion. If markers are unbalanced, returns content
// unchanged.
func replaceManagedRegion(content, newRegion string) string {
	begin := strings.Index(content, instructionsBeginMarker)
	end := strings.Index(content, instructionsEndMarker)
	if begin < 0 || end < 0 || end < begin {
		return content
	}
	endLine := end + len(instructionsEndMarker)
	return content[:begin] + newRegion + content[endLine:]
}

// removeManagedRegion strips the managed block (and a single trailing
// newline if present) from content. Returns (updated, changed).
func removeManagedRegion(content string) (string, bool) {
	begin := strings.Index(content, instructionsBeginMarker)
	end := strings.Index(content, instructionsEndMarker)
	if begin < 0 || end < 0 || end < begin {
		return content, false
	}
	endLine := end + len(instructionsEndMarker)
	// Eat a single trailing newline introduced by the prior write.
	if endLine < len(content) && content[endLine] == '\n' {
		endLine++
	}
	// Eat the leading blank-line separator we wrote before the block,
	// if present.
	leading := begin
	if leading > 0 && content[leading-1] == '\n' {
		leading--
		if leading > 0 && content[leading-1] == '\n' {
			leading--
		}
	}
	updated := content[:leading] + content[endLine:]
	return updated, updated != content
}
