---
about: for file-shaped categories the definition's own `name:` field becomes the overlay key, not the name the manifest wrote — so overrides and requires can silently miss
saw:
  - internal/resolver/resolver.go
  - internal/resolver/requires.go
  - internal/definitions/parser.go
---

Kind: **gotcha**.

The resolver keys the override overlay on the name the *parsed definition* reports, not on
the name the entry asked for: `walkedDef.Name = def.GetCommon().Name`
(`resolver.go:409`), and `loadStack` builds `defKey{Category, w.Name}` from that
(`resolver.go:213`).

Whether that equals the entry's name depends on the category, and the two halves behave
oppositely (`internal/definitions/parser.go:99-105`):

- **Bundle categories (skill, agent)** go through `ParseBundle`, which passes
  `strictName=true` — a `name:` disagreeing with the directory name is a parse *error*.
  Here entry name and key always agree.
- **File categories (rule, instruction, command, hook, mcp, setting)** go through
  `ParseFile` with `strictName=false`: "the file's `name:` field wins; derivedName is used
  only as a fallback when the file omits `name:`". `parseFromFS` does not even hand the
  entry name to `ParseFile` (`resolver.go:401`) — the fallback is the filename stem.

So a manifest that lists `rules: [style]` against a `style.md` whose frontmatter says
`name: house-style` produces a definition keyed `house-style`. Consequences a reader will
not expect:

1. **Overrides silently stop overriding.** Last-wins is keyed on `(category, name)`, so a
   downstream stack listing `style` to shadow the upstream one collides with nothing if the
   two files declare different `name:` values. Both render, no `DiagOverride` is emitted,
   and the stack looks like it was ignored.
2. **`requires:` double-checks for exactly this reason.** `pullOne` probes the overlay with
   the *guessed* key first (`requires.go:112`), resolves, and then re-checks under the key
   the parsed definition actually reports (`requires.go:130-133`) — the comment at
   `requires.go:110-111` names the cause. A requirement written as `rules/style` against a
   file declaring `name: house-style` is pulled in even when it is already present under its
   real key, right up until that second check catches it.

The asymmetry is the part to remember: renaming a *bundle* by editing its frontmatter fails
loudly; renaming a *file definition* the same way changes its identity silently.
