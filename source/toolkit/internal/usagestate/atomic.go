package usagestate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// writeFileAtomic writes data to path through a temporary file in path's
// directory that is synced and renamed over path, so a failure at any point
// leaves path holding the old content or the new, never a partial one. The
// temporary file is removed on every failure.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("usagestate: write %s: %w", path, err)
	}
	tmp := f.Name()
	fail := func(err error) error {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("usagestate: write %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Chmod(fileMode); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("usagestate: write %s: %w", path, err)
	}
	return nil
}

// quarantine renames path to a sibling name ending in .corrupt-<unix nanos> and
// returns that name. The file is kept for inspection, never deleted.
func quarantine(path string) (string, error) {
	dst := fmt.Sprintf("%s.corrupt-%d", path, time.Now().UnixNano())
	if err := os.Rename(path, dst); err != nil {
		return "", fmt.Errorf("usagestate: quarantine %s: %w", path, err)
	}
	return dst, nil
}
