#!/usr/bin/env bash
# A discovery-only cluster in Docker, then a remote run.
# No --at anywhere: the two sites find each other on the LAN multicast.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/.gocache/thrum"
BASE="$(mktemp -d)"
NET=thrum-discover
cleanup() {
  if [ "${KEEP:-0}" != 1 ]; then
    docker rm -f thrum-s1 thrum-s2 >/dev/null 2>&1 || true
    docker network rm "$NET" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT
cleanup
docker network create "$NET" >/dev/null
mkdir -p "$BASE/s1" "$BASE/s2"
run1() { docker run -d --name thrum-s1 --network "$NET" -v "$BIN:/thrum:ro" -v "$BASE:/base" -v "$BASE/s1:/ws" --entrypoint sh alpine/git -c "sleep infinity" >/dev/null; }
run2() { docker run -d --name thrum-s2 --network "$NET" -v "$BIN:/thrum:ro" -v "$BASE:/base" -v "$BASE/s2:/ws2" --entrypoint sh alpine/git -c "sleep infinity" >/dev/null; }
run1; run2

echo '=== s1: create (no --at), actor, discover on'
docker exec thrum-s1 sh -c 'git init -q -b main /base/code && echo demo > /base/code/README && git -C /base/code add -A && git -C /base/code -c user.name=t -c user.email=t@t commit -qm init'
docker exec thrum-s1 sh -c '/thrum create /ws --site s1 --code code=/base/code' >/dev/null
docker exec -e THRUM_ACTOR=a1 thrum-s1 sh -c '/thrum --root /ws actor declare a1 --kind agent'
docker exec thrum-s1 sh -c '/thrum --root /ws site discover on'
docker exec thrum-s1 sh -c '/thrum --root /ws sync' >/dev/null

echo '=== invite (no --at) and join through discovery'
TOKEN=$(docker exec thrum-s1 sh -c '/thrum --root /ws site invite s2')
docker exec thrum-s2 sh -c "/thrum join '$TOKEN' --dir /ws2" >/dev/null
docker exec thrum-s2 sh -c '/thrum --root /ws2 site discover on'
docker exec thrum-s2 sh -c '/thrum --root /ws2 site run on'
docker exec thrum-s2 sh -c '/thrum --root /ws2 sync' >/dev/null

echo '=== the two sites'
docker exec thrum-s1 sh -c '/thrum --root /ws site list'
docker exec thrum-s2 sh -c '/thrum --root /ws2 site list'

echo '=== s1 runs echo on s2'
docker exec -e THRUM_ACTOR=a1 thrum-s1 sh -c '/thrum --root /ws run --site s2 -- echo hello'
