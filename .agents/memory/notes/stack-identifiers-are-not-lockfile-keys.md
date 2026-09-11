---
name: stack-identifiers-are-not-lockfile-keys
kind: gotcha
description: A stack's identifier is built from the raw URL and the ref as written, while its lockfile row uses the split repo URL and the resolved ref — the two never join.
anchors:
  - path: source/toolkit/internal/resolver/resolver.go
    blob: 8095f3afd96d
  - path: source/toolkit/internal/resolver/types.go
    blob: 4fd32a68d9d0
confidence: verified
---

`loadExtends` derives two different keys from one `extends:` entry
(`source/toolkit/internal/resolver/resolver.go:234-266`):

- the **source** row: `s.sources.add(repoURL, rr.Ref, rr.SHA, SourceStack)` (`:245`) — the
  repo half of the URL, in-repo path stripped by `splitGitURL` (`:237`), and the provider's
  *resolved* ref.
- the **stack identifier**: `identifier := ext.URL + "@" + ext.Ref` (`:254`) — the full URL
  *including* the in-repo path, and the ref exactly as the user typed it.

That identifier is what lands in `Plan.StackOrder` (built from `s.order`, appended at
`resolver.go:229`, read at `:91`) and in every `PlannedDefinition.StackName`
(`resolver.go:413`). So `StackName` cannot be joined to `Plan.Sources` by string: the two
differ in the path segment and, whenever the manifest left the ref off, in the ref (`""` vs
the resolved branch name). Anything wanting the sha a definition came from must go through
`PlannedDefinition.SourceURL`/`SourceRef`, which *are* set from `repoURL` and `rr.Ref`
(`resolver.go:359-365`, `:411-412`).

The same split has a second effect. Visit dedupe is on the identifier
(`resolver.go:173`, `:176`), so one stack reached twice — once written `…@main`, once with
the ref omitted — is two identifiers, hence visited and applied **twice**, appending its id
twice to `StackOrder`, while the source table collapses both to a single row because `rr.Ref`
resolved to the same branch. An adapter doing last-wins tiebreaking over `StackOrder` (which
`source/toolkit/internal/resolver/types.go:40-45` says is what it is for) sees a duplicate with no
counterpart in the lockfile.

Note the contrast with local-path extends (`resolver.go:270`), whose identifier is built from
the *parent's* already-resolved `SourceURL@SourceRef` plus the child path, and so does not
carry the raw-vs-resolved discrepancy.
