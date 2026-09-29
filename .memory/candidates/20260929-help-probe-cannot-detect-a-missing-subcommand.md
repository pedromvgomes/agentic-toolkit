---
about: "A --help probe cannot tell whether an installed agtk has a subcommand: an older binary answers it with the parent help and exit 0"
saw:
  - source/toolkit/internal/cli/memory.go
  - definitions/skills/open-pr/SKILL.md
  - source/toolkit/internal/cli/tests/memory_candidate_prose_render_test.go
---

`agtk memory <unknown>` is rejected: the `memory` command declares `Args: cobra.NoArgs` and a `RunE`
so the validation is reachable (`cli/memory.go:34-45`), and the comment there names the danger of an
agent reading help text as a command's output. `--help` is not rejected. Cobra handles the help flag
before it validates arguments, so on a binary that lacks the subcommand, `--help` prints the parent
`memory` help and exits 0.

Measured against `agtk` 0.17.0, which has no `memory hits`:

- `agtk memory hits --help` exits 0 and prints the parent `memory` help.
- `agtk memory hits fold --help` exits 0 and prints the parent help, with no `--check` in it.
- `agtk memory hits fold` exits 1: `unknown command "hits" for "agtk memory"`.
- `agtk memory hits fold --check` exits 1: `unknown flag: --check`.

A probe of the form `agtk memory <sub> --help >/dev/null 2>&1` therefore passes on exactly the
binaries it is meant to reject, and the caller then runs the subcommand and meets the error the probe
was supposed to spare it. What separates the two is output only the newer binary prints:
`agtk memory hits fold --help 2>&1 | grep -q -- '--check'` exits 0 on the current binary and non-zero
on 0.17.0. That is the probe `open-pr` uses (`definitions/skills/open-pr/SKILL.md`, step 2).

A rendered-text test cannot see this. `TestOpenPRProbeAcceptsOnlyAnAgtkThatKnowsHitsFold`
(`cli/tests/memory_candidate_prose_render_test.go`) runs the probe extracted from the rendered skill
under `sh -c` against a fake `agtk` that behaves like the older binary and one that behaves like the
current one, and fails if the probe is the `--help`-only form.

Established by running the saved 0.17.0 binary with `--help` and `--version` and with the two
non-help invocations above, and by running the new test against the old probe text.
