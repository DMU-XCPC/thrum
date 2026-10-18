# Partial connectivity and local discovery

Status: Phases 1–3 are implemented and folded into `thrum.md` and `commands.md`. Phase 1: the
per-peer best-effort sync, the store's live-tag offer, the A—B—C history test. Phase 2:
`site.routes`, the site-local route table (direct edge, then fewest hops, expiry),
`msg.forward` with the seen-site set and the hop bound, routed placement, `site routes`, and
the A—B—C message and relay-down tests. Phase 3: plain UDP multicast announce and join query,
`var/discovered` with an expiry, the local > declared > discovered precedence, the optional
`--at` on `create` and `site invite`, the join through discovery, and the `site show`/`list`
discovered address. Phase 4 remains: mDNS as an optional front end, `site announce` for a
committed address change, and a relayed join only if an operator ever needs one.

## 1. Requirements

- **R1 (primary) — partial connectivity.** A and C may have no direct path; a message, a
  placement lookup, or a history exchange must still reach them through B. The workspace is
  a connected graph, not a mesh. A partition may split it; the parts reconcile later.
- **R2 — an address is declared only where it is stable.** A dynamic LAN node is found, not
  configured. `site/<name>.yaml`'s `reach` is therefore optional and normally empty for
  dynamic nodes; `var/config` and discovery supply the rest.
- **R3 — LAN discovery.** A site announces its address on its LAN and finds the other sites'
  addresses without a central service.
- **R4 — the daemon is the only transport.** No repository has a thrum remote; the git pack
  protocol is the payload; `sync` is a daemon operation. (Already true.)
- **R5 — the replicas.** The line, the ledger and the store are replicated at every site;
  code content is the project's own. (Already true.)
- **R6 — messages are transient.** No durable queue; an id makes a retry idempotent; the
  sender never queues; placement follows the actor's last hello.
- **R7 — joining is URL-free.** One-time nonce; the inviting daemon serves the first pull and
  the key push; the token names no repository.
- **R8 — the site keys are the trust boundary.** The roster's sites are trusted; a rotated
  key is proved by the previous one; an outsider is refused.

The network itself is trusted: discovery packets, relays and addresses are inside the same
trust boundary. The site keys establish identity, not a defense against a hostile LAN.

## 2. What the current model assumes

- Every operation dials the destination directly: `history.exchange` lists every peer with a
  reach and key and fetches each, so one unreachable peer fails the whole sync; messages are
  `Push`ed to the actor's site directly; `actor.where` asks the peers once; there is no
  route, relay or forwarding verb.
- There is no discovery; `site invite --at` is required, so every invited site's roster entry
  carries an address whether or not it is stable.
- The store's published list is site-local, so an intermediate's fetched tag is not offered
  to a third site.
- The pieces that already help: the histories are replicated (the basis of gossip); messages
  carry ids (idempotent retries); the site keys authenticate every hop; `--wait` bounds a
  sender's retry.

## 3. The site graph

- Nodes: the roster's sites. An edge exists when one site can dial the other's daemon.
- Each daemon keeps:
  - the **direct addresses**: `var/config` (explicit), the roster's declared address (stable
    nodes), and `var/discovered` (LAN);
  - the **route table**: for each site, the next hop and the hop count, in `var/routes`;
  - the **neighbours**: the sites it can dial directly.
- Address precedence for a direct edge: local `var/config` > declared roster > discovered.
- The route table is site-local, never committed.

## 4. Routes

- A new authenticated verb, `site.routes`, exchanges a daemon's reachable set: the sites it
  can reach and the hop counts. The direct neighbours are hop 1; a route via a neighbour is
  that neighbour's hop count plus one.
- Selection: a direct edge if its address answers; otherwise the fewest hops; ties by the
  site name. A route is a **next hop**, not the destination's address: the relay knows how to
  continue.
- Daemons poll their directly reachable neighbours periodically and record the last answer;
  a route older than a few periods expires. The hop count is bounded by the roster size, and
  a daemon never installs a route through a site it cannot reach.
- `site doctor` and `site show`/`site list` report the chosen next hop, so a person can see
  why a site is reachable.

## 5. The shared histories

- The histories are replicas, so the histories do not need a relayed git stream: each site
  syncs with its **reachable neighbours** and the content propagates.
- `history.exchange` becomes per-peer best effort: fetch, merge, settle and push with every
  neighbour that answers; report the unreachable ones and leave them for the next round. One
  down site no longer fails the whole sync.
- Convergence follows the graph: with A—B—C and no A—C edge, C syncs with B, B syncs with A,
  and A's records reach C on the next round. A partition converges when the graph reconnects.
- The store: a site offers the tags the line's **live asset records** reference, not only the
  tags it published itself. That is what lets B's fetched copy of A's asset reach C. A
  withdrawn version is no longer live, so the publisher does not fetch its old copy back.
- The code content stays out of the sync, as now.

## 6. Messages and placement

- The destination is the actor's site. If that site has a direct edge, the message is pushed
  directly, as now. Otherwise the sender's daemon hands it to the next hop with `msg.forward`,
  and the relay either delivers it or forwards it again.
- `msg.forward` carries the destination site, the message, the body and files, the original
  sender's site, the seen message ids and the hop count. The destination's duplicate check
  plus the seen set make a retry idempotent and stop loops; the hop limit bounds the walk.
- The relay holds the message only for the duration of the call, with a bounded retry inside
  the sender's `--wait`; it is not a durable queue. A destination that stays down makes the
  send fail, exactly as it does today.
