package cli

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
	"github.com/pedromvgomes/agentic-toolkit/internal/sourcestore"
)

// workflowShowSchema is the JSON Schema (draft 2020-12) of `workflow show
// --json`. It is embedded verbatim so `workflow schema` prints the same bytes
// on every run.
//
//go:embed workflow_show.schema.json
var workflowShowSchema string

func newWorkflowCmd(env *Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Read the Workflow Definitions a repository resolves to",
		Long: "Reads Workflow Definitions. A Workflow Definition renders nothing: it is\n" +
			"read from the repository by whatever executes it, so these commands only\n" +
			"resolve and print, and write nothing to disk.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newWorkflowShowCmd(env), newWorkflowSchemaCmd(env))
	return cmd
}

func newWorkflowShowCmd(env *Env) *cobra.Command {
	var (
		cacheRoot string
		jsonOut   bool
	)
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Print the Workflow Definition that wins for an id",
		Long: "Reads " + ConfigFileName + " and " + LockFileName + ", resolves the plan the way\n" +
			"`agtk render` does (stack layering, dedupe, lockfile pins), and prints\n" +
			"the winning Workflow Definition's YAML source byte for byte.\n" +
			"\n" +
			"--json prints an object with id, name, source (the raw YAML) and\n" +
			"definition (the same document as generic JSON); `agtk workflow schema`\n" +
			"prints its JSON Schema.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkflowShow(env, cacheRoot, args[0], jsonOut)
		},
	}
	cmd.Flags().StringVar(&cacheRoot, "cache", "", "override cache root (defaults to $XDG_CACHE_HOME/agentic-toolkit)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON output")
	return cmd
}

func newWorkflowSchemaCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema of `workflow show --json`",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprint(env.Stdout, workflowShowSchema)
			return err
		},
	}
}

// workflowShowJSON is the `workflow show --json` shape. Definition is the
// source document decoded to generic JSON: hatua's Definition type carries no
// json tags, so marshalling it would not reproduce the file's field names.
type workflowShowJSON struct {
	Version    int            `json:"version"`
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Source     string         `json:"source"`
	Definition map[string]any `json:"definition"`
}

func runWorkflowShow(env *Env, cacheRoot, id string, jsonOut bool) error {
	target, err := loadResolveInput(env)
	if err != nil {
		return err
	}
	lock, err := loadLockfile(env)
	if err != nil {
		return err
	}
	cache, err := buildCache(cacheRoot)
	if err != nil {
		return err
	}
	plan, err := target.resolve(sourcestore.NewFrozenProvider(cache, lock))
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	w, err := winningWorkflow(plan, id)
	if err != nil {
		return err
	}
	if !jsonOut {
		_, err := env.Stdout.Write(w.Source)
		return err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(w.Source, &doc); err != nil {
		return fmt.Errorf("workflow %q: convert source to JSON: %w", id, err)
	}
	return writeJSON(env, workflowShowJSON{
		Version:    jsonVersion,
		ID:         w.ID,
		Name:       w.Title,
		Source:     string(w.Source),
		Definition: doc,
	})
}

// winningWorkflow finds the Workflow Definition the plan resolved for id.
// The plan holds one definition per (category, name), so the one found is the
// one layering left standing.
func winningWorkflow(plan *resolver.Plan, id string) (*definitions.Workflow, error) {
	var ids []string
	for _, d := range plan.Definitions {
		if d.Category != definitions.CategoryWorkflow {
			continue
		}
		w, ok := d.Definition.(*definitions.Workflow)
		if !ok {
			continue
		}
		if d.Name == id {
			return w, nil
		}
		ids = append(ids, d.Name)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil, fmt.Errorf("no workflow %q: the resolved plan has no Workflow Definitions", id)
	}
	return nil, fmt.Errorf("no workflow %q; available: %s", id, strings.Join(ids, ", "))
}
