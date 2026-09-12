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
// last-known-version via updatestate.SaveTo(statePath, ...). On a network
// failure it writes nothing to Result (which is closed regardless, so
// consumers can use a non-blocking select) but still advances the
// persisted last-check, because the throttle counts attempts.
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
		// The throttle bounds network traffic, so it counts attempts. A
		// failure that leaves LastUpdateCheck untouched makes an offline or
		// rate-limited machine pay for a live call on every invocation —
		// exactly the traffic the interval exists to bound. The attempt
		// learns nothing about the latest release, so it advances the
		// timestamp over the persisted state rather than replacing it.
		recordAttempt(statePath, time.Now())
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

// recordAttempt advances LastUpdateCheck without disturbing the rest of
// the persisted state. Failures are dropped: throttle bookkeeping must
// never be louder than the check it is throttling.
func recordAttempt(statePath string, at time.Time) {
	if statePath == "" {
		return
	}
	st, err := updatestate.LoadFrom(statePath)
	if err != nil {
		// Unreadable state is not a reason to skip arming the throttle; a
		// state file nothing can parse would otherwise mean an uncapped
		// live call on every invocation.
		st = updatestate.State{}
	}
	st.LastUpdateCheck = at
	_ = updatestate.SaveTo(statePath, st)
}
