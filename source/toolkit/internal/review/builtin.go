package review

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
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
	m, err := ParseBytes("<built-in default>", defaultManifest)
	if err != nil {
		return nil, err
	}
	m.Builtin = true
	m.Dir = ManifestDir
	return m, nil
}

// DefaultManifestYAML is the embedded default's source, for `agtk
// code-review init` and for anyone who wants to start from it.
func DefaultManifestYAML() []byte { return defaultManifest }

// ManifestPath is where a repo's own manifest lives.
func ManifestPath(projectRoot string) string {
	return filepath.Join(projectRoot, filepath.FromSlash(ManifestDir), ManifestFile)
}

// ManifestRelPath is the manifest's location as git names it, from the repo
// root, with forward slashes on every platform.
const ManifestRelPath = ManifestDir + "/" + ManifestFile

// LegacyManifestRelPath is LegacyManifestDir's manifest as git names it.
const LegacyManifestRelPath = LegacyManifestDir + "/" + ManifestFile

// LegacyManifestPath is where a manifest sits if it was never moved out of
// LegacyManifestDir. Nothing loads from here; callers look for it so a repo
// mid-migration is told, rather than reviewed under the embedded default.
func LegacyManifestPath(projectRoot string) string {
	return filepath.Join(projectRoot, filepath.FromSlash(LegacyManifestRelPath))
}

// legacyManifestErr is the refusal a repo gets when the manifest in its
// working tree is at the path ManifestDir replaced.
//
// An absent manifest otherwise means "this repo declares none" and is
// reviewed under the embedded default. That is right for a repo that never
// wrote one and wrong for a repo whose manifest is sitting one directory
// away: the panels, the judge and the approval floor would all be the
// toolkit's rather than the repo's, and the only outward sign is a `builtin`
// flag nobody reads as an error.
//
// Only LoadAtRef's sibling refuses. A ref is history, and `git mv` cannot
// reach it — so a refusal there would name a remedy that does not exist for
// the one commit it fires on. LoadAtRef reads the older path instead.
func legacyManifestErr(path string) error {
	return &ParseError{
		Path: path,
		Kind: ErrLegacyManifestDir,
		Message: fmt.Sprintf(
			"review manifest found at %s, which agtk no longer reads; move it to %s (`git mv %s %s`)",
			LegacyManifestDir, ManifestDir, LegacyManifestDir, ManifestDir),
	}
}

// refuseIgnoredManifest reports a manifest in the working tree that git
// ignores, at either path a manifest is read from.
//
// Absence at the base ref otherwise means the repo declares none, and the
// branch writing a repo's first manifest is exactly that: on disk, absent
// from the base, and legitimately reviewed under the embedded default. What
// separates it from a mistake is the ignore rule. An ignored manifest cannot
// reach a ref however many times it is committed, so the fallback is not a
// one-off — the repo is reviewed under the toolkit's panels, judge and
// approval floor for as long as the rule stands, and a posted review says
// only `builtin`, which nobody reads as an error.
//
// A path that is merely untracked is left alone. It is indistinguishable from
// the adopting branch above, and refusing would make adopting a manifest
// impossible.
func refuseIgnoredManifest(dir string) error {
	for _, rel := range []string{ManifestRelPath, LegacyManifestRelPath} {
		if _, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); statErr != nil {
			continue
		}
		ignored, ok := gitIgnores(dir, rel)
		if ok && !ignored {
			continue
		}
		if !ok {
			// The same reasoning as the unresolvable ref above: an answer git
			// could not give must not take the path that changes nothing,
			// because that path is the silent one.
			return &ParseError{
				Path: rel,
				Kind: ErrIgnoredManifest,
				Message: fmt.Sprintf(
					"review manifest %s is in the working tree but absent from the base ref, and git "+
						"could not say whether it is ignored; refusing rather than reviewing under the "+
						"built-in default", rel),
			}
		}
		remedy := "un-ignore it and commit it"
		if rel == LegacyManifestRelPath {
			remedy = fmt.Sprintf(
				"it sits in a rendered tree that is ignored wholesale, so move it (`git mv %s %s`) and commit it",
				LegacyManifestDir, ManifestDir)
		}
		return &ParseError{
			Path: rel,
			Kind: ErrIgnoredManifest,
			Message: fmt.Sprintf(
				"review manifest %s is ignored by git, so no ref carries it and the review runs "+
					"under the built-in default; %s", rel, remedy),
		}
	}
	return nil
}

// LoadAtRef returns the manifest as it stands at a git ref.
//
// A review that posts reads its rules from the base ref, never from the tree
// under review: everything on the branch is written by its author, so a
// manifest read from the head would let a change name the reviewers that judge
// it. A ref with no manifest uses the embedded default, exactly as a repo with
// none does.
func LoadAtRef(dir, ref string) (m *Manifest, path string, builtin bool, err error) {
	// The ref is resolved before the path is read, because git answers both
	// "this ref has no such file" and "this ref does not exist" with the same
	// fatal status. Collapsing the two would let an unresolvable base ref —
	// an unfetched commit, a typo — quietly review under the built-in default
	// instead of the repo's own rules, with nothing in the output saying so.
	if _, _, err := gitStatus(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return nil, "", false, fmt.Errorf("resolve %s: %w", ref, err)
	}

	spec := ref + ":" + ManifestRelPath
	readFrom := ManifestDir
	if _, _, err := gitStatus(dir, "cat-file", "-e", spec); err != nil {
		// A ref predating the move still declares its rules, at the path that
		// was current when it was written. Honouring them is what reading
		// rules from the base means (ADR 0007); refusing would make the very
		// change that moves the manifest unreviewable, since the base it
		// merges into is always the older layout.
		legacy := ref + ":" + LegacyManifestRelPath
		if _, _, err := gitStatus(dir, "cat-file", "-e", legacy); err != nil {
			// The ref resolves and neither path is in it. That is the
			// embedded default's case — unless a manifest is sitting in the
			// working tree that git will never let into any ref.
			if ignoredErr := refuseIgnoredManifest(dir); ignoredErr != nil {
				return nil, "", false, ignoredErr
			}
			m, err = DefaultManifest()
			return m, "", true, err
		}
		spec, readFrom = legacy, LegacyManifestDir
	}

	raw, _, err := gitStatus(dir, "show", spec)
	if err != nil {
		return nil, "", false, err
	}
	m, err = ParseBytes(spec, raw)
	if m != nil {
		m.Dir = readFrom
	}
	return m, spec, false, err
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
		legacy := LegacyManifestPath(projectRoot)
		if _, legacyErr := os.Stat(legacy); legacyErr == nil {
			return nil, path, false, legacyManifestErr(legacy)
		}
		m, err = DefaultManifest()
		return m, "", true, err
	}
	m, err = ParseFile(path)
	if m != nil {
		m.Dir = ManifestDir
	}
	return m, path, false, err
}

// PromptSource describes where a runner's prompt body comes from, for
// --explain and for the run that will read it.
func (p PromptRef) PromptSource(projectRoot string) string {
	if p.IsBuiltin() {
		return "built-in " + p.Name
	}
	return filepath.Join(projectRoot, filepath.FromSlash(ManifestDir), filepath.FromSlash(p.Path))
}
