---
about: code-review's GitHub App registration is deliberately machine-local; posting or approving needs it on this machine, or on a configured relay's, but never a bare GH_TOKEN/GITHUB_TOKEN; reading a pull request needs it only on a machine that also holds no such token
saw:
  - source/toolkit/internal/githubapp/credential.go
  - source/toolkit/internal/cli/codereview_initialize.go
  - source/toolkit/internal/cli/codereview.go
  - source/toolkit/internal/cli/codereview_pr.go
  - source/toolkit/internal/githubapp/client.go
  - source/toolkit/internal/relay/relay.go
  - docs/adr/0012-reviews-run-locally-not-in-ci.md
  - docs/adr/0009-the-app-can-push-so-that-it-can-approve.md
  - docs/adr/0006-the-judge-decides-agtk-transmits.md
  - docs/adr/0019-a-token-reads-a-pull-request-and-never-posts.md
  - docs/adr/0020-a-relay-posts-as-the-app-from-a-separate-repository.md
---

Investigated for: making `agtk code-review` work in an ephemeral cloud container (fresh
container per session, no persistent `$XDG_CONFIG_HOME`).

**Why a GitHub App at all (not GH_TOKEN/gh CLI).** Three separate reasons, each pinned to an
ADR:
- ADR 0009: a review's approval only counts toward GitHub's required-review check when its
  author `authorCanPushToRepository`; a plain user/PR-author token can't satisfy that as itself
  without also being able to merge, so the App is granted `contents: write` and holds an
  identity distinct from the PR author (so the tool can review/approve without self-approving).
- ADR 0006: no model-invoking code holds a GitHub credential at all — only `agtk` posts, via
  exactly one API call built from the judge's JSON — so the credential lives in a package
  (`internal/githubapp`) nothing that invokes a model imports.
- ADR 0012: the whole feature is designed to run on a developer laptop with no CI runner and no
  repository secret, specifically because Codex's `auth.json` credential can't safely be shared
  across concurrent/ephemeral processes (single-use refresh tokens) — this is the part that
  breaks down for an ephemeral *agtk* container, since the constraint ADR 0012 was solving was
  about the *coding-agent* CLI credential, not the GitHub App key, but the storage design (one
  registration file under `$XDG_CONFIG_HOME`, "once per machine") was carried over uniformly.

**Storage (verified in code, `source/toolkit/internal/githubapp/credential.go`).** Two files
under `agtk`'s config dir (`userconfig.Dir()`, i.e. `$XDG_CONFIG_HOME/agentic-toolkit/` or
`~/.config/agentic-toolkit/`): `github-app.id` (plain decimal app id, mode 0600) and
`github-app.pem` (RSA private key, PKCS#1 or PKCS#8, mode 0600), directory itself checked to be
0700. `Load` refuses (doesn't just warn) if the dir or key file is group/other-accessible. No env
var of any kind is read inside `githubapp` itself or `cli/codereview_initialize.go` — the App
credential stays file-based and machine-local, unconditionally. `GH_TOKEN`/`GITHUB_TOKEN` are
read, but only in `internal/cli` (`clientSeam.token`), for two purposes that never touch the App
credential itself: building a read-only fallback client when no registration exists at all, and
dispatching a configured relay's `workflow_dispatch` (`internal/relay`) when posting refuses for
the same reason. Neither ever reaches `internal/githubapp`, and a bare token is never itself the
identity that posts — a relay's own runner posts as the App, through its own registration,
elsewhere. The one non-interactive affordance for the
App credential itself is `register --key-stdin`, which reads the PEM from stdin instead of
`--key-file`, explicitly so a key can come from a secret manager "without being written to disk
first" — this is the natural hook for a setup script
(`agtk code-review register --app-id "$APP_ID" --key-stdin <<<"$PEM"`), but it still requires the
register step to run in every fresh container that wants to post, and still lands the key on disk
at the fixed path/mode afterward (Initialize always writes both files).

**Which subcommands need it.** `resolvePullRequest` (`cli/codereview_pr.go`) builds its client
only from `clientSeam.client`, i.e. only from the registration, and both a posting `run --pr` and
`approve` resolve through it — so posting and approving refuse on an unregistered machine unless
`relayOrRefuse` finds `AGTK_CODE_REVIEW_RELAY` set, in which case it dispatches that repository's
own workflow instead of refusing (`internal/relay`; ADR 0020). Reads are different: `explain --pr`
and `run --pr`'s `--dry-run`/`--no-post` paths resolve through `readPullRequest` instead, which
reads as the App when registered and otherwise falls back to a token from `GH_TOKEN` or
`GITHUB_TOKEN` (`githubapp.NewReadClient`), built in `internal/cli` rather than `internal/githubapp`
so the credential-holding package stays source-agnostic (`credential_surface_test.go`'s guard
against `os.Setenv`/`os.Environ` walks `internal/githubapp` and `internal/relay`, not
`internal/cli`). Both fallbacks trigger only when `githubapp.Unregistered(err)` is true — a
broken/partial registration (App id present, key missing) still refuses with "register again"
rather than being read or relayed around. The token client's type has no posting method and cannot
satisfy `reviewapprove.GitHub`, so a read-token reaching a post site is a compile error, and
`internal/relay` never imports `internal/githubapp` at all (an import-graph test enforces this),
so the package that carries a caller's token to the relay repository cannot reach the App's key
even by accident. `--json` refuses to relay (a relayed run reports only how the relay's own run
ended, not the review detail `--json` promises); `--force` and `--full` are accepted, because the
token fallback `run --pr` reads the pull request through always reports `ByViewer: false`, so the
relayed path already reviews the pull request whole whether or not either flag is passed. See
ADR 0019 for the read fallback's design and ADR 0020 for the relay's. Bare `explain` (no `--pr`)
needs no registration at all — it is local-only (manifest, diff profiling), per the comment block
at the top of `cli/codereview.go:14-27`.

**Installation lookup.** One App, no per-repo config: `githubapp.Client.bearer()`
(`client.go:211-245`) looks up the installation id per-repo lazily via `GET
/repos/{slug}/installation` (signed with the App's JWT) the first time a client is used, caches
it in memory for that process's life, then mints a short-lived installation access token. No
separate "reviewer" vs "approver" App — one registration serves `run`, `explain --pr`, and
`approve`.

**A CI/headless design now exists, as the relay.** `AGTK_CODE_REVIEW_RELAY=owner/name` names a
dedicated repository whose own GitHub Actions workflow holds an App registration (ADR 0020). What
it reruns differs by action: `approve` never runs a panel, so its relay reruns
`agtk code-review approve --pr` from scratch, exactly as a registered laptop would. `run --pr`'s
posting path runs its panel on the dispatching machine, through the same token fallback described
above, and hands the relay only the review already computed; the relay's own run is
`agtk code-review post`, which posts that review as the App and starts no model. Because of this,
the relay repository never needs a coding-agent credential of its own — ADR 0012's argument
against running in CI, that Codex's non-shareable `auth.json` needs a shared, standing runner,
does not apply to a workflow that never starts a model. What the relay provisions once,
deliberately, in a repository dedicated to nothing else, is only the App's own posting
credential, dispatched per relayed post rather than run as a general service. This does trade
away something the rerun design would have kept: `code-review post` trusts the review body it is
handed, so whoever can dispatch the relay workflow can make it post any review under the App's
real identity — accepted only because the relay repository's collaborators are the same people
whose pull requests it posts to (ADR 0020's Decision and Consequences).
