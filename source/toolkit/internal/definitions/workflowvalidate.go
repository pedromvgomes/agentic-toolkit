package definitions

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	hatua "hatua.dev/go"
	"hatua.dev/go/expressions"
)

// ValidateWorkflowSource holds a Workflow Definition to hatua's contract and
// returns the decoded document. hatua.LoadDefinition refuses the shape — an
// unknown top-level field such as `permissions:` included — and
// ValidateDefinition, run against the Host manifests, refuses what the shape
// cannot express: a step naming a component nothing declares, a required
// field left empty, a reference to a step or trigger that is not upstream.
//
// Every diagnostic is a failure, whatever the document's `status`: a
// definition in a repository is what a runner reads, so one that hatua would
// not publish is not one to run.
func ValidateWorkflowSource(path string, src []byte) (*hatua.Definition, error) {
	doc, err := hatua.LoadDefinition(src)
	if err != nil {
		return nil, &ParseError{
			Path:    path,
			Line:    hatuaErrorLine(err),
			Kind:    ErrWorkflowInvalid,
			Message: err.Error(),
			Wrapped: err,
		}
	}
	manifests, err := HostManifests()
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrWorkflowInvalid, Message: err.Error(), Wrapped: err}
	}
	functions, err := HostFunctions()
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrWorkflowInvalid, Message: err.Error(), Wrapped: err}
	}
	diagnostics, err := workflowDiagnostics(*doc, manifests, functions)
	if err != nil {
		return nil, &ParseError{Path: path, Kind: ErrWorkflowInvalid, Message: err.Error(), Wrapped: err}
	}
	if len(diagnostics) > 0 {
		lines := make([]string, 0, len(diagnostics))
		for _, d := range diagnostics {
			lines = append(lines, describeDiagnostic(d))
		}
		return nil, newErr(path, ErrWorkflowInvalid, "%s", strings.Join(lines, "; "))
	}
	return doc, nil
}

// workflowDiagnostics is hatua.ValidateDefinition with the Host's functions
// in scope. ValidateDefinition checks every Expression against hatua's own
// functions only, so a call to a Host function would read as a reference to
// nothing. Its expression diagnostics are dropped and the same walk is run
// again with the merged registry; every other rule is hatua's, unchanged.
func workflowDiagnostics(doc hatua.Definition, manifests []hatua.Manifest, functions []expressions.FunctionSpec) ([]hatua.Diagnostic, error) {
	var out []hatua.Diagnostic
	for _, d := range hatua.ValidateDefinition(doc, manifests).All {
		if _, isExpression := expressions.Diagnostics[expressions.Code(d.Code)]; isExpression {
			continue
		}
		out = append(out, d)
	}

	registry := expressions.Registry{}
	for _, spec := range expressions.CoreFunctionSpecs {
		registry[spec.Qualified] = expressions.RegisteredFunction{Spec: spec}
	}
	host := expressions.Registry{}
	for _, spec := range functions {
		host[spec.Qualified] = expressions.RegisteredFunction{Spec: spec}
	}
	registry, err := expressions.MergeRegistries(registry, host)
	if err != nil {
		return nil, fmt.Errorf("host functions: %w", err)
	}
	return append(out, expressionDiagnostics(doc, manifests, registry)...), nil
}

// expressionDiagnostics mirrors hatua.ExpressionRules — every Template on
// every Board, checked against the type its slot declares and the scope its
// step can see — with the given function registry in place of hatua's own.
// Errors only: a warning blocks nothing in hatua and blocks nothing here.
func expressionDiagnostics(doc hatua.Definition, manifests []hatua.Manifest, functions expressions.Registry) []hatua.Diagnostic {
	byUse := make(map[string]hatua.Manifest, len(manifests))
	for _, m := range manifests {
		byUse[m.Use] = m
	}

	var out []hatua.Diagnostic
	hatua.WalkDocument(doc, func(ref hatua.StepRef, step hatua.Step) {
		var scope []expressions.ScopeEntry
		built := false
		check := func(slot expressions.Slot) {
			// A blank Template is a missing value, which the required-field and
			// repeat rules already report.
			if strings.TrimSpace(slot.Template) == "" {
				return
			}
			if !built {
				scope = hatua.ScopeFor(doc, ref, manifests, nil)
				built = true
			}
			for _, found := range expressions.Validate(slot.Template, slot.ExpectedType,
				expressions.CheckContext{Scope: scope, Functions: functions}) {
				if found.Severity != expressions.SeverityError {
					continue
				}
				out = append(out, hatua.Diagnostic{
					Code:     hatua.DefinitionCode(found.Code),
					Message:  found.Message,
					Blocks:   hatua.BlocksPublish,
					StepID:   step.ID,
					BlockID:  ref.Board,
					FieldKey: slot.Name,
				})
			}
		}
		for _, slot := range hatua.SlotsForStep(doc, ref.Board, step, byUse[step.Use]) {
			check(slot)
		}
		for _, branch := range step.Branches {
			if branch.When != "" {
				check(hatua.WhenSlot(branch.When))
			}
		}
		if step.Until != "" {
			check(hatua.RepeatSlot(step.Until))
		}
	})
	return out
}

// describeDiagnostic names a diagnostic's subject, its code and hatua's
// message, so the line points at the step, trigger, block or connection to
// fix.
func describeDiagnostic(d hatua.Diagnostic) string {
	var subject string
	switch {
	case d.StepID != "" && d.BlockID != "":
		subject = fmt.Sprintf("step %q in block %q", d.StepID, d.BlockID)
	case d.StepID != "":
		subject = fmt.Sprintf("step %q", d.StepID)
	case d.TriggerID != "":
		subject = fmt.Sprintf("trigger %q", d.TriggerID)
	case d.BlockID != "":
		subject = fmt.Sprintf("block %q", d.BlockID)
	case d.ConnectionID != "":
		subject = fmt.Sprintf("connection %q", d.ConnectionID)
	default:
		subject = "workflow"
	}
	if d.FieldKey != "" {
		subject += fmt.Sprintf(" field %q", d.FieldKey)
	}
	return fmt.Sprintf("%s: %s: %s", subject, d.Code, d.Message)
}

// hatuaLinePattern finds the line yaml.v3 — the decoder hatua.LoadDefinition
// uses — names in a decode error: "line 5: field permissions not found".
var hatuaLinePattern = regexp.MustCompile(`\bline (\d+):`)

// hatuaErrorLine is the first line a hatua decode error names, or 0 when it
// names none.
func hatuaErrorLine(err error) int {
	m := hatuaLinePattern.FindStringSubmatch(err.Error())
	if m == nil {
		return 0
	}
	line, _ := strconv.Atoi(m[1])
	return line
}
