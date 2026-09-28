---
about: a stack file (reached via `extends:` or an entry manifest's `stacks:`) that sets top-level `memory:` or `platforms:` fails to parse — it is a hard error, not silently ignored
saw:
  - source/toolkit/internal/stack/parser.go
  - source/toolkit/internal/stack/tests/parser_test.go
---

`ParseBytes` calls `detectRepoOnlyFields(filePath, raw)` before decoding
(`internal/stack/parser.go:45`). That function walks `repoOnlyTopLevelKeys =
[]string{"platforms", "memory"}` (`parser.go:394`) and returns `ErrRepoOnlyField`
(`parser.go:384-393`) the moment either key appears at the top level of the raw YAML, with a
message naming the field and pointing at the entry manifest: `"%q is a repo property; set it in
the entry manifest (.agentic-toolkit.yaml), not in a stack."`

Covered by `internal/stack/tests/parser_test.go:101-128`
(`TestParseBytes_PlatformsField_Rejected`, `TestParseBytes_MemoryField_Rejected`) — both assert
`ErrRepoOnlyField`, not a warning or a silent drop.

`MemoryRootFromBytes` (`parser.go:454-465`) does read `memory.root` from raw bytes without this
check, but its doc comment and its only callers are about the **entry manifest**, not a stack —
it is not a code path a `stacks:`/`extends:` file ever reaches, so it does not soften the
parse-error behavior above.

`docs/CONSUMER-GUIDE.md` (around line 355-356, "a `memory.root` in a stack reached through
`stacks:` is deliberately ignored, so YAML and `agtk` disagree") is stale against this and
should be corrected to state the parse error instead.
