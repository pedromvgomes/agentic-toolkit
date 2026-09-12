package tests

import (
	"io/fs"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// makeFS is a terse fstest.MapFS constructor.
func makeFS(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for p, body := range files {
		out[p] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

// makePlan builds a resolver.Plan with the given definitions and stack
// order. Sources is empty (renderer doesn't consult it). Each definition
// must have its Common.Name set.
//
// stackOrder is the depth-first post-order list of stack identifiers the
// resolver visited; the last entry is the entry-point stack (identifier
// ""). Adapters use it for last-wins tiebreaking on settings fragments.
func makePlan(defs []resolver.PlannedDefinition, stackOrder ...string) *resolver.Plan {
	return &resolver.Plan{
		StackOrder:  stackOrder,
		Definitions: defs,
	}
}

// pdSkill builds a PlannedDefinition for a skill bundle. fsys is rooted
// at the bundle directory itself (SKILL.md at root); companion files
// anywhere under that root are copied verbatim by the adapter.
func pdSkill(name, description, body, bundlePath, stackName string, fsys fs.FS) resolver.PlannedDefinition {
	s := &definitions.Skill{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategorySkill,
		Name:       name,
		Definition: s,
		StackName:  stackName,
		EntryPath:  bundlePath + "/SKILL.md",
		SourceFS:   fsys,
	}
}

func pdAgent(name, description, body, bundlePath, stackName string, fsys fs.FS) resolver.PlannedDefinition {
	a := &definitions.Agent{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryAgent,
		Name:       name,
		Definition: a,
		StackName:  stackName,
		EntryPath:  bundlePath + "/AGENT.md",
		SourceFS:   fsys,
	}
}

func pdCommand(name, description, body, stackName string) resolver.PlannedDefinition {
	c := &definitions.Command{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryCommand,
		Name:       name,
		Definition: c,
		StackName:  stackName,
	}
}

func pdRule(name, description, body, stackName string) resolver.PlannedDefinition {
	r := &definitions.Rule{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryRule,
		Name:       name,
		Definition: r,
		StackName:  stackName,
	}
}

func pdInstruction(name, description, body, stackName string) resolver.PlannedDefinition {
	i := &definitions.Instruction{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryInstruction,
		Name:       name,
		Definition: i,
		StackName:  stackName,
	}
}

func pdHook(name, description, event, matcher, command, stackName string) resolver.PlannedDefinition {
	h := &definitions.Hook{
		Common:  definitions.Common{Name: name, Description: description},
		Event:   event,
		Matcher: matcher,
		Handler: definitions.HookHandler{Type: definitions.HandlerCommand, Command: command},
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryHook,
		Name:       name,
		Definition: h,
		StackName:  stackName,
	}
}

func pdMCPStdio(name, description, command string, args []string, stackName string) resolver.PlannedDefinition {
	m := &definitions.MCPServer{
		Common:    definitions.Common{Name: name, Description: description},
		Transport: definitions.TransportStdio,
		Command:   command,
		Args:      args,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryMCP,
		Name:       name,
		Definition: m,
		StackName:  stackName,
	}
}

func pdSetting(name, description string, value map[string]any, stackName string) resolver.PlannedDefinition {
	s := &definitions.Setting{
		Common: definitions.Common{Name: name, Description: description},
		Value:  value,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategorySetting,
		Name:       name,
		Definition: s,
		StackName:  stackName,
	}
}
