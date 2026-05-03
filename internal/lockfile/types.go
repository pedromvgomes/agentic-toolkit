// Package lockfile models .agentic-toolkit.lock.yaml — the resolved-state
// counterpart to internal/stack. The resolver writes it; later runs read
// it to reproduce the same fetch graph deterministically.
//
// The schema is intentionally minimal: a version tag and the list of
// sources touched. Sources include every URL reached via the entry-point
// stack's `extends:` graph plus every URL reached via per-category URL
// entries. No timestamp, no content hash, no resolved-definition manifest.
package lockfile

// Version is the lockfile schema version this build emits and accepts.
// Bumped to 2 when the consumer config + preset format collapsed into
// the single stack manifest (internal/stack). Lockfiles emitted by
// previous versions are rejected with a clear error directing the user
// to regenerate via `agtk lock`.
const Version = 2

// Lockfile is the deserialised .agentic-toolkit/lock.yaml.
type Lockfile struct {
	Version int              `yaml:"version" agtkdoc:"required;Lockfile schema version. Currently must be 1."`
	Sources []ResolvedSource `yaml:"sources" agtkdoc:"required;Every source the resolver touched, in deterministic order."`
}

// ResolvedSource is a fully-pinned source entry. url+ref are what the
// resolver was asked to fetch; sha is the commit it actually resolved to.
type ResolvedSource struct {
	URL string `yaml:"url" agtkdoc:"required;Repository URL (e.g. github.com/owner/repo)."`
	Ref string `yaml:"ref" agtkdoc:"required;Git ref the resolver was asked to fetch (branch, tag, or sha). The resolver records the default-branch name here when the consumer config left ref empty."`
	SHA string `yaml:"sha" agtkdoc:"required;Concrete commit sha the ref pointed to at resolution time."`
}
