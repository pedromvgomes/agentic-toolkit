---
about: ErrUnknownField and the line/column in a ParseError are recovered by string-matching goccy's message text, so a dependency bump can silently degrade them
saw:
  - internal/definitions/parser.go
  - internal/definitions/errors.go
  - go.mod
---

Kind: **gotcha**.

`ErrorKind` exists so callers and tests can branch without string-matching messages
(errors.go:21). Two of the paths that produce it do exactly that against
`github.com/goccy/go-yaml` (v1.19.2 in go.mod):

- `classifyYAMLError` (parser.go:270) returns `ErrUnknownField` only when the decoder's
  message contains the literal substring `"unknown field"`; everything else falls through
  to `ErrYAMLSyntax`.
- `extractYAMLPos` (parser.go:283) reads `Token().Position` off a `*yaml.SyntaxError` when
  the error is that concrete type, and otherwise scrapes `[line:column]` out of the message
  with `yamlPosRE`.

What breaks when someone gets this wrong:

A goccy upgrade that rewords its strict-mode message — or wraps it in a new error type that
is no longer a `*yaml.SyntaxError` — makes every unknown-field failure report as
`ErrYAMLSyntax` and every position collapse to `0:0`. Nothing fails to compile. The one
test that would catch the first case is a single table row,
`internal/definitions/tests/parser_test.go:251`; there is no test pinning the position
fallback at all. `internal/stack/parser.go:387` and `internal/lockfile/parser.go:66` carry
their own copies of the same classification, so the breakage is repo-wide but each package
has to be checked separately.

So: after bumping goccy, re-run the unknown-field row deliberately and eyeball a
deliberately-broken definition's error string for a `path:line:col` prefix. A green suite
is weaker evidence here than it looks.
