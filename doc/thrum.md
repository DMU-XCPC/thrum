# thrum

thrum is the coordination fabric for a workspace. It has two halves: a command-line program
that an actor runs, and one **daemon** per site. The daemons find actors, carry messages
between them, and put the workspace's shared writes in one order. The command line is how an
actor reaches both the fabric and the workspace's files.

One rule holds the whole design up:

> **The fabric is never an authority.**

Everything durable is a file or a git object. Everything a daemon holds — where a name is, what
is in flight, which event came first — is a cache or a channel. Losing a daemon's memory costs
a lookup, a reconnection, or a retry; it never changes a decision, because no decision is
stored there. That is what makes a distributed daemon safe to have: it is allowed to be wrong,
so its concurrency does not have to be perfect.

## Sites

A **site** is one machine's copy of a workspace, running a daemon. A workspace has one or more
of them.

The roster lives in the line, one file per site:

```yaml
# doc/site/build-01.yaml
site: build-01
key: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI...
reach: build-01.internal:7700
line: true
```

| Field | Meaning |
| --- | --- |
| `site` | the name; unique in the workspace |
| `key` | this site's public key. The daemon authenticates peers with it, and with nothing else |
| `reach` | where the daemon listens. Omitted means this site accepts no peers |
| `line` | at most one site carries this: it is where shared writes are ordered |

The declaration travels by git, like every other durable fact, and needs no quorum to read. A
site that is declared but unreachable is a site that is down, not a site that does not exist.

**The daemon speaks its own protocol**, on its own socket, authenticating a peer by the key the
roster pins. It does not shell out to `sshd` to answer a lookup or take a message: the fabric
does both constantly, and putting a process launch in that path would make the fabric's
availability depend on a machine's SSH configuration. `ssh` is kept for the one thing it is
good at here — carrying the initial join — and for git, which a project may serve either way.

## Actors

An actor is a person or an agent. thrum knows two different things about one, and keeps them
apart.

| | **Standing** | **Location** |
| --- | --- | --- |
| what | this name is a real actor, and what kind | which site is serving it right now |
| where | the line, `doc/actor/<name>.yaml` | the daemons, and nowhere else |
| lifetime | until retired | while it is registered, then gone |
| lost by | nothing; it is a git object | a daemon restart, at the cost of one re-registration |

```yaml
# doc/actor/agent-3f9c.yaml
actor: agent-3f9c
kind: agent              # agent | human
about: Uploader reliability
registered: 2026-10-16T09:12:00Z
```

Standing is what makes a name addressable before its owner has ever run, and what stops two
different agents from sharing a name without one of them being refused. It is a decision, so it
is a record.

Location is what makes *send this to `agent-3f9c`* land on the right machine. It is a fact
about now, so it belongs to the fabric, it is soft, and nothing durable depends on it.

**There is no session.** An actor does not begin or end. It is here while it is registered and
then it is not. `thrum actor hello` is what a running process says — usually the harness plugin
says it on the actor's behalf — and it is the only registration step. Nothing pairs with it: a
process that stops, crashes, or is killed simply stops refreshing, and its location ages out.
There is no `offline`, no ttl to choose, and no state file to go stale.

**Location is a hint with an age.** A daemon keeps `name → (site, last heard)` for the
registrations it hosts and, briefly, for the ones it has been told about. An entry older than
the fabric's memory window is not an answer. Nothing durable is refused because a location is
missing; a message that cannot be placed says so, and the sender decides what to do about it.

**`thrum actor list` lists the living**, not a register of the dead: the names currently
registered anywhere in the fabric, each with its site and how long ago it was heard from. A
name that exists but is not running is not in that list, because a list of the absent answers
nothing. `thrum actor list --standing` reads the line for every name that has ever existed,
which is a different question and is asked a different way.

## Finding an actor

To place a name, a daemon looks in its own registrations, then in what it has been told, then
asks its peers. An answer is cached with the time it arrived; a stale entry is a reason to ask
again, not a fact to act on.

**Ask; do not merge.** The previous design had daemons exchange whole rosters and merge by
last-seen, which is exactly the concurrent memory that cannot be trusted: two daemons merging
in different orders settle on different answers. Here there is no merged roster — only a
per-name hint with an age, and a question asked of the sites that might know. A stale answer
costs one failed delivery and one retry. It never produces a wrong decision, because the only
thing built on a location is where to send the next message.

## Messages

A message is **temporary work state**: not a record, never committed, and nothing durable is
derived from it. It exists so that one actor can tell another something now.

```yaml
# handed to the recipient's daemon
id: 01J8ZB3WQ7K2
from: agent-3f9c
to: dana
sent: 2026-10-16T10:04:11Z
subject: the retry branch is ready to look at
job: add-retry-to-uploader       # optional: what this is about
reply_to: 01J8ZA...              # optional: what it answers
attachments: [coverage.html]     # optional
---
The flaky case is fixed; `make test-upload` is green.
```

**Push, to a live actor.** The sender's daemon places `to`, opens a connection to that site's
daemon, and hands the message over. The receiving daemon accepts it into a bounded local store
and pushes an event to the subscriber bound to that actor. Nothing polls the sender, and
nothing waits in a chain of queues on the way.

