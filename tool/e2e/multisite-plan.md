# Multi-site test plan

Status: proposal. The single-site suite (`tool/e2e`, 24 tests) and one Docker happy path exist.
This plan is what has to be added to make the claims in `doc/thrum.md` true *under distance,
concurrency and failure*, not just when everything is up.

Executed 2026-10-17. The default suite (`tool/e2e`, 80 tests including Docker) covers every P0
and P1 and the feasible P2s: 300 ms latency, a six-second soak, four sites and 200 events, 1,000
events, 10 MB attachments, 100 connections, the daemon transport (`history.exchange`;
the Docker test joins through the inviting daemon and stops the code hub after the join), and
the partial graph: A—B—C histories and routed messages through B, the relay going away, the
loop/hop refusals, a join and sync through the LAN hint, and a declared address changed as a
line event; the daemon starts on demand and its command group is hidden; a site can leave and return
without losing its copy; a brief can carry a full body, and attachments carry their metadata and are bounded per message. The
full-scale P2s are long runs done by hand
(there is no CI budget): 10,000 commits, a 24-hour soak, and packet loss. A 100 MB attachment is not promised (the transport is base64 in
the JSON protocol; 10 MB is tested), and the transport is an external git remote in this
version; both are recorded in `doc/thrum.md`.

The plan is deliberately honest about what will fail today. Each scenario is marked:

- **covered** — an existing test exercises it;
- **partial** — only a local or single-site version exists;
- **gap** — not covered, and the current implementation is expected to fail or to need a
  decision first. The gap is named.

## 1. What is promised

The scenarios are derived from the design, not from the implementation:

| # | Claim | Where |
| --- | --- | --- |
| C1 | A site joins only by invitation; the roster declares it before it is trusted. | `thrum.md` Joining |
| C2 | A peer accepts a site only if the roster declares it, and the handshake proves its key. | `thrum.md` The wire |
| C3 | Two sites writing different paths converge; a name claimed twice is settled, and the loser is told. | `thrum.md` The shared histories |
| C4 | The ledger is append-only and merges by union; nothing seen is rewritten. | `thrum.md` The shared histories |
| C5 | A message is placed or it is not sent; the sender never queues. | `thrum.md` Messages |
| C6 | The receiving site holds a message across a daemon restart. | `thrum.md` Messages |
| C7 | An address is asked, not merged; a stale address costs one failed delivery and one retry. | `thrum.md` Finding an actor |
| C8 | A message arrives whole or not at all, and an id cannot deliver twice. | `thrum.md` Messages |
| C9 | A peer being down stops messages there and merging with it, and nothing else. | `thrum.md` When something is down |
| C10 | A daemon being down does not stop local commands. | `thrum.md` When something is down |
| C11 | The protocol is versioned and a mismatch is refused clearly. | `thrum.md` The wire |
| C12 | The daemons serve the repositories, so a peer needs no external remote. | `thrum.md` Git |
| C13 | The store's tags are created once, never moved, deleted only by withdrawal. | `thrum.md` Git |
| C14 | Restarting the daemon loses nothing durable. | `commands.md` Daemon |

## 2. Levels and topologies

| Level | What | Where |
| --- | --- | --- |
| L1 | One site, the CLI, the records | `tool/e2e` (done) |
| L2 | Two sites, happy path over a network | `TestDockerMultiSite` (done) |
| L3 | Two to four sites, adversarial: partitions, crashes, skew, races, revocation | **this plan** |
| L4 | Scale and soak | **this plan** |

Topologies:

- **T2** — two site containers, one hub.
- **T3** — three sites, one hub: transitivity, introductions, three-way claims.
- **T3P** — three sites, one partitioned (A↔C works, A↔B cut), then healed.
- **T4** — four sites for mesh sync and soak.
- **T2N** — no hub: the daemons carry the histories (`history.exchange`, with `git.serve` as the payload); the join goes through the inviting daemon on the one-time nonce.

Each site is a container with its own workspace and its own daemon; the hub is a container
serving the histories (today a test-only smart-HTTP server; switch the URLs to the daemon once
`git.serve` exists).

## 3. Harness

### 3.1 Containers

