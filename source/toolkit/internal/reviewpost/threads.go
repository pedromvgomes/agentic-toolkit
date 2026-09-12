package reviewpost

import (
	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// ReadThreads turns what GitHub reports about a pull request's comment threads
// into what a review run decides against.
//
// The translation lives beside the transport rather than in the pipeline, for
// the reason the pipeline holds no credential at all: internal/reviewrun never
// learns that GitHub exists, and a review of a local working tree reads no
// threads because there are none to read.
func ReadThreads(threads []githubapp.ReviewThread) reviewrun.Threads {
	out := make([]reviewrun.Thread, 0, len(threads))
	for _, t := range threads {
		thread := reviewrun.Thread{
			Path:     t.Path,
			Resolved: t.Resolved,
			Outdated: t.Outdated,
			Body:     t.Body,
		}
		// A fingerprint is an identity only in a comment the App itself wrote.
		// Anyone who can comment on a pull request can type the characters
		// that open a fingerprint marker, and one naming a finding's
		// fingerprint would withhold exactly that finding — which is a way to
		// make a review silent about code somebody chose, without touching the
		// code.
		if t.ByViewer {
			if version, fingerprint, ok := reviewrun.ParseFingerprintMarker(t.Body); ok {
				thread.Version, thread.Fingerprint = version, fingerprint
			}
		}
		out = append(out, thread)
	}
	return reviewrun.ThreadsRead(out)
}
