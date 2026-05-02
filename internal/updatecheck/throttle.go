package updatecheck

import (
	"strings"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/updatestate"
	"github.com/pedromvgomes/agentic-toolkit/internal/userconfig"
)

// Gate captures the inputs ShouldCheck needs to decide whether to
// perform a live network call.
type Gate struct {
	// Now is the wall-clock time. Tests inject; production uses
	// time.Now.
	Now time.Time
	// IsTerminal is true when stdout is a terminal. The CLI sets this
	// from term.IsTerminal(int(os.Stdout.Fd())) at startup.
	IsTerminal bool
	// CurrentVersion is what version.Current() reports.
	CurrentVersion string
	// Config is the user's auto-update preferences.
	Config userconfig.AutoUpdate
	// State is the persisted last-check / last-known-version pair.
	State updatestate.State
}

// ShouldCheck reports whether a live network query is warranted.
//
// All gates must hold:
//
//   - stdout is a terminal (so the result is actually surfaced)
//   - the binary has a real version (not "dev")
//   - the user hasn't disabled auto-update in config
//   - the throttle interval has elapsed since the last live check
func ShouldCheck(g Gate) bool {
	if !g.IsTerminal {
		return false
	}
	if g.CurrentVersion == "" || g.CurrentVersion == "dev" {
		return false
	}
	if !g.Config.Enabled {
		return false
	}
	interval := g.Config.CheckInterval
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if !g.State.LastUpdateCheck.IsZero() && g.Now.Sub(g.State.LastUpdateCheck) < interval {
		return false
	}
	return true
}

// IsNewer returns true when latest is a strictly higher version than
// current under the canonical "vX.Y.Z" semver-prefixed comparison. The
// goal is conservative: when in doubt (parse failure, equal strings,
// pre-release tag confusion), return false rather than misleading the
// user. Both arguments tolerate a leading "v".
func IsNewer(latest, current string) bool {
	if latest == "" || current == "" || latest == current {
		return false
	}
	a := parseSemver(latest)
	b := parseSemver(current)
	if a == nil || b == nil {
		// Parse failure: be conservative and don't claim newness.
		return false
	}
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parseSemver(s string) []int {
	s = strings.TrimPrefix(s, "v")
	// Drop any pre-release / build metadata suffix.
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, ok := atoi(p)
		if !ok {
			return nil
		}
		out[i] = n
	}
	return out
}

// atoi: small dependency-free positive-integer parser. Returns ok=false
// for empty, negative, or non-digit input.
func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
