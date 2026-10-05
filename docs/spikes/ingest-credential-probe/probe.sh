#!/usr/bin/env bash
# Reports what GH_TOKEN / GITHUB_TOKEN resolve to without printing them.
# Output is limited to variable names, prefix class, HTTP status, latency, and
# non-secret response fields/headers.
set -u
scratch="$(mktemp -d)"; trap 'rm -rf "$scratch"' EXIT

echo "== credential-related env var names =="
env | cut -d= -f1 | grep -iE "^(gh_|github_|anthropic_|agtk_)" | sort

for v in GH_TOKEN GITHUB_TOKEN; do
  t="${!v:-}"
  if [ -z "$t" ]; then echo "== $v: unset"; continue; fi
  case "$t" in
    ghp_*) class="ghp (classic PAT)";; gho_*) class="gho (OAuth app)";;
    ghu_*) class="ghu (user-to-server)";; ghs_*) class="ghs (installation)";;
    ghr_*) class="ghr (refresh)";; github_pat_*) class="github_pat (fine-grained PAT)";;
    *) class="unrecognised";;
  esac
  echo "== $v: set, length ${#t}, prefix class: $class"
  curl -sS -o "$scratch/body.json" -D "$scratch/hdr.txt" \
    -w "GET /user status=%{http_code} time=%{time_total}s\n" \
    -H "Authorization: Bearer $t" -H "Accept: application/vnd.github+json" \
    https://api.github.com/user
  grep -iE '^(x-ratelimit-(limit|remaining|resource)|github-authentication-token-expiration|x-oauth-scopes|x-accepted-github-permissions):' "$scratch/hdr.txt"
  python3 -c "import json,sys;d=json.load(open(sys.argv[1]));print({k:d.get(k) for k in ('login','id','type','message')})" "$scratch/body.json"
  rm -f "$scratch/body.json" "$scratch/hdr.txt"
done