- Unique names per run (`thrum-<pid>-<rand>-a`), one network per run, `t.Cleanup` removes both.
- The binary is `docker cp`'d into the containers (not bind-mounted: the daemon may be remote,
  and a file mount onto a missing path becomes a directory).
- State is container-local; the bundle transfer is `docker cp` through a host temp dir.
- `alpine/git:latest` is required; the test skips when it or Docker is absent.

### 3.2 Faults

| Fault | Mechanism | Notes |
| --- | --- | --- |
| Peer unreachable | `docker network disconnect <net> <site>` / `connect` | The cheapest partition; no privileges. |
| Peer frozen | `docker pause` / `unpause` | The TCP stack accepts, the process does not answer: tests timeouts. |
| Daemon crash | `docker exec kill -9 <pid>`; `docker exec pkill -9 thrum` | Tests stale lock and socket. |
| Site restart | `docker restart` | State survives; the daemon must be started again. |
| Slow link | `tc qdisc netem` inside a `NET_ADMIN` container, or a small Go TCP shim | P2; needs `iproute2` in the image or a shim. |
| Clock skew | `GIT_AUTHOR_DATE` / `GIT_COMMITTER_DATE` in the command's environment | Changes the commit time without touching the host clock. This is exactly the field the current settlement reads. |
| Mid-push kill | a large attachment plus `kill -9` at a chosen moment | Tests whole-or-not-at-all; may need retries to hit the window. |
| Hub down | `docker stop <hub>` | Local work continues; sync fails. |

### 3.3 Evidence

Every failing test dumps, for each site: `thrum verify`, `site list`, `site doctor`,
`daemon status`, `git -C <root> log --oneline -20`, `git -C <root>/handoff log --oneline -20`,
`find var/msg -type f`, and the daemon log. The dump is attached to the test log; a helper
keeps the boilerplate out of every test.

### 3.4 Determinism

- No `sleep` as an assertion: poll with a deadline (default 10s) and a clear failure.
- Never assert a generated id, a timestamp or a commit hash; capture what the command printed.
- Docker tests run serially per network; a test that needs parallelism gets its own network.
- Every test leaves no container behind; a `TestMain` sweep removes names matching the prefix
  before the run starts.

## 4. Scenario catalog

### A. Membership and identity

| ID | Pri | Scenario | Expected | Status |
| --- | --- | --- | --- | --- |
| M-01 | P0 | Invite → join → sync between two sites | Both rosters, both keys, joined site verifies | covered (`TestDockerMultiSite`, `TestTwoSitesJoinSync`) |
| M-02 | P0 | Join with a token whose line is a different workspace | Refused; no half-joined directory, key or site file left | covered (`TestJoinRefusesAnotherWorkspace`) |
| M-03 | P0 | Join with a valid token but no roster entry for the new site | Refused ("the roster does not declare this site") | covered (`TestJoinNeedsAnInvitation`) |
| M-04 | P1 | C joins through B; A reaches C after sync; a site not in the roster is refused | A authenticates C; the stranger is refused at the handshake | covered via sync (`TestInvitationFromANonCreator`); the explicit introduce step is not needed with the roster in the line |
| M-05 | P0 | A rotates its key; B sends to A before syncing, then after | Before: refused (old key); after: delivered | covered (`TestKeyRotationTakesEffect`) |
| M-06 | P1 | A revokes B's key | B's handshake is refused; B's messages fail | covered (`TestRevokedKeyIsRefused`) |
| M-07 | P1 | A forgets B | B's name stays taken; messages to B are refused; B's records can still arrive through a remote | covered (`TestForgetRefusesTheForgottenSite`) |
| M-08 | P1 | A token is reused after the site joined | Second join refused (expiry or one-time use) | covered (`TestInvitationIsOneTime`; one-time, not time-expiring) |
| M-09 | P1 | Two sites claim the same site name | The roster path is settled; the loser is told; no two winners | covered (`TestTwoSitesClaimOneSiteName`) |
| M-10 | P2 | `site doctor` with a peer up, down, and rotated | Reports answered / no answer / refused | covered (`TestDoctorReportsPeerState`) |

### B. Histories and convergence

