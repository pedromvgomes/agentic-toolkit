package codex

import (
	"fmt"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

// commandInvocationNotice opens every converted command's skill body.
// Codex has no frontmatter flag matching Claude's
// disable-model-invocation, so the restriction a command carries by
// being a command — a user runs it, the model does not reach for it —
// has to be said in the body or it is not said at all.
const commandInvocationNotice = "> Run this only when the user explicitly asks for it — do not invoke it on your own."

// renderCommandAsSkill renders a command as a Codex skill. Codex retired
// its own custom-prompt files, so a skill is the closest construct a
// command maps onto.
//
// Codex's SKILL.md frontmatter is name and description only. A command's
// argument hint has nowhere to go in it, so it is stated in the body
// alongside the invocation notice; model and tool allowlists have no
// Codex skill equivalent at all and are not rendered.
func renderCommandAsSkill(def definitions.Definition) ([]byte, error) {
	c := def.(*definitions.Command)
	type fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	var b strings.Builder
	b.WriteString(commandInvocationNotice)
	b.WriteString("\n")
	if c.ArgumentHint != "" {
		fmt.Fprintf(&b, ">\n> Arguments: %s\n", c.ArgumentHint)
	}
	if body := strings.TrimSpace(c.Body); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	return frontmatterPlusBody(fm{Name: c.Name, Description: c.Description}, b.String())
}
