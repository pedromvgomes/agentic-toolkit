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
		source string
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter " + ConfigFileName + " to the working directory",
		Long: "Write a starter " + ConfigFileName + " to the working directory.\n\n" +
			"Refuses to overwrite an existing file unless --force is given. The\n" +
			"--source flag seeds the primary toolkit source URL; if omitted, the\n" +
			"scaffold is written with a placeholder the user must edit before\n" +
			"running `agtk lock`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(env, source, force)
		},
	}
	cmd.Flags().StringVar(&source, "source", "", "primary source URL to seed in the scaffold (e.g. github.com/owner/repo)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite an existing "+ConfigFileName)
	return cmd
}

func runInit(env *Env, source string, force bool) error {
	path := filepath.Join(env.WorkDir, ConfigFileName)
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; pass --force to overwrite", ConfigFileName)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
	}
	body := scaffold(source)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintln(env.Stdout, "wrote", path)
	return nil
}

// scaffold returns the YAML body for a fresh consumer config. When
// source is empty, the URL field carries a TODO placeholder and a
// comment instructs the user to edit it.
func scaffold(source string) string {
	urlLine := "source: " + source
	notes := ""
	if source == "" {
		urlLine = "source: TODO/replace-with-toolkit-source-url"
		notes = "# TODO: replace `source` with your toolkit repository URL\n" +
			"#       (e.g. github.com/your-org/agentic-toolkit), then run `agtk lock`.\n\n"
	}
	var b strings.Builder
	b.WriteString("# .agentic-toolkit.yaml — agentic-toolkit consumer config.\n")
	b.WriteString("# Run `agtk lock` after editing to produce " + LockFileName + ".\n\n")
	b.WriteString(notes)
	b.WriteString(urlLine + "\n")
	b.WriteString("# ref: main         # optional; default branch if omitted\n\n")
	b.WriteString("# platforms:\n")
	b.WriteString("#   - claude-code\n\n")
	b.WriteString("# externals:\n")
	b.WriteString("#   - github.com/another-org/their-toolkit@v1.0.0\n\n")
	b.WriteString("presets:\n")
	b.WriteString("  - default\n")
	return b.String()
}