| ID | Pri | Scenario | Expected | Status |
| --- | --- | --- | --- | --- |
| H-01 | P0 | A writes actor a1, B writes actor a2 while apart; both sync | Both sites see both; no conflict | covered (`TestConcurrentSync`) |
| H-02 | P0 | Two sites declare the same repository/actor while apart | Earlier claim wins; loser named; both converge | covered (`TestSettlement`) |
| H-03 | P0 | Three sites claim one name, syncing in three different orders | The same winner at all three | covered (`TestThreeSiteClaim`) |
| H-04 | P1 | Three sites write disjoint events, sync pairwise in different orders | Identical trees at all three | covered (`TestOrderIndependence`) |
| H-05 | P0 | A and B record handoffs for the same job while apart | Both files survive; no conflict | covered (`TestConcurrentLedgerHandoffs`) |
| H-06 | P0 | A and B edit the same `doc/topic/...` file | Sync stops, names the file, and leaves both histories intact | covered (`TestProseConflictStopsAndNamesTheFile`) |
| H-07 | P1 | A and B push at the same time | One rejected push, one retry, no lost event | covered (`TestConcurrentSync`; the retry) |
| H-08 | P1 | After every sync, the histories only grow | No force-push, no rewrite; ancestry holds | covered (`TestHistoriesOnlyGrow`) |
| H-09 | P1 | A site's event clock is far ahead and its commit is earlier; another site claims later in real time with a wall clock | The smaller event clock keeps the name, deterministically and regardless of commit time | covered (`TestEventClockDecidesTheClaim`) |
| H-10 | P2 | 1,000 events, then sync | Bounded time, no corruption | covered (`TestThousandEvents`) |
| H-11 | P0 | B is partitioned; A and B both record; heal and sync | Both converge; nothing lost | covered (Docker partition) |
| H-12 | P1 | The hub refuses one push | One retry succeeds; no duplicate event | covered (the same retry path) |

### C. Messages and placement

| ID | Pri | Scenario | Expected | Status |
| --- | --- | --- | --- | --- |
| S-01 | P0 | Local send, list, read, reply, purge | Delivered and read | covered (`TestMessages`) |
| S-02 | P0 | Cross-site send through the daemons | Delivered and read | covered (`TestCrossSiteMessage`, Docker) |
| S-03 | P0 | a1 moves from s1 to s2; s1 still has the old address; send to s1 | s1 says "not here", asks/corrects, delivers to s2; at most one failed attempt | covered (`TestNotHereRetries`, `TestHelloMovesMail`) |
| S-04 | P0 | `actor hello` at s2 after the move | Subsequent sends from s1 go to s2 | covered (`TestHelloMovesMail`) |
| S-05 | P0 | Deliver, restart the daemon, list | The message is still there | covered (`TestMessagesSurviveADaemonRestart`) |
| S-06 | P0 | Kill s2's daemon; send; restart; list | The send fails; nothing appears later | covered (`TestSenderDoesNotQueue`) |
| S-07 | P1 | Kill the receiver mid-push of a large attachment | No partial message; a resend arrives whole | covered (the orphan directory is allowed, a half-message is not) |
| S-08 | P0 | A caller retries with the same id | Exactly one message | covered (`TestRetryWithTheSameIDDeliversOnce`; `message send --id`) |
| S-09 | P1 | 10 MB binary attachment | Byte-for-byte, hash checked | covered (`TestLargeAttachment`) |
| S-10 | P1 | Reply threading across sites | `reply_to` preserved and readable at the other site | covered (`TestCrossSiteMessage`) |
| S-11 | P1 | `events --watch` sees an arrival, survives a reconnect | One event per message, no duplicates after reconnect | covered (`TestEventsWatchStreams`) |
| S-12 | P0 | A site not in the roster, or with a revoked key, pushes | Handshake refused; daemon survives | covered (undeclared site and revoked key) |
| S-13 | P1 | Malformed JSON, unknown verb, truncated frame, huge frame | Connection closed; daemon alive; other sites unaffected | covered (oversized and malformed; `TestUnknownVerbAndGarbage`) |
| S-14 | P2 | 100 messages, list and purge | Bounded mailbox operations | covered (list; `TestManyMessages`) |

