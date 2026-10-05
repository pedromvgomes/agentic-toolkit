# Ingest credential probe

Which credential can a cloud session present to the ingest Worker: a GitHub token that proves the
session's owner, or a separate per-user ingest key? `probe.sh` and `hook.sh` establish what a cloud
session holds without printing any secret.

## Run

From a cloud session:

```bash
bash probe.sh
claude -p "reply with the single word ok" --settings <(printf '{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"%s/hook.sh"}]}]}}' "$PWD") </dev/null
cat hook.out
```

`probe.sh` lists the credential-related variable names, then for `GH_TOKEN` and `GITHUB_TOKEN`
reports only whether each is set, its length, its prefix class, and the non-secret parts of
`GET /user`: status, latency, `login`, `id`, `type`, the rate-limit headers and the token-expiry
header. `hook.sh` records whether a hook sees `AGTK_CODE_REVIEW_RELAY`, and its length.

## Findings

Measured in a cloud session.

| Question | Finding |
|---|---|
| What `GH_TOKEN` / `GITHUB_TOKEN` resolve to at `GET /user` | The owner: `type: User`, the same `login` and `id` as the account that started the session. Not an installation, not a bot. Both variables resolve identically. |
| What the values look like | 14 characters with no `ghp_`/`ghu_`/`ghs_`/`github_pat_` prefix, far shorter than a real GitHub token. They are likely stand-ins the session's egress proxy replaces on requests to GitHub. The proxy documentation does not say, so this is unconfirmed. |
| Lifetime | `github-authentication-token-expiration` reports an absolute time on the day of the session. Whether it outlives a long session depends on the session's start and length, which the probe does not measure. |
| Rate limit | 15000 requests per hour on the `core` resource. |
| `GET /user` latency | 0.2 to 0.5 seconds from the container to GitHub. Not measured from a Worker. |
| A cloud environment variable reaching `agtk` | Yes. `agtk` reads `AGTK_CODE_REVIEW_RELAY` through `getenv` in `internal/cli/codereview_pr.go`, and the variable is set in the session. |
| A cloud environment variable reaching hooks | Yes. A `SessionStart` hook run by `claude -p` sees `AGTK_CODE_REVIEW_RELAY` set, with the same length as the shell sees. |

## Not established

- Whether a Worker receives a verifiable token. If the proxy swaps the stand-in only for requests
  to GitHub, a Worker that forwards the token to `GET /user` gets the stand-in and the call fails.
  Settling it needs a Worker that reports only the length and prefix of the `Authorization` token
  it receives. Sending `GH_TOKEN` to a host this repository does not control is avoided.
- How a secret kept in the environment's secrets setting is exposed. `AGTK_CODE_REVIEW_RELAY` is
  a repository name, not a secret, so it shows that variables reach `agtk` and hooks but not how a
  masked value behaves.
- Latency and rate-limit headroom for a Worker-side `GET /user` on the ingest hot path.

## Recommendation

1. **Primary credential: a per-user ingest key held as an environment secret.** It reaches `agtk`
   and hooks, and it does not depend on the egress proxy or on the GitHub token's expiry.
2. **GitHub-token verification as an optional second factor.** `GET /user` identifies the owner at
   a cost of roughly half a second, so the Worker verifies once per key and caches the result
   rather than calling GitHub on every ingest.
3. **GitHub-token verification alone is not adopted** until the Worker is shown to receive a
   verifiable token.
