---
about: a reviewer's whole prompt (with the diff) is a single argv value on both providers, and agentic-driver v0.9.0 has no stdin path to move it to
saw:
  - source/toolkit/internal/reviewrun/invoke.go
  - source/toolkit/internal/provider/provider.go
  - source/toolkit/internal/review/capability.go
  - source/toolkit/internal/review/default.yaml
  - docs/adr/0015-a-block-tries-the-other-provider-and-never-posts.md
  - source/toolkit/internal/review/git.go
  - source/toolkit/internal/reviewrun/root.go
---

Traced why `agtk code-review` dies with `fork/exec ...: argument list too long` on Linux
(MAX_ARG_STRLEN caps one argv element at 128 KiB; macOS only enforces an aggregate ~1 MiB
ARG_MAX, so the same request is fine there).

Both driver dialects (module `github.com/pedromvgomes/agentic-driver@v0.9.0`, resolved via
`go mod download` — not vendored in this repo, so grep the module cache path, not `source/`)
put the whole prompt on argv, not stdin:

- claudecode: `args := append(p.baseArgs(), "-p", req.Prompt, ...)` —
  `claudecode/provider.go` in `StreamCommand`, around the `-p` line quoted in its own doc
  comment ("StreamCommand renders a Request as `claude -p`").
- codex: `args = append(args, req.Prompt)` as the last, positional element —
  `codex/provider.go` in `StreamCommand` ("The prompt is positional and last, so nothing it
  contains can be read as a flag").

There is no stdin plumbing anywhere in the driver to move it to: `agentic.Invocation{Args,
Env}` (`provider.go` around the `type Invocation struct` — no `Stdin` field), and
`grep -rn stdin` over the whole module (`driver.go`, `stream.go`, both provider packages)
returns nothing outside tests. Moving the prompt to stdin is a driver-library change, not a
one-file fix in this repo.

Schema is not the same risk for both providers: claudecode's `SchemaArgs` also puts the raw
schema string on argv (`--json-schema`, `claudecode/provider.go`), but codex's `SchemaArgs`
writes it to a file under a system temp dir and passes `--output-schema <path>`
(`codex/provider.go`) — this is what `provider-schemas-require-every-property` already
describes. The review schemas (`source/toolkit/internal/reviewrun/schema.go`) are small
(a few KB), so they are not what blows the 128 KiB cap; the diff embedded in the prompt is.

**This repo already has the "large data goes on stdin, argv stays fixed" pattern**, just not
applied here: `source/toolkit/internal/review/git.go:569` and
`source/toolkit/internal/reviewrun/root.go:218` both run `git cat-file --batch[-Z]` with a
fixed argv and feed object ids/paths through `cmd.Stdin`, with an explicit `#nosec G204`
comment naming why. The reviewer-invocation path doesn't have that option today because argv
construction lives inside the driver's per-provider dialect, not in this repo's own exec call.

Separately (not an argv issue): `codex: executable file not found in $PATH` is expected
behaviour, not a bug, on any machine without the codex CLI installed and on PATH.
`source/toolkit/internal/provider/provider.go`'s `New()` always resolves both providers via
`NewOnPath()` — "Both are taken from PATH rather than vendored: agtk runs on a developer's
machine against the CLI they are already authenticated with, not against a build this repo
would have to pin." `claudecode.NewOnPath()`/`codex.NewOnPath()` do not touch PATH themselves;
the actual `exec.LookPath` happens inside `agentic.New()` → `resolveBinary()`
(`driver.go`), invoked lazily per run by `driverInvoker.build` (`invoke.go:43-56`), not by
`review.CheckCapabilities` (`review/capability.go`), which only checks the manifest's
*declared* capabilities (schema support, confinement vocabulary) against the provider's Go
type — never whether its binary actually exists.

A missing/failing provider does **not** make a panel degrade to the other provider
automatically. `docs/adr/0015-a-block-tries-the-other-provider-and-never-posts.md` documents
the one exception: `agentic.Result.Blocked` (a quota/credential decline). Both the fork/exec
`E2BIG` and a `LookPath` "not found" surface as an ordinary `err != nil` from `Invoke`
(`reviewrun/invoke.go:135-138`, `classify`'s `err != nil` branch → `Unavailable`, never
`Blocked`), so neither triggers the panel's declared `fallback:`. The ADR is explicit that
this is deliberate: "a bad schema, a sandbox refusal, a timeout — is never blocked and always
posts, unchanged" (ADR 0015, "Considered options"). The built-in `quick` and `deep` panels
(`review/default.yaml:36-53`) are both all-claudecode, which is why the user's report says the
argv failure is identical across them — the panel choice never differs by provider until a
`-codex` twin panel is selected (`defaults.pr: quick-codex`, `default.yaml:84`) or a `Blocked`
fallback fires.

No ADR or note found requiring a panel to be multi-provider/cross-model in general — the
cross-model property (`default.yaml:7-10`, "so a change is read by two models trained
differently before anyone else sees it") describes the roster design intent, not an
enforced invariant; nothing in `review/manifest.go` or `capability.go` refuses a
single-provider manifest.
