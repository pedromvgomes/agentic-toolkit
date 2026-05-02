// Package updatecheck runs the background "is there a newer release?"
// query and returns a structured UpdateInfo when one arrives.
//
// The package is split into three pieces:
//
//   - LatestVersionProvider: the interface tests stub. The default
//     implementation hits GitHub's releases API.
//   - Throttle gates: ShouldCheck enforces "version != dev",
//     "auto_update.enabled", "now - last_check >= interval", and an
//     isatty hint passed by the caller.
//   - Checker: spawns a goroutine, posts UpdateInfo via channel.
//
// The CLI wires Checker.Start in PersistentPreRunE and consumes the
// channel in PersistentPostRunE; see internal/cli/root.go.
package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// UpdateInfo carries a single update-availability result back to the
// caller. Available is false when the latest release equals (or is
// older than) the running version.
type UpdateInfo struct {
	// Current is the version the running binary reports.
	Current string
	// Latest is the most recent release tag observed.
	Latest string
	// Available is true when Latest is strictly newer than Current.
	Available bool
}

// LatestVersionProvider returns the most recent release tag (e.g.
// "v1.4.0") for the configured repository. Tests stub this.
type LatestVersionProvider interface {
	LatestVersion(ctx context.Context) (string, error)
}

// GitHubProvider is the default LatestVersionProvider, hitting GitHub
// releases. Unauthenticated 60req/hr/IP is enough for personal use;
// auth is intentionally not implemented yet.
type GitHubProvider struct {
	// Owner and Repo identify the GitHub repository.
	Owner, Repo string
	// HTTPClient is used for the request. nil = http.DefaultClient with
	// a 2s timeout.
	HTTPClient *http.Client
}

// NewGitHubProvider returns a provider for owner/repo.
func NewGitHubProvider(owner, repo string) *GitHubProvider {
	return &GitHubProvider{
		Owner: owner,
		Repo:  repo,
		HTTPClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// LatestVersion fetches the most recent release tag.
func (p *GitHubProvider) LatestVersion(ctx context.Context) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", p.Owner, p.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("updatecheck: %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("updatecheck: %s: %s", url, resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("updatecheck: decode: %w", err)
	}
	if body.TagName == "" {
		return "", fmt.Errorf("updatecheck: %s: empty tag_name", url)
	}
	return body.TagName, nil
}
