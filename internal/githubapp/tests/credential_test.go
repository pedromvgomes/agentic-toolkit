package tests

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// testKey is generated once per package run: a 2048-bit key costs enough that
// generating one per case would dominate the suite's runtime.
var testKey = func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}()

func pkcs1PEM(t *testing.T) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testKey),
	})
}

func pkcs8PEM(t *testing.T) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(testKey)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// register writes a working registration and returns its directory.
func register(t *testing.T, appID int64, body []byte) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "config")
	if err := githubapp.Initialize(dir, appID, body); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return dir
}

func TestARegistrationRoundTrips(t *testing.T) {
	for name, body := range map[string][]byte{
		"pkcs1": pkcs1PEM(t),
		"pkcs8": pkcs8PEM(t),
	} {
		dir := register(t, 4242, body)
		cred, err := githubapp.Load(dir)
		if err != nil {
			t.Fatalf("%s: load: %v", name, err)
		}
		if cred.AppID != 4242 {
			t.Errorf("%s: app id is %d, want 4242", name, cred.AppID)
		}
	}
}

// The blast radius of the App key is one machine's one user. A key another
// account can read makes that untrue, and using it anyway would leave the
// refusal existing only in the documentation.
func TestAKeyOtherAccountsCanReadIsRefused(t *testing.T) {
	dir := register(t, 7, pkcs1PEM(t))
	key := filepath.Join(dir, githubapp.KeyFile)
	for _, mode := range []os.FileMode{0o640, 0o604, 0o644, 0o666} {
		if err := os.Chmod(key, mode); err != nil {
			t.Fatal(err)
		}
		_, err := githubapp.Load(dir)
		if err == nil {
			t.Fatalf("a key at mode %04o was used", mode)
		}
		if !strings.Contains(err.Error(), "chmod") {
			t.Errorf("the refusal at mode %04o does not say how to fix it: %v", mode, err)
		}
	}
}

// initialize is run again when a key is rotated, and os.WriteFile applies its
// mode only when it creates the file. Without the explicit restriction, a
// rotation over a key somebody had widened would report success and leave it
// readable.
func TestReinitializingRestoresTheKeyToItsOwnerAlone(t *testing.T) {
	dir := register(t, 7, pkcs1PEM(t))
	key := filepath.Join(dir, githubapp.KeyFile)
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := githubapp.Initialize(dir, 7, pkcs1PEM(t)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != githubapp.FileMode {
		t.Errorf("the key is mode %04o after re-registering, want %04o", got, githubapp.FileMode)
	}
	if _, err := githubapp.Load(dir); err != nil {
		t.Errorf("a re-registered key does not load: %v", err)
	}
}

func TestAMachineWithNoRegistrationSaysHowToMakeOne(t *testing.T) {
	_, err := githubapp.Load(t.TempDir())
	if !errors.Is(err, githubapp.ErrNotInitialized) {
		t.Fatalf("an unregistered machine reports %v, want ErrNotInitialized", err)
	}
	if !strings.Contains(err.Error(), "code-review initialize") {
		t.Errorf("the refusal does not name the command that fixes it: %v", err)
	}
}

// A half-written registration is a different failure from an absent one, and
// both have to name the command that repairs them rather than failing on a
// missing file.
func TestAnAppIDWithNoKeyIsReportedAsAnIncompleteRegistration(t *testing.T) {
	dir := register(t, 9, pkcs1PEM(t))
	if err := os.Remove(filepath.Join(dir, githubapp.KeyFile)); err != nil {
		t.Fatal(err)
	}
	_, err := githubapp.Load(dir)
	if !errors.Is(err, githubapp.ErrNotInitialized) {
		t.Fatalf("a registration with no key reports %v, want ErrNotInitialized", err)
	}
}

func TestAnAppIDFileThatIsNotANumberIsRefused(t *testing.T) {
	dir := register(t, 9, pkcs1PEM(t))
	if err := os.WriteFile(filepath.Join(dir, githubapp.AppFile), []byte("nine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := githubapp.Load(dir); err == nil {
		t.Fatal("a registration whose App id is not a number was accepted")
	}
}

// The key is parsed before anything is written, so a paste that is not a key
// leaves the machine as it was rather than half-registered.
func TestAKeyThatIsNotAPrivateKeyIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	err := githubapp.Initialize(dir, 5, []byte("-----BEGIN CERTIFICATE-----\nnope\n"))
	if err == nil {
		t.Fatal("a certificate was accepted as an App private key")
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Error("a refused registration created the config directory anyway")
	}
}

func TestANonRSAKeyIsRefused(t *testing.T) {
	// An ed25519 key is a perfectly good private key and cannot sign an App
	// token, so the refusal has to name what is wrong rather than fail inside
	// the signer.
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not a key")})
	if err := githubapp.Initialize(t.TempDir(), 5, block); err == nil {
		t.Fatal("a PEM holding no parseable key was accepted")
	}
}

func TestAnAppIDBelowOneIsRefused(t *testing.T) {
	for _, id := range []int64{0, -1} {
		if err := githubapp.Initialize(t.TempDir(), id, pkcs1PEM(t)); err == nil {
			t.Errorf("%d was accepted as a GitHub App id", id)
		}
	}
}

// A directory another account can write is one where the key file can be
// replaced or turned into a symlink, which the key's own 0600 does nothing
// about. Refusing it is what makes "the blast radius is one machine's one
// user" true of the whole registration rather than of one file in it.
func TestARegistrationDirectoryOtherAccountsCanReachIsRefused(t *testing.T) {
	dir := register(t, 7, pkcs1PEM(t))
	for _, mode := range []os.FileMode{0o750, 0o707, 0o777} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		_, err := githubapp.Load(dir)
		if err == nil {
			t.Fatalf("a registration directory at mode %04o was used", mode)
		}
		if !strings.Contains(err.Error(), "chmod") {
			t.Errorf("the refusal at mode %04o does not say how to fix it: %v", mode, err)
		}
	}
}

