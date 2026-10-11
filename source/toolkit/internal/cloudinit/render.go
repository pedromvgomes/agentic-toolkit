package cloudinit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultRenderRoot is the directory a cloud container clones the session's
// repositories into, and the one Render searches when Options.RenderRoot is
// empty.
const DefaultRenderRoot = "/home/user"

// DefaultRenderTimeout bounds one checkout's render when
// Options.RenderTimeout is zero. A render against a committed lockfile
// resolves no ref, but on an empty cache it still clones every source at its
// pinned commit, so the bound leaves room for a clone and not only for
// writing files.
//
// Nothing bounds a run as a whole beyond its per-step limits: three
// ssh-keygen invocations at Timeout each, then one RenderTimeout per
// checkout. With the defaults and N checkouts that is 45s + N × 2m, which is
// the figure a hook running `agtk cloud init --render` sizes its own timeout
// from.
const DefaultRenderTimeout = 2 * time.Minute

// The files that make a directory a checkout Render considers. They match
// the names the agtk CLI reads.
const (
	ManifestFileName = ".agentic-toolkit.yaml"
	LockFileName     = ".agentic-toolkit.lock.yaml"
)

// renderWaitDelay bounds how long a render killed at its timeout may keep
// its output pipes open through a child of its own, such as a git clone.
const renderWaitDelay = 5 * time.Second

// renderCheckouts runs `agtk render` in every checkout under opts.RenderRoot.
// A checkout that fails does not stop the rest. Once every checkout was
// attempted, any failures come back as one error: a summary line counting
// them, then one indented line per failed checkout, in name order, naming
// its directory and render's reason. That error is the whole report of the
// failures; nothing about them is written anywhere else.
func renderCheckouts(ctx context.Context, opts Options) error {
	dirs, err := checkouts(ctx, opts.RenderRoot)
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		fmt.Fprintf(opts.Stdout, "agtk cloud init: no checkout under %s to render\n", opts.RenderRoot)
		return nil
	}
	agtk := opts.Executable
	if agtk == "" {
		if agtk, err = os.Executable(); err != nil {
			return fmt.Errorf("cloud init: locate the agtk binary: %w", err)
		}
	}
	var failures []string
	for _, dir := range dirs {
		if err := renderOne(ctx, agtk, dir, opts.RenderTimeout); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", dir, err))
			continue
		}
		fmt.Fprintf(opts.Stdout, "agtk cloud init: rendered %s\n", dir)
	}
	return renderFailures(failures, len(dirs))
}

// renderFailures builds renderCheckouts' error from one "<dir>: <reason>"
// entry per failed checkout, or returns nil when there are none.
func renderFailures(failures []string, attempted int) error {
	if len(failures) == 0 {
		return nil
	}
	return fmt.Errorf("cloud init: render failed in %d of %d checkouts:\n  %s",
		len(failures), attempted, strings.Join(failures, "\n  "))
}

// checkouts returns, in name order, each directory directly under root that
// renderable accepts. Only that one level is searched: a cloud container
// clones each repository straight into the root, and looking deeper would
// walk every directory of every checkout. A symlinked directory is not
// followed. A root that does not exist holds no checkouts.
func checkouts(ctx context.Context, root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cloud init: list %s: %w", root, err)
	}
	var dirs []string
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if e.IsDir() && renderable(ctx, dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

// renderable reports whether dir is the top level of a work tree with an
// entry manifest on disk and a lockfile committed at HEAD. A lockfile that
// is only on disk is skipped: its pins are not ones the repository committed
// to.
// A directory nested inside another work tree is skipped too, since HEAD's
// lockfile there is the enclosing checkout's and not its own.
func renderable(ctx context.Context, dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ManifestFileName))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	prefix, err := git(ctx, dir, "rev-parse", "--show-prefix")
	if err != nil || strings.TrimSpace(prefix) != "" {
		return false
	}
	_, err = git(ctx, dir, "cat-file", "-e", "HEAD:"+LockFileName)
	return err == nil
}

// renderOne runs `agtk render` in dir. It never runs `agtk sync`: sync
// relocks whenever the lockfile looks stale, resolving refs over the network
// and rendering whatever they point to at that moment, where render reads
// only the commits the checkout's committed lockfile pins.
//
// The child inherits this process's environment. Its output is captured: a
// successful render prints a line per file, and a failure is reported as one
// line. A captured stdout is not a terminal, so the child starts no
// background update check either.
func renderOne(ctx context.Context, agtk, dir string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd := exec.CommandContext(ctx, agtk, "render") // #nosec G204 G702 -- agtk is this process's own os.Executable unless a caller names one, the argv is fixed, and no shell is involved
	cmd.Dir = dir
	cmd.WaitDelay = renderWaitDelay
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("agtk render did not finish within %s", timeout)
	}
	if msg := oneLine(stderr.String()); msg != "" {
		return fmt.Errorf("agtk render: %w: %s", err, msg)
	}
	return fmt.Errorf("agtk render: %w", err)
}

// oneLine joins the non-blank lines of s with "; ", so a multi-line failure
// reads as one report line.
func oneLine(s string) string {
	var lines []string
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "; ")
}
