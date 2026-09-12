---
name: provider-schemas-require-every-property
kind: invariant
description: Every key in a provider schema's `properties` must also appear in that object's `required`, or OpenAI strict mode refuses the schema and the run dies having read nothing.
anchors:
  - path: source/toolkit/internal/reviewrun/schema.go
    blob: 9debfb476136
  - path: source/toolkit/internal/reviewrun/schema_test.go
    blob: d1d2491081d1
  - path: source/toolkit/internal/reviewrun/invoke.go
    blob: 2437e89a42e9
confidence: verified
---

The schemas in `source/toolkit/internal/reviewrun/schema.go` — `findingSchema`, `validatorSchema`,
`judgeSchema` — reach a provider as `agentic.Request.Schema`
(`source/toolkit/internal/reviewrun/invoke.go:107`). For codex the driver writes the schema to a file, passes
`--output-schema`, and the provider names it `codex_output_schema` in the request.

OpenAI's strict structured-output mode refuses a schema where a key in `properties` is absent
from `required`, and it refuses it **at the provider, not at the model**, so the run fails
having examined nothing:

    Invalid schema for response_format 'codex_output_schema': In context=('properties',
    'findings', 'items'), 'required' is required to be supplied and to be an array including
    every key in properties. Missing 'suggestion'.

A genuinely optional field says so in its **type** — `["string", "null"]` or
`["integer", "null"]`, the way `start_line` and `end_line` do (`schema.go:41-42`) — and still
appears in `required` (`schema.go:38`). Requiring it keeps the schema valid; the null in the
union keeps a reviewer with nothing to say from inventing something. A JSON null decodes into
the Go `string` fields of `reviewerAnswer`/`judgeAnswer` (`schema.go:105-122`) as the empty
string, which every consumer already guards on, so nothing posts the word "null".

This is invisible on Claude, whose schema handling accepts either spelling. The built-in
default roster reviews a worktree on claudecode and a pull request on codex
(`source/toolkit/internal/review/default.yaml:59-60`), so a schema that breaks the rule passes every local
review and kills every posted one.
`TestProviderSchemasRequireEveryDeclaredProperty` (`source/toolkit/internal/reviewrun/schema_test.go:55`)
walks the decoded JSON generically rather than listing field names, so a schema that gains a
property fails there instead of on the next pull request review.
