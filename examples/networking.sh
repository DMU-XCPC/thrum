#!/usr/bin/env bash
# A two-site thrum network, end to end, on one machine.
#
# The bootstrap at the start is what a real deployment replaces with a URL the
# joining machine can reach (a bare repository over ssh, or a bundle): it carries
# the line and the ledger once. After `site transport` the histories move through
# the daemons and the bootstrap is deleted.
#
# Usage: examples/networking.sh [work-directory]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${THRUM:-$ROOT/.gocache/thrum}"
BASE="${1:-$(mktemp -d)}"
S1="$BASE/s1"
S2="$BASE/s2"
BOOT="$BASE/bootstrap"
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

mkdir -p "$BASE" "$BOOT"
run git init -q -b main "$CODE"
echo demo > "$CODE/README"
run git -C "$CODE" add -A
run git -C "$CODE" -c user.name=t -c user.email=t@t commit -qm "init"

# One site creates the workspace.
run "$BIN" create "$S1" --site s1 --code "code=$CODE"
at "$S1" actor declare alice --kind human

# A bootstrap the joining machine can reach. In a real network this is an ssh
# URL or a bundle; here it is a bare directory.
run git init --bare -q "$BOOT/line.git"
run git init --bare -q "$BOOT/ledger.git"
run git -C "$S1" remote add origin "$BOOT/line.git"
run git -C "$S1" push -q -u origin main
run git -C "$S1/handoff" remote add origin "$BOOT/ledger.git"
run git -C "$S1/handoff" push -q -u origin main

# Invite and join.
say "$BIN --root $S1 site invite s2 --at 127.0.0.1:$P2 --line $BOOT/line.git --ledger $BOOT/ledger.git --store $S1/repos/store.git --code code=$S1/repos/code.git"
TOKEN=$("$BIN" --root "$S1" site invite s2 --at "127.0.0.1:$P2" \
  --line "$BOOT/line.git" --ledger "$BOOT/ledger.git" \
  --store "$S1/repos/store.git" --code "code=$S1/repos/code.git")
printf 'token: %s...\n' "${TOKEN:0:48}"
at "$S1" sync
say "$BIN join <token> --dir $S2"
"$BIN" join "$TOKEN" --dir "$S2"
at "$S1" sync

# Where each site listens, and where it reaches the other.
at "$S1" site reach s2 --at "127.0.0.1:$P2"
at "$S2" site reach s1 --at "127.0.0.1:$P1"
at "$S1" site reach s1 --at "127.0.0.1:$P1"

# Move every repository onto the other site's daemon.
at "$S1" site transport s2
at "$S2" site transport s1
say "rm -rf $BOOT   # nothing external is left"
rm -rf "$BOOT"

# Start the two daemons (in a real deployment this is systemd, not a shell).
say "$BIN --root $S1 daemon run &"
"$BIN" --root "$S1" daemon run > "$BASE/s1-daemon.log" 2>&1 &
say "$BIN --root $S2 daemon run &"
"$BIN" --root "$S2" daemon run > "$BASE/s2-daemon.log" 2>&1 &
for _ in $(seq 1 50); do
  if "$BIN" --root "$S1" daemon status >/dev/null 2>&1 && "$BIN" --root "$S2" daemon status >/dev/null 2>&1; then
    break
  fi
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
