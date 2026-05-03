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

// configDir returns the directory containing the entry manifest. The
// lockfile lives here, and local `./...` entries in the manifest
// resolve relative to here.
func configDir(env *Env) string {
	return filepath.Dir(configFilePath(env))
}

// lockfilePath returns the absolute path the lockfile should be read
// from / written to. Always next to the entry manifest.
func lockfilePath(env *Env) string {
	return filepath.Join(configDir(env), LockFileName)
}

// entryFileName returns the basename of the entry manifest, used as
// the entry-point name within the resolver's fs.FS rooted at configDir.
// When --config is unset this is just ConfigFileName; with a custom
// path it's whatever filename the user pointed at.
func entryFileName(env *Env) string {
	return filepath.Base(configFilePath(env))
}
