package cloudinit

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// KeyName is the private key's file name under ~/.ssh. The public key sits
// next to it with a .pub suffix.
const KeyName = "agtk_signing_key"

// applySigning installs the key and points git's signing at it. The git
// config is written only after ssh-keygen has both loaded the key and signed
// with it, so a key that cannot sign never becomes the configured one.
//
// No error it returns carries the key or its base64 encoding: ssh-keygen's
// own messages are scrubbed of both before they are wrapped.
func applySigning(ctx context.Context, opts Options, encoded string) error {
	pem, err := decodeKey(encoded)
	if err != nil {
		return err
	}
	scrub := scrubber(encoded, pem)

	home := opts.Home
	if home == "" {
		if home, err = os.UserHomeDir(); err != nil {
			return fmt.Errorf("cloud init: resolve the home directory: %w", err)
		}
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("cloud init: create %s: %w", sshDir, err)
	}
	if err := os.Chmod(sshDir, 0o700); err != nil {
		return fmt.Errorf("cloud init: restrict %s to 0700: %w", sshDir, err)
	}

	keyPath := filepath.Join(sshDir, KeyName)
	pubPath := keyPath + ".pub"
	var pub []byte
	err = writeAtomic(sshDir, KeyName, pem, func(tmp string) error {
		out, err := sshKeygen(ctx, opts.Timeout, scrub, "-y", "-f", tmp)
		if err != nil {
			return err
		}
		pub = out
		return nil
	})
	if err != nil {
		return err
	}
	if err := writeAtomic(sshDir, KeyName+".pub", pub, nil); err != nil {
		return err
	}
	if err := proveSigning(ctx, opts.Timeout, scrub, pubPath); err != nil {
		return err
	}

	for _, kv := range [][2]string{
		{"gpg.format", "ssh"},
		{"user.signingkey", pubPath},
		{"gpg.ssh.program", "ssh-keygen"},
		{"commit.gpgsign", "true"},
		{"tag.gpgsign", "true"},
	} {
		if err := setGlobal(ctx, kv[0], kv[1]); err != nil {
			return err
		}
	}

	fingerprint, err := sshKeygen(ctx, opts.Timeout, scrub, "-l", "-f", pubPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Stdout, "agtk cloud init: commits and tags are signed with %s\n", strings.TrimSpace(string(fingerprint)))
	return nil
}

// decodeKey turns AGTK_SIGNING_KEY_B64 into the key file's bytes. Whitespace
// is ignored so a wrapped encoding decodes, and padding is optional. The
// error names the variable and never echoes its value.
func decodeKey(encoded string) ([]byte, error) {
	compact := strings.Join(strings.Fields(encoded), "")
	pem, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		pem, err = base64.RawStdEncoding.DecodeString(compact)
	}
	if err != nil || len(pem) == 0 {
		return nil, fmt.Errorf("cloud init: %s is not valid base64", EnvSigningKey)
	}
	if !bytes.HasSuffix(pem, []byte("\n")) {
		pem = append(pem, '\n')
	}
	return pem, nil
}

// writeAtomic writes data to dir/name through a 0600 temp file in the same
// directory, so the rename that puts it in place never crosses a filesystem
// and a reader never sees a partial file. validate, when non-nil, runs against
// the temp file before the rename. The temp file is removed on any failure.
func writeAtomic(dir, name string, data []byte, validate func(tmp string) error) (err error) {
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Errorf("cloud init: name a temp file for %s: %w", name, err)
	}
	tmp := filepath.Join(dir, "."+name+".tmp-"+hex.EncodeToString(suffix))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("cloud init: create a temp file for %s: %w", name, err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("cloud init: write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("cloud init: sync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cloud init: close %s: %w", tmp, err)
	}
	if validate != nil {
		if err := validate(tmp); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("cloud init: move %s into place: %w", name, err)
	}
	return nil
}

// proveSigning signs a throwaway blob the way git will: ssh-keygen -Y sign
// given the .pub path, which loads the private key beside it.
func proveSigning(ctx context.Context, timeout time.Duration, scrub func(string) string, pubPath string) error {
	dir, err := os.MkdirTemp("", "agtk-cloud-init-")
	if err != nil {
		return fmt.Errorf("cloud init: create a directory for the signing check: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	blob := filepath.Join(dir, "blob")
	if err := os.WriteFile(blob, []byte("agtk cloud init signing check\n"), 0o600); err != nil {
		return fmt.Errorf("cloud init: write the signing check blob: %w", err)
	}
	if _, err := sshKeygen(ctx, timeout, scrub, "-Y", "sign", "-n", "git", "-f", pubPath, blob); err != nil {
		return err
	}
	return nil
}

// sshKeygen runs ssh-keygen in a new session with stdin closed, so it has no
// controlling terminal to prompt on and a passphrase prompt reads end of file
// and fails. The timeout covers any prompt that would still wait.
func sshKeygen(ctx context.Context, timeout time.Duration, scrub func(string) string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh-keygen", args...)
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	what := "ssh-keygen " + args[0]
	if len(args) > 1 && args[0] == "-Y" {
		what += " " + args[1]
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("cloud init: %s did not finish within %s; %s must hold a key that needs no passphrase", what, timeout, EnvSigningKey)
	}
	var notFound *exec.Error
	if errors.As(err, &notFound) {
		return nil, fmt.Errorf("cloud init: %s: %w", what, err)
	}
	msg := scrub(strings.TrimSpace(stderr.String()))
	if strings.Contains(strings.ToLower(msg), "passphrase") {
		return nil, fmt.Errorf("cloud init: %s holds a passphrase-protected key; it must hold one that needs no passphrase (%s: %s)", EnvSigningKey, what, msg)
	}
	if msg == "" {
		return nil, fmt.Errorf("cloud init: %s rejected the key from %s: %w", what, EnvSigningKey, err)
	}
	return nil, fmt.Errorf("cloud init: %s rejected the key from %s: %w: %s", what, EnvSigningKey, err, msg)
}

// scrubber returns a function that drops a message entirely when it contains
// the encoded key or any line of the decoded one. Redacting a match in place
// would still leave the rest of a quoted key in the message.
func scrubber(encoded string, decoded []byte) func(string) string {
	var secrets []string
	for _, s := range append(strings.Fields(encoded), strings.Split(string(decoded), "\n")...) {
		if s = strings.TrimSpace(s); len(s) >= 8 && !strings.HasPrefix(s, "-----") {
			secrets = append(secrets, s)
		}
	}
	return func(msg string) string {
		for _, s := range secrets {
			if strings.Contains(msg, s) {
				return "[output withheld: it quoted the key]"
			}
		}
		return msg
	}
}
