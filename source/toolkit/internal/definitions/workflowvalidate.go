package definitions

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	hatua "hatua.dev/go"
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
	if diagnostics := hatua.ValidateDefinition(*doc, manifests).All; len(diagnostics) > 0 {
		lines := make([]string, 0, len(diagnostics))
		for _, d := range diagnostics {
			lines = append(lines, describeDiagnostic(d))
		}
		return nil, newErr(path, ErrWorkflowInvalid, "%s", strings.Join(lines, "; "))
	}
	return doc, nil
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
