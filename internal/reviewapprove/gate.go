package reviewapprove

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
	"github.com/pedromvgomes/agentic-toolkit/internal/review"
	"github.com/pedromvgomes/agentic-toolkit/internal/reviewrun"
)

// Refusal is one reason this head may not be approved.
//
// What is missing and what would answer it, separately, because a refusal that
// only names the state leaves the person who typed the command to guess the
// act — and the whole design rests on there being an act available for every
// refusal but one.
type Refusal struct {
	// Missing is what does not hold.
	Missing string
	// Remedy is what would make it hold, and is empty only for the deadlock
	// that has no remedy but a change to the code.
	Remedy string
}

// Inputs is everything the gate decides from.
//
// A value rather than a client, so the decision is a function of what the pull
// request says and nothing else: the reads happen once, in one place, and the
// gate cannot reach the network to answer a question it should have asked for.
type Inputs struct {
	// Number is the pull request, for a remedy that names the command.
	Number int
	// Head is the commit an approval would be bound to.
	Head string
	// Floor is the severity at or above which a finding obliges an answer.
	Floor review.Severity
	// Reviews are every review on the pull request, oldest first.
	Reviews []githubapp.SubmittedReview
	// Threads are every comment thread on it, with the replies on each.
	Threads []githubapp.AnsweredThread
}

// Check reports every reason this head may not be approved.
//
// Every reason rather than the first. Somebody who has to run the command five
// times to discover five refusals learns the gate is an obstacle; somebody
// told all five once can answer them.
//
// Nothing overrides what it returns. There is no flag that approves anyway,
// because one would make each of these a checklist rather than a control, and
// the person who would type it is the one the gate exists to slow down.
func Check(in Inputs) []Refusal {
	marker, found := lastReview(in.Reviews, in.Head)
	if !found {
		// Nothing else is knowable. A pull request whose head carries no
		// review of this installation's has no findings to weigh and no
		// verdict to read, and listing the threads it happens to carry
		// alongside that would suggest they were the obstacle.
		return []Refusal{{
			Missing: fmt.Sprintf("%s carries no review by this installation", in.Head),
			Remedy:  fmt.Sprintf("agtk code-review run --pr %d", in.Number),
		}}
	}

	var refusals []Refusal
	if !marker.Complete {
		refusals = append(refusals, Refusal{
			Missing: "that review did not reach a verdict, so what it did not find is unknown rather than absent",
			Remedy:  fmt.Sprintf("agtk code-review run --pr %d --force", in.Number),
		})
	}
	refusals = append(refusals, deadlocks(marker)...)
	refusals = append(refusals, unanswered(in, marker)...)
	refusals = append(refusals, unresolved(in.Threads)...)
	return refusals
}

// lastReview finds the newest review this installation posted against head,
// and reads back what it found.
//
// This installation's own, because anyone who can review a pull request can
// type the characters that open a review marker, and one claiming a clean
// review of the current head would be an approval somebody granted themselves
// by commenting.
//
// The newest, because a head re-reviewed with --force carries two, and the
// later one is what the pull request now says. The marker's own head is
// checked against the commit as well as GitHub's: a body can be edited after
// it is posted, and a marker describing another commit is not a review of this
// one whatever the review is attached to.
func lastReview(reviews []githubapp.SubmittedReview, head string) (reviewrun.ReviewMarker, bool) {
	var (
		found  reviewrun.ReviewMarker
		anyYet bool
	)
	for _, r := range reviews {
		if !r.ByViewer || r.CommitSHA != head {
			continue
		}
		marker, ok := reviewrun.ParseReviewMarker(r.Body)
		if !ok || marker.Head != head {
			continue
		}
		found, anyYet = marker, true
	}
	return found, anyYet
}

