package updatecheck

import (
	"context"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/updatestate"
)

// Checker spawns one live LatestVersion call in a goroutine and posts
// the result on Result. The CLI starts a Checker in PersistentPreRunE
// and consumes Result in PersistentPostRunE, with a non-blocking select
// so a slow network never delays the main command's exit.
type Checker struct {
	// Provider is the LatestVersionProvider to query. Required.
	Provider LatestVersionProvider
	// CurrentVersion is the version the running binary reports.
	CurrentVersion string
	// Timeout caps the total goroutine lifetime. 0 = 2s (matches the
	// default GitHubProvider HTTP timeout).
	Timeout time.Duration

	// Result receives at most one UpdateInfo per Checker. Closed when
	// the goroutine exits.
	Result chan UpdateInfo
}

// NewChecker constructs a Checker with a 1-buffered result channel.
func NewChecker(provider LatestVersionProvider, currentVersion string) *Checker {
	return &Checker{
		Provider:       provider,
		CurrentVersion: currentVersion,
		Timeout:        2 * time.Second,
		Result:         make(chan UpdateInfo, 1),
	}
}

// Start launches the goroutine. It returns immediately. On success the
// goroutine writes one UpdateInfo to Result and persists last-check and
// last-known-version via updatestate.SaveTo(statePath, ...). On any
// network failure the goroutine writes nothing (Result is closed
// regardless, so consumers can use a non-blocking select).
//
// statePath is the absolute path the goroutine should write throttle
// metadata to. Pass "" to skip persistence (used by `agtk update --check`
// which does its own I/O accounting).
func (c *Checker) Start(statePath string) {
	go c.run(statePath)
}

func (c *Checker) run(statePath string) {
	defer close(c.Result)
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	latest, err := c.Provider.LatestVersion(ctx)
	if err != nil {
		return
	}
	info := UpdateInfo{
		Current:   c.CurrentVersion,
		Latest:    latest,
		Available: IsNewer(latest, c.CurrentVersion),
	}
	if statePath != "" {
		_ = updatestate.SaveTo(statePath, updatestate.State{
			LastUpdateCheck:    time.Now(),
			LatestKnownVersion: latest,
		})
	}
	c.Result <- info
}
