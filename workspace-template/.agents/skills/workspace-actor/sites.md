# Sites: the fabric, messages, and moving a job

What changes once there is a second machine? A workspace is one project's shared thing; a **site** is
one machine's copy of it. Each site runs a daemon, and the daemons together are the fabric: they
place names, carry messages, and keep the shared histories converging. Most work never notices, and
this file is what to read when it does.

## The shared histories converge, and a merge is never a conflict

**The line** (`doc/`) is the decisions, and no two sites write one file there: a brief has the one
site working its job, and a declaration has the site that made it. **The ledger** (`handoff/`) is the
evidence, and it is append-only: every handoff is a new file whose name carries the job and the
moment the stretch started, so two sites recording at once write two files and both are true. So an
event recorded while sites were apart joins without a merge, and a file that has been seen is never
rewritten.

- **A name claimed twice is settled, not merged.** Two sites can open a job with the same id, or
  declare the same actor, while they are apart, and their events claim the same path. Every event
  carries a clock, every site derives the same order from the same events, and the first claim keeps
  the name. The other is void: the file is discarded and its author is told, by name, on the next
  exchange. You record again under a name that is free.
- **The one place two hands can meet is shared prose** — `doc/topic/` and `doc/convention/`. Two
  people editing one document at once is a merge a person resolves. It is rare, it is visible, and no
  mechanism would be better than saying so.
- **Fetch and publish when another site depends on it**, not on every keystroke. `thrum sync` does
  it: fetch the peers, merge, publish. Nothing requires a fetch in order to start work.

## Messages

Actors reach each other through the fabric, not through the records. A message is **temporary work
state**: not a record, never committed, and it does not travel by git. `messages.md` is the whole of
it; what a site adds is where the message actually is.

```sh
thrum message send dana --subject '…' --body '…' [--attach <file>]
thrum message list [--watch]
thrum message read <id>
thrum message reply <id> --body '…'
thrum message purge <id>
thrum events [--json]
```

- **The site pushes, and the sender keeps nothing.** The fabric places `to` — the site that last took
  mail for the actor, or its `home` — and hands the message over. If the name is not declared, or
  that site cannot be reached, the send fails and says which.
- **The receiving site holds it** until it is read or purged, across a daemon restart.
- **A wake is not thrum's.** `thrum events` is a stream a plugin may subscribe to; a human reads
  their mailbox. Nothing is woken by the fabric, and a missed event is never a lost message.

## Moving a job to another site

A job belongs to the workspace, not to the machine it happens to be running on. Moving one is three
acts, and all three are needed:

1. **The records travel by the shared histories.** The brief is in the line and the handoffs are in
   the ledger; both are already shared.
2. **The branch travels through the fabric.** `job/<job-id>` lives in the code repository, and the
   receiving site fetches it from the site holding the job.
3. **The payload travels as a bundle**, because git cannot carry it.

**A move does not by itself change who owns the job.** The receiving actor may work it under the
owner's authority, or the owner may give it up first and let the receiver claim it. Both are
ordinary, and neither is implied by the move.

### Bundles

A bundle is a transport artifact, not a workspace file: written outside the workspace, nothing in the
workspace ever refers to it, and it is expected to be deleted once unpacked. Its name is a
convenience and not an identity — a file that has been through a chat client comes back renamed — so
what a receiver trusts is the header inside it.

```text
bundle.yaml
job/<job-id>/log|data|ckpt|out/…
```

| Field | Required | Meaning |
| --- | --- | --- |
| `job` | always | the job this payload belongs to |
| `handoff` | always | the handoff it corresponds to, by name |
| `exported` | always | RFC 3339 UTC |
| `site` | optional | where it came from, for whoever finds the file later |

**`handoff` is the whole check.** If the receiving site has that file, it has the records the payload
belongs to; if it does not, the bundle arrived ahead of the history it depends on, and the answer is
to sync rather than to guess.

```sh
thrum bundle export <job-id> --out /somewhere/outside/the/workspace
thrum bundle import <bundle> [--replace]
```

- **Export runs after a handoff and never during work** — the bundle says which handoff it belongs
  to. It is written outside the workspace, and never inside `job/<job-id>/` itself.
- **Import refuses unless the records are already local**: the named handoff must be present, and the
  commit it names must resolve in the code repository. That single check replaces every merge rule
  the workspace would otherwise need.
- **Import refuses when `job/<job-id>/` is already present** and says so. Only `--replace` proceeds,
  discarding what is there and reporting every path it discarded.
- **Import commits nothing**, because it writes nothing versioned.

### What a bundle never carries

`tmp/` and `cache/` are absent by construction. Nothing under `var/` is carried either:

| Not carried | Why not |
| --- | --- |
| `var/env/<label>/` | a materialised environment is **not portable**: a virtualenv embeds absolute paths, a dependency tree may hold native builds, a compiler cache belongs to one architecture |
| `var/state/<service>/` | service data belongs to the site; what a job needs from it is the recipe that resets it, or a private copy in `job/<job-id>/state/` |
| `var/cache/` | regenerable and machine-specific |
| `var/config/`, `var/secrets/` | site-local by definition; that is what they are for |

The environment variables that point a job's tools at the workspace name paths **inside** it, so they
read the same on every site; there is nothing for a bundle to carry.

## Joining a site

Adding a machine is two commands and one token.

1. **Invite**, from a site already in the workspace: `thrum site invite` writes the new roster entry
   at `doc/site/<name>.yaml` and prints a token that names the workspace and the inviting site. It
   expires, and it is the only secret that has to move by hand.
2. **Join**, on the new machine: `thrum join <token>` proves the token, presents a key, takes the
   roster and the repositories, starts this site's daemon, and registers the key in the roster.
3. **Introduce**: the inviting site tells the peers it can reach that the new site exists and where
   it listens. A peer accepts an introduction only for a site the roster already declares.

A site the roster does not declare is refused rather than half-joined, and joining begins no history:
a second first commit would be a second workspace with its own idea of which names are taken.

**The daemon is what makes a site reachable.** It listens on its own port with its own protocol, and
authenticates a peer by the key the roster pins. Reach normally writes itself at join;
`thrum site reach <name> --at <host:port>` is the override, and it is site-local, never committed.
`thrum site doctor` reports, per peer, whether it is declared, addressed, keyed, answering, and
speaking a version this site understands.

A site that accepts no peers is still a site: everything that needs only its own machine works, and
`thrum verify`, `thrum job` and `thrum message list` are unaffected. What stops is reaching another
machine.

## When something is down

- **A peer is unreachable** — messages to actors there, and merging with it, stop. Everything local,
  and every other peer, continues.
- **A claim cannot be established** — refused, never assumed. An unreachable site is not a site that
  said no.
- **A message cannot be placed** — the send fails and says so. Nothing is queued on the sending side,
  because a workspace that assumes delivery loses messages quietly.
