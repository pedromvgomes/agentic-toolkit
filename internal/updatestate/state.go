// Package updatestate persists the throttle and last-known-version
// metadata for the background update checker.
//
// Path: ${XDG_STATE_HOME:-~/.local/state}/agentic-toolkit/state.yaml
//
// Schema:
//
//	last_update_check: 2026-04-30T10:15:00Z
//	latest_known_version: v1.4.0
//
// Reads tolerate a missing file (returns the zero value State); writes
// create the directory tree on demand. The package is IO-only — the
// throttle decision lives in internal/updatecheck.
package updatestate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
)

// FileName is the canonical leaf filename inside the user state dir.
const FileName = "state.yaml"

// DirName is the agtk-owned subdirectory under the XDG state root.
const DirName = "agentic-toolkit"

// State is the persisted throttle metadata for auto-update.
type State struct {
	// LastUpdateCheck is the wall-clock time of the most recent live
	// GitHub API check. Zero when never run.
	LastUpdateCheck time.Time `yaml:"last_update_check,omitempty"`
	// LatestKnownVersion is the most recent release tag the checker
	// has observed. Empty when never run.
	LatestKnownVersion string `yaml:"latest_known_version,omitempty"`
}

// Path returns the absolute path to the user state file.
func Path() (string, error) {
	base, err := stateBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, DirName, FileName), nil
}

// Load returns the persisted State. A missing file returns the zero
// value with no error.
func Load() (State, error) {
	p, err := Path()
	if err != nil {
		return State{}, err
	}
	return LoadFrom(p)
}

// LoadFrom reads State from path. Tests use this to read a tempdir
// state file without exporting XDG_STATE_HOME.
func LoadFrom(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("updatestate: read %s: %w", path, err)
	}
	var st State
	if err := yaml.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("updatestate: parse %s: %w", path, err)
	}
	return st, nil
}

// Save writes st to its canonical path, creating the directory tree
// on demand.
func Save(st State) error {
	p, err := Path()
	if err != nil {
		return err
	}
	return SaveTo(p, st)
}

// SaveTo writes st to path, creating the parent directory if absent.
func SaveTo(path string, st State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("updatestate: mkdir %s: %w", filepath.Dir(path), err)
	}
	raw, err := yaml.Marshal(st)
	if err != nil {
		return fmt.Errorf("updatestate: marshal: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("updatestate: write %s: %w", path, err)
	}
	return nil
}

func stateBase() (string, error) {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("updatestate: locate home: %w", err)
	}
	return filepath.Join(home, ".local", "state"), nil
}
