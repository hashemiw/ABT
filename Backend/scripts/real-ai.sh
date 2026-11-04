#!/usr/bin/env bash
# Local control for real (paid) Gemini calls. Edits only Backend/.env (git-ignored, chmod 600). Never prints the key.
#   scripts/real-ai.sh key          prompt for the key (hidden input; not in shell history, argv or logs)
#   scripts/real-ai.sh on <model>   AI_PROVIDER=gemini + both opt-in flags + model (does NOT restart anything)
#   scripts/real-ai.sh cap <n>      set the persistent real-call cap (REAL_AI_MAX_CALLS); raising it is a deliberate act
#   scripts/real-ai.sh off          back to the offline mock (flags false)
#   scripts/real-ai.sh status       flags, whether a key is set, calls used vs cap
# After on/off/cap run: docker compose up -d api worker   (Compose reads .env; shell exports are not used)
set -euo pipefail
cd "$(dirname "$0")/.."
ENV=.env
[ -f "$ENV" ] || cp .env.example "$ENV"
chmod 600 "$ENV"

setvar() { # setvar NAME VALUE  (value arrives on stdin so it never appears in argv)
  python3 -c '
import sys,re
name=sys.argv[1]; val=sys.stdin.read().rstrip("\n")
lines=open(".env").read().splitlines()
out=[l for l in lines if not l.startswith(name+"=")]
out.append(name+"="+val)
open(".env","w").write("\n".join(out)+"\n")' "$1"
}

case "${1:-status}" in
  key)
    read -r -s -p "Paste your Google AI Studio API key (input hidden): " k; echo
    [ -n "$k" ] || { echo "empty key, nothing changed"; exit 1; }
    printf '%s' "$k" | setvar GEMINI_API_KEY; unset k
    echo "Key saved to Backend/.env (chmod 600, ignored by Git)." ;;
  on)
    [ -n "${2:-}" ] || { echo "usage: $0 on <model-id>"; exit 1; }
    printf '%s' gemini | setvar AI_PROVIDER
    printf '%s' true | setvar ENABLE_REAL_AI_CALLS
    printf '%s' true | setvar ALLOW_PAID_AI_CALLS
    printf '%s' "$2" | setvar GEMINI_IMAGE_MODEL
    printf '%s' 1 | setvar WORKER_CONCURRENCY
    grep -q '^REAL_AI_MAX_CALLS=' "$ENV" || printf '%s' 2 | setvar REAL_AI_MAX_CALLS
    echo "Real calls configured for model $2 (cap $(grep '^REAL_AI_MAX_CALLS=' "$ENV" | cut -d= -f2)). Run: docker compose up -d api worker" ;;
  cap)
    [[ "${2:-}" =~ ^[0-9]+$ ]] || { echo "usage: $0 cap <positive integer>"; exit 1; }
    printf '%s' "$2" | setvar REAL_AI_MAX_CALLS
    echo "Cap set to $2 total real calls (calls already made still count). Run: docker compose up -d api worker" ;;
  off)
    printf '%s' mock | setvar AI_PROVIDER
    printf '%s' false | setvar ENABLE_REAL_AI_CALLS
    printf '%s' false | setvar ALLOW_PAID_AI_CALLS
    echo "Real calls disabled (mock). Run: docker compose up -d api worker" ;;
  status)
    for v in AI_PROVIDER ENABLE_REAL_AI_CALLS ALLOW_PAID_AI_CALLS GEMINI_IMAGE_MODEL REAL_AI_MAX_CALLS; do
      printf '%s=%s\n' "$v" "$(grep "^$v=" "$ENV" | cut -d= -f2- || true)"
    done
    if grep -q '^GEMINI_API_KEY=.\+' "$ENV"; then echo "GEMINI_API_KEY=set"; else echo "GEMINI_API_KEY=EMPTY"; fi
    docker compose exec -T db psql -U abt -d abt -Atc "select 'real calls recorded: '||count(*) from provider_calls where provider='gemini'" 2>/dev/null || true ;;
  *) sed -n 2,10p "$0"; exit 1 ;;
esac
