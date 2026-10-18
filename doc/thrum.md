# thrum

thrum is the coordination fabric for a workspace. It has two halves: a command-line program
that an actor runs, and one **daemon** per site. The daemons find actors, carry messages
between them, and keep the workspace's shared history converging. The command line is how an
actor reaches both the fabric and the workspace's files.

One rule holds the whole design up:

> **The fabric is never an authority.**

Everything durable is a file or a git object. Everything a daemon holds — where a name is, what
is in flight, which way a merge went — is a cache or a channel. Losing a daemon's memory costs
a lookup, a reconnection, or a merge; it never changes a decision, because no decision is
stored there. That is what makes a distributed daemon safe to have: it is allowed to be wrong,
so its concurrency does not have to be perfect.

## Sites

A **site** is one machine's copy of a workspace, running a daemon. A workspace has one or more
of them. The daemon is started on demand by the CLI; a person does not run it as a separate
step, and the commands that manage it are internal.

The roster lives in the line, one file per site:

```yaml
# doc/site/build-01.yaml
site: build-01
key: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI...
reach: build-01.internal:7700
```

| Field | Meaning |
| --- | --- |
| `site` | the name; unique in the workspace |
| `key` | this site's public key. The daemon authenticates peers with it, and with nothing else |
| `reach` | where the daemon can be reached, when the operator declares it. Omitted means no address is declared, not that the site is unreachable |

The declaration travels by git, like every other durable fact, and needs no quorum to read. A
site that is declared but unreachable is a site that is down, not a site that does not exist. A
site that has left has cleared its address and said so; a site that is merely down has not.

**An address is a hint; only a declaration writes the roster.** This machine resolves a peer in
this order: its own `var/config` (written by `site reach`, or by the join for the inviter), the
roster's declared `reach`, and the last LAN announcement in `var/discovered/<site>.yaml`. The
discovery writes only the last; `create --at`, `site invite --at` and `site announce` are what
write the shared roster. A declared address can change: `site announce` writes this site's own
roster entry, or clears it, and commits it as one line event. A site with no declared address is still reachable when discovery
is on and the LAN has found it.

