// Package lockfile models .agentic-toolkit.lock.yaml — the resolved-state
// counterpart to internal/stack. The resolver writes it; later runs read
// it to reproduce the same fetch graph deterministically.
//
// The schema is intentionally minimal: a version tag, the list of sources
// touched, and a digest of the entry manifest that produced them. Sources
// include every URL reached via the entry-point stack's `extends:` graph plus
// every URL reached via per-category URL entries. No timestamp and no
// resolved-definition manifest.
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
)

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
	// ConfigDigest is what makes staleness a question about content. It is
	// the only record of which manifest produced these pins, and without it
	// the alternative is comparing mtimes — which calls an untouched manifest
	// stale whenever a checkout writes it last, and calls an edited one fresh
	// whenever the edit preserves timestamps.
	//
	// Optional so a lockfile predating it still parses. One is treated as
	// stale, which costs a single re-lock and then converges.
	ConfigDigest string `yaml:"config_digest,omitempty" agtkdoc:"Digest of the entry manifest these pins were resolved from."`
}

// Digest renders the canonical digest of an entry manifest's bytes.
//
// Over the raw bytes rather than the parsed stack: a digest of the parse
// would call two manifests identical whenever the parser ignores what
// separates them, and the parser is the thing most likely to change.
func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ResolvedSource is a fully-pinned source entry. url+ref are what the
// resolver was asked to fetch; sha is the commit it actually resolved to.
type ResolvedSource struct {
	URL string `yaml:"url" agtkdoc:"required;Repository URL (e.g. github.com/owner/repo)."`
	Ref string `yaml:"ref" agtkdoc:"required;Git ref the resolver was asked to fetch (branch, tag, or sha). The resolver records the default-branch name here when the consumer config left ref empty."`
	SHA string `yaml:"sha" agtkdoc:"required;Concrete commit sha the ref pointed to at resolution time."`
}
