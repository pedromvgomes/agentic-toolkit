package updater

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestReplaceFile_SwapsContentAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agtk")
	writeFile(t, target, "old", 0o751)

	if err := replaceFile(target, strings.NewReader("new")); err != nil {
		t.Fatalf("replaceFile: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want new", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o751 {
		t.Errorf("mode = %v, want 0751", info.Mode().Perm())
	}
	if names := dirEntries(t, dir); len(names) != 1 {
		t.Errorf("directory holds %v, want only the target", names)
	}
}

func TestReplaceFile_FailedReadLeavesTargetAndNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agtk")
	writeFile(t, target, "old", 0o755)

	if err := replaceFile(target, failingReader{}); err == nil {
		t.Fatal("expected the read error to be returned")
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Errorf("content = %q, want the original", got)
	}
	if names := dirEntries(t, dir); len(names) != 1 {
		t.Errorf("directory holds %v, want only the target", names)
	}
}

func TestReplaceFile_MissingTargetIsAnError(t *testing.T) {
	target := filepath.Join(t.TempDir(), "absent")
	if err := replaceFile(target, strings.NewReader("new")); err == nil {
		t.Fatal("expected an error for a missing target")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("target should not have been created: %v", err)
	}
}

func TestCheckDirWritable_LeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "agtk")
	writeFile(t, target, "x", 0o755)

	if err := checkDirWritable(target); err != nil {
		t.Fatalf("checkDirWritable: %v", err)
	}
	if names := dirEntries(t, dir); len(names) != 1 {
		t.Errorf("directory holds %v, want only the target", names)
	}
}

func TestCheckDirWritable_FailsWhenTheDirectoryIsAbsent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "no-such-dir", "agtk")
	if err := checkDirWritable(target); err == nil {
		t.Fatal("expected an error when the directory does not exist")
	}
}

func TestExecutablePath_IsAbsoluteAndExists(t *testing.T) {
	exe, err := executablePath()
	if err != nil {
		t.Fatalf("executablePath: %v", err)
	}
	if !filepath.IsAbs(exe) {
		t.Errorf("path %q is not absolute", exe)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Errorf("path %q does not exist: %v", exe, err)
	}
}
