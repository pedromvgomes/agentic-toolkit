---
name: memory-config-is-entry-manifest-only
kind: gotcha
description: "A stack (via extends: or stacks:) that sets top-level memory: or platforms: fails to parse with ErrRepoOnlyField; only the entry manifest may set them, and docs/CONSUMER-GUIDE.md still says a stack's memory.root is silently ignored."
anchors:
  - path: source/toolkit/internal/stack/parser.go
    blob: c85fae1c0eb7
  - path: source/toolkit/internal/stack/entrymanifest.go
    blob: 7d07c11d4b26
  - path: source/toolkit/internal/stack/tests/parser_test.go
    blob: 26d8cf4b9e31
  - path: docs/CONSUMER-GUIDE.md
    blob: cefa5e21a515
confidence: verified
---

`stack.ParseBytes` calls `detectRepoOnlyFields` (`source/toolkit/internal/stack/parser.go:45`)
before decoding. It walks `repoOnlyTopLevelKeys = {"platforms", "memory"}` (`parser.go:394`) and
returns `ErrRepoOnlyField` (`parser.go:386-388`) the moment either appears at column zero,
naming the field and pointing at the entry manifest. It is a hard error, not a warning or a
silent drop; pinned by `TestParseBytes_PlatformsField_Rejected` and
`TestParseBytes_MemoryField_Rejected` (`stack/tests/parser_test.go:103-128`).

The entry manifest is its own type (ADR 0016): `ParseEntryManifestBytes`
(`stack/entrymanifest.go:89`) runs only the legacy check and decodes `Memory` and `Platforms`
into `EntryManifest` (`entrymanifest.go:32`). `stack.Stack` has no `Memory` field. The resolver
no longer has an "ignored memory config" diagnostic; `grep -n Memory
source/toolkit/internal/resolver/resolver.go` is empty.

`MemoryRootFromBytes` (`parser.go:454`) reads `memory.root` from raw bytes without that check,
but it serves the entry manifest, not a stack.

Stale text: `docs/CONSUMER-GUIDE.md:354-356` still says a `memory.root` in a stack reached through
`stacks:` "is deliberately ignored, so YAML and `agtk` disagree". That describes the older
behaviour; a stack setting it now errors. Asking `agtk memory stats --json` for `root` remains
the safe way to locate the store.

The key match is a line-anchored regex (`topLevelKeyRE`, `parser.go:400`), so a column-zero
`memory:` inside a block scalar would also trip it. See
[[stack-schema-is-strict-and-legacy-keys-are-intercepted]].
