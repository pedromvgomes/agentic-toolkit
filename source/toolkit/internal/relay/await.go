package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// pollInterval is how long Await waits between reads of the relay's runs.
const pollInterval = 5 * time.Second

// runName is the title a relay run shows for req: "<action> <repo>#<pr>", as
// in "run acme/widgets#42".
//
// This is the relay workflow's `run-name:`, which lives in the relay
// repository rather than here: `${{ inputs.action }} ${{ inputs.repo }}#${{
// inputs.pr }}`. The two have to agree. A relay whose runs are titled any other
// way is only ever found through the run GitHub names on dispatch; when GitHub
// names none, Await matches nothing and runs out its timeout.
func runName(req Request) string {
	return req.Action + " " + req.Repo + "#" + strconv.Itoa(req.PR)
}

// workflowRun is the part of a GitHub Actions run Await reads.
type workflowRun struct {
	ID           int64     `json:"id"`
	Status       string    `json:"status"`
	Conclusion   string    `json:"conclusion"`
	HTMLURL      string    `json:"html_url"`
	DisplayTitle string    `json:"display_title"`
	CreatedAt    time.Time `json:"created_at"`
}

// Await polls the run d started until it completes, and reports how it ended.
//
// A timeout of zero or less means DefaultTimeout. Cancelling ctx stops the
// wait at once, not at the next poll. A GitHub that fails a read with a 5xx or
// a dropped connection is read again within the timeout, because the relay's
// run goes on either way; a refusal of the token is final.
func Await(ctx context.Context, doer Doer, target Target, d Dispatched, timeout time.Duration) (Result, error) {
	return await(ctx, doer, target, d, timeout, pollInterval)
}

func await(ctx context.Context, doer Doer, target Target, d Dispatched, timeout, interval time.Duration) (Result, error) {
	if err := target.check(); err != nil {
		return Result{}, err
	}
	if err := d.Request.check(); err != nil {
		return Result{}, err
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name := runName(d.Request)
	id, url := d.RunID, d.URL
	var lastErr error
	for {
		run, found, err := readRun(waitCtx, doer, target, id, name, d.Since)
		switch {
		case err != nil && waitCtx.Err() != nil:
			// The wait ended mid-request; the stop below reports why.
		case err != nil:
			if final(err) {
				return Result{}, fmt.Errorf("read the relay run on %s: %w", target.Slug, err)
			}
			lastErr = err
		case found:
			id = run.ID
			if run.HTMLURL != "" {
				url = run.HTMLURL
			}
			if run.Status == "completed" {
				return Result{Conclusion: run.Conclusion, URL: url}, nil
			}
		}
		if sleep(waitCtx, interval) != nil {
			return Result{}, stopped(ctx, target, name, id, url, timeout, lastErr)
		}
	}
}

// readRun reads run id, or, while no run is known, looks for the one named
// name created no earlier than since.
func readRun(ctx context.Context, doer Doer, target Target, id int64, name string, since time.Time) (workflowRun, bool, error) {
	if id != 0 {
		var run workflowRun
		path := "/repos/" + target.Slug + "/actions/runs/" + strconv.FormatInt(id, 10)
		if err := call(ctx, doer, target.Token, http.MethodGet, path, nil, &run); err != nil {
			return workflowRun{}, false, err
		}
		return run, true, nil
	}

	// Filtered here rather than with the API's `created` qualifier: the page
	// is a workflow's most recent runs, and a filter that is wrong in Go fails
	// a test where a mistyped qualifier matches nothing until the timeout.
	var page struct {
		Runs []workflowRun `json:"workflow_runs"`
	}
	path := "/repos/" + target.Slug + "/actions/workflows/" + workflowFile + "/runs?per_page=100"
	if err := call(ctx, doer, target.Token, http.MethodGet, path, nil, &page); err != nil {
		return workflowRun{}, false, err
	}
	// GitHub reports created_at to the second, so a run started in the same
	// second as the dispatch reads as earlier than since unless since is cut
	// to the second too.
	cutoff := since.Truncate(time.Second)
	var match workflowRun
	for _, run := range page.Runs {
		if run.DisplayTitle != name || run.CreatedAt.Before(cutoff) {
			continue
		}
		// The earliest run after the dispatch is the likeliest to be its own
		// when two callers dispatch the same request at once.
		if match.ID == 0 || run.CreatedAt.Before(match.CreatedAt) ||
			(run.CreatedAt.Equal(match.CreatedAt) && run.ID < match.ID) {
			match = run
		}
	}
	return match, match.ID != 0, nil
}

// final reports whether a failed read is GitHub refusing the request, which
// asking again does not change, rather than GitHub or the network failing or
// a rate limit that clears on its own.
//
// A 429, or a 403 RetryAfter marks as a rate limit rather than an ordinary
// refusal, is read again within the wait's own timeout instead of ending it:
// the relay's run keeps going regardless, and giving up here would report a
// failure for a run that may still succeed.
func final(err error) bool {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.StatusCode == http.StatusTooManyRequests || (apiErr.StatusCode == http.StatusForbidden && !apiErr.RetryAfter.IsZero()) {
		return false
	}
	return apiErr.StatusCode < 500
}

// sleep waits d, or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// stopped says why a wait ended without the run completing: the caller
// cancelled it, or the timeout ran out before the run was found or finished.
func stopped(ctx context.Context, target Target, name string, id int64, url string, timeout time.Duration, lastErr error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stopped waiting for the relay run %q on %s: %w", name, target.Slug, err)
	}
	var err error
	if id == 0 {
		err = fmt.Errorf("no relay run %q appeared on %s within %s: %w", name, target.Slug, timeout, context.DeadlineExceeded)
	} else {
		run := url
		if run == "" {
			run = strconv.FormatInt(id, 10) + " on " + target.Slug
		}
		err = fmt.Errorf("the relay run %s did not complete within %s: %w", run, timeout, context.DeadlineExceeded)
	}
	if lastErr != nil {
		err = fmt.Errorf("%w; the last read failed: %v", err, lastErr)
	}
	return err
}
