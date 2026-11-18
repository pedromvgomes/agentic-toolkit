package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
	"github.com/pedromvgomes/agentic-toolkit/internal/resolver"
)

// workflowIn returns the one workflow in plan, failing the test when there is
// not exactly one.
func workflowIn(t *testing.T, plan *resolver.Plan) resolver.PlannedDefinition {
	t.Helper()
	var found []resolver.PlannedDefinition
	for _, d := range plan.Definitions {
		if d.Category == definitions.CategoryWorkflow {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("workflows = %+v, want exactly one", found)
	}
	return found[0]
}

func workflowTitle(t *testing.T, d resolver.PlannedDefinition) string {
	t.Helper()
	w, ok := d.Definition.(*definitions.Workflow)
	if !ok {
		t.Fatalf("definition is %T, want *definitions.Workflow", d.Definition)
	}
	return w.Title
}

// ===== stack entries =====

func TestABareWorkflowEntryResolvesUnderTheStacksRoot(t *testing.T) {
	for _, ext := range []string{".yaml", ".yml"} {
		t.Run(ext, func(t *testing.T) {
			plan := resolvePlan(t, map[string]string{
				"stack.yaml": stackBody(nil, map[string][]string{
					"workflows": {"triage"},
				}),
				"definitions/workflows/triage" + ext: validWorkflowBody("triage", "Triage"),
			})

			d := workflowIn(t, plan)
			if d.Name != "triage" {
				t.Errorf("name = %q, want triage", d.Name)
			}
			if d.EntryPath != "triage"+ext {
				t.Errorf("entry path = %q, want triage%s", d.EntryPath, ext)
			}
			if d.StackName != "local:stack.yaml" {
				t.Errorf("stack = %q, want the stack that listed it", d.StackName)
			}
		})
	}
}

func TestAPathWorkflowEntryResolvesAgainstTheStackFile(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		"stack.yaml": stackBody(nil, map[string][]string{
			"workflows": {"./team/release.yaml"},
		}),
		"team/release.yaml": validWorkflowBody("release", "Release"),
	})

	if d := workflowIn(t, plan); d.Name != "release" {
		t.Errorf("name = %q, want release", d.Name)
	}
}

func TestAURLWorkflowEntryResolvesInsideTheFetchedSource(t *testing.T) {
	repoFS := makeMapFS(map[string]string{
		"workflows/triage.yaml": validWorkflowBody("triage", "Upstream triage"),
	})
	provider := newFakeProvider().register("github.com/foo/bar.git", "v1", repoFS)
	entryFS := withEntryManifest(map[string]string{
		"stack.yaml": stackBody(nil, map[string][]string{
			"workflows": {"github.com/foo/bar.git/workflows/triage.yaml@v1"},
		}),
	})

	plan, err := resolveEntry(t, entryFS, provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	d := workflowIn(t, plan)
	if d.Name != "triage" || d.SourceURL != "github.com/foo/bar.git" {
		t.Errorf("got (%q, %q), want triage from github.com/foo/bar.git", d.Name, d.SourceURL)
	}
	if len(plan.Sources) != 1 || plan.Sources[0].Kind != resolver.SourceDefinition {
		t.Errorf("sources = %+v", plan.Sources)
	}
}

// A workflow's canonical name is its `id`, so the file a bare entry finds is
// keyed by what it declares rather than by the name the entry used.
func TestAWorkflowIsKeyedByItsIDNotItsFilename(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		"stack.yaml": stackBody(nil, map[string][]string{
			"workflows": {"triage"},
		}),
		"definitions/workflows/triage.yaml": validWorkflowBody("issue-triage", "Triage"),
	})

	if d := workflowIn(t, plan); d.Name != "issue-triage" {
		t.Errorf("name = %q, want the id issue-triage", d.Name)
	}
}

// ===== layering =====

func TestALaterStacksWorkflowWinsTheSameID(t *testing.T) {
	entryFS := makeMapFS(map[string]string{
		entryManifestPath: entryBody([]string{"./stacks/team.yaml"}, ""),
		"stacks/team.yaml": stackBody([]string{"./base.yaml"}, map[string][]string{
			"workflows": {"./team-triage.yaml"},
		}),
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"workflows": {"triage"},
		}),
		"definitions/workflows/triage.yaml": validWorkflowBody("triage", "Base triage"),
		"stacks/team-triage.yaml":           validWorkflowBody("triage", "Team triage"),
	})

	plan, err := resolveEntry(t, entryFS, newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	d := workflowIn(t, plan)
	if got := workflowTitle(t, d); got != "Team triage" {
		t.Errorf("title = %q, want the importing stack's workflow to win", got)
	}
	if got := diagKinds(plan, resolver.DiagOverride); len(got) != 1 {
		t.Errorf("override diagnostics = %v, want one", got)
	}
}

