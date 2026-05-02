// Package userconfig models the per-user agtk preferences file.
//
// Path: ${XDG_CONFIG_HOME:-~/.config}/agentic-toolkit/config.yaml
//
// Contents (current schema):
//
//	auto_update:
//	  enabled: true
//	  check_interval: 24h
//
// The config is optional: a missing file produces the default Config.
// Unknown keys are rejected so misspellings surface immediately rather
// than silently disabling features.
package userconfig

import "time"

// Config holds the user's CLI preferences.
type Config struct {
	AutoUpdate AutoUpdate `yaml:"auto_update"`
}

// AutoUpdate gates the background update-check goroutine and informs
// `agtk update`.
type AutoUpdate struct {
	// Enabled gates the background check. Default: true.
	Enabled bool `yaml:"enabled"`
	// CheckInterval is the minimum gap between live GitHub API calls.
	// Default: 24h.
	CheckInterval time.Duration `yaml:"check_interval"`
}

// Default returns the config that applies when no file exists.
func Default() Config {
	return Config{
		AutoUpdate: AutoUpdate{
			Enabled:       true,
			CheckInterval: 24 * time.Hour,
		},
	}
}
