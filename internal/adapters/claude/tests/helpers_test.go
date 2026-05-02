package tests

import (
	"io/fs"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/config"
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

// makePlan builds a resolver.Plan with the given definitions and preset
// list. Sources is empty (renderer doesn't consult it). Each definition
// must have its Common.Name set.
func makePlan(defs []resolver.PlannedDefinition, presets ...string) *resolver.Plan {
	return &resolver.Plan{
		Config: &config.ConsumerConfig{
			Source:  config.Source{URL: "fake://primary"},
			Presets: presets,
		},
		Definitions: defs,
	}
}

// pdSkill builds a PlannedDefinition for a skill bundle. fsys is rooted
// such that bundlePath/SKILL.md is the entry; companion files anywhere
// under bundlePath are copied.
func pdSkill(name, description, body, bundlePath, presetName string, fsys fs.FS) resolver.PlannedDefinition {
	s := &definitions.Skill{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategorySkill,
		Name:       name,
		Definition: s,
		PresetName: presetName,
		EntryPath:  bundlePath + "/SKILL.md",
		SourceFS:   fsys,
	}
}

func pdAgent(name, description, body, bundlePath, presetName string, fsys fs.FS) resolver.PlannedDefinition {
	a := &definitions.Agent{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryAgent,
		Name:       name,
		Definition: a,
		PresetName: presetName,
		EntryPath:  bundlePath + "/AGENT.md",
		SourceFS:   fsys,
	}
}

func pdCommand(name, description, body, presetName string) resolver.PlannedDefinition {
	c := &definitions.Command{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryCommand,
		Name:       name,
		Definition: c,
		PresetName: presetName,
	}
}

func pdRule(name, description, body, presetName string) resolver.PlannedDefinition {
	r := &definitions.Rule{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryRule,
		Name:       name,
		Definition: r,
		PresetName: presetName,
	}
}

func pdInstruction(name, description, body, presetName string) resolver.PlannedDefinition {
	i := &definitions.Instruction{
		Common: definitions.Common{Name: name, Description: description},
		Body:   body,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategoryInstruction,
		Name:       name,
		Definition: i,
		PresetName: presetName,
	}
}

func pdHook(name, description, event, matcher, command, presetName string) resolver.PlannedDefinition {
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
		PresetName: presetName,
	}
}

func pdMCPStdio(name, description, command string, args []string, presetName string) resolver.PlannedDefinition {
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
		PresetName: presetName,
	}
}

func pdSetting(name, description string, value map[string]any, presetName string) resolver.PlannedDefinition {
	s := &definitions.Setting{
		Common: definitions.Common{Name: name, Description: description},
		Value:  value,
	}
	return resolver.PlannedDefinition{
		Category:   definitions.CategorySetting,
		Name:       name,
		Definition: s,
		PresetName: presetName,
	}
}
