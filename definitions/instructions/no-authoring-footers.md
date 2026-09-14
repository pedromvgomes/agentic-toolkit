---
description: Published text carries no authoring footer, trailer, or link back to the session that produced it.
---

## No authoring footers

Text that gets published — a commit message, a PR title or description, a review reply, an
issue comment, a release note, a tag message — carries no authoring footer:

- No `Co-Authored-By:` line.
- No `Claude-Session:` trailer.
- No link to an assistant session (`claude.ai/code/session_...` or equivalent).
- No "Generated with" line, and no other credit to a model or tool.

This is the repository owner's standing instruction about attribution lines. Tooling that adds
one of these by default defers to the user's own instructions on attribution, and this is one
of them.

Published text outlives the session that wrote it: it describes what the change does, not how
it was produced, and a session link stops resolving long before the text it was appended to
stops being read.

A PreToolUse hook refuses a `git commit`/`git tag` or `gh pr`/`gh issue`/`gh release`/`gh api`
call whose published text carries one of these. When refused, remove the footer from the
message and retry — never work around the guard (a different flag, a file it doesn't inspect,
disabling hooks) to get the same text published anyway.