- Placement: `actor.where` asks the reachable neighbours (a neighbour's answer is its own
  table, no recursion). `actor.hello` is broadcast to the reachable neighbours, so an
  intermediate knows where the actor last was. A stale answer is corrected by the next hello;
  the `not_here` reply still redirects a sender once.
- Trust: the destination authenticates the last hop, not the original sender. A relay can
  read and forge a message just as a site can write the line, and the network is trusted, so
  this is the same boundary as the rest of the workspace; no end-to-end actor signature.

## 7. Joining

- The token keeps the inviter's site, address (may be empty), public key and the nonce; the
  new site and its address may also be empty.
- The joiner dials the inviter directly if the token names an address; otherwise it asks the
  LAN discovery for the inviter's site and joins through it. A join through a third site is
  not part of this design: the join is a one-time bootstrap, it happens before the joiner has
  a roster or a route table, and a direct or discovered path to the inviter is assumed.

## 8. LAN discovery

- The discovery is off by default. A daemon with `--discover` joins a fixed multicast group and port, and periodically announces
  `{workspace, site, reach, version, sent}`. A peer accepts only its own workspace, ignores
  itself, and writes `var/discovered/<site>.yaml` with the address and the seen time. Entries
  expire after a few intervals.
- The join query: a joining machine has no roster, so it multicasts a query naming the
  workspace and the inviter's site; the inviter answers with its address.
- The announcement is only a hint: the site-key handshake still authenticates the peer, so a
  wrong address is a failed dial, not a false identity. The cache expires and the local
  `site reach` wins over it. The network is trusted, so there is no signature on the packet.
- The Docker/tests can use explicit addresses; the multicast path is tested on one host and,
  if the bridge forwards multicast, between containers. mDNS/DNS-SD can be added later as an
  optional front end over the same cache.

## 9. Addresses

- `create --at`, `site invite --at` and the token's addresses are optional. Only a stable
  node needs a declared address; a dynamic node leaves the roster's `reach` empty and relies
  on discovery or local `site reach`.
- The shared roster's `reach` is written only by an explicit declaration (`create --at`,
  `site invite --at`, and a future `site announce`). The join, discovery and `site reach`
  write site-local files and never touch the roster.
- `site show`/`site list` report, per site: the declared address, the local view, the
  discovered address and the next hop.
- `site reach` remains the explicit local override. Changing the declared address durably
  is a line event: `site announce` writes it, or clears it.

## 10. Failure modes

| What happens | What the design does |
| --- | --- |
| a neighbour is down | its routes and edges expire; the sync skips it; messages take another route if one exists |
| a relay is down | the next round picks another next hop; no message is queued |
| a route loops | seen ids and the hop limit stop the message; the route's hop count and expiry stop the table |
| a partition | each part syncs inside itself; the parts merge when the graph reconnects |
| an address changes | discovery updates the hint; the local view wins; a stable node's declared address is a fallback |
| a spoofed discovery packet | the handshake fails; the cache expires; the local view wins |

## 11. The wire and the layout

- New verbs: `site.routes` (a neighbour's reachable set), `msg.forward` (a message handed to a
  next hop). `history.exchange` stays local and becomes per-peer best effort.
- The store's offer is the live asset tags from the line, not only the local published list.
- New site-local files: `var/discovered/<site>.yaml`, `var/routes.yaml`. Neither is committed.
- The discovery protocol is UDP multicast, outside the JSON wire; `site.hello` stays a status.

## 12. Tests

- A three-site chain A—B—C with no A—C edge: the histories converge through B; C's `verify`
  passes; C's messages reach A through B; placement finds an actor two hops away.
- A partition and reconnection: the parts converge; no message is queued.
- A relay down: the route reconverges; the message takes the other route or fails.
- Discovery: two daemons on one host find each other; a stale entry expires; a local
  `site reach` wins; a spoofed packet is ignored.
- The existing full-mesh suites keep passing unchanged.

## 13. Phases

1. **Histories over a connected graph.** The per-peer best-effort sync; the store's live
   tags; the A—B—C history tests. No new verbs.
2. **Routed messages and placement.** `site.routes`, the route table, `msg.forward`, the
   neighbours' hello/where; the A—B—C message tests; the hop/seen limits; the relay-down
   tests.
3. **Discovery and optional addresses.** The multicast announce/query; `var/discovered`; the
   address precedence; the optional `--at`; `site show`/`list`; the join's discovery.
4. **The relayed join, mDNS, and the docs.** The token naming a relay; the mDNS front end;
   fold this into `thrum.md` and `commands.md`.

## 14. Decisions and what remains

- **Trust:** the network and the sites are trusted; the site keys are identity. A relay may
  read and forge; no end-to-end signature.
- **Discovery:** plain UDP multicast, unsigned, off by default; enabled per daemon with
  `--discover`; a hint that is cached and expires.
- **The store:** offers the line's live asset tags.
- **Routes:** a direct edge first, then the fewest hops; ties by the site name.
- **Addresses:** the shared roster's `reach` is written only by an explicit declaration —
  `create --at`, `site invite --at`, `site announce`; discovery, join and `site reach` write
  site-local files only.
- **Joining:** direct or discovered to the inviter; no join through a third site in this
  design.
- **The relay:** bounded to the caller's `--wait`; no durable queue.
- **Later:** mDNS as an optional discovery front end; a relayed join if an operator ever needs
  one. `site announce` for a committed address change is implemented.
