---
about: codex's agentic-driver dialect cannot report a Block, so fallback only ever fires away from claudecode, never toward it
saw:
  - source/toolkit/internal/reviewrun/invoke.go
  - source/toolkit/internal/review/default.yaml
  - go.mod
---

`agentic-driver` v0.7.0 (`go.mod`) adds `agentic.Result.Blocked` and the optional
`agentic.BlockReporter` capability interface. In the vendored driver's source
(`claudecode/parse.go`, `DetectableBlocks`), claudecode's dialect implements it and reads a
block from the envelope's `api_error_status` (429 -> exhausted, 401 -> rejected). codex's
dialect does not implement `BlockReporter` at all — the driver's own test
(`codex/parse_test.go`, `TestCodexClaimsNoBlocksItCannotRead`) asserts exactly that, because
`turn.failed` carries only prose.

`source/toolkit/internal/reviewrun/invoke.go`'s `classify()` only ever sees `res.Blocked != nil` when the
underlying dialect can detect one. `source/toolkit/internal/review/default.yaml` wires every panel to its
twin on the other provider both ways (`quick.fallback: quick-codex`, `quick-codex.fallback:
quick`) for symmetry and so a manifest author copying the shape gets it right, but in practice
today a `*-codex` panel that gets rate-limited fails as an ordinary outage (posts as before,
Blocked stays false) rather than triggering its own `fallback:` — there is nothing wrong in
the toolkit's code, the signal just does not exist yet on that side. This flips the day
codex's dialect gains the capability, with no toolkit-side change required.
