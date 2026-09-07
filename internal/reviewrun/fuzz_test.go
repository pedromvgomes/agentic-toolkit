package reviewrun

import (
	"strings"
	"testing"
)

// The ls-tree parser reads a stream whose paths are written by the author of
// the branch under review, so its properties are asserted against arbitrary
// bytes rather than against the rows git happens to emit today.
//
// The seeds run as ordinary tests on every `go test`, so the cases that have
// already gone wrong stay covered without anyone running the fuzzer.
func FuzzParseLsTree(f *testing.F) {
	f.Add([]byte("100644 blob aaa\tmain.go\x00"))
	f.Add([]byte("100644 blob aaa\twe\nird.go\x00100644 blob bbb\tafter.go\x00"))
	f.Add([]byte("120000 blob aaa\tlink\x00160000 commit bbb\tsub\x00"))
	f.Add([]byte("garbage\x00100644 blob aaa\tkept.go\x00"))
	f.Add([]byte("100644 blob aaa\t\x00"))
	f.Add([]byte("\x00\x00\x00"))
	f.Add([]byte("100644 blob aaa\t../escape.go\x00"))
	f.Add([]byte("100644blob aaa\tx.go\x00"))
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, out []byte) {
		entries := parseLsTree(out)

		// No entry may be produced that the framing did not contain: one NUL
		// terminates one record, so more entries than separators means the
		// walk invented one.
		if sep := strings.Count(string(out), "\x00"); len(entries) > sep+1 {
			t.Fatalf("%d entries from %d separators", len(entries), sep)
		}

		for _, e := range entries {
			// Every field an entry carries is used to address a git object or
			// a path on disk, so none may be empty.
			if e.Mode == "" || e.Type == "" || e.SHA == "" || e.Path == "" {
				t.Fatalf("entry with an empty field: %+v", e)
			}
			// A NUL is the record separator. One inside a field means the
			// walk read across a boundary.
			for _, field := range []string{e.Mode, e.Type, e.SHA} {
				if strings.ContainsAny(field, "\x00 \t") {
					t.Fatalf("field %q carries a separator: %+v", field, e)
				}
			}
			if strings.ContainsRune(e.Path, 0) {
				t.Fatalf("path carries a NUL: %+v", e)
			}
		}

		// classify must reach a decision about every entry without panicking,
		// and must never route a path it would refuse into the write set.
		write, skipped := classify(entries)
		if len(write)+len(skipped) > len(entries) {
			t.Fatalf("classify produced %d+%d decisions for %d entries",
				len(write), len(skipped), len(entries))
		}
		for _, e := range write {
			if !safeRelPath(e.Path) {
				t.Fatalf("classify would write an unsafe path: %q", e.Path)
			}
			if isInstruction(e.Path) {
				t.Fatalf("classify would write an instruction file: %q", e.Path)
			}
			if e.Mode == modeSymlink || e.Mode == modeGitlink {
				t.Fatalf("classify would write mode %s: %q", e.Mode, e.Path)
			}
		}
	})
}
