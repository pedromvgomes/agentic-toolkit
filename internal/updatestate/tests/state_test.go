package tests

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/updatestate"
)

func TestRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "state.yaml")

	now := time.Now().UTC().Truncate(time.Second)
	in := updatestate.State{LastUpdateCheck: now, LatestKnownVersion: "v1.4.0"}
	if err := updatestate.SaveTo(path, in); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	out, err := updatestate.LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !out.LastUpdateCheck.Equal(in.LastUpdateCheck) {
		t.Errorf("LastUpdateCheck = %v, want %v", out.LastUpdateCheck, in.LastUpdateCheck)
	}
	if out.LatestKnownVersion != in.LatestKnownVersion {
		t.Errorf("LatestKnownVersion = %q, want %q", out.LatestKnownVersion, in.LatestKnownVersion)
	}
}

func TestLoadFrom_MissingIsZero(t *testing.T) {
	out, err := updatestate.LoadFrom(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !out.LastUpdateCheck.IsZero() {
		t.Errorf("expected zero time, got %v", out.LastUpdateCheck)
	}
	if out.LatestKnownVersion != "" {
		t.Errorf("expected empty version, got %q", out.LatestKnownVersion)
	}
}
