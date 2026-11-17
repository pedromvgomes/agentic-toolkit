---
about: "only-lock-resolves-refs still true; glob anchor lost handoff.go, new cli files do not construct providers"
saw:
  - source/toolkit/internal/cli/lock.go
  - source/toolkit/internal/cli/sync.go
  - source/toolkit/internal/cli/fetch.go
  - source/toolkit/internal/cli/plan.go
  - source/toolkit/internal/cli/render.go
  - source/toolkit/internal/cli/status.go
targets: only-lock-resolves-refs
verdict: still-true
---
Stale only because the glob `cli/*.go` saw `handoff.go` removed and many files added/changed.
`grep -n 'NewLiveProvider\|NewFrozenProvider' source/toolkit/internal/cli/*.go` (non-test): live at
`lock.go:57` and `sync.go:72`; frozen at `fetch.go:42`, `plan.go:54`, `render.go:70`, `status.go:81`,
`sync.go:92`. New files (backlog.go, cloud.go, guard.go, prwait.go, codereview_post.go) construct
neither. Line numbers shifted by one in lock.go (:56 -> :57), render.go (:71 -> :70), status.go (:82 -> :81),
sync.go (:73 -> :72, :93 -> :92). The removed handoff.go was not named in the note.
