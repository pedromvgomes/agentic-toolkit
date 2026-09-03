# The explorer stages candidates; only the curator writes notes

An explorer that finds a memory note stale re-checks the note's pointers as part of the work
it was already doing. It records the result as a **verdict** on a candidate
(`still-true | now-false | unchecked`) and leaves `notes/` untouched — it never authors, edits,
deletes or stamps a note, and never runs `agtk memory anchor`. `notes/` has exactly one writer,
the curator.

The tempting alternative was to let the explorer re-stamp a note it had just verified, since it
holds the evidence and the stamp is mechanical. That was rejected because `agtk memory anchor`
does not only record hashes — it *clears the only signal that says nobody has checked this
claim*. An explorer that stamped without genuinely verifying would launder a stale note into
looking fresh, and the failure is silent and permanent: the note now reads as current, so no
later audit will ever flag it again. The plan's premise is that a wrong note is worse than no
note precisely because it short-circuits the verification an agent would otherwise do, and
this is the cheapest way to manufacture one.

Keeping the roles split also puts the judgment where the context is. Promotion, dedupe and
merge decisions land in a curator with a fresh window and the whole store in view, not in a
session that has spent its budget on the task.

## Consequences
- `agtk memory audit` stays red between curator runs, even for a note an explorer verified by
  hand. That is a visible backlog rather than a silent inaccuracy, which is the right way round.
- The curator inherits verification it did not perform, so a candidate has to carry the
  evidence — the command run, the reading, the pointer that moved. That is why a candidate has
  a body and a `verdict` and not just a claim.
- Anchors detect *file* drift only. A body pointer that has moved from `:33` to `:35` is
  invisible to `audit`, so re-checking line numbers is the explorer's job and cannot be
  delegated to the deterministic surface.
- **The rule is prose, not enforcement.** The explorer's definition withholds `Edit`, which
  removes the obvious way to rewrite a note in place, but it grants `Bash` — so `sed -i` and
  `agtk memory anchor` stay reachable, and `Write` can name an existing note's path. Nothing
  available closes that: `disallowed_tools` is tool-scoped, not path-scoped, and a path-scoped
  `deny` in the shared settings definition was tried and backed out because it renders into the
  consumer's global config, where it would block the curator and any manual `agtk memory
  anchor` too. Restricting the explorer to a `Bash` command allowlist would be real
  enforcement; it is not done here, so the honest claim is that the invariant is instructed
  and guarded, not guaranteed.
