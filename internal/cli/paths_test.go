package cli

import (
	"path/filepath"
	"testing"
)

func TestPathHelpers_DefaultBehavior(t *testing.T) {
	// No --config: lockfile and entry filename use the canonical
	// `<WorkDir>/.agentic-toolkit.yaml` form. This is the pre-v0.5
	// behavior; any regression here breaks every existing user.
	env := &Env{WorkDir: "/work"}

	if got, want := configFilePath(env), filepath.Join("/work", ConfigFileName); got != want {
		t.Errorf("configFilePath = %q, want %q", got, want)
	}
	if got, want := stackDir(env), "/work"; got != want {
		t.Errorf("stackDir = %q, want %q", got, want)
	}
	if got, want := lockfilePath(env), filepath.Join("/work", LockFileName); got != want {
		t.Errorf("lockfilePath = %q, want %q", got, want)
	}
	if got, want := entryFileName(env), ConfigFileName; got != want {
		t.Errorf("entryFileName = %q, want %q", got, want)
	}
}

func TestPathHelpers_ConfigOverride(t *testing.T) {
	// `agtk --config /elsewhere/team-stack.yaml lock` from cwd=/cwd:
	//   - config read from /elsewhere/team-stack.yaml
	//   - lockfile written to /elsewhere/.agentic-toolkit.lock.yaml
	//     (always next to the config, never renamed after the user's
	//     custom filename)
	//   - entry filename is the user's basename so the resolver
	//     reads the right file out of the FS rooted at /elsewhere
	//   - apply dir (WorkDir) is unchanged — render output still
	//     goes to /cwd
	env := &Env{
		WorkDir:    "/cwd",
		ConfigPath: "/elsewhere/team-stack.yaml",
	}

	if got, want := configFilePath(env), "/elsewhere/team-stack.yaml"; got != want {
		t.Errorf("configFilePath = %q, want %q", got, want)
	}
	if got, want := stackDir(env), "/elsewhere"; got != want {
		t.Errorf("stackDir = %q, want %q", got, want)
	}
	if got, want := lockfilePath(env), filepath.Join("/elsewhere", LockFileName); got != want {
		t.Errorf("lockfilePath = %q, want %q", got, want)
	}
	if got, want := entryFileName(env), "team-stack.yaml"; got != want {
		t.Errorf("entryFileName = %q, want %q", got, want)
	}
	// Critical: WorkDir (apply dir) must stay independent of the
	// config override. This is the bare-repo workflow's point.
	if env.WorkDir != "/cwd" {
		t.Errorf("WorkDir was mutated to %q", env.WorkDir)
	}
}
