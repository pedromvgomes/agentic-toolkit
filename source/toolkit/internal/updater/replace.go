package updater

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// executablePath returns the path of the running binary with symlinks
// resolved, so a replacement lands on the file itself and not on a link to it.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate self: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", exe, err)
	}
	return resolved, nil
}

// replaceFile swaps the file at target for the bytes of r, keeping target's
// permission bits. The new content is written to a temporary file in the same
// directory and renamed over target, so the swap is atomic on one filesystem:
// a reader sees the old file or the new one, and a running process keeps the
// old inode it already has open. On any failure target is left as it was and
// the temporary file is removed.
func replaceFile(target string, r io.Reader) (err error) {
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".new-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// checkDirWritable reports whether a file can be created next to target, which
// is what replaceFile needs.
func checkDirWritable(target string) error {
	probe, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}
