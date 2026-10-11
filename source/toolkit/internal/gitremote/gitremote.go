// Package gitremote reads the GitHub repository a checkout's remote names.
package gitremote

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// DefaultRemote is the remote read when nobody names one.
const DefaultRemote = "origin"

// gitTimeout bounds the one git command this package runs, which only reads
// local configuration and so has no reason to wait.
const gitTimeout = 30 * time.Second

// Slug is a repository as GitHub addresses it.
type Slug struct {
	Owner string
	Repo  string
}

// String renders the slug the way GitHub writes it.
func (s Slug) String() string { return s.Owner + "/" + s.Repo }

// slugRE reads the owner and repository out of a remote URL, in either of the
// two forms git writes: an SSH scp-style address and an HTTPS URL. The `.git`
// suffix is optional because both forms are used without it.
//
// Anchored at the end rather than searched for, so a URL whose path holds more
// than two segments is refused instead of having its last two taken: a host
// that nests repositories is not github.com, and guessing there would address
// a repository nobody named.
//
// Both captures are held to the characters a GitHub account or repository name
// may actually contain. Accepting anything up to the next `/` would accept `?`,
// `#` and `%2F` — and the slug is interpolated into the API path, so any of the
// three re-points the request at a resource the remote does not name: `a?x=y/b`
// turns the rest of the path into a query, and `owner/repo#x` truncates it.
var slugRE = regexp.MustCompile(`^(?:(?:https?|ssh|git)://)?(?:[^@/]+@)?[^/:]+(?::\d+)?[:/]([A-Za-z0-9][A-Za-z0-9._-]*)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// RemoteSlug reads the owner and repository out of a remote's URL.
//
// Read from git rather than asked for, so the repository addressed is the one
// the checkout was cloned from, and a flag cannot let the two diverge silently.
func RemoteSlug(dir, remote string) (Slug, error) {
	if remote == "" {
		remote = DefaultRemote
	}
	// The configured URL rather than `git remote get-url`, which applies
	// url.<base>.insteadOf. That rewrite is about transport — an operator who
	// pushes over SSH what they cloned over HTTPS, or who abbreviates a host
	// — and it can turn a GitHub address into a local mirror path. Where the
	// bytes travel is git's business; which repository is addressed is the
	// address the remote was configured with.
	out, err := git(dir, "config", "--get", "remote."+remote+".url")
	if err != nil {
		return Slug{}, fmt.Errorf("read the %s remote: %w", remote, err)
	}
	url := strings.TrimSpace(out)
	m := slugRE.FindStringSubmatch(url)
	if m == nil {
		return Slug{}, fmt.Errorf("the %s remote is %q, which is not an owner/repository address", remote, url)
	}
	return Slug{Owner: m[1], Repo: m[2]}, nil
}

// git runs one git command in dir and returns its stdout. Standard input is
// closed and no terminal is attached, so a command that would prompt fails
// instead of waiting.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- arguments are built here from constants and a remote name
	cmd.Dir = dir
	cmd.Stdin = nil
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}
