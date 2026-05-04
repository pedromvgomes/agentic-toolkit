package cli

import "path/filepath"

// configFilePath returns the absolute path to the entry stack manifest
// for this invocation: `--config` if the user supplied one, otherwise
// the canonical `<WorkDir>/.agentic-toolkit.yaml`. PersistentPreRunE
// guarantees env.ConfigPath is absolute when non-empty.
func configFilePath(env *Env) string {
	if env.ConfigPath != "" {
		return env.ConfigPath
	}
	return filepath.Join(env.WorkDir, ConfigFileName)
}

// stackDir returns the directory rooting the stack definition. The
// entry manifest lives here, the lockfile is written here, local
// `./...` entries in the manifest resolve from here, and ambient
// project files like AGENTS.md are sourced from here. Distinct from
// env.WorkDir, which is the apply directory (where rendered output
// lands). The two collapse to the same path when --config is unset.
func stackDir(env *Env) string {
	return filepath.Dir(configFilePath(env))
}

// lockfilePath returns the absolute path the lockfile should be read
// from / written to. Always next to the entry manifest (in stackDir).
func lockfilePath(env *Env) string {
	return filepath.Join(stackDir(env), LockFileName)
}

// entryFileName returns the basename of the entry manifest, used as
// the entry-point name within the resolver's fs.FS rooted at stackDir.
// When --config is unset this is just ConfigFileName; with a custom
// path it's whatever filename the user pointed at.
func entryFileName(env *Env) string {
	return filepath.Base(configFilePath(env))
}
