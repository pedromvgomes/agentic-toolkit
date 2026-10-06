# The dashboard knows a GitHub user, and ingest knows a per-user key

The Worker (`docs/adr/0025-usage-data-lives-in-d1-behind-a-worker-and-cost-is-computed-at-query-time.md`)
faces two callers that have nothing in common. A person opens the dashboard in a browser and reads
every row they own. A collector, running in a terminal hook or in a cloud container that is
reclaimed when the session ends, posts rows and reads nothing. Each needs an identity the Worker can
trust, and the two should not share a credential, because one can read and the other must not. The
probe in `docs/spikes/ingest-credential-probe/` established what a cloud session can hold; the
ingest half of this decision is built on those observations.

## Decision

**The dashboard sits behind Cloudflare Access with the GitHub identity provider.** Access admits only
an allowlist of GitHub logins. The Worker does not rely on Access having admitted the request: it
verifies the `Cf-Access-Jwt-Assertion` token against the Access team's signing keys and the
application's audience tag on every dashboard and query request, and checks the identity in it
against the same allowlist. A request that reaches the Worker's own hostname without passing
through Access carries no valid token and is refused. A person is identified by their GitHub login;
the Worker holds no password, no session store and no OAuth client secret of its own.

**Ingest is authenticated by a per-user ingest key and by nothing else.** A key is a random value of
at least 256 bits that the owner generates once per person, not per machine, and stores as an
environment secret that reaches `agtk` and its hooks (the spike observed that environment variables
reach both). The collector sends it as a bearer token to the ingest endpoint only. The Worker stores
a SHA-256 hash of each key with the GitHub login it belongs to, the key's id and its creation time,
and never the key itself. A key has full entropy, so a fast hash is enough: there is no low-entropy
secret for a slow one to protect. Lookup is by hash; an unknown hash is refused.

**A key can write rows and cannot do anything else.** The ingest endpoint accepts a batch of rows
and returns a count. No route a key can reach returns a row, a total or the existence of one. A key
presented to a query or dashboard route is refused; an Access identity presented to the ingest route
is refused. The two credential types never overlap on a route.

**The Worker stamps the identity, the collector does not supply it.** Every row is stored with an
`owner`, the GitHub login the presented key maps to, and the `key_id` of the key that wrote it. A
request body that carries an owner or a key id is ignored for those columns. A row's owner therefore
says who the Worker authenticated, and revoking a key can find and remove what that key wrote.

**The Claude account is a separate dimension and is data about the row.** A row also carries
`account_uuid` and `organization_uuid`, the Claude account and organization the session ran under,
which the collector reads from the session's own records. They are not an identity the Worker
verifies and they grant nothing. One owner can have several Claude accounts, a personal one and an
organization seat; a drill-down by Claude account is a grouping over this column within the owner's
rows, and never a way to read another owner's. A missing value is stored as null and reported as
unknown rather than guessed.

**Commits showing as unverified plays no part in ingest authentication.** Commit signature
verification is GitHub's statement about who authored a git object. Ingest does not read, push or
inspect a commit, and the credential the Worker checks is a key presented in a request. A session
whose commits show unverified authenticates to ingest exactly as one whose commits are signed, and a
signed commit is not an ingest credential. The dashboard login is likewise established by Access's
GitHub sign-in, not by anything in a repository's history.

## Considered options

**Present the session's `GH_TOKEN` to the Worker and verify it with `GET /user`.** Rejected as the
credential. The spike found the token resolves to the owning user, but its value is 14 characters
with no GitHub token prefix, which looks like a stand-in that the session's egress proxy substitutes
on requests to GitHub; whether a request to a Worker carries a verifiable token is not established.
Sending the real token anywhere would also hand a service a credential that can read every
repository the owner can, in order to prove a name. Worker-side verification adds roughly half a
second and a rate-limited GitHub call to every ingest unless it is cached, and the token's expiry is
a property of the session, which a long session can outlive.

**One shared ingest key for everyone.** Rejected. Every row would be stamped by the Worker with no
owner to name, one leak would affect every user, and revoking it would stop all of them at once.

**A per-user key checked against GitHub as a second factor.** Not adopted. It keeps every cost of
the GitHub-token option and adds a second thing that can fail on the ingest path. The key already
maps to an owner by the Worker's own record. It can be added later without changing the key's
meaning, once a Worker is shown to receive a verifiable token.

**Cloudflare Access service tokens for ingest.** Rejected. A service token authenticates a client at
Cloudflare's edge, but issuing and revoking one per person happens in the Cloudflare account and not
from the owner's own tooling, and the Worker would still have to map the token's identity to an owner
and a revocable key id. The key table gives both in one place.

**Implement GitHub OAuth in the Worker.** Rejected for the dashboard. The Worker would own an OAuth
client secret, session cookies, CSRF handling and logout, none of which has anything to do with
usage data, and every one is a place to be wrong.

**Access with a one-time email PIN.** Rejected. The decision for the dashboard is that identity is a
GitHub user, and an email address is not one.

## Threat considered

A leaked ingest key is the credential most likely to escape: it lives in an environment secret of a
container that runs untrusted repository code. What the holder can do is add rows stamped with the
owner it maps to. They cannot read any row, including the owner's, cannot write as another user, and
cannot reach the dashboard, because no route accepts a key for reading.

The remaining harm is bounded and recoverable:

- **Polluted totals.** Forged rows inflate the owner's cost and attribute it to arbitrary
  projects and skills. Rows keep their `key_id`, so revoking the key and deleting its rows undoes it.
- **Overwritten rows.** Ingest is an upsert on `(session_id, message_id)` and a later write replaces
  an earlier one (ADR 0025), so a holder who knows an id can replace that row's figures. The id is
  not secret to the key's owner, so this is within what the key already allows.
- **Resource use.** The ingest route enforces a request size limit and a per-key rate limit, so a
  leaked key cannot be turned into a way of exhausting the D1 allowance.

A leaked Access identity is the opposite: a read-only view of the owner's data, bounded by the
allowlist, and revoked by removing the login from it. The two credentials do not unlock each other.

## Known limits

- Access exposes the signed-in user's email and identity-provider claims; whether the GitHub login
  itself is present in the token the Worker receives is unverified. If it is not, the allowlist is
  keyed on whatever stable GitHub claim is present, and the login is resolved from it.
- A GitHub login can be renamed. The key table stores the login at creation, so a rename needs the
  mapping updated by hand.
- A Worker's view of a cloud session's `GH_TOKEN` was not measured, and neither was how an
  environment secret behaves when masked. The key's delivery to `agtk` and hooks was observed with a
  non-secret variable only.
- Account and organization UUIDs are reported by the collector. A holder of a key can write any
  value into them, which is the same trust the owner's other row data already carries.

## Consequences

- A person who adds a machine copies one secret into that environment. A person who loses one
  deletes its row and issues another; no change is needed in GitHub or in Cloudflare Access.
- The Worker needs three stores of identity: the Access allowlist, the key table, and its own
  configuration of the Access audience and team domain.
- The collector holds a write-only credential and no GitHub credential. A cloud session's only
  outbound write remains a request to the Worker, as ADR 0025 requires.
- Every query is scoped to the Access identity's owner; there is no route that aggregates across
  owners, so the dashboard's Claude-account drill-down never widens what a person can see.
- Verifying a GitHub token at ingest remains available as an additional check and changes none of
  the decisions above.
