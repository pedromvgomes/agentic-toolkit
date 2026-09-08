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

// ReviewMarkerPrefix opens the HTML comment a posted review's body carries.
//
// Distinct from the fingerprint marker's prefix. One says "this comment is
// finding X"; the other says "this review of commit Y found these findings and
// did or did not reach a verdict", and a reader looking for one must never
// match the other.
const ReviewMarkerPrefix = "<!-- agtk:review"

// Keys the review marker carries beside its findings.
//
// A fingerprint is fingerprintWidth hex characters, so it can never be one of
// these: the grammar is a flat list of `key=value` tokens precisely because
// the two namespaces cannot collide.
const (
	// markerHeadKey names the commit the review was made against.
	markerHeadKey = "head"
	// markerVerdictKey says whether the run reached one.
	markerVerdictKey = "verdict"
	// markerUnanswerableKey lists the findings agtk could give nobody a thread
	// to answer on, so that approval does not demand an answer that cannot be
	// written.
	markerUnanswerableKey = "unanswerable"
	// markerDeadlockedKey lists the prompt-injection findings among those. A
	// deadlock is the right answer there and nowhere else: the material under
	// review addresses the reviewer, and the remedy is to change the code.
	markerDeadlockedKey = "deadlocked"
)

// Whether a run reached a verdict, as the marker spells it.
const (
	// VerdictComplete: the judge answered, every reviewer answered, and the
	// pull request's threads were readable.
	VerdictComplete = "complete"
	// VerdictIncomplete: one of those did not hold. What the missing part
	// would have found is unknown rather than absent, which is the one thing
	// approval must never read as a clean review.
	VerdictIncomplete = "incomplete"
)

// ReviewMarker is the record a posted review leaves of what it found.
//
// Nothing is persisted between runs — ids are per-run and the pull request is
// the only record — so the review says in its own body what approval later
// reads back out of it.
type ReviewMarker struct {
	// Head is the commit reviewed.
	Head string
	// Complete reports whether the run reached a verdict.
	Complete bool
	// Findings are the surviving findings, by fingerprint, in report order.
	Findings []MarkedFinding
}

// MarkedFinding is one surviving finding as the marker records it.
type MarkedFinding struct {
	Fingerprint string
	Severity    Severity
	// Answerable reports whether agtk gave this finding a thread. A finding
	// with none blocks nothing: a gate with no remedy is a deadlock rather
	// than a control, and a finding about code the change does not touch is
	// not a finding about the change.
	Answerable bool
	// Injected reports that this is a prompt-injection finding. One that is
	// also unanswerable refuses approval outright.
	Injected bool
}

// Render writes the marker.
func (m ReviewMarker) Render() string {
	verdict := VerdictIncomplete
	if m.Complete {
		verdict = VerdictComplete
	}
	fields := []string{
		FingerprintVersion,
		markerHeadKey + "=" + m.Head,
		markerVerdictKey + "=" + verdict,
	}
	var unanswerable, deadlocked []string
	for _, f := range m.Findings {
		fields = append(fields, f.Fingerprint+"="+string(f.Severity))
		if f.Answerable {
			continue
		}
		if f.Injected {
			deadlocked = append(deadlocked, f.Fingerprint)
			continue
		}
		unanswerable = append(unanswerable, f.Fingerprint)
	}
	// The lists come last so that the pairs read as the review's own account
	// of what it found, in the order it reported them, and the lists read as
	// what agtk could do about each.
	if len(unanswerable) > 0 {
		fields = append(fields, markerUnanswerableKey+"="+strings.Join(unanswerable, ","))
	}
	if len(deadlocked) > 0 {
		fields = append(fields, markerDeadlockedKey+"="+strings.Join(deadlocked, ","))
	}
	return ReviewMarkerPrefix + " " + strings.Join(fields, " ") + " " + fingerprintMarkerClose
}

// ParseReviewMarker reads the record a posted review's body carries.
//
// Rendered and read in one file, for the reason the fingerprint marker is: two
// definitions of one format drift, and the failure is silent. Here it is worse
// than silent — a marker that does not parse is a review approval cannot see,
// which refuses rather than approves, so the drift shows up as a gate nobody
// can pass and no error saying why.
//
// A body carrying more than one well-formed review marker is refused rather
// than resolved by picking one. agtk writes exactly one and escapes the
// opening delimiter everywhere a model's own words are rendered, so a second
// one is something agtk did not write.
func ParseReviewMarker(body string) (ReviewMarker, bool) {
	var found []ReviewMarker
	for rest := body; ; {
		open := strings.Index(rest, ReviewMarkerPrefix)
		if open < 0 {
			break
		}
		rest = rest[open+len(ReviewMarkerPrefix):]
		end := strings.Index(rest, fingerprintMarkerClose)
		if end < 0 {
			break
		}
		fields := strings.Fields(rest[:end])
		rest = rest[end+len(fingerprintMarkerClose):]
		if marker, ok := parseMarkerFields(fields); ok {
			found = append(found, marker)
		}
	}
	if len(found) != 1 {
		return ReviewMarker{}, false
	}
	return found[0], true
}

// parseMarkerFields reads one marker's tokens.
//
// A marker at another fingerprint scheme is refused rather than read. Its
// fingerprints were taken over different bytes, so they identify nothing here,
// and a finding approval could not match would be a finding it silently did
// not require an answer to.
func parseMarkerFields(fields []string) (ReviewMarker, bool) {
	if len(fields) < 3 || fields[0] != FingerprintVersion {
		return ReviewMarker{}, false
	}
	var (
		marker       ReviewMarker
		verdictSeen  bool
		unanswerable = map[string]bool{}
		deadlocked   = map[string]bool{}
	)
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return ReviewMarker{}, false
		}
		switch key {
		case markerHeadKey:
			marker.Head = value
		case markerVerdictKey:
			switch value {
			case VerdictComplete:
				marker.Complete = true
			case VerdictIncomplete:
				marker.Complete = false
			default:
				return ReviewMarker{}, false
			}
			verdictSeen = true
		case markerUnanswerableKey:
			for _, fp := range strings.Split(value, ",") {
				unanswerable[fp] = true
			}
		case markerDeadlockedKey:
			for _, fp := range strings.Split(value, ",") {
				deadlocked[fp] = true
			}
		default:
			severity := Severity(value)
			if !severity.Valid() {
				return ReviewMarker{}, false
			}
			marker.Findings = append(marker.Findings, MarkedFinding{
				Fingerprint: key, Severity: severity, Answerable: true,
			})
		}
	}
	if marker.Head == "" || !verdictSeen {
		return ReviewMarker{}, false
	}
	for i, f := range marker.Findings {
		if deadlocked[f.Fingerprint] {
			marker.Findings[i].Answerable, marker.Findings[i].Injected = false, true
			continue
		}
		if unanswerable[f.Fingerprint] {
			marker.Findings[i].Answerable = false
		}
	}
	return marker, true
}
