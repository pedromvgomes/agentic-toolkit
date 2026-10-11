---
name: remote-slug-refuses-nested-paths-and-reads-config
kind: gotcha
description: gitremote.RemoteSlug reads remote.<n>.url via git config (not get-url) and refuses any path that is not exactly owner/repo, so it rejects the cloud local-proxy remote.
anchors:
  - path: source/toolkit/internal/gitremote/gitremote.go
    blob: c15558dc1792
  - path: source/toolkit/internal/gitremote/tests/gitremote_test.go
    blob: 462dc8bad5d1
confidence: suspect
---

`RemoteSlug(dir, remote)` (`gitremote/gitremote.go:50`) runs `git config --get remote.<n>.url`
(`:60`), deliberately not `git remote get-url`, which applies `url.<base>.insteadOf`.

`slugRE` (`:44`) is end-anchored with no host check; a path with more than two segments is refused,
not truncated (tests: `gitremote/tests/gitremote_test.go:9,43`). By reading the regex,
`http://local_proxy@127.0.0.1:PORT/git/owner/repo` has three segments and is refused; not run against
a real cloud session, hence suspect. Any Project/Repo derivation that must work in cloud sessions
needs its own handling of that form.
