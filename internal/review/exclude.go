package review

import (
	"path"
	"strings"
)

// Exclusion is why a changed file is not reviewable. The empty value means it
// is.
//
// All but one are mechanical: the file changed, but nobody wrote the change.
// Sizing a review on bulk nobody authored produces a deep panel for a
// dependency bump, and the panel then spends its budget reading a lockfile.
//
// ExcludedByManifest is the exception, and has its own value for exactly that
// reason. A repo excluding a hand-written fixture is making a different claim
// from "a generator wrote this", and a report that borrowed one of the
// mechanical reasons would state something false about a file somebody wrote.
type Exclusion string

const (
	// NotExcluded is a file that counts.
	NotExcluded Exclusion = ""
	// ExcludedByManifest is a path the repo's own manifest names. The only
	// reason here that a person chose rather than a tool detected.
	ExcludedByManifest Exclusion = "excluded by the manifest"
	// ExcludedLockfile is a dependency lock: authored by a resolver.
	ExcludedLockfile Exclusion = "lockfile"
	// ExcludedGenerated is output of a generator, by content marker, by name,
	// or by the repo's own .gitattributes.
	ExcludedGenerated Exclusion = "generated"
	// ExcludedVendored is a dependency tree committed into the repo.
	ExcludedVendored Exclusion = "vendored"
	// ExcludedBinary is a file git could not count lines in.
	ExcludedBinary Exclusion = "binary"
	// ExcludedPureRename is a file that moved and was not edited. The move is
	// mechanical; had it also been edited, its edits would count.
	ExcludedPureRename Exclusion = "pure rename"
	// ExcludedSymlink is a link. Its target is not read — it may be a file
	// outside the repository — but the link is part of the change and is
	// reported, because a change that adds one has added something.
	ExcludedSymlink Exclusion = "symlink"
)

// Reason renders the exclusion for --explain.
func (e Exclusion) Reason() string { return string(e) }

// lockfiles are dependency locks across the ecosystems, by exact basename.
// Matching the name rather than a pattern keeps `package.json` — which people
// write — apart from `package-lock.json`, which a resolver writes.
var lockfiles = map[string]bool{
	"go.sum":              true,
	"package-lock.json":   true,
	"npm-shrinkwrap.json": true,
	"yarn.lock":           true,
	"pnpm-lock.yaml":      true,
	"bun.lockb":           true,
	"Cargo.lock":          true,
	"poetry.lock":         true,
	"uv.lock":             true,
	"Pipfile.lock":        true,
	"pdm.lock":            true,
	"Gemfile.lock":        true,
	"composer.lock":       true,
	"gradle.lockfile":     true,
	"mix.lock":            true,
	"pubspec.lock":        true,
	"flake.lock":          true,
	"packages.lock.json":  true,
}

// vendorDirs are directory names that, anywhere in a path, mark a committed
// dependency tree.
var vendorDirs = map[string]bool{
	"vendor":        true,
	"node_modules":  true,
	"third_party":   true,
	"thirdparty":    true,
	"Godeps":        true,
	".venv":         true,
	"site-packages": true,
}

// generatedSuffixes name files by the convention their generator follows.
// This is the weakest of the three tests and deliberately last: a repo that
// marks its generated trees in .gitattributes is believed over any pattern
// written here.
var generatedSuffixes = []string{
	".pb.go",
	".pb.gw.go",
	"_pb2.py",
	"_pb2_grpc.py",
	".g.dart",
	".freezed.dart",
	".generated.go",
	".generated.ts",
	".gen.go",
	".gen.ts",
	"_generated.go",
	"_generated.ts",
	".designer.cs",
}

