---
about: "hatua's definition-level validation checks expressions against its core functions only, so a Host function call reads as an unknown reference and cannot be validated through the SDK without copying or filtering its diagnostics"
saw:
  - source/toolkit/internal/definitions/workflowvalidate.go
  - source/toolkit/internal/definitions/hostmanifests.go
---

`hatua.ValidateDefinition` builds its function registry from `coreDeclarations()` alone, and
`hatua.Inputs` carries only Context and Connections, so `{{agtk.x(...)}}` is reported as
`EXPR_UNKNOWN_REFERENCE: Nothing named agtk is in scope here`. Host registration exists one layer
down (`expressions.HostFunctions`, `MergeRegistries`) but is not reachable from definition
validation. `ValidateWorkflowSource` therefore ships component and trigger Host manifests only,
and validation is `LoadDefinition` then `ValidateDefinition` with every diagnostic a failure.
Copying hatua's expression walk, or filtering its diagnostics, was tried and rejected as a drift
surface; the function manifest waits on a hatua change (hatua#158, agentic-toolkit#214).
