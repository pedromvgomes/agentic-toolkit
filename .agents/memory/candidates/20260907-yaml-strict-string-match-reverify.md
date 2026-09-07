---
about: yaml.Strict()'s unknown-field detection still relies on string-matching goccy's message text; every line pointer in the note still resolves exactly
saw:
  - internal/definitions/parser.go
  - internal/stack/parser.go
  - internal/lockfile/parser.go
  - go.mod
targets: yaml-error-kinds-are-string-matched
verdict: still-true
---

Re-checked because `agtk memory show` flagged this note stale on `go.mod` (blob moved
90459e9 -> 0b5653a).

`go.mod:6` is unchanged: still `github.com/goccy/go-yaml v1.19.2`. `git log --oneline -- go.mod`
shows the file has moved since (most recently for the memory/curator feature work, e.g.
`b9dc90d feat(memory): curator, candidate surface and session close`), which adds unrelated
`require` lines and is enough to change the file's content hash without touching the goccy
pin — the staleness is anchor drift, not a wrong claim.

All four pointers the note cites still land exactly where it says:
- `internal/definitions/parser.go:270` — `func classifyYAMLError`
- `internal/definitions/parser.go:272` — the `strings.Contains(msg, "unknown field")` check
- `internal/definitions/parser.go:281` — `var yamlPosRE = regexp.MustCompile(...)`
- `internal/definitions/parser.go:283` — `func extractYAMLPos`
- `internal/definitions/parser.go:289` — `yamlPosRE.FindStringSubmatch(err.Error())`
- `internal/stack/parser.go:386` — its own copy of the same `"unknown field"` substring check
- `internal/lockfile/parser.go:65` — same, in that package

No drift in the claim or the line numbers. The note is accurate as written; only the anchor
needs re-stamping.
