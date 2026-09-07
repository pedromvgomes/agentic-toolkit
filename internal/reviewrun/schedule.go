package reviewrun

import (
	"context"
	"sync"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// DefaultParallel is how many runs go at once on a provider that reports no
// limit of its own.
const DefaultParallel = 4

// scheduler bounds how many runs are in flight, globally and per provider.
//
// Two bounds rather than one because they answer different questions. The
// global one is the operator's — how much of this machine a review may use —
// and comes from --max-parallel. The per-provider one is the provider's, and
// is a correctness bound rather than a courtesy: codex reports one because its
// credential is a file it rewrites in place with single-use refresh tokens, so
// a second concurrent run does not merely queue, it invalidates the first.
type scheduler struct {
	global chan struct{}

	mu       sync.Mutex
	perName  map[string]chan struct{}
	resolver func(review.Runner) (int, error)
}

func newScheduler(maxParallel int, resolver func(review.Runner) (int, error)) *scheduler {
	if maxParallel < 1 {
		maxParallel = DefaultParallel
	}
	return &scheduler{
		global:   make(chan struct{}, maxParallel),
		perName:  map[string]chan struct{}{},
		resolver: resolver,
	}
}

// gate returns the per-provider semaphore for a runner, or nil when that
// provider reports no limit.
//
// The limit is asked once per provider and remembered. Asking builds a driver,
// and a review that asked per run would construct one for every instance of
// every reviewer to learn a fact that does not change during a review.
func (s *scheduler) gate(r review.Runner) (chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gate, seen := s.perName[r.Provider]; seen {
		return gate, nil
	}
	limit, err := s.resolver(r)
	if err != nil {
		return nil, err
	}
	var gate chan struct{}
	if limit > 0 {
		gate = make(chan struct{}, limit)
	}
	// A provider with no limit is remembered as nil, so the question is asked
	// once either way.
	s.perName[r.Provider] = gate
	return gate, nil
}

// acquire blocks until this run may proceed, and returns the release.
//
// The two semaphores are always taken in the same order — global, then
// provider — so two runs on different providers cannot each hold half of what
// the other needs.
func (s *scheduler) acquire(ctx context.Context, r review.Runner) (release func(), err error) {
	// Checked before the selects rather than only inside them. A select whose
	// send and whose <-ctx.Done() are both ready picks between them at random,
	// so a review cancelled while slots are free would still start runs — and
	// spend on them — for as long as the scheduler kept winning the coin toss.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	gate, err := s.gate(r)
	if err != nil {
		return nil, err
	}

	select {
	case s.global <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if gate == nil {
		return func() { <-s.global }, nil
	}

	select {
	case gate <- struct{}{}:
		return func() { <-gate; <-s.global }, nil
	case <-ctx.Done():
		<-s.global
		return nil, ctx.Err()
	}
}

// job is one scheduled model invocation.
//
// proto carries what is known before the run is made — its label, its role and
// what would run it — so a job that never starts still reports as itself
// rather than as a zero value.
type job struct {
	runner review.Runner
	proto  RunReport
	fn     func(context.Context) RunReport
}

// runAll makes every job, bounded by the scheduler, and returns the results in
// the order the jobs were given.
//
// Order is preserved so two reviews over the same change produce the same
// report. Scheduling is concurrent; reporting is not.
//
// A job that never acquires its slot reports that it could not answer, naming
// why. It must not come back as a zero value: an empty report is a run that
// found nothing, and "the review was cancelled" and "the code is clean" are
// the two readings this package exists to keep apart.
func (s *scheduler) runAll(ctx context.Context, jobs []job) []RunReport {
	out := make([]RunReport, len(jobs))
	var wg sync.WaitGroup
	for i := range jobs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			release, err := s.acquire(ctx, jobs[i].runner)
			if err != nil {
				out[i] = jobs[i].proto
				out[i].Report = Unavailable("%s was never started: %v", jobs[i].proto.Label, err)
				return
			}
			defer release()
			out[i] = jobs[i].fn(ctx)
		}(i)
	}
	wg.Wait()
	return out
}
