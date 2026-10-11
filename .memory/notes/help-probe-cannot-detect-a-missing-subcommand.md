---
name: help-probe-cannot-detect-a-missing-subcommand
kind: gotcha
description: "A --help probe cannot tell whether an installed agtk has a subcommand: an older binary answers it with the parent help and exit 0."
anchors:
  - path: source/toolkit/internal/cli/memory.go
    blob: ab6ee22918af
  - path: definitions/skills/open-pr/SKILL.md
    blob: 1602c2ec7614
  - path: source/toolkit/internal/cli/tests/memory_candidate_prose_render_test.go
    blob: 65587d0c8207
confidence: verified
---

`agtk memory <unknown>` is rejected because the `memory` command declares `Args: cobra.NoArgs` and a
`RunE` (`cli/memory.go:34-45`). `--help` is not rejected: cobra handles the help flag before it
validates arguments, so a binary lacking the subcommand prints the parent `memory` help and exits 0.

Measured against `agtk` 0.17.0, which has no `memory hits`: `memory hits --help` and
`memory hits fold --help` exit 0 with the parent help (no `--check` in it); `memory hits fold` exits 1
(`unknown command "hits"`). A probe of the form `agtk memory <sub> --help >/dev/null 2>&1` therefore
passes on exactly the binaries it should reject.

What separates them is output only the newer binary prints:
`agtk memory hits fold --help 2>&1 | grep -q -- '--check'`, the probe in
`definitions/skills/open-pr/SKILL.md:118`. `TestOpenPRProbeAcceptsOnlyAnAgtkThatKnowsHitsFold`
(`cli/tests/memory_candidate_prose_render_test.go:117`) runs the rendered probe against fake old and
current `agtk` binaries. See also [[hook-must-tolerate-an-older-agtk]].
