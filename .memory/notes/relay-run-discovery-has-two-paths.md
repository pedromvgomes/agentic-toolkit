---
name: relay-run-discovery-has-two-paths
kind: gotcha
description: relay.Await finds its dispatched run by id when GitHub returns one, else by matching the run title to a run-name in the separate relay repository, which no test here can check.
anchors:
  - path: source/toolkit/internal/relay/relay.go
    blob: 1254d81c9b7f
  - path: source/toolkit/internal/relay/await.go
    blob: 5abf87310773
confidence: suspect
---

`Dispatch` sends `return_run_details` (`relay.go:170`). If GitHub answers with a run id, `Await`
reads that run directly. Otherwise (a plain 204; GitHub has changed and reverted this endpoint)
`readRun` lists the workflow's recent runs and matches `display_title` (`await.go:33`) against
`runName(req)` (`await.go:23`, `"<action> <repo>#<pr>"`) plus a since-cutoff truncated to the
second.

That match depends on the relay repository's own workflow having `run-name: ${{ inputs.action }}
${{ inputs.repo }}#${{ inputs.pr }}`; `runName`'s doc comment (`await.go:15-22`) states the
contract. A mismatch matches nothing and `Await` runs out its timeout. The two live in different
repositories, so no test here catches drift.

Suspect: the relay repository's actual workflow file and GitHub's dispatch behaviour were not
checked here.
