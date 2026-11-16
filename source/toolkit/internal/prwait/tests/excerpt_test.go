package tests

import (
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

func TestAThreadExcerptKeepsAHundredAndTwentyRunesWholeAndCutsOneMoreWithAnEllipsis(t *testing.T) {
	const limit = 120
	cases := []struct {
		name string
		body string
		want string
	}{
		{"at the limit", strings.Repeat("é", limit) + "\nmore", strings.Repeat("é", limit)},
		{"one over the limit", strings.Repeat("é", limit+1) + "\nmore", strings.Repeat("é", limit) + "…"},
	}
	for _, tc := range cases {
		reader := &fakeReader{states: []state{{
			pr:      open(headA),
			runs:    []githubapp.CheckRun{run("build", "completed", "success")},
			threads: []githubapp.ReviewThread{{Path: "a.go", Body: tc.body}},
		}}}
		res := wait(t, reader, newClock())
		if len(res.WaitingThreads) != 1 || res.WaitingThreads[0].Excerpt != tc.want {
			t.Errorf("%s: waiting = %+v, want excerpt %q", tc.name, res.WaitingThreads, tc.want)
		}
	}
}
