package tests

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pedromvgomes/agentic-toolkit/internal/definitions"
)

const workflowHeader = "id: deliver\nname: Deliver\nversion: 1\nstatus: published\n"

func parseWorkflow(t *testing.T, body string) (definitions.Definition, error) {
	t.Helper()
	fsys := fstest.MapFS{
		"definitions/workflows/deliver.yaml": &fstest.MapFile{Data: []byte(workflowHeader + body)},
	}
	return definitions.ParseInCatalog(fsys, "definitions/workflows/deliver.yaml")
}

// wantRefusal asserts a hatua refusal that names the file and every
// fragment of hatua's diagnostic.
func wantRefusal(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if !definitions.IsKind(err, definitions.ErrWorkflowInvalid) {
		t.Fatalf("got %v, want ErrWorkflowInvalid", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "definitions/workflows/deliver.yaml") {
		t.Errorf("the error does not name the file: %s", msg)
	}
	for _, f := range fragments {
		if !strings.Contains(msg, f) {
			t.Errorf("the error does not carry %q: %s", f, msg)
		}
	}
}

func TestAWorkflowUsingEveryHostManifestIsValid(t *testing.T) {
	def, err := parseWorkflow(t, ""+
		"triggers:\n"+
		"  - id: hook\n"+
		"    use: component.webhook\n"+
		"steps:\n"+
		"  - id: check\n"+
		"    use: component.script\n"+
		"    with:\n"+
		"      run: make check\n"+
		"      timeout: 600\n"+
		"  - id: implement\n"+
		"    use: component.agentic\n"+
		"    with:\n"+
		"      provider: codex\n"+
		"      model: gpt-5\n"+
		"      prompt: \"Fix {{triggers.hook.inputs.issue}} given {{steps.check.stdout}}\"\n"+
		"      worktree: new\n"+
		"      agtk: render\n"+
		"      uses: implement-backlog-item\n"+
		"      leases: claude-code\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	w := def.(*definitions.Workflow)
	if w.Document == nil || w.Document.ID != "deliver" || len(w.Document.Steps) != 2 {
		t.Errorf("the decoded document is not the file: %+v", w.Document)
	}
}

func TestAWorkflowWithAnUnknownTopLevelFieldIsRefused(t *testing.T) {
	_, err := parseWorkflow(t, "permissions:\n  contents: write\nsteps: []\n")
	wantRefusal(t, err, "permissions")
	var pe *definitions.ParseError
	if errors.As(err, &pe) && pe.Line != 5 {
		t.Errorf("line = %d, want 5, where permissions: sits", pe.Line)
	}
}

func TestAReferenceToAStepThatIsNotUpstreamIsRefused(t *testing.T) {
	_, err := parseWorkflow(t, ""+
		"steps:\n"+
		"  - id: build\n"+
		"    use: component.script\n"+
		"    with:\n"+
		"      run: make\n"+
		"  - id: check\n"+
		"    use: component.script\n"+
		"    with:\n"+
		"      run: \"echo {{steps.later.stdout}}\"\n"+
		"  - id: later\n"+
		"    use: component.script\n"+
		"    with:\n"+
		"      run: make\n")
	wantRefusal(t, err, `step "check" field "run"`, "EXPR_UNKNOWN_REFERENCE", "steps.later")
}

func TestAReferenceToAnUndeclaredTriggerIsRefused(t *testing.T) {
	_, err := parseWorkflow(t, ""+
		"triggers:\n"+
		"  - id: hook\n"+
		"    use: component.webhook\n"+
		"steps:\n"+
		"  - id: check\n"+
		"    use: component.script\n"+
		"    with:\n"+
		"      run: \"echo {{triggers.cron.inputs}}\"\n")
	wantRefusal(t, err, "EXPR_UNKNOWN_REFERENCE", "triggers.cron")
}

func TestAStepUsingAComponentNoManifestDeclaresIsRefused(t *testing.T) {
	_, err := parseWorkflow(t, ""+
		"steps:\n"+
		"  - id: deploy\n"+
		"    use: component.deploy\n")
	wantRefusal(t, err, `step "deploy"`, "COMPONENT_UNKNOWN", "component.deploy")
}

func TestAnAgenticStepMissingARequiredFieldIsRefused(t *testing.T) {
	_, err := parseWorkflow(t, ""+
		"steps:\n"+
		"  - id: implement\n"+
		"    use: component.agentic\n"+
		"    with:\n"+
		"      provider: claude-code\n"+
		"      worktree: new\n"+
		"      agtk: none\n")
	wantRefusal(t, err, `field "prompt"`, "FIELD_REQUIRED", "Prompt is required.")
}

func TestTheHostManifestsLoadAndDeclareTheHostsComponents(t *testing.T) {
	manifests, err := definitions.HostManifests()
	if err != nil {
		t.Fatalf("loading the host manifests: %v", err)
	}
	byUse := map[string]string{}
	outputs := map[string][]string{}
	for _, m := range manifests {
		byUse[m.Use] = string(m.Kind)
		for _, o := range m.Outputs {
			outputs[m.Use] = append(outputs[m.Use], o.K)
		}
	}
	want := map[string]string{
		"component.agentic": "component",
		"component.script":  "component",
		"component.webhook": "trigger",
	}
	if len(byUse) != len(want) {
		t.Errorf("manifests = %v, want %v", byUse, want)
	}
	for use, kind := range want {
		if byUse[use] != kind {
			t.Errorf("%s kind = %q, want %q", use, byUse[use], kind)
		}
	}
	if got := strings.Join(outputs["component.script"], ","); got != "exit_code,stdout" {
		t.Errorf("component.script outputs = %s, want exit_code,stdout", got)
	}
	if got := strings.Join(outputs["component.agentic"], ","); got != "report" {
		t.Errorf("component.agentic outputs = %s, want report", got)
	}

	for _, m := range manifests {
		if m.Use != "component.agentic" {
			continue
		}
		fields := map[string][]string{}
		for _, f := range m.Fields {
			for _, o := range f.Options {
				fields[f.K] = append(fields[f.K], o.Value)
			}
			if _, ok := fields[f.K]; !ok {
				fields[f.K] = nil
			}
		}
		for k, opts := range map[string]string{
			"provider": "claude-code,codex",
			"worktree": "new,inherit",
			"agtk":     "none,render,sync",
			"model":    "", "prompt": "", "uses": "", "leases": "",
		} {
			got, ok := fields[k]
			if !ok {
				t.Errorf("component.agentic has no %s field", k)
				continue
			}
			if strings.Join(got, ",") != opts {
				t.Errorf("component.agentic %s options = %v, want %s", k, got, opts)
			}
		}
	}
}

func TestTheHostManifestsAreOrderedByUse(t *testing.T) {
	manifests, err := definitions.HostManifests()
	if err != nil {
		t.Fatalf("loading the host manifests: %v", err)
	}
	uses := make([]string, 0, len(manifests))
	for _, m := range manifests {
		uses = append(uses, m.Use)
	}
	want := "component.agentic,component.script,component.webhook"
	if got := strings.Join(uses, ","); got != want {
		t.Errorf("uses = %s, want %s", got, want)
	}
}

func TestTheHostManifestFilesAreServedAsShipped(t *testing.T) {
	names, err := fs.Glob(definitions.HostManifestFS(), "*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := "component.agentic.yaml,component.script.yaml,component.webhook.yaml"
	if got := strings.Join(names, ","); got != want {
		t.Errorf("files = %s, want %s", got, want)
	}
}
