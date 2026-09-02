package memory

import (
	"crypto/sha1" // #nosec G505 -- git's blob object id is sha1 by definition; not used for security
	"encoding/hex"
	"fmt"
	"os"
)

// BlobHashLen is how many hex characters of the blob id are stored. Full
// ids make a note's frontmatter unreadable in review once a glob expands to
// a dozen files; 12 characters is well past collision risk at repo scale.
// Both sides of every comparison are truncated identically, so the check is
// unaffected.
const BlobHashLen = 12

// BlobHash returns the git blob object id of content, truncated to
// BlobHashLen. Computing it in-process rather than shelling out to git
// keeps the store usable in a checkout with no history, keeps tests free of
// a git dependency, and — unlike `git ls-files -s`, which reports the index
// — reflects the working tree the agent is about to read.
func BlobHash(content []byte) string {
	// nosemgrep: go.lang.security.audit.crypto.use_of_weak_crypto.use-of-sha1
	h := sha1.New() // #nosec G401 -- git's object id format, not a signature
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))[:BlobHashLen]
}

// HashFile reads path and returns its blob id.
func HashFile(path string) (string, error) {
	content, err := os.ReadFile(path) // #nosec G304 -- hashes the anchored file the note itself names
	if err != nil {
		return "", err
	}
	return BlobHash(content), nil
}
