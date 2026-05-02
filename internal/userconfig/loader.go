package userconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// FileName is the canonical leaf filename inside the user config dir.
const FileName = "config.yaml"

// DirName is the agtk-owned subdirectory under the XDG config root.
const DirName = "agentic-toolkit"

// Path returns the absolute path to the user config file. It honours
// XDG_CONFIG_HOME and falls back to ~/.config.
func Path() (string, error) {
	base, err := configBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, DirName, FileName), nil
}

// Load reads and parses the user config from its canonical path. A
// missing file returns Default() with no error. Any other read or
// parse error propagates.
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadFrom(p)
}

// LoadFrom reads and parses the user config from path. Tests use this
// to point at a tempdir-rooted config without setting XDG_CONFIG_HOME.
func LoadFrom(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("userconfig: read %s: %w", path, err)
	}
	cfg := Default()
	dec := yaml.NewDecoder(bytes.NewReader(raw), yaml.Strict())
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("userconfig: parse %s: %w", path, err)
	}
	return cfg, nil
}

// configBase returns the XDG config root for the current user.
func configBase() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("userconfig: locate home: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}
