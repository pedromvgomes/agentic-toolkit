package tests

import (
	"strings"
	"testing"
)

// TestUpdate_DevBuild_ShortCircuits: under the test build (no ldflags),
// version.IsDev() is true. `agtk update --check` exits 0 with a clear
// message — no network call, no installer call.
func TestUpdate_DevBuild_ShortCircuits(t *testing.T) {
	stdout, _, err := runCLI(t, t.TempDir(), "update", "--check")
	if err != nil {
		t.Fatalf("update --check on dev build: %v", err)
	}
	if !strings.Contains(stdout, "dev build") {
		t.Errorf("expected dev-build notice in stdout: %q", stdout)
	}
}

func TestUpdate_DevBuild_InstallShortCircuits(t *testing.T) {
	stdout, _, err := runCLI(t, t.TempDir(), "update", "--yes")
	if err != nil {
		t.Fatalf("update --yes on dev build: %v", err)
	}
	if !strings.Contains(stdout, "dev build") {
		t.Errorf("expected dev-build notice in stdout: %q", stdout)
	}
}
