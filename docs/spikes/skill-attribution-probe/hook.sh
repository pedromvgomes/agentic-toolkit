#!/bin/bash
# Appends each hook event's stdin JSON and the env a hook sees to hooks.log.
# SessionStart is the only event that is given CLAUDE_ENV_FILE; the line it appends
# is exported into every later Bash call of the session.
in=$(cat)
{
  echo "=== $(date +%s.%N) argv=$*"
  echo "$in"
  echo "ENV: SPAN=${SPAN_PROBE-unset} HOOKSET=${HOOKSET-unset} SID=${CLAUDE_CODE_SESSION_ID-} ENVFILE=${CLAUDE_ENV_FILE-}"
} >> "$CLAUDE_PROJECT_DIR/hooks.log"
case "$1" in
  SessionStart) [ -n "$CLAUDE_ENV_FILE" ] && echo "export HOOKSET=from-$1" >> "$CLAUDE_ENV_FILE" ;;
esac
exit 0
