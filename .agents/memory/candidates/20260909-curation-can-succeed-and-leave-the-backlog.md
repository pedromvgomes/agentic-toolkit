---
about: a curate run can rule on every candidate and still fail to delete the files, so the backlog stays non-empty after a successful curation and the session-start digest keeps reporting work that is already done
saw:
  - internal/curator/curator.go
  - internal/cli/memory.go
---

`allowedTools` constructs a deletion grant scoped to the staging directory —
`Bash(rm <candidatesDir>/*)` at `internal/curator/curator.go:268`, deliberately scoped rather
than a bare `rm` so the one agent holding a constructed grant cannot remove anything else. The
grant is correct and is handed to the run.

It is not the only thing gating the delete. The provider applies its own working-directory
guard to deletion, separately from the tool allowlist, and that guard can refuse the same path
the grant permits. An observed run promoted three candidates, re-stamped a fourth, and then had
every deletion refused with `may only remove files from the allowed working directories for this
session`, naming a directory that was a parent of the files it was refusing. Absolute paths,
`cd` into the directory and `/bin/rm` were all refused identically.

The cause was not established — a realpath or symlink mismatch is consistent with the message
but was not confirmed.

What matters for anyone reading `stats`: **a non-empty backlog is not evidence that curation has
not run.** The notes may already exist and the index may already be current, with only the
staging files left behind. Check `notes/` and `agtk memory lint` before concluding there is work
outstanding, and clear the files from an ordinary shell if they are stale.
