---
name: unreadable-candidates-surface-in-four-places-and-parsing-stays-strict
kind: gotcha
description: A candidate that does not parse (e.g. an unquoted colon in about:) is named by lint, candidates, stats and curate, never curated or cleared; parsing stays strict on purpose.
anchors:
  - path: source/toolkit/internal/memory/candidate.go
    blob: 9392281d59b9
  - path: source/toolkit/internal/memory/lint.go
    blob: 69162c090427
  - path: source/toolkit/internal/memory/stats.go
    blob: 604c78753358
  - path: source/toolkit/internal/cli/memory.go
    blob: ac2b4d13579f
  - path: source/toolkit/internal/curator/curator.go
    blob: cb07c4d5ea3f
  - path: definitions/agents/memory-explorer/AGENT.md
    blob: 02e85111f1fc
  - path: definitions/skills/open-pr/SKILL.md
    blob: 3d7115737e03
confidence: verified
---

An unquoted colon inside `about:` is invalid YAML and the candidate drops out of `LoadCandidates`.
Every load failure is a `*memory.UnreadableCandidateError` carrying the file
(`memory/candidate.go:101-112`, produced at `:140` and `:145`). Where it is reported:

- `agtk memory lint`: `Store.LintCandidates` (`memory/lint.go:108-133`) makes an "unreadable
  candidate: ..." issue and the CLI appends it (`cli/memory.go:469-472`), so lint exits non-zero
  (test `cli/tests/memory_test.go:345`).
- `agtk memory candidates`: text mode prints `unreadable:` lines (`cli/memory.go:755`); `--json`
  carries an `unreadable` list beside `candidates`, never null (`cli/memory.go:716`,
  `cli/jsonout.go:181-193`). `staged` counts both; a consumer of `.candidates[]` alone sees only
  the readable ones (test `memory_test.go:830`).
- `agtk memory stats`: `candidates` counts every `*.md` in the directory (`memory/stats.go:81-88`).
- `agtk memory curate`: `Run` re-reads candidates after the child ends (`curator.go:619`), lists
  them in `Result.Unreadable` and returns `unreadableErr` (`curator.go:629-631`, `:657-671`), dry
  runs included; the CLI prints them (`cli/memory.go:971`, `:980-982`). The curator is told not to
  touch them (`prompt.md:13-16`, test `curator/tests/unreadable_test.go:185`), a limited run skips
  and counts them (`curator.go:713-724`, `:755-765`), a backlog run does not fail for leaving one
  (`verify.go:215`), and clearing never removes one (`curator.go:624`, test `unreadable_test.go:118`).

Parsing stays strict: `ParseCandidate` decodes with `yaml.Strict()` (`memory/candidate.go:92`),
because a typo'd key would otherwise be dropped silently and a candidate that lost `targets:` would
read as a new finding instead of a re-check (`candidate.go:79-84`).

Writers are told to quote: `AGENT.md:154` and `:170-173`; `open-pr/SKILL.md:82-85`, with the writer
running `agtk memory lint` after staging (`:87-93`).
See also [[lint-excludes-unreadable-candidates-from-verification]].
