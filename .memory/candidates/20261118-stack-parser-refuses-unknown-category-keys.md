---
about: "A stack file listing a category key the running agtk does not know fails to parse with unknown_field, so a shared stack adopting a new category breaks every consumer on an older binary until the binary release precedes the stack"
saw:
  - source/toolkit/internal/stack/parser.go
  - source/toolkit/internal/stack/tests/parser_test.go
---

`stack.ParseBytes` decodes with strict YAML, so a `workflows:` list (or any category key the binary
lacks) is refused as `unknown_field: unknown field "workflows"` rather than dropped. The entry
manifest has no per-category lists at all, so it refuses them the same way. The consequence for a
new Category is ordering: the binary release carrying it has to ship before any shared stack lists
it. `TestParseBytes_UnknownCategoryKeyIsAnUnknownField` pins the refusal.

Established by running the unmodified parser against a stack with a `workflows:` list.
