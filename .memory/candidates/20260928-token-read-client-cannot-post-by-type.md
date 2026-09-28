---
about: githubapp.ReadClient cannot post to GitHub because its type has no posting method and never exposes the *Client it wraps; a write method added to it would only be caught by a reflection test, not the compiler alone
saw:
  - source/toolkit/internal/githubapp/client.go
  - source/toolkit/internal/cli/tests/credential_surface_test.go
  - source/toolkit/internal/reviewapprove
  - docs/adr/0019-a-token-reads-a-pull-request-and-never-posts.md
---

`ReadClient` (`internal/githubapp/client.go`) wraps an unexported `c *Client` field and does not
embed it, so no method `*Client` has is promoted onto `ReadClient` — only the three `Read*`
methods `ReadClient` declares itself exist on it. `reviewapprove.GitHub` requires `CreateReview`
among its methods, and posting call sites in `internal/cli` call `CreateReview`/
`CreateFileComment` directly on a `*githubapp.Client`; `*ReadClient` satisfies neither, so wiring
it into either fails at compile time rather than at a runtime check somebody has to remember to
write.

That guarantee holds only as long as `ReadClient` never gains a method whose name isn't prefixed
`Read`. Nothing in the type system enforces that discipline going forward — it holds today
because the type happens to have no other methods, not because Go refuses one. The guard against
regressing it is `TestATokenClientReadsAndNothingElse`
(`internal/cli/tests/credential_surface_test.go`), which reflects over every exported method of
`*githubapp.ReadClient` and fails if any method name doesn't start with `Read`, and separately
asserts `!reflect.TypeFor[*githubapp.ReadClient]().Implements(reflect.TypeFor[reviewapprove.GitHub]())`.
Removing or weakening that test is what would let a future change quietly turn this from a
compile-time impossibility into an unenforced convention.
