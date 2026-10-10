#!/usr/bin/env python3
"""Compare the probe's turn.step usage with the transcript's assistant usage for one session.

usage: compare.py <usage-probe-<session>.jsonl> <session>.jsonl [subagents-dir]
Prints message ids and counts only.
"""
import glob, json, sys

KEYS = ("input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens")


def transcript(path):
    rows = {}
    for line in open(path):
        r = json.loads(line)
        m = r.get("message") or {}
        if r.get("type") == "assistant" and m.get("usage"):
            rows[m["id"]] = (r.get("sessionId"), r.get("agentId"), tuple(m["usage"].get(k, 0) for k in KEYS), m.get("model"), r.get("isSidechain"))
    return rows


probe_path, tr_path = sys.argv[1], sys.argv[2]
tr = transcript(tr_path)
for p in glob.glob(sys.argv[3] + "/*.jsonl") if len(sys.argv) > 3 else []:
    tr.update(transcript(p))
steps = []
for line in open(probe_path):
    r = json.loads(line)
    if r["ev"] == "turn.step" and r.get("usage"):
        steps.append((r["agentId"], tuple(r["usage"][k] for k in KEYS), r["usage"]["model"]))
print(f"transcript messages: {len(tr)}  mod steps: {len(steps)}")
left = dict(tr)
for agent, counts, model in steps:
    hit = next((i for i, v in left.items() if v[2] == counts), None)
    print("step", agent, counts, model, "->", hit or "NO MATCH", "" if not hit else f"(transcript agentId={left[hit][1]}, sidechain={left[hit][4]})")
    if hit:
        del left[hit]
print("transcript rows unmatched by any step:", list(left))
