package reviewrun

import "encoding/json"

// The schemas are the single definition of what each run answers with. No
// prompt body restates them: a shape written twice is a shape that drifts, and
// the copy in prose is the one nothing validates.
//
// start_line and end_line are `integer` or `null` rather than a string like
// "line or range, or n/a". A number needs no parser, and a finding's line has
// to be a number anyway before it can become an inline comment.
//
// severity and confidence are enums, so a reviewer cannot answer "high-ish"
// and have it read as a severity nothing on the ladder matches.

// findingSchema is what a reviewer answers with.
var findingSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["findings"],
  "properties": {
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["path", "start_line", "end_line", "category", "severity", "confidence", "issue", "evidence"],
        "properties": {
          "path":       {"type": "string", "description": "File the finding is in, relative to the repository root."},
          "start_line": {"type": ["integer", "null"], "description": "First line of the region, or null for a claim with no line."},
          "end_line":   {"type": ["integer", "null"], "description": "Last line of the region, or null."},
          "category":   {"type": "string", "description": "Kind of problem, e.g. correctness, security:prompt-injection, performance."},
          "severity":   {"type": "string", "enum": ["RED", "AMBER", "GREEN"]},
          "confidence": {"type": "string", "enum": ["high", "medium", "low"]},
          "issue":      {"type": "string", "description": "The claim: what is wrong and what it causes."},
          "evidence":   {"type": "string", "description": "The offending line or lines, quoted verbatim from the file."},
          "suggestion": {"type": "string", "description": "What to do about it."}
        }
      }
    }
  }
}`)

// validatorSchema is what a validator answers with. It sees one finding, so it
// returns one verdict and never a set.
var validatorSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["verdict", "severity", "reason"],
  "properties": {
    "verdict":  {"type": "string", "enum": ["upheld", "rejected", "downgraded"]},
    "severity": {"type": "string", "enum": ["RED", "AMBER", "GREEN"]},
    "reason":   {"type": "string", "description": "Why, in one or two sentences, citing the code."}
  }
}`)

// judgeSchema is what the judge answers with: ids, severities and prose.
//
// It carries no path, no line and no evidence. Those are re-attached by agtk
// from the candidate that was issued the id, because a pass that may rewrite a
// quote is a pass that may silently break a finding's identity across runs.
// See ADR 0008.
var judgeSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["findings", "good"],
  "properties": {
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "severity", "issue"],
        "properties": {
          "id":         {"type": "string", "description": "The id this finding was given in the input set. Never invent one."},
          "severity":   {"type": "string", "enum": ["RED", "AMBER", "GREEN"]},
          "issue":      {"type": "string", "description": "The final wording of the claim."},
          "suggestion": {"type": "string", "description": "What to do about it."}
        }
      }
    },
    "good": {
      "type": "array",
      "items": {"type": "string"},
      "description": "What this change does well. Attributed to nothing, so it needs no id."
    }
  }
}`)

// reviewerAnswer is the decoded shape of findingSchema.
type reviewerAnswer struct {
	Findings []struct {
		Path       string   `json:"path"`
		StartLine  *int     `json:"start_line"`
		EndLine    *int     `json:"end_line"`
		Category   string   `json:"category"`
		Severity   Severity `json:"severity"`
		Confidence string   `json:"confidence"`
		Issue      string   `json:"issue"`
		Evidence   string   `json:"evidence"`
		Suggestion string   `json:"suggestion"`
	} `json:"findings"`
}

// judgeAnswer is the decoded shape of judgeSchema.
type judgeAnswer struct {
	Findings []struct {
		ID         string   `json:"id"`
		Severity   Severity `json:"severity"`
		Issue      string   `json:"issue"`
		Suggestion string   `json:"suggestion"`
	} `json:"findings"`
	Good []string `json:"good"`
}
