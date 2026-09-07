package review

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultManifest is the review a repo gets when it declares none of its own.
// It ships with the binary rather than with the lockfile-pinned definitions,
// so `agtk code-review` works on a repo that has not rendered — including the
// repo that has just adopted the toolkit.
//
//go:embed default.yaml
var defaultManifest []byte

// BuiltinPrompts are the prompt bodies that ship with agtk, named by
// `builtin:<name>`.
//
// The list is validated when a manifest is read, so `builtin:corectness` is a
// refusal rather than a reviewer that starts with no instructions and answers
// anyway.
var BuiltinPrompts = []string{
	"unified",
	"correctness",
	"security",
	"performance",
	"judge",
	"validator",
}

// IsBuiltinPrompt reports whether name is one that ships with agtk.
func IsBuiltinPrompt(name string) bool {
	for _, p := range BuiltinPrompts {
		if p == name {
			return true
		}
	}
	return false
}

// DefaultManifest parses the embedded default.
//
// It is parsed rather than kept as a value so the default is held to the same
// validation every consumer's manifest is: a default that could not itself
// pass would be a rule the toolkit does not follow.
func DefaultManifest() (*Manifest, error) {
	return ParseBytes("<built-in default>", defaultManifest)
}

// DefaultManifestYAML is the embedded default's source, for `agtk
// code-review init` and for anyone who wants to start from it.
func DefaultManifestYAML() []byte { return defaultManifest }

// ManifestPath is where a repo's own manifest lives.
func ManifestPath(projectRoot string) string {
	return filepath.Join(projectRoot, filepath.FromSlash(ManifestDir), ManifestFile)
}

// Load returns the manifest governing projectRoot: the repo's own if it has
// one, the embedded default otherwise.
//
// builtin reports which was used, because a repo that believes it wrote a
// manifest and is being reviewed by the default needs to be told so — the two
// produce entirely different panels and nothing else in the output would say
// which was read.
func Load(projectRoot string) (m *Manifest, path string, builtin bool, err error) {
	path = ManifestPath(projectRoot)
	if _, statErr := os.Stat(path); statErr != nil {
		if !os.IsNotExist(statErr) {
			return nil, path, false, fmt.Errorf("read %s: %w", path, statErr)
		}
		m, err = DefaultManifest()
		return m, "", true, err
	}
	m, err = ParseFile(path)
	return m, path, false, err
}

// PromptSource describes where a runner's prompt body comes from, for
// --explain and for the run that will read it.
func (p PromptRef) PromptSource(projectRoot string) string {
	if p.IsBuiltin() {
		return "built-in " + p.Name
	}
	return filepath.Join(projectRoot, filepath.FromSlash(ManifestDir), filepath.FromSlash(strings.TrimPrefix(p.Path, "./")))
}
