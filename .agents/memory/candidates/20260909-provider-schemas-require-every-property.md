---
about: every key in a provider schema's `properties` must also appear in that object's `required`, or OpenAI strict structured-output mode refuses the whole schema and the run dies having read nothing
saw:
  - internal/reviewrun/schema.go
  - internal/reviewrun/schema_test.go
  - internal/reviewrun/invoke.go
---
The three schemas in `internal/reviewrun/schema.go` — `findingSchema`, `validatorSchema`,
`judgeSchema` — are handed to a provider by `request()` (internal/reviewrun/invoke.go) as
`agentic.Request.Schema`. For codex the driver writes them to a file and passes
`--output-schema`, and the provider names it `codex_output_schema` in the request.

OpenAI's strict structured-output mode refuses a schema where a key in `properties` is absent
from `required`, and it refuses it **at the provider, not at the model**, so the run fails
having examined nothing:

    Invalid schema for response_format 'codex_output_schema': In context=('properties',
    'findings', 'items'), 'required' is required to be supplied and to be an array including
    every key in properties. Missing 'suggestion'.

A field that is genuinely optional says so in its **type** — `["string", "null"]`, the way
`start_line` and `end_line` do — and still appears in `required`. Requiring it keeps the schema
valid; the null in the union keeps a reviewer with nothing to say from inventing something.
A JSON null decodes into the Go `string` fields of `reviewerAnswer`/`judgeAnswer` as the empty
string, which is what every consumer already guards on, so nothing posts the word "null".

This is invisible on Claude, whose schema handling accepts either spelling. The built-in
default roster reviews a worktree on claudecode and a pull request on codex, so a schema that
breaks the rule passes every local review and kills every posted one.
`TestProviderSchemasRequireEveryDeclaredProperty` (internal/reviewrun/schema_test.go) walks the
decoded JSON generically rather than listing field names, so a schema that gains a property
fails there instead of on the next pull request review.
