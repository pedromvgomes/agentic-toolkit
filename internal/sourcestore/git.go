package sourcestore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitResolveRef runs `git ls-remote --symref` against repoURL to map ref
// to (sha, resolvedRef). When ref is empty or "HEAD" the symref output
// is parsed to recover the actual default branch name.
//
// The returned resolvedRef is the human-readable name the caller should
// record in the lockfile: the input ref unchanged, or for HEAD/empty,
// the default branch with the `refs/heads/` prefix stripped.
func gitResolveRef(repoURL, ref string) (sha, resolvedRef string, err error) {
	probe := ref
	if probe == "" {
		probe = "HEAD"
	}
	out, err := runGit("", "ls-remote", "--symref", repoURL, probe)
	if err != nil {
		return "", "", err
	}
	var symref, firstSHA string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, "ref: ") {
			rest := strings.TrimPrefix(line, "ref: ")
			if tab := strings.IndexByte(rest, '\t'); tab > 0 {
				symref = rest[:tab]
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && firstSHA == "" {
			firstSHA = fields[0]
		}
	}
	if firstSHA == "" {
		return "", "", fmt.Errorf("ref %q not found at %s", probe, repoURL)
	}
	resolvedRef = ref
	if (ref == "" || ref == "HEAD") && symref != "" {
		resolvedRef = strings.TrimPrefix(symref, "refs/heads/")
	}
	return firstSHA, resolvedRef, nil
}

// gitFetch fetches ref from repoURL into a fresh worktree at dest, then
// verifies the checked-out commit equals expectedSHA. dest must not yet
// exist; the function creates it atomically via tmp+rename.
func gitFetch(repoURL, ref, expectedSHA, dest string) error {
	if ref == "" {
		ref = "HEAD"
	}
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", parent, err)
	}
	tmp, err := os.MkdirTemp(parent, ".tmp-fetch-*")
	if err != nil {
		return fmt.Errorf("mkdir tmp under %s: %w", parent, err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(tmp)
		}
	}()
	if _, err := runGit(tmp, "init", "--quiet"); err != nil {
		return err
	}
	if _, err := runGit(tmp, "fetch", "--quiet", "--depth", "1", repoURL, ref); err != nil {
		return fmt.Errorf("fetch %s ref %q: %w", repoURL, ref, err)
	}
	if _, err := runGit(tmp, "checkout", "--quiet", "FETCH_HEAD"); err != nil {
		return err
	}
	out, err := runGit(tmp, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	gotSHA := strings.TrimSpace(out)
	if gotSHA != expectedSHA {
		return fmt.Errorf("%w: fetched %s but lockfile pins %s for %s@%s",
			ErrSHAMismatch, gotSHA, expectedSHA, repoURL, ref)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, dest, err)
	}
	cleanup = false
	return nil
}

// runGit invokes `git <args...>`, optionally inside dir. Stdout is
// returned (untrimmed); stderr is folded into the returned error.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return stdout.String(), nil
}

// ErrSHAMismatch indicates the SHA recorded in the lockfile does not
// match the commit that the remote returned for the same ref. This is
// a hard failure: the caller should not silently proceed because the
// upstream history has been rewritten or the ref points elsewhere.
var ErrSHAMismatch = errors.New("sha mismatch")
