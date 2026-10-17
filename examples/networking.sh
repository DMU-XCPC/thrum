#!/usr/bin/env bash
# A two-site thrum network, end to end, on one machine.
#
# Site one starts its daemon and invites site two with only the address the new
# site will listen on. Site two joins through that daemon: it takes the line, the
# ledger and the store over the authenticated channel and registers its key
# there. No repository, no URL and no transport is named by hand.
#
# Usage: examples/networking.sh [work-directory]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${THRUM:-$ROOT/.gocache/thrum}"
BASE="${1:-$(mktemp -d)}"
S1="$BASE/s1"
S2="$BASE/s2"
P1=7801
P2=7802
CODE="$BASE/code"

if [ ! -x "$BIN" ]; then
  echo "build first: make build (or set THRUM=/path/to/thrum)" >&2
  exit 1
fi

say() { printf '\n$ %s\n' "$*"; }
run() { say "$*"; "$@"; }
at() {
  local root=$1; shift
  say "thrum --root $root $*"
  "$BIN" --root "$root" "$@"
}

mkdir -p "$BASE"
run git init -q -b main "$CODE"
echo demo > "$CODE/README"
run git -C "$CODE" add -A
run git -C "$CODE" -c user.name=t -c user.email=t@t commit -qm "init"

# Site one: create, place an actor, say where it listens, start the daemon.
run "$BIN" create "$S1" --site s1 --at "127.0.0.1:$P1" --code "code=$CODE"
at "$S1" actor declare alice --kind human
say "$BIN --root $S1 daemon run --listen 127.0.0.1:$P1 &"
"$BIN" --root "$S1" daemon run --listen "127.0.0.1:$P1" > "$BASE/s1-daemon.log" 2>&1 &
for _ in $(seq 1 50); do
  if "$BIN" --root "$S1" daemon status >/dev/null 2>&1; then break; fi
  sleep 0.1
done

# Invite; the token names no repository.
say "$BIN --root $S1 site invite s2 --at 127.0.0.1:$P2"
TOKEN=$("$BIN" --root "$S1" site invite s2 --at "127.0.0.1:$P2")
printf 'token: %s...\n' "${TOKEN:0:48}"

# Site two joins through site one's daemon.
say "$BIN join <token> --dir $S2"
"$BIN" join "$TOKEN" --dir "$S2"

# Site two's daemon.
say "$BIN --root $S2 daemon run --listen 127.0.0.1:$P2 &"
"$BIN" --root "$S2" daemon run --listen "127.0.0.1:$P2" > "$BASE/s2-daemon.log" 2>&1 &
for _ in $(seq 1 50); do
  if "$BIN" --root "$S2" daemon status >/dev/null 2>&1; then break; fi
  sleep 0.1
done
at "$S1" daemon status
at "$S2" daemon status

# Record on both sides and let the daemons exchange.
at "$S2" actor declare bob --kind agent
at "$S1" sync
at "$S2" sync
at "$S1" actor list
at "$S2" actor list

# A message across the fabric.
say "THRUM_ACTOR=alice $BIN --root $S1 message send bob --subject 'hello over the daemon'"
THRUM_ACTOR=alice "$BIN" --root "$S1" message send bob --subject "hello over the daemon"
say "THRUM_ACTOR=bob $BIN --root $S2 message list"
THRUM_ACTOR=bob "$BIN" --root "$S2" message list

at "$S1" verify
at "$S2" verify
at "$S1" daemon stop
at "$S2" daemon stop
printf '\nworkspaces: %s and %s\n' "$S1" "$S2"
