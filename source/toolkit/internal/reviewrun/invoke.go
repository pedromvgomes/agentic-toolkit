package reviewrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	agentic "github.com/pedromvgomes/agentic-driver"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/provider"
	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/review"
)

// invoker is how a run reaches a model.
//
// A seam rather than a direct call, because the orchestration this package
// exists for — a quorum in parallel, a semaphore per provider, a judge over
// what survived — is most of the logic and none of it needs a process to be
// exercised. agentictest.Fake writes one canned stdout and overwrites a single
// recording file, so it can neither script two different answers nor tell two
// concurrent runs apart; it still covers the single-run argv and confinement
// assertions, as it does for the curator.
type invoker interface {
	// Invoke makes one run. It returns an error only when the run could not
	// be carried out at all; a run that was made and did not answer comes
	// back as a Result with IsError set.
	Invoke(ctx context.Context, r review.Runner, req agentic.Request) (agentic.Result, error)
	// Limit reports how many runs may share this runner's provider
	// credential at once, or zero for no limit.
	Limit(r review.Runner) (int, error)
}

// driverInvoker is the production implementation: one driver per runner.
type driverInvoker struct {
	// binary pins the executable instead of resolving a name on PATH.
	binary string
}

// build constructs the driver for one runner.
func (d driverInvoker) build(r review.Runner, timeout time.Duration, workDir string) (*agentic.Driver, error) {
	p, err := provider.New(r.Provider)
	if err != nil {
		return nil, err
	}
	opts := []agentic.Option{
		agentic.WithWorkDir(workDir),
		agentic.WithTimeout(timeout),
	}
	if d.binary != "" {
		opts = append(opts, agentic.WithBinary(d.binary))
	}
	return agentic.New(p, opts...)
}

// Limit asks the provider how many of its runs may share one credential.
//
// Asked, never switched on by name. Codex answers one, because its credential
// is a file it rewrites in place and its refresh tokens are effectively
// single-use, so a second concurrent run invalidates the first — see ADR 0012.
// A provider with a static bearer token answers zero and its runs go in
// parallel.
func (d driverInvoker) Limit(r review.Runner) (int, error) {
	drv, err := d.build(r, time.Minute, "")
	if err != nil {
		return 0, err
	}
	return drv.MaxConcurrentRuns(), nil
}

func (d driverInvoker) Invoke(ctx context.Context, r review.Runner, req agentic.Request) (agentic.Result, error) {
	drv, err := d.build(r, req.Timeout, req.WorkDir)
	if err != nil {
		return agentic.Result{}, err
	}
	if err := drv.Ready(); err != nil {
		return agentic.Result{}, err
	}
	return drv.Run(ctx, req)
}

// request builds the driver request for one run.
//
// MaxTurns is always zero. Codex refuses a non-zero one outright — it has no
// configuration field for a turn bound, and the nearest-looking spelling is
// accepted and ignored — so a request carrying one would succeed on one
// provider and die at spawn on the other.
//
// Agents is nil and SessionID is empty, which is what makes the instances of a
// quorum independent: two runs that shared a session would agree because they
// were the same conversation, and agreement is the signal quorum exists to
// produce.
func request(r review.Runner, prompt string, schema json.RawMessage, root *Root, timeout time.Duration) (agentic.Request, error) {
	p, err := provider.New(r.Provider)
	if err != nil {
		return agentic.Request{}, err
	}
	b, err := provider.ReadOnly(p, review.ReadOnlyTools)
	if err != nil {
		return agentic.Request{}, err
	}
	return agentic.Request{
		Prompt:         prompt,
		Model:          r.Model,
		Schema:         schema,
		AllowedTools:   b.AllowedTools,
		PermissionMode: b.Mode,
		WorkDir:        root.Work,
		Timeout:        timeout,
	}, nil
}

// classify turns one driver outcome into a report.
//
// Five outcomes, four of which are "could not answer":
//
//   - an error is an outage: the run could not be carried out.
//   - Blocked is a run whose provider declined to serve the credential
//     rather than attempting and failing at it — a quota exhausted or a
//     credential rejected. Checked ahead of IsError, which the driver's
//     contract sets alongside it: this is the one outcome a caller may
//     route around by trying a different provider, and folding it into the
//     ordinary IsError case would lose the one signal that makes that
//     possible.
//   - IsError is a run that was made and did not answer — an unmet schema
//     constraint, a sandbox refusal, or the CLI declaring its own failure.
//     They are indistinguishable at this layer and Text carries whatever
//     account exists.
//   - a populated Structured is an answer.
//   - neither set cannot happen per the driver's contract, and is treated as
//     "could not answer" anyway. The alternative reading is "found nothing",
//     which is the one failure a review must never make silently.
func classify(res agentic.Result, err error, label string) (json.RawMessage, Report) {
	switch {
	case err != nil:
		return nil, Unavailable("%s could not be run: %v", label, err)
	case res.Blocked != nil:
		return nil, Blocked("%s's provider declined to serve the credential (%s): %s",
			label, res.Blocked.Reason, account(res.Text))
	case res.IsError:
		return nil, Unavailable("%s ran and did not answer: %s", label, account(res.Text))
	case len(res.Structured) == 0:
		return nil, Unavailable(
			"%s reported neither an answer nor a failure, which its driver says cannot happen; "+
				"treating it as unanswered rather than as a clean review", label)
	default:
		return res.Structured, Report{Available: true}
	}
}

// account trims a run's own explanation to something a terminal can hold,
// keeping the head, which is where a CLI puts its reason.
func account(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "it said nothing about why"
	}
	// Trimmed on a rune boundary. A byte slice through UTF-8 leaves an invalid
	// sequence in the reason a person reads and in the --json output, and the
	// text is a CLI's own error message, which is exactly where a non-ASCII
	// path or a localised message turns up.
	if len(text) > accountLimit {
		trimmed := text[:accountLimit]
		for len(trimmed) > 0 && !utf8.ValidString(trimmed) {
			trimmed = trimmed[:len(trimmed)-1]
		}
		return trimmed + "…"
	}
	return text
}

// accountLimit is how much of a failed run's explanation is repeated. Enough
// to name the cause, short enough that a stack trace does not become the
// report.
const accountLimit = 400

// decodeFindings turns a reviewer's structured answer into findings.
func decodeFindings(raw json.RawMessage, reviewer string) ([]Finding, error) {
	var answer reviewerAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("read %s's answer: %w", reviewer, err)
	}
	out := make([]Finding, 0, len(answer.Findings))
	for _, f := range answer.Findings {
		// A finding with no quote has no identity, and the schema compels the
		// quote. One that arrives without it is dropped rather than carried
		// with an empty fingerprint that would collide with every other.
		if strings.TrimSpace(f.Evidence) == "" {
			continue
		}
		severity := f.Severity
		if !severity.Valid() {
			severity = SeverityAmber
		}
		out = append(out, Finding{
			Reviewer:   reviewer,
			Path:       f.Path,
			StartLine:  f.StartLine,
			EndLine:    f.EndLine,
			Category:   f.Category,
			Severity:   severity,
			Confidence: f.Confidence,
			Issue:      f.Issue,
			Evidence:   f.Evidence,
			Suggestion: f.Suggestion,
		})
	}
	return out, nil
}
