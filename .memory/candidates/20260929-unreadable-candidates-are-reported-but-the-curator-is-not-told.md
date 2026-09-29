---
about: "a candidate with invalid frontmatter is reported under `unreadable` and counted by stats, but curator paths ignore that list and nothing tells writers to quote about"
saw:
  - source/toolkit/internal/memory/candidate.go
  - source/toolkit/internal/memory/stats.go
  - source/toolkit/internal/cli/memory.go
  - source/toolkit/internal/cli/jsonout.go
  - source/toolkit/internal/curator/curator.go
  - source/toolkit/internal/curator/prompt.md
  - definitions/agents/memory-explorer/AGENT.md
---

Reproduced on a build of HEAD in a scratch store (candidate with `about: nothing: in CI regenerates`
next to a good one):

- `agtk memory candidates --json` exits 0, `"staged": 2`, `candidates` holds only the good one, and
  the bad one is in `"unreadable"` as a string ("mapping value is not allowed in this context").
  Text mode prints `2 candidates staged:` and an `unreadable:` line. So it is not dropped from the
  command's output; it is dropped from `.candidates[]`, which is all a consumer reading only that
  array sees. Code: `LoadCandidates` collects errors (`memory/candidate.go:106-140`);
  `cli/memory.go:703-714`, `jsonout.go:181-193`. Test: `cli/tests/memory_test.go:778-825`.
- `agtk memory stats --json` `candidates` counts every `*.md` by directory listing
  (`memory/stats.go:81-88`), so it is NOT affected by the parse failure (scratch run: `"candidates": 2`).
- `agtk memory lint` does not look at candidates at all (`grep -i candidat memory/lint.go` -> 0 hits);
  scratch lint reported only "index is out of date".
- Curator side: `limitedCandidateIDs` discards the error list (`curator/curator.go:662-663`) so
  `--limit` never names an unreadable candidate, and `prompt.md:13-14` describes `candidates --json`
  as entries with `about/saw/body`, never mentioning `unreadable`. The snapshot in `verify.go:66-78`
  deliberately uses the directory listing, so it does see the file.
- Writers: the explorer's template shows an unquoted `about:` (`definitions/agents/memory-explorer/AGENT.md:154`)
  with no quoting instruction, while the curator prompt does tell the curator to always quote
  `description` (`prompt.md:85-88`). `open-pr` (`definitions/skills/open-pr/SKILL.md:78`) just says
  "carrying `about`, `saw` and a body".
- Parsing is strict (`candidate.go:83-96`, `yaml.Strict()`), so lenient parsing would be a behaviour
  change against the stated reason (a typo'd `targets:` reads as a new finding).
