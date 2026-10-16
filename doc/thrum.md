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
of them.

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
| `reach` | where the daemon listens. Omitted means this site accepts no peers |

The declaration travels by git, like every other durable fact, and needs no quorum to read. A
site that is declared but unreachable is a site that is down, not a site that does not exist.

**The daemon has its own port and its own protocol.** It listens on its own socket,
authenticates a peer by the key the roster pins, and answers typed requests. It does not shell
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

To place a name, a daemon looks at what it knows, then asks its peers. The answer is the last
site that took mail for the name, or, if it has never said hello, the `home` in its standing.
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

**Push, to the actor's place.** The sender's daemon resolves `to`, opens a connection to that
site's daemon, and hands the message over. The receiving daemon writes it into the mailbox,
where it stays until it is read or purged. The sender keeps nothing.

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

## The line

Every workspace has one **line**: the shared history that every durable coordination fact goes
into — actor and site declarations, conventions, topics, briefs, handoffs, manifests. It is a
git history, and several sites write it at once.

**A merge is never a conflict, because no two sites write one file.** Each file in the line has
exactly one writer:

| Path | Its one writer |
| --- | --- |
| `doc/site/<name>.yaml` | that site |
| `doc/actor/<name>.yaml` | the site that declared the actor |
| `doc/task/<job>/brief.md` | the site the job is being worked at |
| `handoff/<job>/<stamp>.md` | the site that wrote the stretch — a new file every time |
| `doc/asset/<name>/<version>.md` | the site that published the version |
| `doc/archive/**` | the site performing the retirement |
| `doc/topic/**`, `doc/convention/**` | whoever is editing — the one place two hands can meet |

Handoffs are the bulk of the traffic and are new files with distinct names, so they merge by
union. A brief has one writer because a job is worked at one site. The last row is the honest
exception: shared prose is edited by people, and two people editing one document at once is a
merge that a person resolves. That is rare, it is visible, and no mechanism would be better
than saying so.

**A name claimed twice is settled, not merged.** Two sites can open a job with the same id, or
declare the same actor, while they are apart. Their commits then add the same path. Every event
carries a monotonic clock, so every site computes the same order from the same events — clock,
then site name, then event id — and the first claim in that order keeps the name. The other is
void: the file is discarded, and its author is told, by name, on the next exchange. Nothing
anybody has seen is rewritten; a loser records again under a name that is free.

**Convergence is automatic.** A site commits locally; the daemon then fetches its peers' heads,
merges them — cleanly, by the rule above — and publishes the result. Two sites publishing at
once is one rejected push and one retry, which is git's own answer and needs no protocol of
ours. What comes out is a history that only ever grows, with a total order that every site
derives the same way rather than one that a writer had to be appointed to impose.

**If a project wants one order, it can name one.** A project that would rather not derive an
order can declare a site as the line's sequencer; peers then submit events to it and it appends
them one at a time, which yields a strictly linear history at the cost of a single writer.
Everything else in this document is unchanged either way. The default is the mergeable one,
because a workspace whose sites are apart for a day should not have to stop recording.

## Git

The daemons serve the workspace's repositories, so peers fetch and push through the fabric
rather than needing an external remote. The rules are the ones the line already implies:

| Repository | Ref | Rule |
| --- | --- | --- |
| the line (`doc/`, `handoff/`) | the line ref | one writer per path; merged, never rewritten |
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

The verbs are named `noun.verb` throughout — `site.hello`, `actor.where`, `actor.hello`,
`msg.push`, `line.exchange`, `git.serve` — because the previous design's half-and-half naming
was a regret, and a wire is the wrong place to discover that.

## When something is down

| What is down | What stops | What continues |
| --- | --- | --- |
| a peer | messages to actors there, and merging with it | everything local, and every other peer |
| the line's sequencer, if one is declared | recording durable events | reading, working, messages, and this site's own commits |
| a daemon | one site's place in the fabric | its files, and the command line's local operations |

Nothing is refused because the fabric is unhappy, except the thing that genuinely needed the
fabric. There is no hidden queue and no invented liveness: a thing that could not be placed was
not placed, and it says so.

## What the fabric must not do

Hold durable truth; decide anything about a job; start, stop, or watch an actor; guess whether
an actor is working; or answer a question it has not just been told or just been asked. Its
whole job is to move things between sites and to keep the shared history converging.
