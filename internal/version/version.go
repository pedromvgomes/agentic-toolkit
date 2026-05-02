// Package version exposes the running agtk binary's version.
//
// Resolution order (first match wins):
//
//  1. The Version var, when set to anything other than "dev". Release
//     builds set it via -ldflags "-X .../version.Version=vX.Y.Z".
//  2. runtime/debug.ReadBuildInfo()'s Main.Version when not "(devel)".
//     This kicks in for `go install github.com/.../agentic-toolkit@vX.Y.Z`
//     users where ldflags isn't applied.
//  3. The literal "dev". Auto-update treats this as "no version known"
//     and skips checks.
package version

import "runtime/debug"

// Version is the link-time-injected release tag for ldflags builds. The
// public surface is Current() — Version stays exported only because the
// ldflags toolchain demands a package-level identifier.
var Version = "dev"

// Current returns the effective version string for this binary.
func Current() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

// IsDev reports whether the current binary is a dev build (no real
// release tag attached). Auto-update gates skip when this is true.
func IsDev() bool {
	return Current() == "dev"
}
