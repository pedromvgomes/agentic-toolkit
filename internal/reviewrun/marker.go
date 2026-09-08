package reviewrun

import (
	"fmt"
	"strings"
)

// FingerprintMarkerPrefix opens the HTML comment a posted inline comment
// carries its fingerprint in. Invisible in rendered markdown, so a later run
// reads identity back off the pull request rather than re-deriving it.
const FingerprintMarkerPrefix = "<!-- agtk:finding"

// fingerprintMarkerClose ends it.
const fingerprintMarkerClose = "-->"

// FingerprintMarker renders the fingerprint marker for one fingerprint.
//
// Both words, always: CONTEXT.md lists the bare noun under Signal's `_Avoid_`,
// and a property of a change that agtk detects and a comment that carries an
// identity are unrelated things.
//
// The version is part of what is written. A change to what is hashed makes
// every existing fingerprint marker mismatch, and without a version in it that
// reads as "every finding is new" rather than as "the scheme moved".
func FingerprintMarker(fingerprint string) string {
	return fmt.Sprintf("%s %s %s %s", FingerprintMarkerPrefix, FingerprintVersion, fingerprint, fingerprintMarkerClose)
}

// ParseFingerprintMarker reads the identity a posted comment carries.
//
// Rendering and reading are one file, because they are one format. Two
// definitions of where the version sits would drift, and the failure would be
// silent: an unreadable fingerprint marker suppresses nothing, which looks
// exactly like a pull request that carries nothing to suppress.
//
// A body carrying more than one well-formed fingerprint marker is refused
// rather than resolved by picking one. agtk writes exactly one and escapes the
// opening delimiter everywhere a model's own words are rendered, so a second
// one is something agtk did not write — and a reader that chose between them
// would be choosing which claim about identity to believe.
func ParseFingerprintMarker(body string) (version, fingerprint string, ok bool) {
	var found [][2]string
	for rest := body; ; {
		open := strings.Index(rest, FingerprintMarkerPrefix)
		if open < 0 {
			break
		}
		rest = rest[open+len(FingerprintMarkerPrefix):]
		end := strings.Index(rest, fingerprintMarkerClose)
		if end < 0 {
			break
		}
		fields := strings.Fields(rest[:end])
		rest = rest[end+len(fingerprintMarkerClose):]
		if len(fields) != 2 {
			continue
		}
		found = append(found, [2]string{fields[0], fields[1]})
	}
	if len(found) != 1 {
		return "", "", false
	}
	return found[0][0], found[0][1], true
}