// A workflow is keyed by (category, name), so one sharing its id with a
// definition of another category does not displace it.
func TestAWorkflowDoesNotCollideWithAnotherCategorysName(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		"stack.yaml": stackBody(nil, map[string][]string{
			"skills":    {"triage"},
			"workflows": {"triage"},
		}),
		"definitions/skills/triage/SKILL.md": validSkillBody("Triage skill"),
		"definitions/workflows/triage.yaml":  validWorkflowBody("triage", "Triage"),
	})

	if !planHas(plan, definitions.CategorySkill, "triage") || !planHas(plan, definitions.CategoryWorkflow, "triage") {
		t.Errorf("definitions = %+v, want both the skill and the workflow", plan.Definitions)
	}
	if got := diagKinds(plan, resolver.DiagOverride); len(got) != 0 {
		t.Errorf("override diagnostics = %v, want none", got)
	}
}

func TestAScannedWorkflowOverridesAComposedStacksWorkflow(t *testing.T) {
	provider := composedProvider(map[string]string{
		"stacks/base.yaml": stackBody(nil, map[string][]string{
			"workflows": {"triage"},
		}),
		"definitions/workflows/triage.yaml": validWorkflowBody("triage", "Upstream triage"),
	})
	entryFS := makeMapFS(map[string]string{
		entryManifestPath:              entryBody([]string{composedStackRef}, ""),
		"agentic/workflows/triage.yml": validWorkflowBody("triage", "Local triage"),
	})

	plan, err := resolveEntry(t, entryFS, provider)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	d := workflowIn(t, plan)
	if d.StackName != "" || !d.Scanned {
		t.Errorf("winner = (stack %q, scanned %v), want the entry manifest's scanned workflow", d.StackName, d.Scanned)
	}
	if got := workflowTitle(t, d); got != "Local triage" {
		t.Errorf("title = %q, want the repo's own workflow to win", got)
	}
}

// ===== convention scan =====

// The scan takes the same files a bare entry would name: YAML directly under
// workflows/. A markdown note or a nested directory beside them is not a
// failed definition.
func TestTheWorkflowScanTakesYAMLFilesDirectlyUnderWorkflows(t *testing.T) {
	plan, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath:                   entryBody(nil, ""),
		"agentic/workflows/triage.yaml":     validWorkflowBody("triage", "Triage"),
		"agentic/workflows/release.yml":     validWorkflowBody("release", "Release"),
		"agentic/workflows/README.md":       "notes about the workflows\n",
		"agentic/workflows/drafts/old.yaml": validWorkflowBody("old", "Old"),
	}), newFakeProvider())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.Definitions) != 2 ||
		!planHas(plan, definitions.CategoryWorkflow, "triage") ||
		!planHas(plan, definitions.CategoryWorkflow, "release") {
		t.Errorf("definitions = %+v, want triage and release only", plan.Definitions)
	}
}

func TestTheWorkflowScanRefusesTwoFilesDeclaringOneID(t *testing.T) {
	_, err := resolveEntry(t, makeMapFS(map[string]string{
		entryManifestPath:          entryBody(nil, ""),
		"agentic/workflows/a.yaml": validWorkflowBody("triage", "One"),
		"agentic/workflows/b.yaml": validWorkflowBody("triage", "Another"),
	}), newFakeProvider())
	if err == nil {
		t.Fatal("resolve succeeded, want two files declaring one id refused")
	}
}

// ===== requirements =====

func TestAWorkflowNamedInRequiresIsPulledIn(t *testing.T) {
	plan := resolvePlan(t, map[string]string{
		"stack.yaml": stackBody(nil, map[string][]string{
			"skills": {"run-triage"},
		}),
		"definitions/skills/run-triage/SKILL.md": bodyRequiring("Runs triage", "workflows/triage"),
		"definitions/workflows/triage.yaml":      validWorkflowBody("triage", "Triage"),
	})

	if !planHas(plan, definitions.CategoryWorkflow, "triage") {
		t.Fatalf("the required workflow was not pulled in: %+v", plan.Definitions)
	}
	if got := diagKinds(plan, resolver.DiagPulledRequirement); len(got) != 1 {
		t.Errorf("diagnostics = %v, want one saying it was pulled in", got)
	}
}
