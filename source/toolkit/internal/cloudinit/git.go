package cloudinit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// applyIdentity writes each supplied identity key globally, which overrides
// whatever identity the platform wrote, and into the checkout at opts.Dir
// only when that checkout has no value of its own. A repo-local identity is a
// deliberate split between work and personal identities and is never
// overwritten.
func applyIdentity(ctx context.Context, opts Options, name, email string) error {
	repo, err := workTree(ctx, opts.Dir)
	if err != nil {
		return err
	}
	for _, kv := range []struct{ key, value string }{
		{"user.name", name},
		{"user.email", email},
	} {
		if kv.value == "" {
			continue
		}
		if err := setGlobal(ctx, kv.key, kv.value); err != nil {
			return err
		}
		fmt.Fprintf(opts.Stdout, "agtk cloud init: %s set to %q globally\n", kv.key, kv.value)
		if repo == "" {
			continue
		}
		has, err := hasLocal(ctx, repo, kv.key)
		if err != nil {
			return err
		}
		if has {
			fmt.Fprintf(opts.Stdout, "agtk cloud init: %s keeps its own %s\n", repo, kv.key)
			continue
		}
		if _, err := git(ctx, repo, "config", "--local", kv.key, kv.value); err != nil {
			return err
		}
		fmt.Fprintf(opts.Stdout, "agtk cloud init: %s set to %q in %s\n", kv.key, kv.value, repo)
	}
	return nil
}

// workTree returns the top level of the work tree containing dir, or "" when
// dir is in none. An empty dir means the working directory.
func workTree(ctx context.Context, dir string) (string, error) {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cloud init: resolve the working directory: %w", err) // [lydite:exclude_from_mutation][applyIdentity checks err before reading the path, so the value beside an error is never observed]
		}
		dir = wd
	}
	out, err := git(ctx, dir, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(out) != "true" {
		return "", nil
	}
	top, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err // [lydite:exclude_from_mutation][applyIdentity checks err before reading the path, so the value beside an error is never observed]
	}
	return strings.TrimSpace(top), nil
}

// hasLocal reports whether the checkout's own config sets key. git exits 1
// for a key that is absent; any other failure is an error.
func hasLocal(ctx context.Context, repo, key string) (bool, error) {
	_, err := git(ctx, repo, "config", "--local", "--get", key)
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, err // [lydite:exclude_from_mutation][every caller reads the bool only when err is nil, so a failed lookup is observed through the error alone]
}

func setGlobal(ctx context.Context, key, value string) error {
	_, err := git(ctx, "", "config", "--global", key, value)
	return err
}

// git runs git in dir and returns its stdout. The returned error wraps the
// *exec.ExitError so callers can read the exit code.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- argv is fixed git subcommands built in this package; the identity values reach git as single argv entries, never through a shell
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("cloud init: git %s: %w", strings.Join(args, " "), err) // [lydite:exclude_from_mutation][every caller discards the output when err is non-nil; the in-package tests pin it]
		}
		return "", fmt.Errorf("cloud init: git %s: %w: %s", strings.Join(args, " "), err, msg) // [lydite:exclude_from_mutation][every caller discards the output when err is non-nil; the in-package tests pin it]
	}
	return stdout.String(), nil
}
