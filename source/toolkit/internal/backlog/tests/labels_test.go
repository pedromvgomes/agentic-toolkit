package tests

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/backlog"
)

var hexColour = regexp.MustCompile(`^[0-9a-f]{6}$`)

func TestTheLabelSetIsTheSevenCanonicalLabelsInOrder(t *testing.T) {
	var names []string
	for _, label := range backlog.Labels() {
		names = append(names, label.Name)
	}
	want := []string{
		backlog.LabelStub, backlog.LabelBlocked, backlog.LabelQueued, backlog.LabelRunning,
		backlog.LabelInReview, backlog.LabelFailed, backlog.LabelNeedsHuman,
	}
	if !slices.Equal(names, want) {
		t.Errorf("labels = %v, want %v", names, want)
	}
	for _, name := range want {
		if !strings.HasPrefix(name, "agtk:") {
			t.Errorf("%s is outside the agtk: prefix", name)
		}
	}
}

// gh label create --color takes six hex digits and refuses a leading '#'.
func TestEveryLabelHasAHexColourAndAOneLineDescription(t *testing.T) {
	for _, label := range backlog.Labels() {
		if !hexColour.MatchString(label.Color) {
			t.Errorf("%s colour %q is not six lower-case hex digits", label.Name, label.Color)
		}
		if strings.TrimSpace(label.Description) == "" || strings.ContainsAny(label.Description, "\r\n") {
			t.Errorf("%s description %q is not one non-empty line", label.Name, label.Description)
		}
		// GitHub refuses a label description longer than 100 characters.
		if len([]rune(label.Description)) > 100 {
			t.Errorf("%s description is %d characters, more than GitHub accepts", label.Name, len([]rune(label.Description)))
		}
	}
}

func TestChangingTheReturnedLabelsChangesNoLaterRead(t *testing.T) {
	first := backlog.Labels()
	first[0].Name = "agtk:tampered"
	if backlog.Labels()[0].Name != backlog.LabelStub {
		t.Error("a change to one caller's labels reached the next caller")
	}
}
