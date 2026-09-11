package tests

import (
	"io/fs"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/resolver"
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

// pdAgent builds a PlannedDefinition for a subagent. ext is nil when the
// test doesn't need any Codex-specific override.
func pdAgent(name, description, body, model, stackName string, ext *definitions.CodexAgentExt) resolver.PlannedDefinition {
	a := &definitions.Agent{
		Common: definitions.Common{Name: name, Description: description},
		Model:  model,
		Body:   body,
	}
	if ext != nil {
		a.Extensions.Codex = ext
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryAgent,
		Name:       name,
		Definition: a,
		StackName:  stackName,
	}
}

func pdCommand(name, description, body, argumentHint, stackName string) resolver.PlannedDefinition {
	c := &definitions.Command{
		Common:       definitions.Common{Name: name, Description: description},
		ArgumentHint: argumentHint,
		Body:         body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryCommand,
		Name:       name,
		Definition: c,
		StackName:  stackName,
	}
}

// pdMCP builds a PlannedDefinition for an MCP server. ext is nil when
// the test doesn't need any Codex-specific override.
func pdMCP(name string, transport definitions.Transport, m *definitions.MCPServer, ext *definitions.CodexMCPExt, stackName string) resolver.PlannedDefinition {
	m.Common = definitions.Common{Name: name, Description: name + " desc"}
	m.Transport = transport
	if ext != nil {
		m.Extensions.Codex = ext
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryMCP,
		Name:       name,
		Definition: m,
		StackName:  stackName,
	}
}

func pdHook(name, event, matcher string, handler definitions.HookHandler, timeout int, ext *definitions.CodexHookExt, stackName string) resolver.PlannedDefinition {
	h := &definitions.Hook{
		Common:  definitions.Common{Name: name, Description: name + " desc"},
		Event:   event,
		Matcher: matcher,
		Handler: handler,
		Timeout: timeout,
	}
	if ext != nil {
		h.Extensions.Codex = ext
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryHook,
		Name:       name,
		Definition: h,
		StackName:  stackName,
	}
}

func pdSetting(name string, value map[string]any, stackName string) resolver.PlannedDefinition {
	s := &definitions.Setting{
		Common: definitions.Common{Name: name, Description: name + " desc"},
		Value:  value,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategorySetting,
		Name:       name,
		Definition: s,
		StackName:  stackName,
	}
}