// deadlocks reports the prompt-injection findings agtk could give nobody a
// thread to answer on.
//
// The one refusal with no remedy in it. The material under review addresses
// the reviewer, there is nothing to reply to, and the way past is to remove
// the text — ADR 0007.
func deadlocks(marker reviewrun.ReviewMarker) []Refusal {
	var stuck []string
	for _, f := range marker.Findings {
		if f.Injected && !f.Answerable {
			stuck = append(stuck, f.Fingerprint)
		}
	}
	if len(stuck) == 0 {
		return nil
	}
	return []Refusal{{
		Missing: fmt.Sprintf("%d finding(s) quote an instruction addressed at the reviewer and name no path a comment can hang off (%s)",
			len(stuck), strings.Join(stuck, ", ")),
	}}
}

// unanswered reports the findings at or above the floor that nobody has
// answered.
//
// A finding is answered by changing the code, in which case the next review
// does not report it and it is not here, or by saying on its thread that it is
// not a defect. Those are the two ways, and both are acts on the pull request.
func unanswered(in Inputs, marker reviewrun.ReviewMarker) []Refusal {
	byFingerprint := agtkThreads(in.Threads)
	var refusals []Refusal
	for _, f := range marker.Findings {
		if !f.Answerable || !f.Severity.AtOrAbove(in.Floor) {
			continue
		}
		thread, carried := byFingerprint[f.Fingerprint]
		if !carried {
			// The review says it gave this finding a comment and the pull
			// request does not carry one, so the request that would have
			// opened the thread did not land. Nobody can answer a thread that
			// is not there, and re-posting it is the only act that helps.
			refusals = append(refusals, Refusal{
				Missing: fmt.Sprintf("%s %s carries no thread on this pull request, so there is nothing to answer on",
					f.Severity, f.Fingerprint),
				Remedy: fmt.Sprintf("agtk code-review run --pr %d --force", in.Number),
			})
			continue
		}
		if _, cleared := clearedBy(thread); cleared {
			continue
		}
		missing := fmt.Sprintf("%s %s on %s is neither fixed nor marked a false positive",
			f.Severity, f.Fingerprint, threadLocation(thread))
		if thread.Truncated {
			// A marking past the cut is a marking approval did not read, and
			// refusing on what it cannot see is the safe half of that.
			missing += ", and its replies were too many to read whole"
		}
		refusals = append(refusals, Refusal{
			Missing: missing,
			Remedy:  fmt.Sprintf("change the code, or reply on that thread with %q and a reason, from an account that can push", Marking),
		})
	}
	return refusals
}

// agtkThreads keys the threads this installation opened by the finding each
// carries.
//
// The root comment alone. A reply is written by whoever replied, so it may
// assert that a finding is wrong and may never assert which finding it is —
// and a fingerprint marker in a comment somebody else wrote is a claim about
// identity from an author who does not hold it.
func agtkThreads(threads []githubapp.AnsweredThread) map[string]githubapp.AnsweredThread {
	out := map[string]githubapp.AnsweredThread{}
	for _, thread := range threads {
		if !thread.ByViewer {
			continue
		}
		version, fingerprint, ok := reviewrun.ParseFingerprintMarker(thread.Body)
		if !ok || version != reviewrun.FingerprintVersion {
			continue
		}
		out[fingerprint] = thread
	}
	return out
}

// unresolved reports the comment threads nobody has closed.
//
// Every thread, not only the ones agtk opened. A question a reviewer asked and
// nobody answered is an open conversation about this change, and approving
// over it is the thing an approval is supposed to mean did not happen.
func unresolved(threads []githubapp.AnsweredThread) []Refusal {
	var open []string
	for _, thread := range threads {
		if !thread.Resolved {
			open = append(open, threadLocation(thread))
		}
	}
	if len(open) == 0 {
		return nil
	}
	sort.Strings(open)
	return []Refusal{{
		Missing: fmt.Sprintf("%d comment thread(s) are unresolved: %s", len(open), strings.Join(open, ", ")),
		Remedy:  "resolve each of them on the pull request",
	}}
}

// threadLocation names where a thread hangs, for a refusal somebody has to act
// on.
func threadLocation(thread githubapp.AnsweredThread) string {
	if thread.Path == "" {
		return "this pull request"
	}
	return thread.Path
}
