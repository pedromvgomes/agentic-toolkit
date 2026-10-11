package tests

import (
	"strings"
	"testing"
)

// The git instruction branches on AGTK_CLOUD. A cloud checkout has no .bare/
// and no .envrc, so without the branch the layout rule sends the agent to ask
// the user in every session; without the identity step the commits carry the
// platform's identity instead of the user's.
func TestTheGitInstructionBranchesOnAgtkCloud(t *testing.T) {
	apply := renderDefaultStack(t)
	claude := readRendered(t, apply, "CLAUDE.md")

	for _, want := range []string{
		"AGTK_CLOUD",
		"agtk cloud init",
		"git var GIT_COMMITTER_IDENT",
		"Do not hand the identity back",
		"1a. Working in a cloud session",
		"Claude-Session:",
	} {
		if !strings.Contains(claude, want) {
			t.Errorf("the rendered git instruction is missing %q", want)
		}
	}
}
