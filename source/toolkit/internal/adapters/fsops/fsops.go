// Package fsops provides the whole-owned-file write/track machinery shared
// by every platform adapter: render an entry (and, for bundles, copy its
// companion files verbatim), record what was written in a sidecar
// .agtk-manifest.json keyed by path relative to the tracked root, refuse to
// overwrite anything on disk that isn't already tracked (unless the caller
// forces it), and remove files a re-render no longer plans to write.
//
// A caller names itself once via New(prefix) — the prefix appears in every
// error message this package returns, so two adapters sharing this
// machinery still produce adapter-attributable errors.
package fsops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// WholeOp is one planned write of a whole-owned file. Bundle copies expand
// into one WholeOp per file.
type WholeOp struct {
	// RelPath is forward-slash-relative to the tracked root. Used as the
	// manifest key.
	RelPath string
	// AbsPath is the destination on disk.
	AbsPath string
	// Content is the bytes to write.
	Content []byte
}

// Ops binds the shared machinery to one adapter's error-message prefix.
type Ops struct {
	Prefix string
}

// New returns an Ops that tags every error it returns with prefix (e.g.
// "claude", "codex").
func New(prefix string) Ops { return Ops{Prefix: prefix} }

// BuildBundleOps renders an entry file plus a verbatim copy of every
// companion file under path.Dir(d.EntryPath) in d.SourceFS. root is the
// tracked root the manifest keys are relative to; dirName/entryFilename
// place the bundle at <root>/<dirName>/<d.Name>/<entryFilename>.
func (o Ops) BuildBundleOps(
	d resolver.PlannedDefinition,
	root, dirName, entryFilename string,
	renderEntry func(definitions.Definition) ([]byte, error),
) ([]WholeOp, error) {
	entryContent, err := renderEntry(d.Definition)
	if err != nil {
		return nil, err
	}
	bundleRel := path.Join(dirName, d.Name)
	ops := []WholeOp{{
		RelPath: path.Join(bundleRel, entryFilename),
		AbsPath: filepath.Join(root, dirName, d.Name, entryFilename),
		Content: entryContent,
	}}
	if d.SourceFS == nil {
		// Defensive: no SourceFS means we can't copy companions. The
		// resolver always populates SourceFS, so this is unreachable in
		// practice. Skip companions silently rather than fail.
		return ops, nil
	}
	bundleDir := path.Dir(d.EntryPath)
	if bundleDir == "." {
		bundleDir = ""
	}
	walkRoot := bundleDir
	if walkRoot == "" {
		walkRoot = "."
	}
	werr := fs.WalkDir(d.SourceFS, walkRoot, func(p string, dirent fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if dirent.IsDir() {
			return nil
		}
		// Skip the entry file (already rendered above from parsed
		// Definition). Compare against the absolute entry path that the
		// resolver recorded.
		if p == d.EntryPath {
			return nil
		}
		rel := strings.TrimPrefix(p, bundleDir)
		rel = strings.TrimPrefix(rel, "/")
		raw, rerr := fs.ReadFile(d.SourceFS, p)
		if rerr != nil {
			return rerr
		}
		ops = append(ops, WholeOp{
			RelPath: path.Join(bundleRel, rel),
			AbsPath: filepath.Join(root, dirName, d.Name, filepath.FromSlash(rel)),
			Content: raw,
		})
		return nil
	})
	if werr != nil {
		return nil, fmt.Errorf("%s: walk bundle %s/%s: %w", o.Prefix, dirName, d.Name, werr)
	}
	return ops, nil
}

// SingleFileOp builds the WholeOp for a one-file-per-definition category
// (e.g. a rule or a Claude command), rooted the same way BuildBundleOps is.
func SingleFileOp(root, dirName, name string, content []byte) WholeOp {
	rel := path.Join(dirName, name)
	return WholeOp{
		RelPath: rel,
		AbsPath: filepath.Join(root, filepath.FromSlash(rel)),
		Content: content,
	}
}

// DetectCollisions returns one error per existing-but-unmanaged target.
func (o Ops) DetectCollisions(ops []WholeOp, manifest ManifestState) []error {
	var errs []error
	for _, op := range ops {
		if _, tracked := manifest.Files[op.RelPath]; tracked {
			continue
		}
		if _, err := os.Stat(op.AbsPath); err == nil {
			errs = append(errs, fmt.Errorf("%s: %s exists and is not tracked by agtk; rerun with --force to overwrite", o.Prefix, op.AbsPath))
		}
	}
	return errs
}

