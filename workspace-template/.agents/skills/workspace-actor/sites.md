# Sites: the fabric, messages, and moving a job

What changes once there is a second machine? A workspace is one project's shared thing; a **site** is
one machine's copy of it. A site has a daemon — the endpoint other sites reach — and the daemons
together are the fabric: they place names, carry messages, and keep the shared histories
converging. The CLI starts that daemon when a command needs it, so it is never a process you run.
Most work never notices, and this file is what to read when it does.

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
  it: fetch the reachable peers, merge, publish, and leave an unreachable peer for the next round.
  Nothing requires a fetch in order to start work.

## Using another site's machine (the helper workflow)

When the work needs another machine — a GPU, a different architecture, a larger disk, a
network location, or assets already materialised there — use it while staying where you are.
Your site keeps the name, the mail, the session and the handoff's `agent`; the other site is a
**helper**, not a second owner.

**The job stays yours.** Its `held_at` is your site: the branch, the commits and the worktree
are yours, and `thrum work` on the helper refuses. The helper receives no hello and no mail.
Your project's git is not modified: no remote, no branch and no ref is added there.

**A connection between two daemons is a link: once either side has reached the other, both have a
route over it, and the address book is only what starts it. You do not have to arrange the
direction.

The helper's operator turns it on, once:**

```sh
thrum site code on     # allow receiving a job's source snapshot
thrum site run on      # allow executing remote commands
```

Both are site-local, never committed, and off by default. The daemon reads them when it starts;
the CLI restarts it on the next command. The caller still has to ask (`sync --code`, `run`).

**The workflow:**

1. Here, open or claim the job and work it as usual. Commit what you want the snapshot's
   tracked part to be current.
2. Copy the working tree to the helper — tracked, uncommitted and untracked files, without
   `.git` and without the files `.gitignore` excludes:
   ```sh
   thrum sync --code --job <id> --site <helper>
   ```
   It is one-way and replaces `job/<id>/data/src/` on the helper. No repository is touched.
3. Bring the inputs. On the helper: `thrum sync` (line, ledger, store), then
   `thrum asset materialise <name> <version> --job <id>`, or `thrum bundle import` for a payload
   from this site.
4. Run there. The default directory is the helper's `job/<id>/data/src/`; `TMPDIR`, the cache
   and the state are the helper's payload; stdout and the exit status come back; a log is
   written under `job/<id>/log/`:
   ```sh
   thrum run --site <helper> --job <id> -- make test
   ```
5. Bring the results home. Small output is in the run's stdout; large results live in the
   helper's `job/<id>/out/`. Export/import them with `thrum bundle`, or publish durable bytes
   with `thrum asset publish` and let this site sync the tag.
6. Write the handoff here — you are the agent — naming the artifacts by their raw hashes. Close
   as usual. The branch and commits remain yours; the helper's repository is unchanged.

**What it is not:**

- **No sandbox.** The helper's operator trusts you: `run` executes as that site's user. The
  command is yours; thrum does not decide it.
- **Not a live stream.** The run's output comes back whole (captured); the full log is on the
  helper.
- **Not a git transfer.** `sync --code` carries files, not history; the helper's own git
  remains the project's. Nothing is fetched, pushed or branched.
- **Not a second writer.** The snapshot replaces `data/src/`; the helper never commits or holds
  the job. Moving the writer is a different act: handoff/close here, make the branch available
  at the other site through the project's git, then work it there.
- **The source is disposable.** `data/src/` is a transient input, collected with the payload
  after the job closes.

**When it refuses:**

- `code receiving is off here (site code on)` — the helper's operator must run `thrum site code on`.
- `remote run is off here (site run on)` — likewise for `thrum site run on`.
- `connect: connection refused` — the helper's daemon is not running; its next command starts it.
- `job <id> is held at <site>` — you are on the helper; work the job at its holder.

## Messages

Actors reach each other through the fabric, not through the records. A message is **temporary work
state**: not a record, never committed, and it does not travel by git. `messages.md` is the whole of
it; what a site adds is where the message actually is.

```sh
thrum message send dana --subject '…' --body '…' [--attach <src[:name]>]
thrum message list
thrum message read <id> [--out <dir>] [--force]
thrum message reply <id> --body '…'
thrum message purge <id>
```

