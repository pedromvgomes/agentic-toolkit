// Package cloudinit points a cloud container's git at the user's own identity
// and, when a key is supplied, at the user's own signing key. With
// Options.Render it then renders each checkout's agent configuration, which a
// fresh clone lacks because the rendered output is gitignored.
//
// Everything it reads comes from three environment variables, and nothing it
// does is exported back to the environment: a child process cannot change its
// parent's, and a value placed there is one every later child inherits.
package cloudinit

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

// The variables Run reads. Each is optional, and an empty value counts as
// unset.
const (
	EnvUser       = "AGTK_GH_USER"
	EnvEmail      = "AGTK_GH_EMAIL"
	EnvSigningKey = "AGTK_SIGNING_KEY_B64" // gitleaks:allow -- the variable's name, not a key
)

// NothingToDo is the one line Run prints when none of the variables is set.
// The session-start hook runs in every session, local ones included, so an
// unconfigured session sees this and no change to its git config.
const NothingToDo = "agtk cloud init: none of AGTK_GH_USER, AGTK_GH_EMAIL or AGTK_SIGNING_KEY_B64 is set; doing nothing"

// DefaultTimeout bounds each ssh-keygen invocation when Options.Timeout is
// zero.
const DefaultTimeout = 15 * time.Second

// Options configures Run. Every field has a usable zero value.
type Options struct {
	// Stdout receives the report. Nil discards it.
	Stdout io.Writer

	// Getenv looks up the AGTK_* variables. Nil means os.Getenv.
	Getenv func(string) string

	// Home is the directory holding .ssh/. Empty means os.UserHomeDir. git
	// resolves its own global config from the inherited environment, so a
	// Home that differs from $HOME moves the key and not the config.
	Home string

	// Dir is the checkout whose repo-local identity is filled in. Empty means
	// the working directory; a Dir outside any work tree is skipped.
	Dir string

	// Timeout bounds each ssh-keygen invocation. Zero means DefaultTimeout.
	Timeout time.Duration

	// Stderr receives one line per checkout whose render failed. Nil
	// discards it.
	Stderr io.Writer

	// Render runs `agtk render` in each checkout under RenderRoot once the
	// identity and the signing key are applied.
	Render bool

	// RenderRoot is the directory whose immediate subdirectories Render
	// considers. Empty means DefaultRenderRoot.
	RenderRoot string

	// Executable is the agtk binary Render runs. Empty means os.Executable,
	// so the render comes from the same build as the command running it.
	Executable string

	// RenderTimeout bounds each checkout's render. Zero means
	// DefaultRenderTimeout.
	RenderTimeout time.Duration
}

// Run applies whichever of the identity and the signing key the environment
// supplies. It is idempotent: a resumed container runs it again, and a run
// re-creates a key file that was wiped in between.
//
// An unset identity variable leaves that key alone, and an unset signing key
// leaves every gpg.* key and commit.gpgsign/tag.gpgsign as they are, including
// values an earlier run set.
//
// With none of the variables set nothing else runs either, Render included:
// a session the user has not configured is left exactly as it was.
func Run(ctx context.Context, opts Options) error {
	opts = withDefaults(opts)
	name := opts.Getenv(EnvUser)
	email := opts.Getenv(EnvEmail)
	key := opts.Getenv(EnvSigningKey)

	if name == "" && email == "" && key == "" {
		fmt.Fprintln(opts.Stdout, NothingToDo)
		return nil
	}

	if err := applyIdentity(ctx, opts, name, email); err != nil {
		return err
	}
	if key != "" {
		if err := applySigning(ctx, opts, key); err != nil {
			return err
		}
	}
	if !opts.Render {
		return nil
	}
	return renderCheckouts(ctx, opts)
}

func withDefaults(opts Options) Options {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.RenderRoot == "" {
		opts.RenderRoot = DefaultRenderRoot
	}
	if opts.RenderTimeout <= 0 {
		opts.RenderTimeout = DefaultRenderTimeout
	}
	return opts
}