// Classify reports why a changed file is not reviewable, or NotExcluded.
//
// exclude are the globs the repo's manifest names. They are consulted first
// because they are the repo saying so outright, and because the reason
// reported has to be the one the reader can act on: a path the manifest names
// and a generator also wrote is excluded either way, and only one of the two
// reasons points at a line somebody can edit.
//
// attrGenerated names the paths the repo's own .gitattributes marks as
// generated or as not-diffable. The repo is a better authority on its own
// generated trees than any table here, so it is consulted next — but only as
// an addition, because the many repos that set no attributes at all would
// otherwise get no exclusions whatsoever.
func Classify(f DiffFile, attrGenerated map[string]bool, exclude []string) Exclusion {
	for _, pattern := range exclude {
		if MatchGlob(pattern, f.Path) {
			return ExcludedByManifest
		}
	}
	if attrGenerated[f.Path] {
		return ExcludedGenerated
	}
	if f.Symlink {
		return ExcludedSymlink
	}
	if f.Binary {
		return ExcludedBinary
	}
	if isVendored(f.Path) {
		return ExcludedVendored
	}
	if lockfiles[path.Base(f.Path)] {
		return ExcludedLockfile
	}
	for _, suffix := range generatedSuffixes {
		if strings.HasSuffix(f.Path, suffix) {
			return ExcludedGenerated
		}
	}
	// Last, because a file can be a pure rename and also a lockfile, and the
	// more specific reason is the more useful one to report.
	if f.Renamed() && f.Changed() == 0 {
		return ExcludedPureRename
	}
	return NotExcluded
}

// isVendored reports whether any path segment names a committed dependency
// tree.
func isVendored(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if vendorDirs[seg] {
			return true
		}
	}
	return false
}

// generatedMarkerPrefixes are the line prefixes a generator writes into its
// own output. Go's convention is exact and machine-checked; `@generated` is
// the convention the rest of the ecosystem converged on.
var generatedMarkerPrefixes = []string{
	"// Code generated ",
	"# Code generated ",
	"/* Code generated ",
	"@generated",
	"// @generated",
	"# @generated",
	"<!-- @generated",
}

// HasGeneratedMarker reports whether a file's opening lines declare it
// generated. Only the head of a file is read: the marker is a header by every
// convention that defines one, and scanning further would let a file that
// merely mentions the phrase exclude itself.
func HasGeneratedMarker(head string) bool {
	lines := strings.SplitN(head, "\n", generatedMarkerScanLines+1)
	for i, line := range lines {
		if i >= generatedMarkerScanLines {
			break
		}
		trimmed := strings.TrimSpace(line)
		for _, prefix := range generatedMarkerPrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				return true
			}
		}
	}
	return false
}

// generatedMarkerScanLines is how far into a file the marker may sit. Enough
// for a shebang, a licence header and a blank line before it.
const generatedMarkerScanLines = 20

// AttrGenerated asks git which of paths the repo marks as generated.
//
// Two attributes, because repos use both: `linguist-generated` is what GitHub
// reads to collapse a diff, and `-diff` is what git itself reads to stop
// producing one. Either is the repo saying nobody wrote this file.
//
// Each is honoured in both spellings git reports. `a.ts linguist-generated`
// yields the value `set`, while `a.ts linguist-generated=true` — the form
// Linguist's own documentation uses — yields `true`; a check that accepted
// only one of them would ignore half the repos that mark their generated
// trees.
func AttrGenerated(dir string, paths []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(paths) == 0 {
		return out, nil
	}
	args := append([]string{"check-attr", "-z", "linguist-generated", "diff", "--"}, paths...)
	raw, err := git(dir, args...)
	if err != nil {
		// A repo with no .gitattributes is the common case and not a failure;
		// so is a git too old for -z. Falling back to the built-in patterns
		// loses a repo's own opinion, which is worth reporting but never
		// worth refusing a review over.
		return out, err
	}

	// Records run path, attribute, value, repeating.
	fields := strings.Split(string(raw), "\x00")
	for i := 0; i+2 < len(fields); i += 3 {
		p, attr, value := fields[i], fields[i+1], fields[i+2]
		switch {
		case attr == "linguist-generated" && (value == "set" || value == "true"):
			out[p] = true
		case attr == "diff" && (value == "unset" || value == "false"):
			out[p] = true
		}
	}
	return out, nil
}