- **The site pushes, and the sender keeps nothing.** The fabric places `to` — the site that last took
  mail for the actor, or its `home` — and hands the message over, directly or through a relay. If
  the name is not declared, or no route reaches the site, the send fails and says which.
- **The receiving site holds it** until it is read or purged, across a daemon restart.
- **Attachments stay small**: a message carries at most ten megabytes of exact bytes; the larger
  payload belongs in the store, and `messages.md` has the details.
- **A wake is not thrum's.** The plugin reaches delivery through the extension interfaces
  (the design's `doc/extension.md`); an actor reads their mailbox. Nothing is woken by the fabric, and a missed
  event is never a lost message.

## Moving a job to another site

A job belongs to the workspace, not to the machine it happens to be running on. Moving one is three
acts, and all three are needed:

1. **The records travel by the shared histories.** The brief is in the line and the handoffs are in
   the ledger; both are already shared.
2. **The branch travels through the fabric.** `job/<job-id>` lives in the job's repository —
   `repo` in the brief — and the receiving site fetches it from the site holding the job.
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
  commit it names must resolve in the job's repository. That single check replaces every merge rule
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
   at `doc/site/<name>.yaml` and prints a token that names the workspace, the inviting site, and,
   when they are known, the address of each. It expires, and it is the only secret that has to move
   by hand.
2. **Join**, on the new machine: `thrum join <token>` proves the token, presents a key, takes the
   roster and every declared repository, and registers the key. It starts no process; the next
   command starts the endpoint. If the token carries no address, the machine asks the LAN first —
   which needs `thrum site discover on` at the inviting site.
3. **The roster is the introduction.** The new entry travels in the line, and the peers see it on
   their next sync; there is no separate announcement to accept.

A site the roster does not declare is refused rather than half-joined, and joining begins no history:
a second first commit would be a second workspace with its own idea of which names are taken.

## The daemon, and addresses

The daemon is internal. The CLI starts it on demand at the site's address, or the fixed default port
7700, and it stops itself after six route intervals with no reachable neighbour. There is no
`daemon run` in your loop, no service to manage and nothing to watch. `thrum site doctor` reports,
per peer, whether it is declared, addressed, keyed and answering; `thrum site routes` prints the
learned route table.

**An address is a hint.** This machine resolves a peer in this order: its own `var/config` (written
by `site reach`, or by the join for the inviter), the roster's declared `reach`, and the last LAN
announcement under `var/discovered`. The discovery is opt-in (`thrum site discover on`), emits
nothing until asked, and never writes the roster. Only an explicit declaration writes the roster:
`create --at`, `site invite --at`, and `thrum site announce`. A site with no declared address is
still reachable when discovery is on and the LAN has found it. A hint is a few seconds old at
most; an explicit `--at` (or `thrum site route <peer> --via <host:port>` on this machine) is what
survives a longer drop.

`thrum site reach <name> --at <host:port>` is the local override; `thrum site announce --at
<host:port>` is the shared declaration, and it restarts this site's endpoint on the next command.
**The same machine in two workspaces runs two daemons** — give them different addresses, because the
default port 7700 is per machine.

A site that accepts no peers is still a site: everything that needs only its own machine works, and
`thrum verify`, `thrum job` and `thrum message list` are unaffected. What stops is reaching another
machine.

**Leaving and returning.** `thrum site leave` clears this site's declared address, commits the
departure, writes a site-local left flag and stops the endpoint. The line, ledger, store, mail and
code stay; the peers stop dialling on the next sync. `thrum site stay [--at <host:port>]` reverses
it. A job's worktree blocks the leave — hand it off first.

## When something is down

- **A peer is unreachable** — messages to actors there, and merging with it directly, stop.
  Everything local, every other peer, and every route through a neighbour continues. Nothing is
  queued.
- **A relay is down** — the routes through it expire; messages take another route or fail.
- **A site has left** — it is not down: it said it is not serving. The peers skip it (or keep their
  own local view), and `site stay` returns it.
- **The daemon stops** — it is internal; the next command starts it again. Mail and records are
  files, so nothing durable is lost.
- **A claim cannot be established** — refused, never assumed. An unreachable site is not a site that
  said no.
- **A message cannot be placed** — the send fails and says so. Nothing is queued on the sending side,
  because a workspace that assumes delivery loses messages quietly.
