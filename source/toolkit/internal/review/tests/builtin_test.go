package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// The default is held to the same validation every consumer's manifest is. A
// default that could not itself pass would be a rule the toolkit does not
// follow.
func TestTheEmbeddedDefaultParses(t *testing.T) {
	m, err := review.DefaultManifest()
	if err != nil {
		t.Fatalf("DefaultManifest: %v", err)
	}
	if err := review.CheckCapabilities("<built-in default>", m); err != nil {
		t.Errorf("CheckCapabilities: %v", err)
	}
}

// The panels a default escalates between have to be ordered by what they
// spend, or "rules only raise" is a sentence rather than a property.
func TestTheDefaultPanelsAreOrderedByCost(t *testing.T) {
	m, err := review.DefaultManifest()
	if err != nil {
		t.Fatalf("DefaultManifest: %v", err)
	}
	quick, standard, deep := m.Panels["quick"].Cost(), m.Panels["standard"].Cost(), m.Panels["deep"].Cost()
	if !(quick < standard && standard < deep) {
		t.Errorf("costs quick=%d standard=%d deep=%d, want strictly increasing", quick, standard, deep)
	}
}

// A misspelled builtin prompt is a reviewer that would start with no
// instructions and answer anyway, so it is refused when the manifest is read.
func TestAMisspelledBuiltinPromptIsRefused(t *testing.T) {
	err := refuse(t, strings.Replace(complete, "builtin:correctness", "builtin:corectness", 1))

	if !review.IsKind(err, review.ErrInvalidPrompt) {
		t.Fatalf("kind = %v, want invalid_prompt", err)
	}
	for _, want := range []string{"corectness", "unified, correctness, security"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

// A repo-local prompt is resolved against the manifest's own directory, so a
// manifest and the prompts it names travel as one tree — which is what lets
// agtk read both from the base ref and a branch rewrite neither.
func TestARepoPromptStaysInsideTheManifestDirectory(t *testing.T) {
	for _, bad := range []string{"./../../etc/passwd", "./prompts/../../../x.md"} {
		t.Run(bad, func(t *testing.T) {
			err := refuse(t, strings.Replace(complete, "./prompts/perf.md", bad, 1))
			if !strings.Contains(err.Error(), ".agentic-toolkit/code-review") {
				t.Errorf("error = %q, want it to name the directory the path must stay inside", err)
			}
		})
	}
}

// Two forms and no third: a bare name would have to be resolved against
// something, and the lookup order would silently decide which body a reviewer
// got.
func TestABarePromptNameIsRefused(t *testing.T) {
	err := refuse(t, strings.Replace(complete, "builtin:correctness", "correctness", 1))

	if !strings.Contains(err.Error(), "neither `builtin:<name>` nor a path starting with `./`") {
		t.Errorf("error = %q", err)
	}
}

// A base ref that declares no manifest is the embedded default's case.
func TestLoadAtRefFallsBackWhenTheRefDeclaresNoManifest(t *testing.T) {
	r := newRepo(t)
	r.write("seed.txt", "x\n")
	rev := r.commit("base")

	m, path, builtin, err := review.LoadAtRef(r.dir, rev)
	if err != nil {
		t.Fatalf("LoadAtRef: %v", err)
	}
	if !builtin || path != "" {
		t.Errorf("LoadAtRef = (path %q, builtin %v), want the embedded default", path, builtin)
	}
	if m == nil || !m.Builtin {
		t.Error("the fallback manifest should be marked built-in")
	}
}

// A ref that does not resolve is a failure, not a repo declaring no manifest.
// Collapsing the two would quietly review an unfetched base under the default
// instead of the repo's own rules.
func TestLoadAtRefRefusesARefThatDoesNotResolve(t *testing.T) {
	r := newRepo(t)
	r.write("seed.txt", "x\n")
	r.commit("base")

	_, _, builtin, err := review.LoadAtRef(r.dir, "0000000000000000000000000000000000000001")
	if err == nil {
		t.Fatalf("LoadAtRef accepted a ref that does not exist (builtin=%v)", builtin)
	}
	if !strings.Contains(err.Error(), "resolve") {
		t.Errorf("error = %q, want it to say the ref could not be resolved", err)
	}
}

// A manifest at the path ManifestDir replaced is a refusal, not a repo
// declaring none. Reviewing it under the embedded default would swap the
// panels, the judge and the approval floor for the toolkit's own, and the
// only outward sign would be a `builtin` flag nothing reads as an error.
func TestLoadRefusesAManifestLeftAtTheLegacyPath(t *testing.T) {
	r := newRepo(t)
	r.write(review.LegacyManifestDir+"/manifest.yaml", complete)

	_, _, builtin, err := review.Load(r.dir)
	if err == nil {
		t.Fatalf("Load accepted a repo whose only manifest is at the legacy path (builtin=%v)", builtin)
	}
	if !review.IsKind(err, review.ErrLegacyManifestDir) {
		t.Errorf("error kind = %v, want ErrLegacyManifestDir (%q)", err, err)
	}
	if !strings.Contains(err.Error(), review.ManifestDir) {
		t.Errorf("error = %q, want it to name where the manifest belongs", err)
	}
}

// At a ref the older path is read rather than refused. A ref is history and
// `git mv` cannot reach it, so a refusal would name a remedy that does not
// exist — and the base of the very change that moves the manifest is always
// the older layout, which would make that change unreviewable.
func TestLoadAtRefReadsAManifestAtTheLegacyPath(t *testing.T) {
	r := newRepo(t)
	r.write(review.LegacyManifestDir+"/manifest.yaml", complete)
	rev := r.commit("base")

	m, path, builtin, err := review.LoadAtRef(r.dir, rev)
	if err != nil {
		t.Fatalf("LoadAtRef: %v", err)
	}
	if builtin || m == nil {
		t.Fatalf("LoadAtRef = (builtin %v), want the manifest the ref declares", builtin)
	}
	if !strings.Contains(path, review.LegacyManifestRelPath) {
		t.Errorf("path = %q, want the legacy manifest", path)
	}
	// Prompt bodies sit beside the manifest that names them, so the directory
	// travels with it.
	if m.Dir != review.LegacyManifestDir {
		t.Errorf("Dir = %q, want %q", m.Dir, review.LegacyManifestDir)
	}
}

// A repo that has moved its manifest and left the old copy behind is reviewed
// by the one agtk reads, not refused: the legacy path is consulted only when
// the current one holds nothing.
func TestTheCurrentPathWinsOverALeftoverLegacyCopy(t *testing.T) {
	r := newRepo(t)
	r.write(review.ManifestDir+"/manifest.yaml", complete)
	r.write(review.LegacyManifestDir+"/manifest.yaml", "version: 1\npanels: [not, a, map]\n")
	rev := r.commit("base")

	for _, tc := range []struct {
		name string
		load func() (*review.Manifest, string, bool, error)
	}{
		{"worktree", func() (*review.Manifest, string, bool, error) { return review.Load(r.dir) }},
		{"ref", func() (*review.Manifest, string, bool, error) { return review.LoadAtRef(r.dir, rev) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, path, builtin, err := tc.load()
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if builtin || m == nil {
				t.Errorf("load = (builtin %v), want the repo's own manifest", builtin)
			}
			if !strings.Contains(path, review.ManifestDir) {
				t.Errorf("path = %q, want the current manifest directory", path)
			}
			if m.Dir != review.ManifestDir {
				t.Errorf("Dir = %q, want %q", m.Dir, review.ManifestDir)
			}
		})
	}
}
