#!/usr/bin/env bash
# Records whether a hook can see AGTK_CODE_REVIEW_RELAY, and its length, never its value.
v="${AGTK_CODE_REVIEW_RELAY-}"
printf 'hook sees AGTK_CODE_REVIEW_RELAY: %s length=%s\n' "$([ -n "$v" ] && echo set || echo unset)" "${#v}" >> "$(dirname "$0")/hook.out"
