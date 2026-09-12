package tests

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/adapters/fsops"
	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// sourceFS is typed as the fs.FS interface, not a concrete fstest.MapFS,
// so that passing nil for the "no SourceFS" test case produces a true nil
// interface rather than a non-nil interface wrapping a nil map.
func planned(name, entryPath string, sourceFS fs.FS) resolver.PlannedDefinition {
	return resolver.PlannedDefinition{
		Category:   definitions.CategorySkill,
		Name:       name,
		EntryPath:  entryPath,
		SourceFS:   sourceFS,
		Definition: &definitions.Skill{Common: definitions.Common{Name: name, Description: "d"}},
	}
}

func renderEntry(def definitions.Definition) ([]byte, error) {
	return []byte("---\nname: " + def.GetCommon().Name + "\n---\nbody\n"), nil
}

func TestBuildBundleOps_CopiesCompanionsAndSkipsEntry(t *testing.T) {
	src := fstest.MapFS{
		"skills/foo/SKILL.md":       &fstest.MapFile{Data: []byte("ignored: rendered from Definition instead")},
		"skills/foo/scripts/run.sh": &fstest.MapFile{Data: []byte("#!/bin/sh\necho hi\n")},
	}
	d := planned("foo", "skills/foo/SKILL.md", src)

	ops, err := fsops.New("test").BuildBundleOps(d, "/root", "skills", "SKILL.md", renderEntry)
	if err != nil {
		t.Fatalf("BuildBundleOps: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("want 2 ops (entry + companion), got %d: %+v", len(ops), ops)
	}

	var entry, companion *fsops.WholeOp
	for i := range ops {
		switch ops[i].RelPath {
		case "skills/foo/SKILL.md":
			entry = &ops[i]
		case "skills/foo/scripts/run.sh":
			companion = &ops[i]
		}
	}
	if entry == nil || !strings.Contains(string(entry.Content), "name: foo") {
		t.Fatalf("entry op missing or not rendered from Definition: %+v", entry)
	}
	if companion == nil || string(companion.Content) != "#!/bin/sh\necho hi\n" {
		t.Fatalf("companion op missing or not copied verbatim: %+v", companion)
	}
	if entry.AbsPath != filepath.Join("/root", "skills", "foo", "SKILL.md") {
		t.Errorf("entry AbsPath = %q", entry.AbsPath)
	}
}

func TestBuildBundleOps_NilSourceFSSkipsCompanionsSilently(t *testing.T) {
	d := planned("foo", "skills/foo/SKILL.md", nil)
	ops, err := fsops.New("test").BuildBundleOps(d, "/root", "skills", "SKILL.md", renderEntry)
	if err != nil {
		t.Fatalf("BuildBundleOps: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("want just the entry op, got %d", len(ops))
	}
}

func TestSingleFileOp(t *testing.T) {
	op := fsops.SingleFileOp("/root", "rules", "foo.md", []byte("content"))
	if op.RelPath != "rules/foo.md" {
		t.Errorf("RelPath = %q", op.RelPath)
	}
	if op.AbsPath != filepath.Join("/root", "rules", "foo.md") {
		t.Errorf("AbsPath = %q", op.AbsPath)
	}
}

func TestDetectCollisions(t *testing.T) {
	tmp := t.TempDir()
	untracked := filepath.Join(tmp, "untracked.md")
	if err := os.WriteFile(untracked, []byte("hand-written"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracked := filepath.Join(tmp, "tracked.md")
	if err := os.WriteFile(tracked, []byte("agtk-owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := []fsops.WholeOp{
		{RelPath: "untracked.md", AbsPath: untracked},
		{RelPath: "tracked.md", AbsPath: tracked},
		{RelPath: "new.md", AbsPath: filepath.Join(tmp, "new.md")},
	}
	manifest := fsops.NewManifestState()
	manifest.Files["tracked.md"] = "somehash"

	errs := fsops.New("test").DetectCollisions(ops, manifest)
	if len(errs) != 1 {
		t.Fatalf("want exactly 1 collision, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), untracked) {
		t.Errorf("collision error should name the untracked file, got %q", errs[0])
	}
}

func TestApplyWholeOp_WritesAndSkipsUnchanged(t *testing.T) {
	tmp := t.TempDir()
	op := fsops.WholeOp{
		RelPath: "sub/file.md",
		AbsPath: filepath.Join(tmp, "sub", "file.md"),
		Content: []byte("hello"),
	}
	ops := fsops.New("test")
	m := fsops.NewManifestState()

	var out bytes.Buffer
	if err := ops.ApplyWholeOp(op, m, &out); err != nil {
		t.Fatalf("ApplyWholeOp: %v", err)
	}
	if !strings.Contains(out.String(), "wrote ") {
		t.Errorf("expected a wrote line, got %q", out.String())
	}
	got, err := os.ReadFile(op.AbsPath)
	if err != nil || string(got) != "hello" {
		t.Fatalf("file not written correctly: %v %q", err, got)
	}
	if m.Files["sub/file.md"] != fsops.ContentHash([]byte("hello")) {
		t.Errorf("manifest not updated with content hash")
	}

	out.Reset()
	if err := ops.ApplyWholeOp(op, m, &out); err != nil {
		t.Fatalf("ApplyWholeOp (repeat): %v", err)
	}
	if !strings.Contains(out.String(), "unchanged ") {
		t.Errorf("expected an unchanged line on identical re-apply, got %q", out.String())
	}
}

func TestRemoveStale(t *testing.T) {
	tmp := t.TempDir()
	stale := filepath.Join(tmp, "stale.md")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(tmp, "kept.md")
	if err := os.WriteFile(kept, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldManifest := fsops.NewManifestState()
	oldManifest.Files["stale.md"] = "h1"
	oldManifest.Files["kept.md"] = "h2"
	newManifest := fsops.NewManifestState()
	newManifest.Files["kept.md"] = "h2"

	var out bytes.Buffer
	errs := fsops.New("test").RemoveStale(tmp, oldManifest, newManifest, &out)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale.md should have been removed")
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("kept.md should still exist: %v", err)
	}
	if !strings.Contains(out.String(), "removed") {
		t.Errorf("expected a removed line, got %q", out.String())
	}
}

// TestRemoveStale_RefusesEntriesOutsideRoot: the manifest is committed,
// so its keys are input to a render rather than something the render
// wrote. An entry that resolves outside the root is reported and the
// file it names is left alone — otherwise checking out a branch and
// rendering deletes whatever the entry points at.
func TestRemoveStale_RefusesEntriesOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	outsider := filepath.Join(base, "outsider.md")
	if err := os.WriteFile(outsider, []byte("not agtk's"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldManifest := fsops.NewManifestState()
	oldManifest.Files["../outsider.md"] = "h1"
	oldManifest.Files[".."] = "h2"

	var out bytes.Buffer
	errs := fsops.New("test").RemoveStale(root, oldManifest, fsops.NewManifestState(), &out)
	if len(errs) != 2 {
		t.Fatalf("want one error per escaping entry, got %d: %v", len(errs), errs)
	}
	for _, err := range errs {
		if !strings.Contains(err.Error(), "resolves outside") {
			t.Errorf("error should say why it refused: %v", err)
		}
	}
	if _, err := os.Stat(outsider); err != nil {
		t.Errorf("a file outside the root was removed: %v", err)
	}
	if _, err := os.Stat(base); err != nil {
		t.Errorf("the root's parent was removed: %v", err)
	}
	if strings.Contains(out.String(), "removed") {
		t.Errorf("a refused entry was reported as removed: %q", out.String())
	}
}

// TestRemoveStale_RefusesEntriesBehindAnEscapingSymlink: `..` segments
// are one way out of the root and a symlinked ancestor is the other. A
// lexical check passes the second, and the removal then follows the
// link out of the repository — so containment is decided after
// resolving symlinks.
func TestRemoveStale_RefusesEntriesBehindAnEscapingSymlink(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{root, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	victim := filepath.Join(outside, "victim.md")
	if err := os.WriteFile(victim, []byte("not agtk's"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	oldManifest := fsops.NewManifestState()
	oldManifest.Files["escape/victim.md"] = "h1"

	errs := fsops.New("test").RemoveStale(root, oldManifest, fsops.NewManifestState(), nil)
	if len(errs) != 1 {
		t.Fatalf("want one refusal, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "resolves outside") {
		t.Errorf("error should say why it refused: %v", errs[0])
	}
	if _, err := os.Stat(victim); err != nil {
		t.Errorf("a file outside the root was deleted through a symlink: %v", err)
	}
}

// TestRemoveStale_FollowsSymlinksThatStayInside: the check refuses an
// escape, not a symlink. A consumer whose layout routes a directory
// through one still gets its stale files cleaned up.
func TestRemoveStale_FollowsSymlinksThatStayInside(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(real, "stale.md")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "via")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	oldManifest := fsops.NewManifestState()
	oldManifest.Files["via/stale.md"] = "h1"

	errs := fsops.New("test").RemoveStale(root, oldManifest, fsops.NewManifestState(), nil)
	if len(errs) != 0 {
		t.Fatalf("a symlink staying inside the root was refused: %v", errs)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file behind an internal symlink was not removed: %v", err)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	ops := fsops.New("test")

	empty, err := ops.ReadManifest(tmp)
	if err != nil {
		t.Fatalf("ReadManifest (missing): %v", err)
	}
	if len(empty.Files) != 0 {
		t.Errorf("missing manifest should read as empty, got %+v", empty)
	}

	m := fsops.NewManifestState()
	m.Files["a.md"] = "hash-a"
	if err := ops.WriteManifest(tmp, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	got, err := ops.ReadManifest(tmp)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.Files["a.md"] != "hash-a" {
		t.Errorf("round-tripped manifest = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(tmp, fsops.ManifestFileName)); err != nil {
		t.Errorf("manifest file not written at expected name: %v", err)
	}
}

func TestReportDryRunWholeOps(t *testing.T) {
	tmp := t.TempDir()
	existing := filepath.Join(tmp, "existing.md")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	ops := []fsops.WholeOp{
		{RelPath: "existing.md", AbsPath: existing, Content: []byte("new")},
		{RelPath: "brand-new.md", AbsPath: filepath.Join(tmp, "brand-new.md"), Content: []byte("x")},
	}
	manifest := fsops.NewManifestState()
	manifest.Files["existing.md"] = "irrelevant"
	manifest.Files["gone.md"] = "irrelevant"

	var out bytes.Buffer
	fsops.New("test").ReportDryRunWholeOps(&out, ops, tmp, manifest)
	got := out.String()

	for _, want := range []string{"would update", "would write", "would remove"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run report missing %q, got:\n%s", want, got)
		}
	}
	if _, err := os.ReadFile(existing); err != nil {
		t.Fatalf("dry-run must not touch the filesystem: %v", err)
	}
}
