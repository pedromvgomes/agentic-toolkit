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

	"github.com/pedromvgomes/agentic-toolkit/internal/userconfig"
)

// KeyFile and AppFile are what a registration is kept in, inside agtk's own
// config directory. Two files rather than one because a PEM is a file format
// somebody pastes in whole, and mixing it into YAML makes the thing a person
// has to get right harder to get right.
const (
	KeyFile = "github-app.pem"
	AppFile = "github-app.id"
)

// FileMode is what both files are written with, and the widest permission
// either may carry when read back.
const FileMode fs.FileMode = 0o600

// DirMode is what the config directory is created with. The private key inside
// it is checked on its own, but a world-traversable directory is not what a
// machine-local credential means.
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
// answers: one is `agtk code-review initialize`, the other is a broken file.
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
	if err := writePrivate(filepath.Join(dir, KeyFile), pemBytes); err != nil {
		return err
	}
	return writePrivate(filepath.Join(dir, AppFile), []byte(strconv.FormatInt(appID, 10)+"\n"))
}

// writePrivate writes one file at FileMode, and holds it there even if the
// file already existed at a wider mode.
//
// os.WriteFile applies its mode only when it creates the file, so re-running
// initialize over a key somebody had chmod'd to 0644 would leave it readable
// and report success.
func writePrivate(path string, body []byte) error {
	if err := os.WriteFile(path, body, FileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, FileMode); err != nil {
		return fmt.Errorf("restrict %s to %s: %w", path, FileMode, err)
	}
	return nil
}

// Load reads the registration this machine holds.
//
// A key file any account other than its owner can read is refused rather than
// used. The whole shape of ADR 0005 is that the blast radius of the App key is
// one machine's one user; a group-readable key makes that untrue, and using it
// anyway would mean the refusal exists only in the documentation.
func Load(dir string) (*Credential, error) {
	idPath := filepath.Join(dir, AppFile)
	keyPath := filepath.Join(dir, KeyFile)

	raw, err := os.ReadFile(idPath) // #nosec G304 -- agtk's own registration at its XDG path
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: run `agtk code-review initialize` to register one", ErrNotInitialized)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", idPath, err)
	}
	appID, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s does not hold a GitHub App id: %w", idPath, err)
	}

	if err := checkPrivate(keyPath); err != nil {
		return nil, err
	}
	pemBytes, err := os.ReadFile(keyPath) // #nosec G304 -- agtk's own registration at its XDG path
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s holds an App id but no private key; run `agtk code-review initialize` again",
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