// A machine that has never registered has an ordinary 0755 config directory,
// and the answer to that is the command that registers one — not a complaint
// about the permissions of a directory holding nothing.
func TestAnUnregisteredMachineIsNotRefusedForItsDirectoryMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := githubapp.Load(dir)
	if !errors.Is(err, githubapp.ErrNotInitialized) {
		t.Fatalf("an unregistered machine reports %v, want ErrNotInitialized", err)
	}
}

// MkdirAll applies its mode only to directories it creates, so registering
// into a config directory that already existed has to narrow it.
func TestInitializeNarrowsAConfigDirectoryThatAlreadyExisted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := githubapp.Initialize(dir, 7, pkcs1PEM(t)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != githubapp.DirMode {
		t.Errorf("the config directory is mode %04o after registering, want %04o", got, githubapp.DirMode)
	}
	if _, err := githubapp.Load(dir); err != nil {
		t.Errorf("a registration written into a pre-existing directory does not load: %v", err)
	}
}

// The key is never readable by another account, including while it is being
// written. Truncating in place would put the new key into whatever mode the
// old file carried and narrow it afterwards, so the file is replaced — which
// shows up as a different inode.
func TestReplacingAKeyNeverWritesItIntoAWiderFile(t *testing.T) {
	dir := register(t, 7, pkcs1PEM(t))
	key := filepath.Join(dir, githubapp.KeyFile)
	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	before := inodeOf(t, key)

	if err := githubapp.Initialize(dir, 7, pkcs8PEM(t)); err != nil {
		t.Fatal(err)
	}
	if after := inodeOf(t, key); after == before {
		t.Error("the key file was written in place, so the new key existed at the old file's mode before being narrowed")
	}
	info, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != githubapp.FileMode {
		t.Errorf("the replaced key is mode %04o, want %04o", got, githubapp.FileMode)
	}
}

// inodeOf identifies the file behind a path, so a test can tell a file that
// was replaced from one that was rewritten.
func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("this platform does not report inodes")
	}
	return uint64(st.Ino)
}
