---
about: sync decides whether to hit the network by comparing file mtimes, so a fresh clone re-locks and an mtime-preserving edit does not
saw:
  - internal/cli/sync.go
  - internal/cli/root.go
---

Kind: **gotcha**.

`agtk sync` re-locks against the network when `lockIsStale` says so
(`internal/cli/sync.go:68`), and that function (`sync.go:125`, decision at `sync.go:137`) is
purely:

```go
return cfgInfo.ModTime().After(lockInfo.ModTime()), nil
```

No hash, no content comparison, no record of which manifest produced the lockfile. Two
failure directions, both silent:

- **False stale → unexpected network.** The lockfile is
  `.agentic-toolkit.lock.yaml` and the manifest `.agentic-toolkit.yaml`
  (`internal/cli/root.go:96`, `:100`). `git clone`/`checkout` writes files in index order, and
  `.agentic-toolkit.lock.yaml` sorts *before* `.agentic-toolkit.yaml`, so the manifest is
  written last and its mtime is >= the lock's. Whenever the two land in different filesystem
  timestamp ticks, the first `agtk sync` on a fresh checkout re-locks. That is the one branch
  in the whole command that builds `sourcestore.NewLiveProvider` (`sync.go:75`) — every other
  path is frozen — so an offline or network-restricted CI runner fails here, on a repo whose
  committed lockfile was perfectly usable.
- **False fresh → stale render.** A manifest edit whose mtime does not advance past the
  lockfile's (mtime-preserving restore, an archive extract, same-tick writes) leaves `stale`
  false; sync then hydrates and renders from the old lockfile and prints nothing about it.
  `sync` only announces "sync: locking against the network" (`sync.go:74`) on the stale
  branch — silence is not evidence the lockfile matches the manifest.

Also note the mtime pair is cross-tree under `--source`: `configFilePath` resolves into
`SourceDir` while `lockfilePath` resolves into `WorkDir` (`internal/cli/paths.go`), so the
comparison is between a shared checkout's mtime and a local file's.