### D. Jobs, code, bundles, assets

| ID | Pri | Scenario | Expected | Status |
| --- | --- | --- | --- | --- |
| J-01 | P0 | A opens a job and hands off; B syncs and verifies | B sees the brief and the handoff; the head resolves | covered (Docker) |
| J-02 | P0 | B tries to work A's job branch without a decision | Either the design's one-writer rule is enforced, or the gap is documented | covered (the owner's refusal; `TestWorkingAnotherSitesJobIsRefused`) |
| J-03 | P1 | Bundle exported at A, imported at B | Payload intact | covered (Docker) |
| J-04 | P1 | Import at B before the code arrived | Refused ("fetch the code first") | covered |
| J-05 | P0 | Asset published at A, `sync`, materialise at B | Bytes arrive | covered (Docker cross-site asset; `sync` exchanges store tags) |
| J-06 | P1 | Withdraw at A, sync, verify at B | The archived manifest is there; the tag is gone at A | covered |
| J-07 | P1 | Both sites publish the same asset version | One wins; the other is refused (the version is taken) | covered |
| J-08 | P1 | A crafted bundle with `../` paths | Refused; nothing written outside `job/<id>/` | covered |
| J-09 | P2 | A and B open the same job id while apart | Settlement; the loser is told; no two briefs | covered (`TestTwoSitesOpenOneJob`) |

### E. Daemon, protocol, security

| ID | Pri | Scenario | Expected | Status |
| --- | --- | --- | --- | --- |
| D-01 | P0 | Handshake: right keys; wrong workspace; undeclared site; bad signature | Accepted / refused with a reason | covered (`TestDaemonRefusesBadHandshakes`) |
| D-02 | P0 | A peer sends `stop` | Refused (local only) | covered (`TestPeerCannotStopTheDaemon`) |
| D-03 | P0 | `kill -9` the daemon, then `daemon run` again | Starts; the stale lock is not a tombstone | covered (`TestDaemonRecoversFromAStaleLock`) |
| D-04 | P0 | A second daemon on the same workspace | Refused | covered (`TestDaemonLock`) |
| D-05 | P1 | A peer speaks `thrum/2` | Refused clearly | covered (the version is refused, with a sentence) |
| D-06 | P1 | Unknown verb, malformed request | An error response; the daemon stays up | covered (`TestUnknownVerbAndGarbage`) |
| D-07 | P1 | Unix socket permissions; another user | Only this user can ask | covered (the socket is 0600; another user is not exercised) |
| D-08 | P1 | `site reach` | Written under `var/`, never committed | covered (`TestSiteCommands`: the line stays clean) |
| D-09 | P2 | One slow peer holds a connection | Other peers are not blocked | covered (`TestSlowPeerDoesNotBlock`) |
| D-10 | P2 | A 100 MB attachment | Memory bound, clear refusal or success | accepted: 10 MB tested, 100 MB not promised (`doc/thrum.md` Known limits) |

### F. Failure and recovery

| ID | Pri | Scenario | Expected | Status |
| --- | --- | --- | --- | --- |
| R-01 | P0 | Partition A/B; both record; heal; sync | Both converge; messages that needed the cut fail and say so | covered (Docker partition) |
| R-02 | P0 | Stop the hub; keep working locally; restart | Local commands work; sync fails cleanly; then converges | covered (Docker hub down) |
| R-03 | P1 | Kill the daemon mid-push | No partial mailbox entry; a retry works | covered (`TestKilledDaemonLeavesNoPartialMessage`) |
| R-04 | P1 | `docker restart` a site | Workspace survives; daemon restarts; mailbox intact | covered (Docker restart) |
| R-05 | P1 | Simultaneous pushes | One retry; no loss | covered (`TestConcurrentSync`) |
| R-06 | P1 | A prose conflict | Merge aborted; `git status` clean; no half-merge | covered (`TestProseConflictStopsAndNamesTheFile`) |
| R-07 | P2 | 300 ms latency, 5% loss | Handshake and sync still work within the timeout | partial: 300 ms covered (`TestSlowLink`); loss is a long run (manual) |
| R-08 | P2 | A clock step (NTP) during a run | No corruption; settlement still deterministic | covered (the persisted clock never goes backwards; `TestEventClockNeverGoesBackwards`) |

### G. Scale and soak

| ID | Pri | Scenario | Expected | Status |
| --- | --- | --- | --- | --- |
| X-01 | P1 | 4 sites, 200 events, full mesh sync | Converges; bounded time | covered (`TestFourSitesScale`) |
| X-02 | P2 | 10,000 line commits | `sync`, `verify` still usable | partial: 1,000 in the default suite; 10,000 is a long run (manual) |
| X-03 | P2 | 24 h soak: messages and syncs every minute | No leak, no unbounded file growth | partial: 6 s in the default suite; 24 h is a long run (manual) |
| X-04 | P2 | 100 connect/disconnect cycles | No fd leak; no stale lock | covered (`TestManyConnections`) |

## 5. Coverage matrix

| Claim | Scenarios |
| --- | --- |
| C1 | M-01, M-03, M-08 |
| C2 | M-04, D-01, D-05, S-12 |
| C3 | H-02, H-03, H-04, H-09, J-09 |
| C4 | H-05, H-08, J-06 |
| C5 | S-06, S-03 |
| C6 | S-05, R-04 |
| C7 | S-03, S-04 |
| C8 | S-07, S-08 |
| C9 | H-11, R-01, R-02 |
| C10 | R-02, R-04 |
| C11 | D-05 |
| C12 | T2N, J-04, J-05 |
| C13 | J-05, J-06, J-07 |
| C14 | D-03, R-04 |

## 6. Gaps this plan will expose

These are read off the code, not guessed; each gets a test before it gets a fix.

The first execution (2026-10-17) closed six of them: join atomicity, the roster requirement,
the one-time token, idempotent retries, the store exchange and the stale lock. Their tests are
named in §4; the rows are gone below.

| Gap | Evidence | Scenario |
| --- | --- | --- |
| Two stretches starting in the same second share a name | The handoff name is the started stamp, to the second; the test separates them with a sleep | H-05 |
| Two hellos in the same second tie | The heard stamp has second granularity; an equal-or-newer place overwrites, so the arrival decides | S-03/S-04 (tests separate them) |
| A withdrawal leaves a peer's already-fetched tag in place | The peer does not re-push it and the remote is cleaned, but a materialise at that peer still works | J-06 (accepted) |
| `--capacity` and "start the daemon on join" are absent | Accepted and recorded in `doc/thrum.md` / `doc/commands.md`: no capacity in this version; the machine's init starts the daemon | M-08 |

## 7. Execution

### Phase 0 — before writing more tests

- Decide the gaps that are design questions rather than code: C8's retry identity, C7's
  ask/correct, C12's transport (decided: an external remote in this version; `git.serve` is
  reserved), C11's version policy. A test that encodes the wrong answer is worse than none.

