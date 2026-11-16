---
about: "ADR 0020 separate-repo reasoning is about App key blast radius (contents:write via ADR 0009), and would apply to any App that writes"
saw:
  - docs/adr/0009-the-app-can-push-so-that-it-can-approve.md
  - docs/adr/0012-reviews-run-locally-not-in-ci.md
  - docs/adr/0019-a-token-reads-a-pull-request-and-never-posts.md
  - docs/adr/0020-a-relay-posts-as-the-app-from-a-separate-repository.md
  - docs/adr/0027-cloud-init-signs-with-the-users-own-key-when-one-is-supplied.md
  - docs/adr/0002-no-model-calls-in-agtk.md
---
Read while answering a question on moving handoffs to GitHub issues driven by an unattended app.

- ADR 0020 puts the relay in a separate repo because the App's installation token carries contents:write
  on every installed repo (ADR 0009: needed so approvals count), so the raw key must have exactly one
  holder in a repo with no other traffic. It is not about labels vs reviews: any new App, or the same App
  reused, that mints installation tokens carries the same blast-radius argument, so a factory app
  holding its key in agentic-toolkit's own workflows is the case 0020 rejects. A new App with
  issues:write only does not inherit 0009's contents:write, which weakens but does not remove the argument.
- ADR 0012: no model credential in any repo secret and no CI runner; Claude's static token
  (CLAUDE_CODE_OAUTH_TOKEN) suits a secret, codex auth.json does not. An unattended Claude Code cloud
  session is therefore the only shape consistent with 0012 for model work; a dispatcher workflow here
  would be the first workflow-hosted component that holds any App key, which 0020 forbids in this repo.
- ADR 0019: a bare GH_TOKEN reads and never posts, enforced by type (githubapp.ReadClient) and by a
  guard keeping internal/relay from importing internal/githubapp. Anything that writes labels/comments
  as an app needs its own registration path; ReadClient clears ByViewer, so issue bookkeeping that
  trusts "written by the app" must use a separate identity check.
- ADR 0002: agtk calls no model; a dispatcher that merely labels and fires sessions is deterministic and
  is consistent, provided agtk itself never invokes the session (only `memory curate` does).
- ADR 0027: cloud init sets identity/signing only; env vars AGTK_GH_USER/AGTK_GH_EMAIL/AGTK_SIGNING_KEY_B64;
  nothing about GitHub API auth. A session's GitHub access is the platform's (MCP tools) per the
  consumer CLAUDE.md; the note cloudinit-is-environment-surface-not-credential-surface says cloudinit
  is banned from the credential surface.
