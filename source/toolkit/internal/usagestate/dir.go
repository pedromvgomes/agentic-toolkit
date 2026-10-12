package usagestate

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// stateDirName is the agtk-owned directory under the XDG state root.
	stateDirName = "agentic-toolkit"

	// usageDirName holds the Collector's state inside stateDirName.
	usageDirName = "usage"

	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// Dir returns the directory holding the Collector's state:
// ${XDG_STATE_HOME:-~/.local/state}/agentic-toolkit/usage. It does not create it.
func Dir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("usagestate: locate home: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, stateDirName, usageDirName), nil
}

// ensureDir creates dir and any missing parents, owner-only. The state is
// agtk's own and nothing else needs to read it.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("usagestate: mkdir %s: %w", dir, err)
	}
	return nil
}
