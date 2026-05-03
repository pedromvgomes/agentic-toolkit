package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newInitCmd(env *Env) *cobra.Command {
	var (
		extendsURL string
		force      bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter " + ConfigFileName + " to the working directory",
		Long: "Write a starter " + ConfigFileName + " to the working directory.\n\n" +
			"Refuses to overwrite an existing file unless --force is given. The\n" +
			"--extends flag seeds the first stack import URL; if omitted, the\n" +
			"scaffold is written with a placeholder the user must edit before\n" +
			"running `agtk lock` or `agtk sync`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(env, extendsURL, force)
		},
	}
	cmd.Flags().StringVar(&extendsURL, "extends", "", "stack URL to seed in the scaffold's extends list (e.g. github.com/owner/repo.git/stacks/default.yaml@main)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing "+ConfigFileName)
	return cmd
}

func runInit(env *Env, extendsURL string, force bool) error {
	path := configFilePath(env)
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; pass --force to overwrite", path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	body := scaffold(extendsURL)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintln(env.Stdout, "wrote", path)
	return nil
}

// scaffold returns the YAML body for a fresh entry-point stack manifest.
// When extendsURL is empty, the file carries a TODO placeholder line and
// a comment instructing the user to edit it.
func scaffold(extendsURL string) string {
	var b strings.Builder
	b.WriteString("# .agentic-toolkit.yaml — entry-point stack manifest.\n")
	b.WriteString("# Run `agtk sync` to fetch and render in one step, or\n")
	b.WriteString("# `agtk lock && agtk render` for the two-pass workflow.\n\n")
	if extendsURL == "" {
		b.WriteString("# TODO: replace the extends entry below with a real stack URL\n")
		b.WriteString("#       (e.g. github.com/your-org/agentic-toolkit.git/stacks/default.yaml@main),\n")
		b.WriteString("#       then run `agtk lock`.\n\n")
		b.WriteString("extends:\n")
		b.WriteString("  - TODO/replace-with-stack-url.git/stacks/default.yaml@main\n")
	} else {
		b.WriteString("extends:\n")
		b.WriteString("  - " + extendsURL + "\n")
	}
	b.WriteString("\n")
	b.WriteString("# Add definitions on top of the imported stack(s):\n")
	b.WriteString("# skills:\n")
	b.WriteString("#   - ./local-skills/my-skill\n")
	b.WriteString("# rules:\n")
	b.WriteString("#   - github.com/owner/repo.git/rules/style.md@main\n")
	return b.String()
}
