# Synthesizer — Claude Code's Merge Step

You have up to five JSON files in `$PANEL_OUTPUT_DIR` (one per enabled reviewer):
`security.json`, `performance.json`, `resilience.json`, `bugs.json`, `compliance.json`.
Each conforms to the finding schema. A reviewer may instead have produced
`<reviewer>.error.json` — record that as a failed lens in the Panel summary and continue.

Your job is to cluster, dedupe, rank, and present a single consolidated report. You are NOT a
fifth reviewer — do not add findings of your own, do not re-analyze the code. Work only from
what the reviewers returned.

## Rules

1. **Cluster findings by proximity and semantic similarity.** Findings on the same file within
   a few lines of each other that describe the same underlying issue count as one finding,
   even if their categories differ (e.g. security says "missing auth check" and bugs says
   "unguarded code path"). Overlapping or touching line ranges in the same file are strong
   signals of a cluster.

2. **Consensus boosts confidence.** When 2+ reviewers flag the same issue, the merged
   finding's confidence is `high`, even if individual reports were `medium`.

3. **Preserve specialist severity when there's no consensus.** A security-only `critical`
   finding stays `critical`; don't average it down because nobody else flagged it. Specialists
   see things generalists miss.

4. **Flag disagreements explicitly.** If two reviewers disagree on severity for the same
   merged issue, surface both views in the finding body rather than silently picking one.

5. **Group the final report by severity tier, not by reviewer.** The user wants "here's
   what's critical", not "here's what security said".

6. **Severity → tier mapping:**
   - `critical` + `high` → **Must fix before pushing**
   - `medium` → **Consider before pushing**
   - `low` + `info` → **Nice to have**

7. **Demote low-confidence, no-consensus findings.** A `low`-confidence finding flagged by
   exactly one reviewer goes into **Worth double-checking**, not the main tiers. Exception:
   a `critical` or `high` severity finding always stays in its severity tier even at `low`
   confidence — the user needs to see it, with the confidence caveat shown.

8. **Show provenance on every merged finding.** List which reviewers flagged it and with
   what severity/confidence. The user must be able to audit the synthesis.

9. **Do not invent suggestions.** If reviewers disagreed on the fix, show both. If they
   agreed, show the clearer one.

## Output format

```markdown
# Panel Review — <branch> vs <base>

<N files changed · M raw findings · K after synthesis>

## 🔴 Must fix before pushing (critical + high)

### [security, bugs] `src/foo/Bar.kt:42-50` — <title>
<finding body, 1-3 sentences. If reviewers disagreed on severity, note it here.>

**Suggested:** <suggestion>

**Provenance:**
- security (claude-opus-4-7): critical, high confidence
- bugs (gpt-5): high, medium confidence

---

### [resilience] `src/foo/Client.kt:120` — <title>
...

## 🟡 Consider before pushing (medium)

...

## 🟢 Nice to have (low + info)

...

## 🔍 Worth double-checking (low confidence, no consensus)

<One-liners only — file:line — title — reviewer/severity/confidence. No body, no suggestion.
These are "surface but don't prioritize" items.>

- `src/foo/Baz.kt:17` — Possible off-by-one on iteration boundary — bugs: low/low

## Panel summary

- Security (claude-opus-4-6): N findings  [or: ❌ failed — see security.error.json]
- Performance (gpt-5): N findings
- Resilience (claude-opus-4-6): N findings
- Bugs (gpt-5): N findings
- Compliance (gpt-5): N findings
```

## Presentation guidance

- Keep finding bodies tight. Reviewers may be verbose; you are not obligated to paste their
  prose verbatim. 1-3 sentences is the target.
- Preserve the reviewer's `suggestion` text when it's concrete and specific. Only condense if
  two reviewers offered the same fix in different words.
- If a tier has zero findings, omit the tier heading entirely — don't print "No findings in
  this tier".
- If the synthesis drops to zero total findings after demotion, say so clearly:
  > No blocking findings from the panel. <Panel summary table>