// ApplyWholeOp writes one op, updating newManifest with its hash.
// Idempotent: identical existing content is skipped (still tracked).
func (o Ops) ApplyWholeOp(op WholeOp, newManifest ManifestState, stdout io.Writer) error {
	hash := ContentHash(op.Content)
	newManifest.Files[op.RelPath] = hash

	existing, err := os.ReadFile(op.AbsPath)
	if err == nil && string(existing) == string(op.Content) {
		if stdout != nil {
			fmt.Fprintf(stdout, "unchanged %s\n", op.AbsPath)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(op.AbsPath), 0o755); err != nil { // #nosec G301 -- 0755: a directory of agent assets in the user's repo, meant to be committed
		return fmt.Errorf("%s: mkdir %s: %w", o.Prefix, filepath.Dir(op.AbsPath), err)
	}
	if err := os.WriteFile(op.AbsPath, op.Content, 0o644); err != nil { // #nosec G306 -- 0644: an agent asset in the user's repo, meant to be committed and read by tools
		return fmt.Errorf("%s: write %s: %w", o.Prefix, op.AbsPath, err)
	}
	if stdout != nil {
		fmt.Fprintf(stdout, "wrote %s\n", op.AbsPath)
	}
	return nil
}

// RemoveStale deletes paths tracked by oldManifest but absent from
// newManifest — files a previous render owned that this one no longer
// plans to write.
//
// The manifest is a committed file, so its keys are input rather than
// something this process wrote in this run. A key that resolves outside
// root is reported and left alone: without the check, a manifest entry
// turns a render into a delete of any file the invoking user can write.
//
// Containment is decided after resolving symlinks, because a lexical
// check answers only half of it. `..` segments are one way out of the
// root; a key whose ancestor directory is a symlink pointing elsewhere
// is the other, and the branch that can commit the key can commit the
// symlink beside it.
func (o Ops) RemoveStale(root string, oldManifest, newManifest ManifestState, stdout io.Writer) []error {
	// Resolve the root itself too, so a consumer who symlinks their
	// render root somewhere is not mistaken for an escape.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		realRoot = filepath.Clean(root)
	}

	var errs []error
	for relPath := range oldManifest.Files {
		if _, kept := newManifest.Files[relPath]; kept {
			continue
		}
		full := filepath.Join(root, relPath)
		parent, perr := filepath.EvalSymlinks(filepath.Dir(full))
		if perr != nil {
			// The directory is gone, so the entry is too. Nothing to
			// remove, and nothing that could escape through it.
			continue
		}
		// Resolve only the parent: the entry itself may legitimately be
		// a symlink, and removing it unlinks the name rather than
		// whatever it points at.
		resolved := filepath.Join(parent, filepath.Base(full))
		if !withinRoot(realRoot, resolved) {
			errs = append(errs, fmt.Errorf("%s: manifest entry %q resolves outside %s; not removed", o.Prefix, relPath, root))
			continue
		}
		if rerr := os.Remove(resolved); rerr != nil && !os.IsNotExist(rerr) {
			errs = append(errs, fmt.Errorf("%s: remove stale %s: %w", o.Prefix, resolved, rerr))
		} else if stdout != nil {
			fmt.Fprintf(stdout, "removed %s\n", resolved)
		}
	}
	return errs
}

// withinRoot reports whether full names a path inside root. Both are
// expected to be symlink-resolved already, so this is the lexical half
// of a check whose other half is its caller's EvalSymlinks. Equality
// with root itself does not count: a render removes files under its
// root, never the root.
func withinRoot(root, full string) bool {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ReportDryRunWholeOps prints each op's intended action, and each
// no-longer-planned tracked file's pending removal, without touching disk.
// Callers with mixed-ownership files of their own (settings.json-shaped,
// CLAUDE.md/AGENTS.md-shaped) print those separately.
func (o Ops) ReportDryRunWholeOps(stdout io.Writer, ops []WholeOp, root string, manifest ManifestState) {
	if stdout == nil {
		return
	}
	for _, op := range ops {
		existing, err := os.ReadFile(op.AbsPath)
		switch {
		case err != nil:
			fmt.Fprintf(stdout, "would write %s\n", op.AbsPath)
		case string(existing) == string(op.Content):
			fmt.Fprintf(stdout, "unchanged %s\n", op.AbsPath)
		default:
			fmt.Fprintf(stdout, "would update %s\n", op.AbsPath)
		}
	}
	for relPath := range manifest.Files {
		stillTracked := false
		for _, op := range ops {
			if op.RelPath == relPath {
				stillTracked = true
				break
			}
		}
		if !stillTracked {
			fmt.Fprintf(stdout, "would remove %s\n", filepath.Join(root, relPath))
		}
	}
}

// ===== manifest =====

// ManifestFileName is the sidecar manifest's fixed name under the tracked
// root.
const ManifestFileName = ".agtk-manifest.json"

const manifestVersion = 1

// ManifestState is the sidecar .agtk-manifest.json shape: which paths a
// prior render wrote, and their content hashes.
type ManifestState struct {
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

// NewManifestState returns an empty manifest at the current version.
func NewManifestState() ManifestState {
	return ManifestState{Version: manifestVersion, Files: map[string]string{}}
}

// ReadManifest reads the manifest under root, or an empty one if it
// doesn't exist yet.
func (o Ops) ReadManifest(root string) (ManifestState, error) {
	p := filepath.Join(root, ManifestFileName)
	raw, err := os.ReadFile(p) // #nosec G304 -- reads back the manifest agtk itself wrote under the tracked root
	if err != nil {
		if os.IsNotExist(err) {
			return NewManifestState(), nil
		}
		return ManifestState{}, fmt.Errorf("%s: read manifest %s: %w", o.Prefix, p, err)
	}
	var m ManifestState
	if err := json.Unmarshal(raw, &m); err != nil {
		return ManifestState{}, fmt.Errorf("%s: parse manifest %s: %w", o.Prefix, p, err)
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	return m, nil
}

// WriteManifest writes m under root, creating root if needed.
func (o Ops) WriteManifest(root string, m ManifestState) error {
	if err := os.MkdirAll(root, 0o755); err != nil { // #nosec G301 -- 0755: the tracked root in the user's repo, meant to be committed
		return fmt.Errorf("%s: mkdir %s: %w", o.Prefix, root, err)
	}
	p := filepath.Join(root, ManifestFileName)
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: marshal manifest: %w", o.Prefix, err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(p, raw, 0o644); err != nil { // #nosec G306 -- 0644: the manifest in the user's repo, meant to be committed
		return fmt.Errorf("%s: write manifest %s: %w", o.Prefix, p, err)
	}
	return nil
}

// ContentHash returns the hex-encoded sha256 of b, used as the manifest's
// per-file fingerprint.
func ContentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