**The LAN hint is plain UDP multicast, off unless asked for.** With discovery on
(`site discover on`; the daemon's internal `--discover`), a daemon announces `{workspace, site, reach}` on a fixed group and port on an interval, and answers a
join query for its own name. A peer keeps the last announcement, ignores one older than a few
intervals, and never treats it as identity: the site key still authenticates every connection.
The network is trusted, so the packet is not signed. Several daemons on one host hear each
other through the loopback. Explicit addresses work without it; turn it on when the machines
share a LAN and the operator wants them found automatically.

**The daemon has its own port and its own protocol.** It listens on its own socket, starts on
demand at the site's address or the fixed default port 7700, authenticates a peer by the key
the roster pins, and answers typed requests. The CLI starts it when a command needs it, and the
next command starts it again after a crash or an address change. It does not shell
out to `sshd` to answer a lookup or take a message: the fabric does both constantly, and
putting a process launch in that path would make the fabric's availability depend on a
machine's SSH configuration. `ssh` is kept for the one thing it is good at here — carrying the
initial join — and for git, which a project may serve either way.

## Actors

An actor is a person or an agent, and thrum treats the two the same. It is worth saying plainly
what that means, because everything below depends on it: **an actor is not a process.** A human
at a keyboard is an actor. An agent that runs one command and exits is an actor. An agent whose
harness keeps a plugin connected is an actor. Nothing here assumes a plugin, a connection, a
window of activity, or a lifetime. What an actor has is a name with standing and a last-known
place.

| | **Standing** | **Where** |
| --- | --- | --- |
| what | this name is a real actor, and what kind | the site that last took mail for it |
| where | the line, `doc/actor/<name>.yaml` | the daemons |
| lifetime | until retired | until the actor says it is somewhere else |

```yaml
# doc/actor/dana.yaml
actor: dana
kind: human              # human | agent
about: Works on the uploader
home: build-01           # where mail waits until this actor says hello somewhere else
registered: 2026-10-16T09:12:00Z
```

Standing is a decision, so it is a record. A name exists before its owner has ever run, it is
unique, and it can be the target of a message. **`thrum actor list` is this register** — every
name that exists, its kind, and where it was last known to be — and an actor that is finished
with is retired rather than left in it.

Where an actor is is a fact about now, so it belongs to the fabric, and it is soft. **There is
no liveness and no session.** Nothing in thrum says whether an actor is working, and nothing
tries to find out: that cannot be observed, and a design that guesses it will guess wrong.
`thrum actor hello` is a CLI command meaning *my mail is here now*, and it is the only thing
that moves an actor. It is the same command for a human at a keyboard and for an agent's
wrapper; a plugin may run it on the actor's behalf, but nothing depends on a plugin existing.

**Nothing ages out.** A last-known place is corrected by the actor saying hello somewhere else,
and by a delivery that arrives where the actor no longer is, and by nothing else. There is no
ttl, no `present`/`idle`/`gone`, and no list that thins itself. A daemon may drop a cached
address to save memory, which costs one question — that is an implementation detail, not a rule
about actors.

**A human is not a degenerate agent.** A message to a human lands in the mailbox at their site
and waits there until they run `thrum message list`; no wake is required, and none is expected.
A message to an agent is delivered the same way, and a plugin may additionally turn the arrival
into a turn. The plugin is an optimization on top of delivery, never the delivery path, and a
harness without one loses only the nudge. `kind` records which of the two an actor is, so a
reader knows what to expect; it changes nothing about what is permitted.

## Finding an actor

To place a name, a daemon looks at what it knows, then asks its reachable neighbours. A
neighbour answers from its own table and does not ask further, so the question cannot loop. The
answer is the last site that took mail for the name, or, if it has never said hello, the `home`
in its standing.
The answer is cached; it is corrected by `actor hello` and by a delivery that finds the actor
has moved.

**Ask; do not merge.** The previous design had daemons exchange whole rosters and merge by
last-seen, which is exactly the concurrent memory that cannot be trusted: two daemons merging
in different orders settle on different answers. Here there is no merged roster — only a
per-name address, a question asked of the sites that might know, and a correction when a
delivery comes back *not here*. A stale address costs one failed delivery and one retry. It
never produces a wrong decision, because the only thing built on an address is where to send
the next message.

## Messages

A message is **temporary work state**: not a record, never committed, and nothing durable is
derived from it. It exists so that one actor can tell another something now.

```yaml
# handed to the recipient's daemon
id: 01J8ZB3WQ7K2
from: flash-9c2e
to: dana
sent: 2026-10-16T10:04:11Z
subject: the retry branch is ready to look at
job: add-retry-to-uploader       # optional: what this is about
reply_to: 01J8ZA...              # optional: what it answers
attachments: [coverage.html]     # optional
---
The flaky case is fixed; `make test-upload` is green.
```

**Route, to the actor's place.** The sender's daemon resolves `to` and hands the message to
the site that takes it: a direct peer gets a push over the site-authenticated channel, a
farther one gets the message forwarded through the next hop (`msg.forward`), and the relay
pushes it on or forwards it again. The receiving daemon writes it into the mailbox, where it
stays until it is read or purged. The sender keeps nothing.

Each daemon keeps a route table. A direct edge is one hop; a route through a neighbour is that
neighbour's distance plus one. The direct edge wins a tie, then the fewest hops, then the
site's name. The table is learned by asking the reachable peers (`site.routes`) on an interval,
is site-local (`var/routes`), and is never committed. A route expires when the neighbour stops
answering, so a relay going away is a route that disappears, not a stale address kept forever.

A forward carries the destination site, the message, the sites it has already passed through,
and a hop count. The message id makes a retry idempotent; the passed-through set and the hop
bound stop a loop. The relay holds the message only for the call: there is no durable queue,
and a destination that stays down makes the send fail, exactly as a direct send does. The
network and the sites are trusted, and the destination authenticates the last hop rather than
the original sender, so a relay can read what it carries.

- **The sender never queues.** A message is placed or it is not sent. If no site is known for
  the name, or the site that would take it cannot be reached, the send fails and says which of
  the two happened. A caller that wants to wait may ask for a bounded window (`--wait 30s`) —
  a bound it chose, not a lifetime the fabric invented. Deferring an operation until an actor
  appears is a real thing a later version will want; it is deliberately not this one.
- **The receiving site holds it.** Once accepted, a message is the receiving site's
  responsibility: it survives a daemon restart, and it waits for a human who reads it a day
  later exactly as it waits for an agent that polls in a second.
- **Whole or not at all.** Attachments are written before the message that names them, so a
  reader never sees a message whose bytes have not arrived.
- **An id is unique and never reused**, so a caller that retries a send cannot deliver twice.
- **Not retractable.** A message may already have started something; a correction is another
  message with `reply_to` set.
- **The plugin wakes an agent; thrum does not.** Waking is a fact about a harness — a session's
  turn, a terminal, a bell — and this design deliberately knows none of them. The fabric
  promises a live event stream and a message that arrives whole; the plugin decides what a wake
  is.

**Binding is the agent's step, and the harness's tool.** A session that wants its mail as turns
tells its harness which actor it is, once, at the start; the plugin records the binding and may say
hello on the actor's behalf. thrum does not know how that call is spelled — the harness's tool list
is where the session finds it, and the workspace does not name it, because a tool named after one
harness is not true of every workspace — but it knows the result: a session bound to a name, and an
event stream that reaches it. `doc/extension.md` is the plugin author's side of this.

## The shared histories

Every workspace has **two** shared histories, and several sites write each of them at once.

**The line** (`doc/`) holds the decisions: actor, site and worktree declarations, conventions,
topics, briefs, asset manifests. A merge in it is never a conflict, because no two sites write one
file:

| Path | Its one writer |
| --- | --- |
| `doc/site/<name>.yaml` | that site |
| `doc/actor/<name>.yaml` | the site that declared the actor |
| `doc/task/<job>/brief.md` | the site the job is being worked at |
| `doc/asset/<name>/<version>.md` | the site that published the version |
| `doc/archive/**` | the site performing the retirement |
| `doc/topic/**`, `doc/convention/**` | whoever is editing — the one place two hands can meet |

A brief has one writer because a job is worked at one site. The last row is the honest exception:
shared prose is edited by people, and two people editing one document at once is a merge that a
person resolves. That is rare, it is visible, and no mechanism would be better than saying so.

**The ledger** (`handoff/`) holds the evidence: one file per stretch of work. It needs no rule at
all, because it is append-only. A handoff's name carries the job and the moment the stretch
started, so two sites recording at once write two files, and both are true about their own
stretch. Nothing in it is ever edited or deleted; a correction is a further new file.

**A name claimed twice is settled, not merged.** Two sites can open a job with the same id, or
declare the same actor, while they are apart. Their commits then add the same path. Every event
carries a monotonic clock, so every site computes the same order from the same events — clock,
then site name, then event id — and the first claim in that order keeps the name. The other is
void: the file is discarded, and its author is told, by name, on the next exchange. Nothing
anybody has seen is rewritten; a loser records again under a name that is free.

**Convergence is automatic, in both.** A site commits locally; the daemon then fetches its peers'
heads, merges them — cleanly, by the rules above — and publishes the result. Two sites publishing
at once is one rejected push and one retry, which is git's own answer and needs no protocol of
ours. What comes out are histories that only ever grow, with a total order that every site derives
the same way rather than one that a writer had to be appointed to impose.

**If a project wants one order, it can name one.** A project that would rather not derive an
order can declare a site as the line's sequencer; peers then submit events to it and it appends
them one at a time, which yields a strictly linear history at the cost of a single writer. The
ledger needs none: its files are independent, and a union is the whole of its merge. Everything
else in this document is unchanged either way. The default is the mergeable one, because a
workspace whose sites are apart for a day should not have to stop recording.

## Git

The daemon is the only transport. No repository has a thrum remote; the CLI never configures
one. `sync` is a daemon operation: the local daemon fetches, merges, settles and pushes the
line, the ledger and the store with the **reachable** peers over the site-authenticated
channel; a peer that does not answer is reported and left for the next sync, so no full mesh
is required. Git's pack protocol is only the payload it carries, and the transport is built in
memory, never written into a repository. The store offers the tags the line calls live, so a
peer's fetched copy can serve a third site. A repository's own git remotes belong to the project and play no part in
`sync`. The code repositories' declarations are in the line, but their content is the project's
own git: thrum neither serves nor exchanges it. The rules are the ones the shared histories
already imply:

| Repository | Ref | Rule |
| --- | --- | --- |
| the line (`doc/`) | the line ref | one writer per path; merged, never rewritten |
| the ledger (`handoff/`) | the ledger ref | append-only; merged by union, never rewritten |
| each code repository (`repos/<name>.git`) | `job/<id>` | one writer: the site holding that job |
| each code repository | the integration refs | one writer: the site the project declares |
| the store (`repos/store.git`) | tags | created once, never moved, deleted only by withdrawal |

Every bare repository lives under `repos/`; the line and the ledger keep their git directory
with their checkout, because they have one, and a code repository is checked out under
`worktree/` only while a job holds it.

A ref has one writer, and a writer is replaced by a decision rather than by a push. Where a
project wants an integration to be exact, thrum performs it as one operation at the declared
site; where it wants to merge by hand, thrum steps out of the way, and the guarantee goes with
it.

## Joining

1. **Invite**, from a site already in the workspace: one event writing the new roster entry,
   and a one-time token that names the workspace, the inviting site, and, when they are known,
   the inviting daemon's address and the new site's address. Either address may be omitted when the
   daemons have the hint on; otherwise the token must carry the inviting daemon's address.
2. **Join**, on the new machine: if the token carries no address, ask the LAN for the inviting
   site first; then prove the token to that daemon, take the line, the ledger and the store
   through it, present a key, and register it in the roster
   as a second event. Starting a daemon is the machine's own init, not the join; the join
   writes this site's files (the invitation already recorded its address), and leaves no
   repository with a thrum remote.
3. **Introduce**: the peers accept the new site once the line that carries its key reaches
   them; the next sync is the introduction.

The token is the only secret that has to move by hand. It names no repository and no URL: the
joining site needs only the inviting daemon's address, its public key, and the nonce.

A site's address is a `host:port` and may be a name the LAN resolves, so a changing address needs
no thrum command while the name follows it. Each site's roster entry declares its address — the
creator's through `create --at`, an invited site's through the invite's `--at` — and each site's
local view in `var/config/` wins over it. `daemon run --listen 0.0.0.0:<port>` keeps the listener
independent of the machine's address.

## Leaving

A site can leave without destroying its copy. `thrum site leave`, run there:

1. refuses while this site holds a job's worktree;
2. clears this site's own declared address in the roster and commits `site <name>: left`;
3. exchanges once more, so the peers that answer learn the departure before the endpoint stops;
4. writes a site-local `inactive` flag, so the CLI does not start the endpoint again;
5. stops the endpoint.

The line, the ledger, the store, the mail and the code stay. The peers see the empty address on
their next sync and stop dialling the site; their own local view still wins, and `site forget
<name>` removes the entry for good. `thrum site stay [--at <host:port>]` reverses it: clear the
flag, write the address locally and in the roster (`site <name>: serving at ...`), and start the
endpoint again. A left site is not a down site: it has said it is not serving.

## The wire

One protocol, versioned from the start, with a mutual-authentication handshake and typed
requests. The handshake carries the workspace, the site, the protocol range, and the verbs the
caller answers; a mismatch is refused clearly, which is a small cost now and the thing that
makes a mixed-version fleet debuggable later.

The verbs are named `noun.verb` throughout — `site.hello`, `site.routes`, `actor.where`,
`actor.hello`, `msg.push`, `msg.forward`, `history.exchange`, `store.published`, `git.serve`
and `stop` — because the
previous design's half-and-half naming was a regret, and a wire is the wrong place to discover
that. `history.exchange` is local only: it is this machine asking its own daemon to sync.
`git.serve` is the daemon's internal payload, never a remote a repository names.

## When something is down

| What is down | What stops | What continues |
| --- | --- | --- |
| a peer | messages to actors there, if no route leads another way; its direct history exchange | everything local, every other peer, and any route through a neighbour; its records reach the others through the remaining graph |
| a relay | the routes through it, after the table expires | messages take another route or fail; the histories still gossip through the rest of the graph |
| the LAN hint | finding a fresh address | the local view and the roster's declared address; a join then has to carry one |
| a site that has left | its declared address, so the peers stop dialling | its line, ledger, store, mail and code; `site stay` returns it |
| the line's sequencer, if one is declared | recording durable events | reading, working, messages, and this site's own commits |
| a daemon | messages, placement and `sync` at that site, until the next command starts it again | its files, and the command line's local operations |

Nothing is refused because the fabric is unhappy, except the thing that genuinely needed the
fabric. There is no hidden queue and no invented liveness: a thing that could not be placed was
not placed, and it says so.

## Known limits

A handoff's name is the moment its stretch started, to the second, so two stretches that start
in the same second share a name and one of them has to record again. A hello's `heard` stamp has
the same granularity: two hellos in one second are ordered by arrival.

A withdrawal deletes the publisher's tag and the remote's, and a peer never pushes a tag it did
not publish. A peer that already fetched the tag keeps its local copy — it still materialises —
and the version number stays taken.

A message carries its attachments as base64 inside the JSON protocol. The message's attachments
total ten megabytes (the tested bound; a hundred is not promised), each with its name, size,
sha256 and MIME. A duplicate name is refused rather than silently overwriting, and a larger
payload belongs in the store (`asset`), not in a message. `site invite` has no capacity in this
version.

The daemon is internal: the CLI starts it on demand and restarts it on the next command after a
crash or an address change; the user-facing commands never ask for it. It stops itself after
six route intervals with no reachable neighbour, so a single-site workspace runs it only around
the commands that need it.

A discovered address is a hint, not a promise: it expires after a few intervals and a stale one
is ignored before a dial. The LAN packet is unsigned because the network is trusted; the site
key, not the packet, decides who a peer is.

A route is a hint. It is learned from neighbours and never committed; a stale route costs one
failed send and a retry, never a wrong record. The sites and the network are trusted, so a
relay may read or forge what it forwards, as any site may write the line.

The daemon transport is a git `ext::` command built in memory: the workspace and binary paths
are carried as written, so a path with a space cannot be carried; the daemon refuses such a
sync rather than write a broken command. A repository has no thrum remote to edit.

## What the fabric must not do

Hold durable truth; decide anything about a job; start, stop, or watch an actor; guess whether
an actor is working; or answer a question it has not just been told or just been asked. Its
whole job is to move things between sites and to keep the shared histories converging.
