package review

// Four hand-written parsers read three git output formats, and every defect
// this package has shipped has been one of them believing its input. These
// targets feed them input nobody would write.
//
// In-package rather than under tests/, because two of the four parsers are
// unexported and exporting them to test them would widen the surface to suit
// the test rather than the caller.
//
// Each target asserts a property, not merely the absence of a panic. A parser
// that returns nothing for everything never panics and is also useless; what
// these hold is that whatever comes back is attributable to something that was
// actually in the input.

import (
	"strconv"
	"strings"
	"testing"
)

// FuzzParseNumstat holds the record shape: every file that comes back was
// named by the input, and a count that was reported is a count that parses.
func FuzzParseNumstat(f *testing.F) {
	f.Add("1\t2\tmain.go\x00")
	f.Add("4\t0\tod\td.go\x00")             // a tab inside a path
	f.Add("0\t0\t\x00old.go\x00new.go\x00") // a rename
	f.Add("-\t-\tlogo.png\x00")             // binary
	f.Add("1\t2\ta.go\x001\t2\tb.go\x00")   // two records
	f.Add("0\t0\t\x00only-one-path.go\x00") // a rename announced with one path
	f.Add("1\t2\t\n.go\x00")                // a newline inside a path
	f.Add("")
	f.Add("\x00\x00\x00")
	f.Add("999999999999999999999999\t0\ta.go\x00")

	f.Fuzz(func(t *testing.T, out string) {
		files, err := parseNumstat([]byte(out))
		if err != nil {
			if files != nil {
				t.Errorf("parseNumstat returned %d files alongside an error", len(files))
			}
			return
		}
		for _, file := range files {
			if file.Path == "" {
				t.Errorf("a file with no name came back from %q", out)
			}
			// Every name must appear verbatim in the input. A parser that
			// truncates a path at a separator inside it produces a name the
			// input never contained, which is how a tab in a filename made a
			// whole file unreadable.
			if !strings.Contains(out, file.Path) {
				t.Errorf("path %q is not present in the input %q", file.Path, out)
			}
			if file.OldPath != "" && !strings.Contains(out, file.OldPath) {
				t.Errorf("old path %q is not present in the input %q", file.OldPath, out)
			}
			if file.Added < 0 || file.Removed < 0 {
				t.Errorf("negative counts for %q: +%d -%d", file.Path, file.Added, file.Removed)
			}
			if file.Binary && (file.Added != 0 || file.Removed != 0) {
				t.Errorf("binary file %q reported line counts", file.Path)
			}
		}
	})
}

// FuzzWalkPatch holds attribution: a line is only ever delivered under a name
// the patch announced in a header, and never under one invented by its own
// content. Both defects this parser has had were violations of exactly that.
func FuzzWalkPatch(f *testing.F) {
	f.Add(samplePatch)
	f.Add(patchWithDeletion)
	f.Add(patchWithDecoyBodyLines)
	f.Add(patchWithQuotedName)
	f.Add("@@ -1 +1 @@\n+orphan line with no header\n")
	f.Add("+++ b/a.go\n")
	f.Add("--- \n+++ \n@@ -0,0 +1 @@\n+x\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, patch string) {
		announced := announcedPaths(patch)

		walkPatch(patch, func(file, line string) {
			if file == "" {
				t.Errorf("a line was delivered with no file: %q", line)
				return
			}
			if !announced[file] {
				t.Errorf("line %q delivered under %q, which no header announced; patch:\n%q", line, file, patch)
			}
		})
	})
}

// FuzzHunks holds the same attribution rule plus the arithmetic: a region
// addresses a real range, and `git log -L` is never asked for a nonsensical
// one.
func FuzzHunks(f *testing.F) {
	f.Add(samplePatch)
	f.Add(patchWithDeletion)
	f.Add(patchWithDecoyBodyLines)
	f.Add(patchWithQuotedName)
	f.Add("@@ -0,0 +1,2 @@\n")
	f.Add("--- a/x\n+++ b/x\n@@ -99999999999999999999,1 +1 @@\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, patch string) {
		announced := announcedPaths(patch)

		for _, h := range Hunks(patch) {
			if h.File == "" {
				t.Error("a hunk with no file")
				continue
			}
			if !announced[h.File] {
				t.Errorf("hunk attributed to %q, which no header announced; patch:\n%q", h.File, patch)
			}
			if h.Start < 0 || h.Length < 0 {
				t.Errorf("hunk %q addresses %d,%d", h.File, h.Start, h.Length)
			}
		}
	})
}

// FuzzParseBatchResponse holds the framing: a head only ever lands on a path
// that was asked for, and a forged record boundary can shift nothing onto a
// file that did not produce it.
func FuzzParseBatchResponse(f *testing.F) {
	f.Add("abc blob 5\x00hello\x00", 3)
	f.Add("HEAD:gone.go missing\x00abc blob 2\x00hi\x00", 2)
	f.Add("abc blob 999\x00short\x00", 2)
	f.Add("HEAD:a\nb.go missing\x00abc blob 2\x00hi\x00", 2)
	f.Add("abc blob -1\x00x\x00", 2)
	f.Add("", 1)
	f.Add("\x00\x00\x00\x00", 4)

	f.Fuzz(func(t *testing.T, out string, n int) {
		if n < 0 || n > 64 {
			return
		}
		requested := make([]string, n)
		for i := range requested {
			requested[i] = "path" + strconv.Itoa(i) + ".go"
		}

		heads := parseBatchResponse([]byte(out), requested, headBytes)

		want := make(map[string]bool, len(requested))
		for _, p := range requested {
			want[p] = true
		}
		for path, body := range heads {
			if !want[path] {
				t.Errorf("a head landed on %q, which was never requested", path)
			}
			if len(body) > headBytes {
				t.Errorf("head for %q is %d bytes, past the %d limit", path, len(body), headBytes)
			}
			// Every body is a run of bytes lifted from the response, so it
			// cannot contain anything the response did not.
			if !strings.Contains(out, body) {
				t.Errorf("head for %q is not a substring of the response", path)
			}
		}
		if len(heads) > len(requested) {
			t.Errorf("%d heads for %d requests", len(heads), len(requested))
		}
	})
}

// announcedPaths collects the names a patch's headers declare, by the same
// rule the parsers use — a section's own two sides — so the properties above
// test attribution rather than restating the implementation.
func announcedPaths(patch string) map[string]bool {
	out := map[string]bool{}
	var section diffSection
	for _, line := range strings.Split(patch, "\n") {
		if !section.track(line) {
			continue
		}
		if section.oldPath != "" {
			out[section.oldPath] = true
		}
		if section.newPath != "" {
			out[section.newPath] = true
		}
	}
	return out
}

const samplePatch = `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -3 +3,2 @@
-old
+new
+newer
`

const patchWithDeletion = `diff --git a/gone.go b/gone.go
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package gone
-var mu sync.Mutex
diff --git a/kept.go b/kept.go
--- a/kept.go
+++ b/kept.go
@@ -1 +1 @@
-a
+b
`

const patchWithDecoyBodyLines = `diff --git a/w.go b/w.go
--- a/w.go
+++ b/w.go
@@ -1,2 +1,3 @@
--- a sql comment
+++ b/hijacked
+var mu sync.Mutex
`

const patchWithQuotedName = `diff --git "a/od\td.go" "b/od\td.go"
--- "a/od\td.go"
+++ "b/od\td.go"
@@ -1 +1 @@
-a
+b
`
