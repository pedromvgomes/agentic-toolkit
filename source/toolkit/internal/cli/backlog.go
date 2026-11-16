package cli

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/backlog"
	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

func newBacklogCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backlog",
		Short: "Read a repository's backlog items from its GitHub issues",
		Long: "Reads backlog items: the open GitHub issues carrying an agtk: label.\n" +
			"\n" +
			"Every subcommand only reads. Labels are created on a repository by the\n" +
			"plan-feature skill, and nothing here posts, labels or edits.",
		// Without a RunE, cobra takes an unknown subcommand as an argument,
		// prints help and exits 0, so a factory naming a subcommand this
		// binary lacks would read as having succeeded.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	seam := clientSeam{}
	cmd.AddCommand(newBacklogListCmd(env, seam), newBacklogVerifyCmd(env, seam), newBacklogLabelsCmd(env))
	return cmd
}

// backlogOwnerRE and backlogRepoRE are the characters GitHub allows in an
// owner and a repository name. Both end up in a request path, so anything
// else is refused rather than escaped.
var (
	backlogOwnerRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)
	backlogRepoRE  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	backlogNumRE   = regexp.MustCompile(`^[1-9][0-9]*$`)
	backlogAppRE   = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)
)

// parseBacklogRepo reads "owner/repo", and refuses anything with a missing
// part, an extra segment or a character GitHub does not allow.
func parseBacklogRepo(s string) (string, error) {
	owner, repo, ok := strings.Cut(s, "/")
	if !ok || !backlogOwnerRE.MatchString(owner) || !backlogRepoRE.MatchString(repo) || repo == "." || repo == ".." {
		return "", fmt.Errorf("%q is not a repository: want owner/repo", s)
	}
	return owner + "/" + repo, nil
}

// parseBacklogItem reads "owner/repo#N", where N is a positive issue number
// written in plain decimal.
func parseBacklogItem(s string) (string, int, error) {
	slug, num, ok := strings.Cut(s, "#")
	if !ok {
		return "", 0, fmt.Errorf("%q is not a backlog item: want owner/repo#N", s)
	}
	repo, err := parseBacklogRepo(slug)
	if err != nil {
		return "", 0, fmt.Errorf("%q is not a backlog item: want owner/repo#N", s)
	}
	if !backlogNumRE.MatchString(num) {
		return "", 0, fmt.Errorf("%q is not a backlog item: the issue number must be a positive integer", s)
	}
	number, err := strconv.Atoi(num)
	if err != nil {
		return "", 0, fmt.Errorf("%q is not a backlog item: the issue number is out of range", s)
	}
	return repo, number, nil
}

// backlogReader builds the client every backlog read goes through.
//
// It reads with the token in GH_TOKEN or GITHUB_TOKEN and nothing else. The
// App registration is never consulted, even on a machine that holds one: the
// review App may hold no issue permissions on the repository, and nothing a
// backlog read decides depends on reading as it (ADR 0028). The variable the
// token came from is returned for errors to name.
func backlogReader(seam clientSeam, repo string) (*githubapp.ReadClient, string, error) {
	token, variable := seam.token()
	if token == "" {
		return nil, "", fmt.Errorf("no GitHub token to read %s with: set GH_TOKEN or GITHUB_TOKEN to a token that can read its issues", repo)
	}
	reader, err := githubapp.NewReadClient(token, repo, seam.options()...)
	if err != nil {
		return nil, "", err
	}
	return reader, variable, nil
}

