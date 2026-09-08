---
name: yaml-error-kinds-are-string-matched
kind: gotcha
description: ErrUnknownField and a ParseError's line/column are recovered by string-matching goccy's message text, so a dependency bump can silently degrade them.
anchors:
  - path: internal/definitions/parser.go
    blob: c59af791af5f
  - path: internal/definitions/errors.go
    blob: c869370353ec
  - path: internal/stack/parser.go
    blob: cdbb1d5c5dec
  - path: internal/lockfile/parser.go
    blob: 2039f12de5d8
  - path: go.mod
    blob: 0f0370fd51bc
confidence: verified
---

`ErrorKind` exists so callers and tests can branch without string-matching messages
(`internal/definitions/errors.go:20-22`). Two of the paths that produce it do exactly that
against `github.com/goccy/go-yaml` (v1.19.2, `go.mod:6`):

- `classifyYAMLError` (`internal/definitions/parser.go:270`) returns `ErrUnknownField` only
  when the decoder's message contains the literal substring `"unknown field"`
  (`parser.go:272`); everything else falls through to `ErrYAMLSyntax`.
- `extractYAMLPos` (`parser.go:283`) reads `Token().Position` off a `*yaml.SyntaxError` when
  the error is that concrete type, and otherwise scrapes `[line:column]` out of the message
  with `yamlPosRE` (`parser.go:281`, applied at `:289`).

What breaks: a goccy upgrade that rewords its strict-mode message — or wraps it in a new error
type that is no longer a `*yaml.SyntaxError` — makes every unknown-field failure report as
`ErrYAMLSyntax` and every position collapse to `0:0`. Nothing fails to compile. The one test
that would catch the first case is a single table row,
`internal/definitions/tests/parser_test.go:251`; there is no test pinning the position
fallback at all.

`internal/stack/parser.go:386` and `internal/lockfile/parser.go:65` carry their own copies of
the same `"unknown field"` substring check, so the breakage is repo-wide but each package has
to be checked separately.

So: after bumping goccy, re-run the unknown-field row deliberately and eyeball a
deliberately-broken definition's error string for a `path:line:col` prefix. A green suite is
weaker evidence here than it looks.
