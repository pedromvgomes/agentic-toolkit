package reviewrun

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
)

// tracker records the high-water mark of concurrent runs per provider.
type tracker struct {
	mu      sync.Mutex
	inWork  map[string]int
	highest map[string]int
}

func newTracker() *tracker {
	return &tracker{inWork: map[string]int{}, highest: map[string]int{}}
}

func (tr *tracker) enter(provider string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.inWork[provider]++
	if tr.inWork[provider] > tr.highest[provider] {
		tr.highest[provider] = tr.inWork[provider]
	}
}

func (tr *tracker) leave(provider string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.inWork[provider]--
}

func (tr *tracker) peak(provider string) int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.highest[provider]
}

// runsFor builds n scheduled runs on one provider, each recording its own overlap.
func runsFor(tr *tracker, provider string, n int) []scheduled {
	runs := make([]scheduled, 0, n)
	for i := 0; i < n; i++ {
		runner := review.Runner{Provider: provider}
		runs = append(runs, scheduled{
			runner: runner,
			proto:  RunReport{Label: provider, Provider: provider},
			fn: func(context.Context) RunReport {
				tr.enter(provider)
				// Long enough that a scheduler which did not serialise would
				// reliably overlap, short enough not to slow the suite.
				time.Sleep(5 * time.Millisecond)
				tr.leave(provider)
				return RunReport{Label: provider, Provider: provider, Report: Answered(nil)}
			},
		})
	}
	return runs
}

// A provider whose credential cannot be shared must never have two runs in
// flight. On codex this is correctness, not courtesy: its refresh tokens are
// effectively single-use, so a second concurrent run invalidates the first.
func TestAProviderThatReportsALimitNeverHasTwoRunsInFlight(t *testing.T) {
	tr := newTracker()
	sched := newScheduler(8, func(r review.Runner) (int, error) {
		if r.Provider == "serialised" {
			return 1, nil
		}
		return 0, nil
	})

	reports := sched.runAll(context.Background(), runsFor(tr, "serialised", 6))

	if got := tr.peak("serialised"); got != 1 {
		t.Errorf("%d runs were in flight at once on a provider limited to 1", got)
	}
	for i, r := range reports {
		if !r.Report.Available {
			t.Errorf("run %d did not answer: %s", i, r.Report.Reason)
		}
	}
}

// A provider that reports no limit runs in parallel, bounded only by the
// operator's --max-parallel.
func TestAnUnconstrainedProviderRunsInParallelUpToTheGlobalBound(t *testing.T) {
	tr := newTracker()
	sched := newScheduler(3, func(review.Runner) (int, error) { return 0, nil })

	sched.runAll(context.Background(), runsFor(tr, "free", 12))

	peak := tr.peak("free")
	if peak > 3 {
		t.Errorf("%d runs were in flight against a global bound of 3", peak)
	}
	if peak < 2 {
		t.Errorf("nothing ran in parallel (peak %d); the global bound is not being used", peak)
	}
}

// The provider bound is asked once and remembered. Asking builds a driver, and
// a review that asked per run would construct one for every instance to learn
// a fact that does not change.
func TestTheProviderLimitIsAskedOncePerProvider(t *testing.T) {
	var asked int32
	sched := newScheduler(4, func(review.Runner) (int, error) {
		atomic.AddInt32(&asked, 1)
		return 0, nil
	})
	sched.runAll(context.Background(), runsFor(newTracker(), "free", 5))

	if n := atomic.LoadInt32(&asked); n != 1 {
		t.Errorf("the provider was asked its limit %d times, want 1", n)
	}
}

// Two providers are bounded independently: a serialised one must not stall a
// free one, and the free one must not lend its parallelism to the other.
//
// The global bound is deliberately lower than the run count, so it is a real
// constraint. A bound wide enough to admit every run at once cannot tell a
// scheduler that queues correctly from one that lets a serialised provider pin
// the whole budget, because nothing ever waits.
func TestProvidersAreBoundedIndependently(t *testing.T) {
	tr := newTracker()
	sched := newScheduler(4, func(r review.Runner) (int, error) {
		if r.Provider == "serialised" {
			return 1, nil
		}
		return 0, nil
	})

	jobs := append(runsFor(tr, "serialised", 12), runsFor(tr, "free", 4)...)
	sched.runAll(context.Background(), jobs)

	if got := tr.peak("serialised"); got != 1 {
		t.Errorf("the serialised provider peaked at %d", got)
	}
	// The global bound is 4 and one slot is held by whichever serialised run
	// is in flight, so an unconstrained provider must reach the remaining 3. A
	// run waiting on another provider's limit must not be holding a slot.
	if got := tr.peak("free"); got < 3 {
		t.Errorf("the free provider peaked at %d of an available 3; runs queued on the serialised provider's limit are pinning global slots", got)
	}
}

// A cancelled review must report its runs as unmade rather than as empty. An
// empty report is a run that found nothing, and "the review was cancelled" and
// "the code is clean" are the two readings this package exists to keep apart.
func TestACancelledRunReportsItselfRatherThanComingBackEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sched := newScheduler(1, func(review.Runner) (int, error) { return 0, nil })
	reports := sched.runAll(ctx, []scheduled{{
		runner: review.Runner{Provider: "free"},
		proto:  RunReport{Label: "security", Role: RoleReviewer, Provider: "free"},
		fn: func(context.Context) RunReport {
			t.Error("a run started under a cancelled context")
			return RunReport{}
		},
	}})

	if len(reports) != 1 {
		t.Fatalf("want one report, got %d", len(reports))
	}
	r := reports[0]
	if r.Report.Available {
		t.Fatal("a cancelled run reports itself as having answered")
	}
	if r.Label != "security" || r.Role != RoleReviewer {
		t.Errorf("a cancelled run came back as a zero value: %+v", r)
	}
	if r.Report.Reason == "" {
		t.Error("a cancelled run gives no reason")
	}
}

// A provider whose limit cannot be established is a run that cannot be
// scheduled safely, so it does not run.
func TestARunnerWhoseLimitCannotBeReadIsNotRun(t *testing.T) {
	sched := newScheduler(2, func(review.Runner) (int, error) {
		return 0, errors.New("the CLI is not installed")
	})
	reports := sched.runAll(context.Background(), []scheduled{{
		runner: review.Runner{Provider: "broken"},
		proto:  RunReport{Label: "security", Provider: "broken"},
		fn: func(context.Context) RunReport {
			t.Error("a run started though its provider limit could not be read")
			return RunReport{}
		},
	}})
	if reports[0].Report.Available {
		t.Fatal("a run with an unreadable limit reports as having answered")
	}
}

// An operator who names no bound gets the default rather than a review that
// makes every run at once.
func TestAnAbsentGlobalBoundBecomesTheDefault(t *testing.T) {
	for _, given := range []int{0, -1} {
		sched := newScheduler(given, func(review.Runner) (int, error) { return 0, nil })
		if got := cap(sched.global); got != DefaultParallel {
			t.Errorf("--max-parallel %d produced a bound of %d, want %d", given, got, DefaultParallel)
		}
	}
}

func TestSchedulingNothingIsHarmless(t *testing.T) {
	sched := newScheduler(4, func(review.Runner) (int, error) { return 0, nil })
	if got := sched.runAll(context.Background(), nil); len(got) != 0 {
		t.Errorf("runAll(nil) = %+v", got)
	}
}
