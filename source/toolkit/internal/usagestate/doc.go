// Package usagestate holds the usage Collector's per-machine state: the
// Ledger of rows already handed off and the Outbox of rows Ingest has not yet
// accepted. Both live under
// ${XDG_STATE_HOME:-~/.local/state}/agentic-toolkit/usage/, a directory agtk
// owns (mode 0700, files 0600) and nothing commits.
//
// Layout:
//
//	usage.lock    flock target serialising every reader and writer
//	ledger.json   {"version": 1, "files": {"<transcript path>": {"sent": ["<message id>", ...]}}}
//	outbox/       one file per pending batch
//
// The Ledger records which message ids of each Transcript have been recorded,
// so a repeated flush ships nothing twice. It is an efficiency device only:
// Ingest upserts on (session_id, message_id), so a row sent again is counted
// once. The Outbox holds the rows themselves, so a batch Ingest rejected
// survives a deleted transcript and is retried; it does not outlive a
// reclaimed container.
//
// Caller contract:
//
//   - Callers pass usage.Result.Rows and never usage.Result.Held. A message
//     still being streamed is absent from the Ledger, so it ships once it
//     completes.
//   - A fileKey is the absolute transcript path exactly as usage.ParseSession
//     sees it. Subagent transcripts are keyed by their own paths, each its own
//     key. The caller re-parses the whole transcript on every flush; the
//     message ids, not a byte offset, say what is new.
//   - Every file in the directory is read and written under the lock. A
//     process that cannot take it within a bounded wait gets ErrBusy rather
//     than hanging a hook.
//   - A Ledger that cannot be read is moved aside to a quarantine name and
//     treated as empty; re-sent rows are harmless.
//
// The lock is flock(2), so the package builds on unix only. Releases target
// darwin and linux, and no other platform has a stub.
package usagestate
