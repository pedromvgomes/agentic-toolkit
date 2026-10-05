---
name: probe
description: probe skill for spike
---
With Bash run exactly: `printenv SPAN_PROBE HOOKSET CLAUDE_CODE_SESSION_ID`. Then with Bash run exactly: `sh -c "printenv SPAN_PROBE HOOKSET"`. Then spawn one general-purpose subagent telling it to run with Bash exactly `printenv SPAN_PROBE HOOKSET CLAUDE_CODE_SESSION_ID` and reply with the output. Report all three outputs verbatim.
