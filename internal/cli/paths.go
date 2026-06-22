package cli

import (
	"path"
	"path/filepath"
)

// There are three ways a command can locate the entry stack manifest and
// decide where the lockfile + rendered output land:
//
//   - default:  manifest at <WorkDir>/.agentic-toolkit.yaml; lockfile next
//     to it; output and FS root both at WorkDir.
//   - --config: manifest at the given path; lockfile next to it; FS root at
//     the manifest's directory; output still at WorkDir (bare-repo/worktree).
//   - --source: apply from the toolkit tree at SourceDir exactly as if
//     agtk were run there — the entry manifest is <SourceDir>/.agentic-
//     toolkit.yaml by default, or <SourceDir>/stacks/<stack>.yaml when the
//     optional --stack names one. FS root at SourceDir (so bare-name
//     definitions resolve against the source's definitions/); lockfile and
//     output at WorkDir, keeping the shared/read-only source tree clean.
//
// --source and --config are mutually exclusive (enforced in PersistentPreRunE).

// configFilePath returns the absolute path to the entry stack manifest on
// disk (used by `agtk init` to write, and by loadStack to parse).
func configFilePath(env *Env) string {
	if env.SourceDir != "" {
		return filepath.Join(env.SourceDir, entryRelPath(env))
	}
	if env.ConfigPath != "" {
		return env.ConfigPath
	}
	return filepath.Join(env.WorkDir, ConfigFileName)
}

// stackDir returns the directory rooting the resolver's fs.FS: bare-name
// definitions, local `./...` refs, and (outside --source) ambient project
// files like AGENTS.md are sourced from here. In --source mode this is the
// source toolkit directory; otherwise the directory containing the manifest.
func stackDir(env *Env) string {
	if env.SourceDir != "" {
		return env.SourceDir
	}
	if env.ConfigPath != "" {
		return filepath.Dir(env.ConfigPath)
	}
	return env.WorkDir
}

// entryRelPath returns the entry manifest's path relative to stackDir,
// i.e. the entry-point name within the resolver's fs.FS rooted at stackDir.
// In --source mode this is `stacks/<stack>.yaml` when --stack is given, else
// the default ConfigFileName (apply the source folder as if run there);
// otherwise the manifest's basename (ConfigFileName, or the --config basename).
func entryRelPath(env *Env) string {
	if env.SourceDir != "" {
		if env.StackName != "" {
			return path.Join("stacks", env.StackName+".yaml")
		}
		return ConfigFileName
	}
	return filepath.Base(configFilePath(env))
}

// lockfilePath returns the absolute path the lockfile is read from / written
// to. It sits next to the manifest (in stackDir) for default/--config, but
// in --source mode it lands in the apply directory (WorkDir) so the shared
// source tree is never written to.
func lockfilePath(env *Env) string {
	if env.SourceDir != "" {
		return filepath.Join(env.WorkDir, LockFileName)
	}
	return filepath.Join(stackDir(env), LockFileName)
}

// renderStackDir returns the directory the renderer should treat as the
// project's manifest dir (where it looks for an AGENTS.md to seed CLAUDE.md).
// In --source mode that's the apply project (WorkDir), NOT the shared source
// tree; otherwise it's stackDir.
func renderStackDir(env *Env) string {
	if env.SourceDir != "" {
		return env.WorkDir
	}
	return stackDir(env)
}
