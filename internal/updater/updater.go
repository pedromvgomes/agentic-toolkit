// Package updater downloads and atomically replaces the running agtk
// binary with a newer release archive from GitHub.
//
// Flow:
//
//  1. Resolve the platform-specific archive name (matches goreleaser's
//     `agtk_<version>_<os>_<arch>.tar.gz` pattern).
//  2. Download the archive and the release's `checksums.txt`.
//  3. Verify the archive's SHA256 against the checksums file.
//  4. Extract the `agtk` binary from the archive.
//  5. Atomic-rename it over the current executable via
//     github.com/minio/selfupdate.
//
// The Installer interface is the test seam: production calls
// GitHubInstaller, tests inject stubs that exercise the CLI flow
// without ever touching the network or the filesystem.
package updater

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

// Installer applies the named version on top of the running binary.
// The caller has already verified that version is strictly newer than
// what's currently installed.
type Installer interface {
	Install(version string) error
}

// GitHubInstaller is the production Installer. It downloads from the
// agtk release on the configured GitHub repo and atomic-renames over
// the running binary.
type GitHubInstaller struct {
	// Owner and Repo identify the GitHub repository.
	Owner, Repo string
	// HTTPClient is used for archive + checksums downloads. nil =
	// http.DefaultClient with a generous 60s timeout (releases can
	// be tens of MB).
	HTTPClient *http.Client
	// GOOS / GOARCH let tests force a platform; empty = runtime.
	GOOS, GOARCH string
	// BinaryName is the executable inside the archive (e.g. "agtk").
	// Empty = "agtk".
	BinaryName string
}

// NewGitHubInstaller returns a default-configured installer.
func NewGitHubInstaller(owner, repo string) *GitHubInstaller {
	return &GitHubInstaller{
		Owner: owner,
		Repo:  repo,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Install downloads version, verifies its checksum, and replaces the
// running binary.
func (g *GitHubInstaller) Install(version string) error {
	goos := g.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := g.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	binary := g.BinaryName
	if binary == "" {
		binary = "agtk"
	}

	archiveName := fmt.Sprintf("%s_%s_%s_%s.tar.gz", binary, strings.TrimPrefix(version, "v"), goos, goarch)
	base := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s",
		url.PathEscape(g.Owner), url.PathEscape(g.Repo), url.PathEscape(version))
	archiveURL := base + "/" + archiveName
	checksumsURL := base + "/checksums.txt"

	archive, err := g.download(archiveURL)
	if err != nil {
		return fmt.Errorf("updater: download archive: %w", err)
	}
	checksums, err := g.download(checksumsURL)
	if err != nil {
		return fmt.Errorf("updater: download checksums: %w", err)
	}

	want, err := lookupChecksum(checksums, archiveName)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("updater: checksum mismatch for %s", archiveName)
	}

	binaryBytes, err := extractTarGz(archive, binary)
	if err != nil {
		return fmt.Errorf("updater: extract %s: %w", binary, err)
	}

	if err := selfupdate.Apply(strings.NewReader(string(binaryBytes)), selfupdate.Options{}); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return fmt.Errorf("updater: apply failed and rollback failed (%v): %w", rerr, err)
		}
		return fmt.Errorf("updater: apply: %w", err)
	}
	return nil
}

func (g *GitHubInstaller) download(u string) ([]byte, error) {
	client := g.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// lookupChecksum scans a checksums.txt body (one "<sha256>  <filename>"
// entry per line, two-space separator per goreleaser's default) and
// returns the hex digest for filename. Returns an error when the entry
// is missing.
func lookupChecksum(body []byte, filename string) (string, error) {
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if fields[1] == filename {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("updater: %s not listed in checksums.txt", filename)
}

// extractTarGz returns the bytes of the named entry from a tar.gz
// archive. Returns an error when no such entry exists.
func extractTarGz(archive []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		// Match either bare filename or trailing path component
		// (goreleaser archives put the binary at the archive root).
		if h.Name == name || strings.HasSuffix(h.Name, "/"+name) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("entry %q not found in archive", name)
}

// Permission is a friendlier error wrapper for the common case of the
// running binary living in a write-protected directory.
type Permission struct {
	Path string
	Err  error
}

func (e *Permission) Error() string {
	return fmt.Sprintf("updater: cannot replace %s (%v); try `sudo agtk update` or reinstall by re-downloading", e.Path, e.Err)
}

func (e *Permission) Unwrap() error { return e.Err }

// CheckExecutableWritable returns a *Permission error when the running
// executable's directory is not writable by the current user. CLI
// callers run this as a pre-flight before attempting Install.
func CheckExecutableWritable() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("updater: locate self: %w", err)
	}
	opts := selfupdate.Options{TargetPath: exe}
	if err := opts.CheckPermissions(); err != nil {
		return &Permission{Path: exe, Err: err}
	}
	return nil
}
