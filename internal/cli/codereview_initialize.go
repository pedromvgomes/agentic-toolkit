package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// initializeFlags name the registration a machine is being given.
type initializeFlags struct {
	appID  int64
	key    string
	stdin  bool
	dryRun bool
}

func newCodeReviewInitializeCmd(env *Env) *cobra.Command {
	var flags initializeFlags

	cmd := &cobra.Command{
		Use:   "initialize",
		Short: "Register this machine's GitHub App, so reviews can be posted",
		Long: "Stores the GitHub App's id and private key under agtk's config directory, so\n" +
			"`code-review run --pr` can post as the App.\n" +
			"\n" +
			"Once per machine, not once per repository: adding a repository is an App\n" +
			"installation rather than a secret ceremony. The key never leaves this\n" +
			"machine and nothing in a repository holds a credential, so a fork, a clone\n" +
			"or a leaked secret scan has nothing to find.\n" +
			"\n" +
			"The key file is written readable by its owner alone, and a key that any\n" +
			"other account can read is refused rather than used.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCodeReviewInitialize(cmd, env, flags)
		},
	}
	cmd.Flags().Int64Var(&flags.appID, "app-id", 0, "the GitHub App's numeric id, from its settings page")
	cmd.Flags().StringVar(&flags.key, "key-file", "", "path to the App's private key, as downloaded from its settings page")
	cmd.Flags().BoolVar(&flags.stdin, "key-stdin", false, "read the private key from standard input instead of a file")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "report where the registration would be written and write nothing")
	return cmd
}

func runCodeReviewInitialize(cmd *cobra.Command, env *Env, flags initializeFlags) error {
	dir, err := githubapp.Dir()
	if err != nil {
		return err
	}
	if flags.appID == 0 {
		return errors.New("name the App with --app-id; its numeric id is on the App's settings page")
	}
	if flags.key == "" && !flags.stdin {
		return errors.New("name the App's private key with --key-file, or pass it on standard input with --key-stdin")
	}
	if flags.key != "" && flags.stdin {
		return errors.New("--key-file and --key-stdin both name where the private key comes from; pass one")
	}

	if flags.dryRun {
		fmt.Fprintf(env.Stdout, "app id:  %d\n", flags.appID)
		fmt.Fprintf(env.Stdout, "key:     %s\n", filepath.Join(dir, githubapp.KeyFile))
		fmt.Fprintf(env.Stdout, "id file: %s\n", filepath.Join(dir, githubapp.AppFile))
		fmt.Fprintln(env.Stdout, "\nNothing was written.")
		return nil
	}

	pemBytes, err := readKey(cmd, flags)
	if err != nil {
		return err
	}
	if err := githubapp.Initialize(dir, flags.appID, pemBytes); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "Registered GitHub App %d. Its private key is at %s, readable by you alone.\n",
		flags.appID, filepath.Join(dir, githubapp.KeyFile))
	fmt.Fprintln(env.Stdout, "Install the App on each repository you want reviewed; nothing further is stored there.")
	return nil
}

// readKey reads the private key from wherever the operator put it.
//
// Standard input exists so a key can arrive from a password manager without
// being written to disk first. A key that passes through a temporary file is a
// key that outlives the command that used it.
func readKey(cmd *cobra.Command, flags initializeFlags) ([]byte, error) {
	if flags.stdin {
		body, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxKeyBytes))
		if err != nil {
			return nil, fmt.Errorf("read the private key from standard input: %w", err)
		}
		return body, nil
	}
	body, err := os.ReadFile(flags.key) // #nosec G304 -- the operator named this file on their own command line
	if err != nil {
		return nil, fmt.Errorf("read the private key: %w", err)
	}
	return body, nil
}

// maxKeyBytes bounds what is read from standard input. An RSA private key is a
// few kilobytes; anything past this was not one.
const maxKeyBytes = 64 << 10
