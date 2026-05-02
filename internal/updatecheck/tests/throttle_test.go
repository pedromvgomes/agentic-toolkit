package tests

import (
	"context"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/updatecheck"
	"github.com/pedromvgomes/agentic-toolkit/internal/updatestate"
	"github.com/pedromvgomes/agentic-toolkit/internal/userconfig"
)

func baseGate() updatecheck.Gate {
	return updatecheck.Gate{
		Now:            time.Now(),
		IsTerminal:     true,
		CurrentVersion: "v1.0.0",
		Config:         userconfig.AutoUpdate{Enabled: true, CheckInterval: 24 * time.Hour},
	}
}

func TestShouldCheck_AllGatesOpen(t *testing.T) {
	if !updatecheck.ShouldCheck(baseGate()) {
		t.Errorf("expected ShouldCheck true with default-open gates")
	}
}

func TestShouldCheck_DevVersionSkipped(t *testing.T) {
	g := baseGate()
	g.CurrentVersion = "dev"
	if updatecheck.ShouldCheck(g) {
		t.Errorf("dev build should skip")
	}
}

func TestShouldCheck_NotTerminalSkipped(t *testing.T) {
	g := baseGate()
	g.IsTerminal = false
	if updatecheck.ShouldCheck(g) {
		t.Errorf("non-terminal stdout should skip")
	}
}

func TestShouldCheck_DisabledSkipped(t *testing.T) {
	g := baseGate()
	g.Config.Enabled = false
	if updatecheck.ShouldCheck(g) {
		t.Errorf("disabled config should skip")
	}
}

func TestShouldCheck_RecentCheckThrottled(t *testing.T) {
	g := baseGate()
	g.State = updatestate.State{LastUpdateCheck: g.Now.Add(-time.Hour)}
	if updatecheck.ShouldCheck(g) {
		t.Errorf("recent check inside interval should throttle")
	}
}

func TestShouldCheck_ExpiredIntervalAllows(t *testing.T) {
	g := baseGate()
	g.State = updatestate.State{LastUpdateCheck: g.Now.Add(-25 * time.Hour)}
	if !updatecheck.ShouldCheck(g) {
		t.Errorf("expired interval should allow check")
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v1.4.0", "v1.3.0", true},
		{"v1.4.0", "v1.4.0", false},
		{"v1.3.9", "v1.4.0", false},
		{"v1.4.1", "v1.4.0", true},
		{"v2.0.0", "v1.99.99", true},
		{"v1.4.0-rc1", "v1.3.0", true},
		{"garbage", "v1.0.0", false}, // conservative
	}
	for _, tc := range cases {
		if got := updatecheck.IsNewer(tc.latest, tc.current); got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

// stubProvider is a LatestVersionProvider that returns a canned tag.
type stubProvider struct {
	tag string
	err error
}

func (s *stubProvider) LatestVersion(_ context.Context) (string, error) {
	return s.tag, s.err
}

func TestChecker_PostsAvailableInfo(t *testing.T) {
	c := updatecheck.NewChecker(&stubProvider{tag: "v2.0.0"}, "v1.0.0")
	c.Start("")
	select {
	case info, ok := <-c.Result:
		if !ok {
			t.Fatal("Result closed without value")
		}
		if !info.Available || info.Latest != "v2.0.0" || info.Current != "v1.0.0" {
			t.Errorf("got %+v", info)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Checker did not return within 2s")
	}
}

func TestChecker_NetworkErrorSilent(t *testing.T) {
	c := updatecheck.NewChecker(&stubProvider{err: context.DeadlineExceeded}, "v1.0.0")
	c.Start("")
	select {
	case _, ok := <-c.Result:
		if ok {
			t.Error("expected channel close on error, got value")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Checker did not exit within 2s")
	}
}
