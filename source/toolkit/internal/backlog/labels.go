// Package backlog reads a repository's backlog items from GitHub issues and
// decides whether one may be acted on.
//
// A backlog item's body chooses what an unattended session changes and which
// command it runs, so Verify answers trusted only when GitHub's own record
// shows that every hand that shaped the body has write access, a trusted
// actor queued it, and nobody changed it after that (ADR 0028). Everything
// here reads: nothing posts, labels or edits.
package backlog

// The canonical label names.
const (
	LabelStub       = "agtk:stub"
	LabelBlocked    = "agtk:blocked"
	LabelQueued     = "agtk:queued"
	LabelRunning    = "agtk:running"
	LabelInReview   = "agtk:in-review"
	LabelFailed     = "agtk:failed"
	LabelNeedsHuman = "agtk:needs-human"
)

// Label is one label of the canonical set.
type Label struct {
	Name string `json:"name"`
	// Color is the label's colour as six hex digits with no leading '#',
	// which is the form `gh label create --color` takes.
	Color       string `json:"color"`
	Description string `json:"description"`
}

// labels is the canonical set, in the order it is printed.
var labels = []Label{
	{Name: LabelStub, Color: "cfd3d7", Description: "Holds a goal and nothing to implement yet; never queued while it is a stub"},
	{Name: LabelBlocked, Color: "fbca04", Description: "Waiting on a predecessor that is not done"},
	{Name: LabelQueued, Color: "0e8a16", Description: "Ready for the factory to dispatch"},
	{Name: LabelRunning, Color: "1d76db", Description: "A session is working on it"},
	{Name: LabelInReview, Color: "5319e7", Description: "Its pull request is open and awaiting review"},
	{Name: LabelFailed, Color: "b60205", Description: "Its run ended without a pull request"},
	{Name: LabelNeedsHuman, Color: "d93f0b", Description: "Stopped until a person looks at it"},
}

// Labels returns the canonical label set. The slice is a copy, so a caller
// that changes it changes nothing anyone else reads.
func Labels() []Label {
	return append([]Label(nil), labels...)
}

// stateLabels are the labels that name an item's state. agtk:stub is not
// one: it is a flag an item carries beside its state.
var stateLabels = []string{LabelBlocked, LabelQueued, LabelRunning, LabelInReview, LabelFailed, LabelNeedsHuman}
