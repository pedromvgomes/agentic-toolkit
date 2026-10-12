//go:build unix

package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/usagestate"
)

func TestDirReturnsNoDirectoryWhenTheHomeCannotBeLocated(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")

	got, err := usagestate.Dir()
	if err == nil {
		t.Fatalf("Dir = %q, want an error locating the home directory", got)
	}
	if got != "" {
		t.Errorf("Dir = %q beside an error, want no directory", got)
	}
}