func newBacklogListCmd(env *Env, seam clientSeam) *cobra.Command {
	var (
		repoFlag string
		jsonOut  bool
	)
	cmd := &cobra.Command{
		Use:   "list --repo owner/repo",
		Short: "List the open backlog items of a repository, with their states, predecessors and Feature",
		Long: "Lists every open issue carrying an agtk: label, in issue-number order.\n" +
			"\n" +
			"An item is shown in each state label it carries and in no other, so an\n" +
			"item is queued here only when it carries agtk:queued. Listing says nothing\n" +
			"about whether an item is trusted: that is `agtk backlog verify`, which\n" +
			"this command never runs.\n" +
			"\n" +
			"Titles are printed quoted, since anybody who can open an issue writes\n" +
			"them. With --json, stdout is a JSON array of items.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := parseBacklogRepo(repoFlag)
			if err != nil {
				return fmt.Errorf("--repo: %w", err)
			}
			reader, variable, err := backlogReader(seam, repo)
			if err != nil {
				return err
			}
			items, err := backlog.List(cmd.Context(), reader, repo)
			if err != nil {
				return fmt.Errorf("list the backlog of %s with the token in %s: %w", repo, variable, err)
			}
			if jsonOut {
				if items == nil {
					items = []backlog.Item{}
				}
				return writeJSON(env, items)
			}
			printBacklogItems(env, repo, items)
			return nil
		},
	}
	cmd.Flags().StringVar(&repoFlag, "repo", "", "the repository to read, as owner/repo")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a JSON array of items")
	_ = cmd.MarkFlagRequired("repo")
	return cmd
}

// printBacklogItems writes one line per item.
//
// Every string GitHub supplied is printed with %q: a title is written by
// whoever opened the issue, and a newline in one would otherwise print lines
// of its own into whatever reads this output.
func printBacklogItems(env *Env, repo string, items []backlog.Item) {
	if len(items) == 0 {
		fmt.Fprintf(env.Stdout, "no open backlog items on %s\n", repo)
		return
	}
	for _, item := range items {
		states := "none"
		if len(item.States) > 0 {
			quoted := make([]string, len(item.States))
			for i, state := range item.States {
				quoted[i] = fmt.Sprintf("%q", state)
			}
			states = strings.Join(quoted, " ")
		}
		stub := "no"
		if item.Stub {
			stub = "yes"
		}
		predecessors := "none"
		if len(item.Predecessors) > 0 {
			numbers := make([]string, len(item.Predecessors))
			for i, n := range item.Predecessors {
				numbers[i] = "#" + strconv.Itoa(n)
			}
			done := "all done"
			if !item.PredecessorsDone {
				done = "not all done"
			}
			predecessors = strings.Join(numbers, " ") + ", " + done
		}
		feature := "none"
		if item.Feature != nil {
			feature = fmt.Sprintf("#%d %q", item.Feature.Number, item.Feature.Title)
		}
		fmt.Fprintf(env.Stdout, "#%d %q | states: %s | stub: %s | predecessors: %s | feature: %s\n",
			item.Number, item.Title, states, stub, predecessors, feature)
	}
}

// backlogVerifiedJSON is verify's answer for a trusted item.
type backlogVerifiedJSON struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	SHA256 string `json:"sha256"`
	Body   string `json:"body"`
}

// backlogRefusedJSON is verify's answer for a refused item.
type backlogRefusedJSON struct {
	Repo    string         `json:"repo"`
	Number  int            `json:"number"`
	Kind    backlog.Kind   `json:"kind"`
	Reason  backlog.Reason `json:"reason"`
	Message string         `json:"message"`
}

