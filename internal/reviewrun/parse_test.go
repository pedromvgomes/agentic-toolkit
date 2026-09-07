package reviewrun

import (
	"strings"
	"testing"
)

// record renders one ls-tree row, so a test says what it means rather than
// what the framing looks like.
func record(mode, typ, sha, path string) string {
	return mode + " " + typ + " " + sha + "\t" + path + "\x00"
}

func TestParseLsTreeReadsEveryField(t *testing.T) {
	out := record("100644", "blob", "aaa", "main.go") +
		record("100755", "blob", "bbb", "scripts/run.sh")

	got := parseLsTree([]byte(out))
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
	}
	if got[0] != (treeEntry{Mode: "100644", Type: "blob", SHA: "aaa", Path: "main.go"}) {
		t.Errorf("first entry: %+v", got[0])
	}
	if got[1].Mode != "100755" || got[1].Path != "scripts/run.sh" {
		t.Errorf("second entry: %+v", got[1])
	}
}

// A filename may contain a newline, and under a PR review the filename comes
// from a branch somebody else wrote. NUL framing is what stops one entry
// becoming two.
func TestParseLsTreeKeepsANewlineInAPathInOneEntry(t *testing.T) {
	out := record("100644", "blob", "aaa", "we\nird.go") +
		record("100644", "blob", "bbb", "after.go")

	got := parseLsTree([]byte(out))
	if len(got) != 2 {
		t.Fatalf("a newline in a path split the walk: %d entries: %+v", len(got), got)
	}
	if got[0].Path != "we\nird.go" {
		t.Errorf("path lost its newline: %q", got[0].Path)
	}
	if got[1].Path != "after.go" {
		t.Errorf("the entry after the newline is misread: %q", got[1].Path)
	}
}

// A malformed row costs itself and not the review: refusing the whole tree
// would let one crafted entry deny every reviewer their material.
func TestParseLsTreeSkipsAMalformedRowAndKeepsWalking(t *testing.T) {
	out := "not a tree row at all\x00" +
		record("100644", "blob", "aaa", "kept.go")

	got := parseLsTree([]byte(out))
	if len(got) != 1 || got[0].Path != "kept.go" {
		t.Fatalf("a malformed row ended the walk: %+v", got)
	}
}

func TestParseLsTreeSkipsARowWithNoPath(t *testing.T) {
	if got := parseLsTree([]byte("100644 blob aaa\t\x00")); len(got) != 0 {
		t.Fatalf("an empty path became an entry: %+v", got)
	}
}

func TestSafeRelPathRefusesEscapes(t *testing.T) {
	for _, p := range []string{
		"", "/etc/passwd", "../out.go", "a/../../b.go", "a//b.go", "./a.go",
		"a\x00b.go", "a\\b.go",
	} {
		if safeRelPath(p) {
			t.Errorf("%q is accepted as a path inside the review root", p)
		}
	}
	for _, p := range []string{"a.go", "a/b/c.go", "..hidden/x.go", "a..b/c.go"} {
		if !safeRelPath(p) {
			t.Errorf("%q is refused and is an ordinary path", p)
		}
	}
}

func TestIsInstructionCoversNamesAndDirectories(t *testing.T) {
	for _, p := range []string{
		"AGENTS.md", "nested/AGENTS.md", "AGENTS.override.md", "TEAM_GUIDE.md",
		".agents.md", "CLAUDE.md", "pkg/CLAUDE.md",
		".codex/config.toml", ".claude/settings.json", "sub/.claude/agents/x.md",
		".codex", ".claude",
	} {
		if !isInstruction(p) {
			t.Errorf("%q would be written into the review root", p)
		}
	}
	for _, p := range []string{
		"AGENTS.go", "docs/agents.md", "claude.go", "codexes/x.go", "my.claude/x",
	} {
		if isInstruction(p) {
			t.Errorf("%q is withheld and is ordinary source", p)
		}
	}
}

func TestClassifyRefusesSymlinksAndGitlinks(t *testing.T) {
	entries := parseLsTree([]byte(
		record("100644", "blob", "aaa", "main.go") +
			record(modeSymlink, "blob", "bbb", "link.go") +
			record(modeGitlink, "commit", "ccc", "vendor/dep") +
			record("100644", "blob", "ddd", "AGENTS.md") +
			record("100644", "blob", "eee", "../escape.go"),
	))

	write, skipped := classify(entries)
	if len(write) != 1 || write[0].Path != "main.go" {
		t.Fatalf("the review root would hold: %+v", write)
	}

	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Path] = s.Reason
	}
	for path, want := range map[string]string{
		"link.go":      SkipSymlink,
		"vendor/dep":   SkipGitlink,
		"AGENTS.md":    SkipInstructionFile,
		"../escape.go": SkipUnsafePath,
	} {
		if reasons[path] != want {
			t.Errorf("%s is skipped for %q, want %q", path, reasons[path], want)
		}
	}
}

// A record whose declared size runs past the response would otherwise slice
// out of bounds, or write one file's bytes under another file's name.
func TestSpreadBatchRefusesASizeThatOverrunsTheResponse(t *testing.T) {
	dir := t.TempDir()
	entries := []treeEntry{{Mode: "100644", Type: "blob", SHA: "aaa", Path: "a.go"}}
	err := spreadBatch([]byte("aaa blob 9999\nshort\n"), dir, entries)
	if err == nil {
		t.Fatal("an oversized record was accepted")
	}
	if !strings.Contains(err.Error(), "readable size") {
		t.Errorf("the refusal does not name the problem: %v", err)
	}
}

func TestSpreadBatchRefusesANegativeSize(t *testing.T) {
	entries := []treeEntry{{Mode: "100644", Type: "blob", SHA: "aaa", Path: "a.go"}}
	if err := spreadBatch([]byte("aaa blob -1\nx\n"), t.TempDir(), entries); err == nil {
		t.Fatal("a negative size was accepted")
	}
}

func TestSpreadBatchRefusesAMissingObject(t *testing.T) {
	entries := []treeEntry{{Mode: "100644", Type: "blob", SHA: "aaa", Path: "a.go"}}
	if err := spreadBatch([]byte("aaa missing\n"), t.TempDir(), entries); err == nil {
		t.Fatal("a missing object was accepted")
	}
}

func TestSpreadBatchRefusesAResponseThatEndsEarly(t *testing.T) {
	entries := []treeEntry{
		{Mode: "100644", Type: "blob", SHA: "aaa", Path: "a.go"},
		{Mode: "100644", Type: "blob", SHA: "bbb", Path: "b.go"},
	}
	err := spreadBatch([]byte("aaa blob 2\nhi\n"), t.TempDir(), entries)
	if err == nil {
		t.Fatal("a truncated response was accepted")
	}
	if !strings.Contains(err.Error(), "b.go") {
		t.Errorf("the refusal does not name the file it ran out on: %v", err)
	}
}

func TestWriteFileRefusesAPathThatLeavesTheRoot(t *testing.T) {
	err := writeFile(t.TempDir(), treeEntry{Mode: "100644", Path: "../escaped.go"}, []byte("x"))
	if err == nil {
		t.Fatal("a path outside the review root was written")
	}
}
