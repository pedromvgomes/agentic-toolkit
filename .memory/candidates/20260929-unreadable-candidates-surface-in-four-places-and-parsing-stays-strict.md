---
about: "a candidate that does not parse is named by lint, candidates, stats and curate, is never counted as curatable, and is never cleared"
saw:
  - source/toolkit/internal/memory/candidate.go
  - source/toolkit/internal/memory/lint.go
  - source/toolkit/internal/memory/stats.go
  - source/toolkit/internal/cli/memory.go
  - source/toolkit/internal/cli/jsonout.go
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/prompt.md
  - definitions/agents/memory-explorer/AGENT.md
  - definitions/skills/open-pr/SKILL.md
---

An unquoted colon inside `about:` is invalid YAML and the candidate drops out of
`LoadCandidates`. Every load failure is a `*memory.UnreadableCandidateError` carrying the file
(`memory/candidate.go:101-112`, produced at `:140` and `:145`). Where it is reported:

- `agtk memory lint`: `Store.LintCandidates` (`memory/lint.go:108-133`) turns each into an issue
  "unreadable candidate: ..." with the file, and the CLI appends it to `Store.Lint`'s issues
  (`cli/memory.go:469-472`), so lint exits non-zero. Test `cli/tests/memory_test.go:345`.
- `agtk memory candidates`: text mode prints an `unreadable:` line per file
  (`cli/memory.go:755`) and `--json` carries an `unreadable` list beside `candidates`, never null
  (`cli/memory.go:716`, `cli/jsonout.go:181-193`). `staged` counts both. A consumer reading only
  `.candidates[]` sees the readable ones. Test `cli/tests/memory_test.go:830`.
- `agtk memory stats`: `candidates` counts every `*.md` in the directory
  (`memory/stats.go:81-88`), so a candidate that does not parse is still counted.
- `agtk memory curate`: `Run` reads candidates again after the child ends
  (`curator.go:619`), lists the unreadable ones in `Result.Unreadable` and returns
  `unreadableErr` (`curator.go:629-631`, `:657-671`), dry runs included. The
  CLI prints `unreadable:` lines or an `unreadable` JSON list (`cli/memory.go:971`, `:980-982`).
  The curator is told not to touch them (`prompt.md:13-16`, test
  `curator/tests/unreadable_test.go:185`), a limited run skips them and names their count
  (`curator.go:713-724`; `limitedCandidateIDs` `:755-765` returns them), a backlog run does not
  fail for leaving one staged (`verify.go:215`), and clearing never removes one
  (`curator.go:624`, test `unreadable_test.go:118`).

Parsing stays strict: `ParseCandidate` decodes with `yaml.Strict()` (`memory/candidate.go:92`),
because a typo'd key would otherwise be dropped in silence and a candidate that lost its
`targets:` would read as a new finding instead of a re-check (`candidate.go:79-84`). Lenient
parsing would trade this failure for that one.

Writers are told to quote: `definitions/agents/memory-explorer/AGENT.md:154` shows a quoted
`about` and `:170-173` says to always quote it; `definitions/skills/open-pr/SKILL.md:82-85` says
the same and `:87-93` has the writer run `agtk memory lint` after staging.
