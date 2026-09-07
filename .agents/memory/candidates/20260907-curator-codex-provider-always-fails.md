---
about: agtk memory curate with memory.agent set to codex always fails, because curator.go unconditionally grants a non-empty AllowedTools and codex's PermissionArgs refuses any non-empty allowedTools
saw:
  - internal/curator/curator.go
  - internal/curator/tests/run_test.go
  - internal/curator/tests/curator_test.go
  - /Users/pedrogomes/work/repositories-personal/agentic-driver/main/codex/provider.go
---

Found while tracing how internal/curator constructs an agentic-driver Driver and grants
(question in scope for mirroring the curator's conventions in a new subsystem).

`curator.Providers` (`internal/curator/curator.go:46`) is `[]string{"claudecode", "codex"}`,
and `CheckProvider`'s refusal message names both (asserted in
`internal/curator/tests/curator_test.go:49`). But `Run` (`curator.go:353-359`) always calls
`driver.Run` with `AllowedTools: allowedTools(...)`, and `allowedTools`
(`curator.go:204-263`) never returns an empty slice for a real (non-dry-run) call — the
read-only tools (`Read`, `Grep`, `Glob`, `Bash(agtk memory show *)`, etc.) are unconditional,
appended before any directory check.

`agentic-driver`'s codex provider explicitly rejects this
(`main/codex/provider.go:121-146`, doc comment at `:122-128`):

```go
func (p *Provider) PermissionArgs(mode string, allowedTools []string) ([]string, error) {
	if len(allowedTools) > 0 {
		return nil, fmt.Errorf("%w: codex has no per-tool allowlist, so %s cannot be granted; ...",
			agentic.ErrInvalidRequest, strings.Join(allowedTools, ", "), ...)
	}
	...
}
```

The reasoning recorded there: "Codex constrains a run by sandbox, not by tool. Its `tools`
configuration table has exactly one field — web_search — and there is no allowlist of any
kind, so an allowedTools this accepted could only be discarded."

No test in `internal/curator/tests` exercises `codex` end-to-end — every test in
`run_test.go` defaults `Provider` to `"claudecode"` (line 29-30) and uses a fake CLI, so this
path is untested. The practical consequence: setting `memory.agent: codex` and running `agtk
memory curate` will error out of `driver.Run` on every invocation, not just misconfigured
ones — codex is not actually a usable curation provider today despite being advertised as
one in `Providers` and in the refusal message.

Relevant if a new `agtk code-review` subsystem also drives agentic-driver with a tool grant:
the codex provider cannot be given any `AllowedTools`/`Edit(...)`/`Bash(...)` grant at all: it
must either be excluded from the roster, or the new subsystem's grant-construction must special-
case codex to pass an empty AllowedTools and rely on `PermissionMode`/sandbox mode instead.