func newBacklogVerifyCmd(env *Env, seam clientSeam) *cobra.Command {
	var (
		trustedApp string
		jsonOut    bool
	)
	cmd := &cobra.Command{
		Use:   "verify owner/repo#N",
		Short: "Decide whether a backlog item may be acted on, and print its body and SHA-256 when it may",
		Long: "Reads GitHub's record of a backlog item and answers whether it is trusted\n" +
			"(ADR 0028): the author and every editor of the body have write access, the\n" +
			"latest agtk:queued label came from the trusted App or an account with write\n" +
			"access, and no body edit is at or after that label. Only the body is the\n" +
			"work; no comment is read.\n" +
			"\n" +
			"It reads with the token in GH_TOKEN or GITHUB_TOKEN, and never as an App\n" +
			"registration, even on a machine that holds one.\n" +
			"\n" +
			"Trusted, it exits 0. Its text output is one line, `sha256 <hex>`, and\n" +
			"after that line's newline the body byte for byte, with nothing appended.\n" +
			"With --json, stdout is {\"repo\",\"number\",\"sha256\",\"body\"}.\n" +
			"\n" +
			"Refused, it exits 1 and stderr names the kind, the reason and what was\n" +
			"found: \"not trusted\" when GitHub's record fails a condition, and\n" +
			"\"could not determine\" when a read failed and nothing about the item was\n" +
			"learned. With --json, stdout also carries the refusal as\n" +
			"{\"repo\",\"number\",\"kind\",\"reason\",\"message\"}, where kind is\n" +
			"\"not-trusted\" or \"could-not-determine\".",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, number, err := parseBacklogItem(args[0])
			if err != nil {
				return err
			}
			if trustedApp != "" && !backlogAppRE.MatchString(trustedApp) {
				return fmt.Errorf("--trusted-app %q is not an App slug: name the App as it appears in its URL, without \"[bot]\"", trustedApp)
			}
			reader, _, err := backlogReader(seam, repo)
			if err != nil {
				return err
			}
			verified, refusal := backlog.Verify(cmd.Context(), reader, repo, number, trustedApp)
			if refusal != nil {
				return refuseBacklogItem(env, repo, number, refusal, jsonOut)
			}
			if jsonOut {
				return writeJSON(env, backlogVerifiedJSON{Repo: repo, Number: number, SHA256: verified.SHA256, Body: verified.Body})
			}
			// The body is the payload, consumed byte for byte and compared
			// against the hash, so it is written raw and nothing follows it.
			if _, err := fmt.Fprintf(env.Stdout, "sha256 %s\n", verified.SHA256); err != nil {
				return err
			}
			_, err = io.WriteString(env.Stdout, verified.Body)
			return err
		},
	}
	cmd.Flags().StringVar(&trustedApp, "trusted-app", "", "the slug of the App whose agtk:queued label is trusted; without it, only accounts with write access are")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the answer, trusted or refused, as JSON on stdout")
	return cmd
}

// refuseBacklogItem reports a refusal and returns the error that makes the
// command exit non-zero. The message quotes logins and labels GitHub
// supplied, and is printed with %q as a whole so nothing in it can start a
// line of its own.
func refuseBacklogItem(env *Env, repo string, number int, refusal *backlog.Refusal, jsonOut bool) error {
	if jsonOut {
		if err := writeJSON(env, backlogRefusedJSON{
			Repo: repo, Number: number, Kind: refusal.Kind, Reason: refusal.Reason, Message: refusal.Message,
		}); err != nil {
			return err
		}
	}
	verdict := "could not determine whether it is trusted"
	if refusal.Kind == backlog.Untrusted {
		verdict = "not trusted"
	}
	return fmt.Errorf("%s#%d: %s (%s): %q", repo, number, verdict, refusal.Reason, refusal.Message)
}

func newBacklogLabelsCmd(env *Env) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "labels",
		Short: "Print the canonical backlog label set: name, colour and description",
		Long: "Prints the agtk: labels a backlog uses, in order, each with its colour as\n" +
			"six hex digits (the form `gh label create --color` takes) and its\n" +
			"description. Nothing is read from or written to GitHub. With --json,\n" +
			"stdout is a JSON array of {\"name\",\"color\",\"description\"}.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			labels := backlog.Labels()
			if jsonOut {
				if labels == nil {
					labels = []backlog.Label{}
				}
				return writeJSON(env, labels)
			}
			for _, label := range labels {
				fmt.Fprintf(env.Stdout, "%q %s %q\n", label.Name, label.Color, label.Description)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a JSON array of labels")
	return cmd
}