### Phase 1 — P0 (must pass before a multi-site claim)

Order: M-02, M-03, M-08, D-03, S-03/04, S-05/06/08, J-05, H-03, H-05, H-11, R-01, R-02,
D-01, D-02, S-12.

Estimated 2–3 days including the fixes that tests force.

### Phase 2 — P1

H-04, H-06, H-07, H-09, H-12, M-05..M-07, S-07, S-09..S-11, J-04, J-06..J-08, D-05..D-08,
R-03..R-06, X-01.

### Phase 3 — P2

Latency, long soak, scale, larger payloads, slowloris, clock steps.

### Where the tests run

On a workstation, with `make test`; there is no CI budget for this repository and none of the
suite is scheduled. Docker is optional: the multi-site test skips when Docker or
`alpine/git:latest` is absent. The long runs (10,000 commits, the 24-hour soak, packet loss) are
done by hand when a change touches what they exercise; they are not part of the default run.

Each test is run three times before it is trusted; a flake is a bug in the test or in the code,
and is not retried away.

### Exit criteria

- All P0 green on three consecutive local runs.
- The default suite green on a workstation; a P1 flake is treated as a bug.
- Every gap in §6 is either closed or recorded as an accepted deviation in `doc/thrum.md`, with
  the test marked "expected" and the reason.
