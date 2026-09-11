// Package githubapp holds the GitHub App registration this machine posts
// reviews as, and mints the short-lived tokens a post is made with.
//
// It is the only package that holds a GitHub credential, and nothing that
// invokes a model imports it. The App installation token reaches every
// repository the App is installed on, and a review run is driven by a model
// reading a diff somebody else wrote — see ADR 0006.
package githubapp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/source/toolkit/internal/userconfig"
)

// KeyFile and AppFile are what a registration is kept in, inside agtk's own
// config directory. Two files rather than one because a PEM is a file format
// somebody pastes in whole, and mixing it into YAML makes the thing a person
// has to get right harder to get right.
const (
	KeyFile = "github-app.pem"
	AppFile = "github-app.id"
)

// FileMode is what both files are written with, and the widest permission the
// private key may carry when read back.
//
// The App id is written at the same mode for tidiness rather than for secrecy:
// it identifies the App and is visible to anyone who can see a review the App
// posted, so a wider mode on it is not a finding to refuse a run over.
const FileMode fs.FileMode = 0o600

// DirMode is what the config directory is held at. The private key inside it
// is checked on its own, but a world-traversable directory is not what a
// machine-local credential means: a directory another account may write is one
// where the key file can be replaced or made a symlink, which the key's own
// 0600 does nothing about.
const DirMode fs.FileMode = 0o700

// Credential is the App registration this machine holds.
type Credential struct {
	// AppID is the App's numeric id, which is what a JWT is issued by.
	AppID int64
	// Dir is where the registration was read from, for a message that names
	// the file a person has to fix.
	Dir string

	key *rsa.PrivateKey
}

// Dir returns the directory a registration lives in.
func Dir() (string, error) { return userconfig.Dir() }

// ErrNotInitialized is what Load reports when this machine holds no
// registration. Separate from a read failure because the two have different
// answers: one is `agtk code-review register`, the other is a broken file.
var ErrNotInitialized = errors.New("this machine holds no GitHub App registration")

// Initialize writes a registration, replacing any this machine already holds.
//
// The key is parsed before anything is written, so a paste that is not a
// private key leaves the machine as it was rather than half-registered.
func Initialize(dir string, appID int64, pemBytes []byte) error {
	if appID < 1 {
		return fmt.Errorf("%d is not a GitHub App id", appID)
	}
	if _, err := parseKey(pemBytes); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	// MkdirAll applies its mode only to the directories it creates, so a
	// config directory that already existed keeps whatever mode it had.
	if err := os.Chmod(dir, DirMode); err != nil {
		return fmt.Errorf("restrict %s to %s: %w", dir, DirMode, err)
	}
	if err := writePrivate(filepath.Join(dir, KeyFile), pemBytes); err != nil {
		return err
	}
	return writePrivate(filepath.Join(dir, AppFile), []byte(strconv.FormatInt(appID, 10)+"\n"))
}

// writePrivate writes one file that is never readable by anyone but its owner,
// including while it is being written.
//
// The file is removed and recreated rather than truncated in place. Both
// os.WriteFile and a plain O_TRUNC open apply their mode argument only when
// they create the file, so writing over a key somebody had chmod'd to 0644
// puts the new private key into a world-readable file and narrows it
// afterwards — a window in which the key it is replacing was better protected
// than the one replacing it. Unlinking first means the mode always belongs to
// the bytes being written.
func writePrivate(path string, body []byte) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, FileMode) // #nosec G304 -- agtk's own registration at its XDG path
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// checkDir refuses a registration directory other accounts can reach into.
func checkDir(dir string) error {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	if mode := info.Mode().Perm(); mode&^DirMode != 0 {
		return fmt.Errorf("%s is mode %04o, which lets accounts other than its owner reach this machine's GitHub App key; "+
			"run `chmod %04o %s`", dir, mode, DirMode, dir)
	}
	return nil
}

// Load reads the registration this machine holds.
//
// A key file any account other than its owner can read is refused rather than
// used. The whole shape of ADR 0012 is that the blast radius of the App key is
// one machine's one user; a group-readable key makes that untrue, and using it
// anyway would mean the refusal exists only in the documentation.
func Load(dir string) (*Credential, error) {
	idPath := filepath.Join(dir, AppFile)
	keyPath := filepath.Join(dir, KeyFile)

	raw, err := os.ReadFile(idPath) // #nosec G304 -- agtk's own registration at its XDG path
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: run `agtk code-review register` to register one", ErrNotInitialized)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", idPath, err)
	}
	appID, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s does not hold a GitHub App id: %w", idPath, err)
	}

	// Checked only once a registration is known to exist. An XDG config
	// directory a machine has never registered on is ordinarily 0755, and
	// refusing that would answer "this machine holds no registration" with a
	// permissions complaint about a directory holding nothing.
	if err := checkDir(dir); err != nil {
		return nil, err
	}
	if err := checkPrivate(keyPath); err != nil {
		return nil, err
	}
	pemBytes, err := os.ReadFile(keyPath) // #nosec G304 -- agtk's own registration at its XDG path
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s holds an App id but no private key; run `agtk code-review register` again",
			ErrNotInitialized, dir)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", keyPath, err)
	}
	key, err := parseKey(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", keyPath, err)
	}
	return &Credential{AppID: appID, Dir: dir, key: key}, nil
}

// checkPrivate refuses a key file anyone but its owner can read.
func checkPrivate(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if mode := info.Mode().Perm(); mode&^FileMode != 0 {
		return fmt.Errorf("%s is mode %04o, which lets accounts other than its owner read this machine's GitHub App key; "+
			"run `chmod %04o %s`", path, mode, FileMode, path)
	}
	return nil
}

// parseKey reads the RSA private key GitHub issues for an App.
//
// Both PEM types are accepted because GitHub has issued both: a downloaded key
// is PKCS#1 (`RSA PRIVATE KEY`), and a key round-tripped through openssl is
// usually PKCS#8 (`PRIVATE KEY`). Refusing the second would refuse a key that
// signs perfectly well, for a reason nobody could act on.
func parseKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("not a PEM private key; GitHub issues one from the App's settings page, as a .pem file")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("read the private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("the private key is %T; GitHub signs App tokens with RSA", parsed)
	}
	return key, nil
}
