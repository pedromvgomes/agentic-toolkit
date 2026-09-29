---
name: code-review-app-credential-is-machine-local
kind: gotcha
description: Posting or approving a review needs the GitHub App registration on this machine or a configured relay's, never a bare GH_TOKEN; a token only reads, so an ephemeral container must register per run or use the relay.
anchors:
  - path: source/toolkit/internal/githubapp/credential.go
    blob: e8965be34ce5
  - path: source/toolkit/internal/cli/codereview_initialize.go
    blob: b27674748a1e
  - path: source/toolkit/internal/cli/codereview_pr.go
    blob: 51aae07b3ca6
  - path: source/toolkit/internal/githubapp/client.go
    blob: f2a998cd4b15
  - path: source/toolkit/internal/relay/relay.go
    blob: 1254d81c9b7f
confidence: suspect
---

The App credential is two files under agtk's config dir (`github-app.id`, `github-app.pem`,
`internal/githubapp/credential.go`); `Load` refuses group/other-accessible files. No
environment variable is read inside `internal/githubapp` (grep for `os.Getenv` there is empty).
`GH_TOKEN`/`GITHUB_TOKEN` are read only in `internal/cli` (`tokenVariables`,
`codereview_pr.go:154`), for a read-only fallback client when no registration exists
(`githubapp.NewReadClient`, see [[token-read-client-cannot-post-by-type]]) and to dispatch a
configured relay. A bare token is never the identity that posts.

Posting `run --pr` and `approve` resolve through the registered client only; unregistered, they
refuse unless `AGTK_CODE_REVIEW_RELAY` (`relayVariable`, `codereview_pr.go:173`) names a relay,
which dispatches a workflow in a separate repository that posts as the App (ADR 0020). Reads
(`explain --pr`, `run --pr --dry-run/--no-post`) fall back to a token only when the error is
`Unregistered`; a partial registration still refuses. `--json` refuses to relay. The one
non-interactive registration path is `register --key-stdin`
(`codereview_initialize.go:60`), but it still writes both files, so every fresh container must
run it. Bare `explain` needs no registration.

Why an App at all is in ADRs 0006, 0009, 0012, 0019 and 0020. Suspect: the ADR-derived reasoning
and the relay's internals were not re-checked here; the file layout and env-var claims were.
