package review

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// untrackedProbeBytes is how much of a file is read to decide whether it is
// text. Git's own heuristic looks at the head of a file for a NUL byte, and
// looking further would mean reading a gigabyte to classify it.
const untrackedProbeBytes = 8000

// collectUntracked reads the files git does not track into diff records and a
// synthetic patch.
//
// A new file has no pre-image, so every one of its lines is an addition and it
// has no hunk history — which is exactly what the record and the patch here
// say. Synthesising the patch rather than shelling out to `diff --no-index`
// per file keeps a change that adds two hundred files to one process.
func collectUntracked(dir string) ([]DiffFile, string, error) {
	names, err := UntrackedFiles(dir)
	if err != nil {
		return nil, "", err
	}

	var files []DiffFile
	var patch strings.Builder
	for _, name := range names {
		full := filepath.Join(dir, filepath.FromSlash(name))
		// Lstat rather than Stat: following a link would copy a file from
		// outside the repository into the patch every reviewer reads. The
		// entry is still recorded, with no content, because a change that adds
		// a link has added something and a profile that omitted it would
		// report the file as never having existed.
		info, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() {
			files = append(files, DiffFile{Path: name, Symlink: true})
			continue
		}
		body, err := os.ReadFile(full) // #nosec G304 -- a regular file git listed inside the repository
		if err != nil {
			// A file that vanished between the listing and the read is not a
			// change to report on, and is not worth failing a review over.
			continue
		}
		if isBinary(body) {
			files = append(files, DiffFile{Path: name, Binary: true})
			continue
		}

		lines := splitLines(string(body))
		files = append(files, DiffFile{Path: name, Added: len(lines)})

		fmt.Fprintf(&patch, "diff --git a/%s b/%s\n", name, name)
		fmt.Fprintf(&patch, "--- /dev/null\n+++ b/%s\n", name)
		fmt.Fprintf(&patch, "@@ -0,0 +1,%d @@\n", len(lines))
		for _, line := range lines {
			fmt.Fprintf(&patch, "+%s\n", line)
		}
	}
	return files, patch.String(), nil
}

// isBinary applies git's own test: a NUL byte near the head of a file.
func isBinary(body []byte) bool {
	head := body
	if len(head) > untrackedProbeBytes {
		head = head[:untrackedProbeBytes]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// splitLines splits a file into lines, without a trailing empty one for the
// final newline.
func splitLines(body string) []string {
	if body == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	return lines
}
