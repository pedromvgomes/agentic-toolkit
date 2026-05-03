package completioninstall

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectShell(t *testing.T) {
	cases := map[string]string{
		"":          "",
		"/bin/zsh":  "zsh",
		"/bin/bash": "bash",
		"fish":      "fish",
	}
	for in, want := range cases {
		if got := detectShell(in); got != want {
			t.Errorf("detectShell(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolvePath(t *testing.T) {
	home := "/home/u"
	t.Run("zsh without brew falls back to ~/.zsh", func(t *testing.T) {
		path, note := resolvePath("zsh", "agtk", home, "")
		if path != filepath.Join(home, ".zsh", "completions", "_agtk") {
			t.Fatalf("path = %q", path)
		}
		if !strings.Contains(note, "fpath") {
			t.Errorf("note missing fpath hint: %q", note)
		}
	})
	t.Run("bash uses bash-completion dir", func(t *testing.T) {
		path, _ := resolvePath("bash", "agtk", home, "")
		want := filepath.Join(home, ".local", "share", "bash-completion", "completions", "agtk")
		if path != want {
			t.Errorf("path = %q, want %q", path, want)
		}
	})
	t.Run("fish uses fish completions dir", func(t *testing.T) {
		path, _ := resolvePath("fish", "agtk", home, "")
		want := filepath.Join(home, ".config", "fish", "completions", "agtk.fish")
		if path != want {
			t.Errorf("path = %q, want %q", path, want)
		}
	})
	t.Run("unsupported shell returns empty", func(t *testing.T) {
		path, _ := resolvePath("nu", "agtk", home, "")
		if path != "" {
			t.Errorf("expected empty path for unsupported shell, got %q", path)
		}
	})
}

func TestResolvePathZshBrewWritable(t *testing.T) {
	// Use a real temp dir as a stand-in for brew prefix's
	// share/zsh/site-functions. dirWritable does an actual probe, so
	// the dir must exist on disk.
	prefix := t.TempDir()
	site := filepath.Join(prefix, "share", "zsh", "site-functions")
	if err := mkdir(site); err != nil {
		t.Fatal(err)
	}
	path, note := resolvePath("zsh", "agtk", "/home/u", prefix)
	if path != filepath.Join(site, "_agtk") {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(note, "restart your shell") {
		t.Errorf("note = %q", note)
	}
}

func TestInstall_Disabled(t *testing.T) {
	var buf bytes.Buffer
	res, err := Install(&buf, Options{Disabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || res.Reason != "disabled" {
		t.Errorf("want skipped=disabled, got %+v", res)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no stdout when disabled, got %q", buf.String())
	}
}

func TestInstall_UnsupportedShell(t *testing.T) {
	res, err := Install(nil, Options{Shell: "nu", Home: "/h", Executable: "/x/agtk"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || !strings.Contains(res.Reason, "unsupported shell") {
		t.Errorf("got %+v", res)
	}
}

func TestInstall_ShellNotDetected(t *testing.T) {
	t.Setenv("SHELL", "")
	res, err := Install(nil, Options{Home: "/h", Executable: "/x/agtk"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || res.Reason != "shell not detected" {
		t.Errorf("got %+v", res)
	}
}

func TestInstall_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	res, err := Install(nil, Options{Shell: "zsh", Executable: "/x/agtk"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Skipped || res.Reason != "no $HOME" {
		t.Errorf("got %+v", res)
	}
}

func TestInstall_HappyPathZsh(t *testing.T) {
	home := t.TempDir()
	var (
		ranExe   string
		ranShell string
		wrote    map[string][]byte
	)
	wrote = map[string][]byte{}

	var buf bytes.Buffer
	res, err := Install(&buf, Options{
		Shell:      "zsh",
		Home:       home,
		Executable: "/path/to/agtk",
		BrewPrefix: func() string { return "" }, // force ~/.zsh fallback
		Run: func(exe, shell string) ([]byte, error) {
			ranExe = exe
			ranShell = shell
			return []byte("# zsh completion script\n"), nil
		},
		WriteFile: func(path string, data []byte) error {
			wrote[path] = data
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(home, ".zsh", "completions", "_agtk")
	if res.Path != wantPath {
		t.Errorf("Path = %q, want %q", res.Path, wantPath)
	}
	if ranExe != "/path/to/agtk" || ranShell != "zsh" {
		t.Errorf("Run got exe=%q shell=%q", ranExe, ranShell)
	}
	if string(wrote[wantPath]) != "# zsh completion script\n" {
		t.Errorf("wrote = %q", wrote[wantPath])
	}
	if !strings.Contains(buf.String(), "installed zsh completion") {
		t.Errorf("stdout missing summary: %q", buf.String())
	}
}

func TestInstall_RunError(t *testing.T) {
	res, err := Install(nil, Options{
		Shell:      "zsh",
		Home:       t.TempDir(),
		Executable: "/x/agtk",
		BrewPrefix: func() string { return "" },
		Run: func(exe, shell string) ([]byte, error) {
			return nil, errors.New("boom")
		},
		WriteFile: func(path string, data []byte) error {
			t.Fatalf("WriteFile should not be called when Run errors")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if res.Shell != "zsh" {
		t.Errorf("res.Shell = %q", res.Shell)
	}
}

func mkdir(p string) error {
	return defaultWriteFile(filepath.Join(p, ".keep"), []byte{})
}