- **Not placed, not queued.** If no site is serving the name, or the site serving it cannot be
  reached, the send fails and says which of the two happened. It is not spooled "for later",
  because the actor is as transient as the message, and a spool is a promise that the actor
  will come back to something that was only ever meant for now. A caller that wants to wait may
  ask for a bounded window (`--wait 30s`) — a bound it chose, rather than a lifetime the fabric
  invented.
- **The site holds, the sender does not.** The receiving daemon writes an accepted message
  under `var/msg/<actor>/` until it is read or its window passes, so a restart, or an actor
  that is between two commands, does not lose what the site already took responsibility for.
- **Whole or not at all.** Attachments are written before the message that names them, so a
  subscriber never sees a message whose bytes have not arrived.
- **An id is unique and never reused**, so a caller that retries a send cannot deliver twice.
- **Not retractable.** A message may already have started something; a correction is another
  message with `reply_to` set.
- **The plugin wakes the actor; thrum does not.** Waking an agent is a fact about a harness — a
  session's turn, a terminal, a bell — and this design deliberately knows none of them. The
  fabric promises a live event stream and a message that arrives whole; the plugin decides what
  a wake is. A harness with no plugin loses only the nudge: `thrum message list` still shows
  what arrived, for as long as the receiving daemon keeps it.

## The line

Every workspace has one **line**: the shared, append-only history that every durable
coordination fact goes into — actor and site declarations, conventions, topics, briefs,
handoffs, manifests.

**One site sequences it.** The site whose roster entry says `line: true` is the line's origin,
and its daemon orders events. A peer submits an event to it; the origin appends it to the tip
and moves the ref, one event at a time. Nobody else writes the line ref. Any number of sites
may read it.

This is the main reason the fabric is worth having, and it is a real simplification of the
previous design. Ordering shared writes does not need a protocol every writer repeats, plus a
fetch, a rebase, and a retry loop; it needs one queue at one place. The line is linear by
construction, and recording an event is a submit rather than a race.

- **The origin is a role, not a privilege.** It is declared, it can be moved by one event, and
  a workspace with one site is its own origin. Losing it stops writes and nothing else: reads,
  work, messages, and lookups all continue.
- **An event is refused, never merged.** If an event cannot sit on the tip — it changes a file
  the tip has changed, or it claims a name that is now taken — the origin refuses it and says
  so. What the actor wrote stays in the working tree, to be recorded again.
- **A site that is alone keeps its events** and submits them when the origin returns. They were
  never in the line, so putting them on top rewrites nothing anybody has seen.

## Git

The daemons serve the workspace's repositories, so peers fetch and push through the fabric
rather than needing an external remote. The rules are the ones the line already implies:

| Repository | Ref | Rule |
| --- | --- | --- |
| the line (`doc/`, `handoff/`) | the line ref | written only by the origin's daemon, by appending an event |
| code | `job/<id>` | one writer: the site holding that job |
| code | the integration refs | one writer: the site the project declares |
| assets | tags | created once, never moved, deleted only by withdrawal |

A ref has one writer, and a writer is replaced by a decision rather than by a push. Where a
project wants an integration to be exact, thrum performs it as one operation at the declared
site; where it wants to merge by hand, thrum steps out of the way, and the guarantee goes with
it.

## Joining

1. **Invite**, from a site already in the workspace: one event writing the new roster entry,
   and a token that names the workspace and the inviting site and expires.
2. **Join**, on the new machine: prove the token, present a key, take the roster and the
   repositories, start the daemon, and register the key in the roster as a second event.
3. **Introduce**: the inviting site tells the peers it can reach that the new site exists and
   where it listens. A peer accepts an introduction only for a site the roster declares.

The token is the only secret that has to move by hand. If the two machines can run `ssh`, that
is the easiest channel for it; nothing depends on that.

## The wire

One protocol, versioned from the start, with a mutual-authentication handshake and typed
requests. The handshake carries the workspace, the site, the protocol range, and the verbs the
caller answers; a mismatch is refused clearly, which is a small cost now and the thing that
makes a mixed-version fleet debuggable later.

The verbs are named `noun.verb` throughout — `site.hello`, `actor.where`, `actor.here`,
`msg.push`, `line.append`, `git.serve` — because the previous design's half-and-half naming was
a regret, and a wire is the wrong place to discover that.

## When something is down

| What is down | What stops | What continues |
| --- | --- | --- |
| a peer | messages to actors there, and placements that needed it | everything local, and every other peer |
| the line's origin | recording durable events | reading, working, messages, and this site's own backlog |
| a daemon | one site's place in the fabric | its files, and the command line's local operations |

Nothing is refused because the fabric is unhappy, except the thing that genuinely needed the
fabric. There is no hidden queue and no invented liveness: a thing that could not be placed was
not placed, and it says so.

## What the fabric must not do

Hold durable truth; decide anything about a job; start, stop, or watch an actor; keep a message
past its window; or answer a question it has not just been told or just been asked. Its whole
job is to move things between sites and to put the line in one order.
