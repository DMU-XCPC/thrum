# The workspace tool

`thrum` is the tooling [`../doc/design.md`](../doc/design.md) designs and
[`../doc/tooling.md`](../doc/tooling.md) specifies. It is pure Go
with cgo disabled, and it shells out to `git` rather than reimplementing it.

It adds no rules of its own. Every command below is an operation from
[`../doc/tooling.md`](../doc/tooling.md), and every rule it enforces is one from
[`../doc/design.md`](../doc/design.md); where this file and those disagree, they are right
and this file is wrong.

## Build, test, try

```sh
make build      # writes .gocache/thrum
make test       # every package, plus the end-to-end fixtures
make vet fmt tidy
```

Nothing has to be installed to try it: the binary lands in `.gocache/`, beside the module
and build caches. If the module proxy is unreachable, the Makefile already uses
`goproxy.cn`; `make` is the supported way to build for that reason.

```sh
./.gocache/thrum create /tmp/demo --site build-01
cd /tmp/demo && .../thrum verify

# Or, on a second machine, join a workspace that already exists. Declare the site in
# doc/resource/site.yaml at a site that is already in it first: the roster is what the
# others read to decide who exists.
./.gocache/thrum join /tmp/lab --remote <coordination-url> --site lab-02 --code <code-url>
```

Joining commits nothing: it takes the shared line and fills in what a repository cannot
carry. The reach between two sites is entered on **both** sides, and nothing relays — a
site that cannot be reached is refused, not proxied — so mutual reachability is required
exactly between sites that have to share a claim or a lease.

## An actor is a name

There is no join, no registration step, and no list to be added to. The first time a name
is used — any command with `--actor`, or `$THRUM_ACTOR` — it exists, and the roster
records when and where it was last heard from. Nothing is started, nothing is bound to a
process, and nothing is ever refused because a name is missing: a name is not a
permission.

```sh
thrum actor list                     # every name, and where it was last heard from
thrum actor show claude-opus
thrum actor ask --job add-retry      # who is on it, from the claim every site has
```

`actor ask` reports the claim at `var/occupy/`, and says so. A name that stayed behind
means somebody was working; nothing on disk can say whether they still are, and the
tooling does not pretend otherwise.

## What an actor is handed

A workspace materialises two things for whoever starts an agent: `AGENTS.md` at its root, the
entry point, and `.agents/skills/workspace-actor/`, the skill it names. The skill is the
workspace's own rules written once — the loop, and one file per decision an actor has to make:
where a thing goes, what a record has to say, how a name is made, what outlives a job, how an
asset is kept, what a claim is, and what a second site changes. `SKILL.md` is the index as well
as the loop; an actor reads the file it needs when that decision comes up.

They state what is true of **every** workspace, because the design they come from is in none of
them — and they never restate a record: this project's rules stay in `doc/convention/`, and this
job's intent in its brief. They carry what the tooling says about itself as well: that it is
still being built, and what an actor should do with a defect it meets — keep the evidence, name
it in the handoff, say what was expected — because a workspace has no issue tracker and a
workaround nobody wrote down is a workaround nobody can remove.

They are the tooling's, not the project's: unversioned, ignored like the rest of the skeleton, and
**restored by `update` rather than merged**, because a manual that has fallen behind is replaced
whole. `verify` reports one that is missing or has been edited.

```sh
thrum update                # rewrites the entry point and the skill from the binary
```

A project's own agent guidance is not written there. It is a convention, in `doc/convention/`,
which is versioned, travels with the records, and belongs to whoever writes it.

## Where your tools write

```sh
eval "$(thrum env --job add-retry)"
```

That prints `THRUM_ROOT`, `THRUM_JOB`, `TMPDIR`, `XDG_CACHE_HOME` and `XDG_STATE_HOME`
pointed inside the workspace. A tool run without it writes outside the workspace, where
nothing collects or reviews what it leaves. `--root` defaults to `$THRUM_ROOT`, so a
command run from inside a working directory still finds the workspace.

## Messages

Actors reach each other through the tooling. A message is process information: not a
record, never committed, never validated, and kept in a mailbox under `var/` until
somebody removes it.

```sh
thrum send --to claude-opus --subject "the timeout is explained" --body "…" [--file report.csv]
thrum inbox [--json]                 # ids and subjects, which are separate things
thrum inbox --watch --json           # keep reporting arrivals, one JSON object per line
thrum read <id> [--json] [--out dir] # and take its attachments
thrum reply <id> --body "…"
thrum purge <id>                     # nothing collects messages for you
```

- **`inbox --watch` is the stream a harness reads to be woken.** It prints the mailbox it
  finds — the only chance to see what arrived while nobody was watching, because nothing is
  re-delivered — then one line per message as it arrives, and it does not exit. With `--json`,
  each line is one message, so the stream is JSON Lines and a reader takes one object per line;
  the body is still `thrum read <id>`. A mailbox is created by the first message delivered into
  it, so the watch follows the way down to it: where the system reports changes to a directory
  the kernel reports them, and where it does not, the same stream is fed by looking again on a
  timer. A reader cannot tell which it got.

- **Delivered or refused, never queued.** A recipient at another site is reached through
  that site's daemon; if it cannot be reached the send fails and says so.
- **Cannot be retracted or edited.** It may already have started something, so a
  correction is another message with `--reply-to`.
- **Attachments are bytes.** Not assets: no content addressing, no tag, no manifest, and
  never `store/`. Anything that has to survive is published as an asset and referred to
  by name.
- **`--json` is the interface for other tooling.** `id` and `reply_to` are what a plugin
  refers to; `subject` is what a person reads.

On one machine none of this needs anything running: a mailbox is a directory.

**The local socket** is how a command finds out whether this site is being served, and the
way in for anything local that wants to ask; every other question is answered by reading
the workspace or by dialling the site that can answer it.

## Rules the tool reads

A workspace's own rules for its agents are the conventions, in `doc/convention/`, in number
order — and a workspace **starts with none of them**: they are the project's, and the
tooling never writes one. `thrum verify` is what checks them, and one of the things a
convention can carry is a **value a tool has to act on**, in a fenced block whose keys name
the tool and the value:

```values
collect.window: 14d
```

So the retention window lives with the rule that uses it rather than in a configuration file
of its own, and everyone in the workspace reads the same one. The fence opens at the left
margin, because an indented one is how an example is written and an example must not declare
anything; values are scalars; one block belongs to one convention and one key to one
convention. A value the tooling reads but cannot parse is an error rather than a default —
a policy nobody can read is a policy nobody has.

## The command surface, and the operation it implements

| the operation in `../doc/tooling.md` | Here |
| --- | --- |
| `create` | `thrum create <dir> [--site]` |
| `join` | `thrum join <dir> --remote <url> --site <name> [--code <url>] [--store <url>]` |
| `update` | `thrum update [--dry-run] [--adopt-template] [--narrow]` |
| `verify` | `thrum verify` |
| `status` | `thrum status` |
| `clean` | `thrum clean [--dry-run]` |
| `sync` | `thrum sync [--job <id>] [--fetch]` |
| `roster` | `thrum actor list`, `thrum actor show <name>` |
| `ask` | `thrum actor ask --job <id>` |
| `env` | `thrum env --job <id>` |
| `send`, `inbox`, `read`, `reply`, `attach`, `purge` | `thrum send`, `inbox`, `read`, `reply` (`--file`), `purge` |
| `open` | `thrum open --job <id> --label <label> [--goal/--scope/--criteria\|--brief <file>] [--create-label]` |
| `claim` | `thrum claim --job <id> [--owner <actor>]` |
| `reassign`, `relinquish` | `thrum claim --owner <actor>`, `thrum relinquish` |
| `close` | `thrum close --job <id> [--abandoned]` |
| `add worktree`, `release worktree` | `thrum worktree add`, `thrum worktree release` |
| `occupy`, `vacate`, `sweep` | `thrum occupancy occupy`, `vacate`, `sweep` |
| `declare`, `remove`, `resources` | `thrum resource declare --kind <kind>`, `remove`, `list` |
| `take lease`, `renew`, `release`, `leases` | `thrum lease take`, `renew`, `release`, `list` |
| `ingest`, `publish`, `materialise`, `withdraw`, `store` | `thrum asset ingest`, `publish`, `materialise`, `withdraw`, `store` |
| `write handoff` | `thrum handoff --job <id> --status <s> [--artifact <path>] [--metric k=v]` |
| `commit` | `thrum commit --summary <text> -- <path>…` |
| `collect` | `thrum collect [--window <d>] [--dry-run]` (the window comes from the `collect.window` convention, else 14 days) |
| `export`, `import` | `thrum bundle export`, `thrum bundle import <bundle>` |
| `coordinate` | part of `thrum lease take`: a resource is leased where it lives |
| `serve` | the daemon serves this site's repositories to its peers |
| `peers` | the `sites` section of `thrum status` |

`--root` is global and defaults to `$THRUM_ROOT`, then the current directory. `--actor` is
global and defaults to `$THRUM_ACTOR`, then `$USER`: an actor is one name for the work it is
doing, not a per-command choice.

**Every command that records something** brings this site up to date from the shared line
first, and then puts the event in it or takes it back out. So a workspace whose sites share
a remote has one line with no merges and nothing rewritten, at the cost of a round trip per
recorded event; a workspace with no remote has one line trivially. A site that cannot reach
the remote keeps working, and publishes when it can. If the same file moved at another site
in the meantime, the event is refused rather than merged: what it wrote stays in the working
tree, uncommitted, and the message names the file.

Two commands carry documents written by hand. `thrum open --goal/--scope/--criteria` (or
`--brief <file>`) writes a brief that says something, and `thrum commit` commits a
document an actor edited — a brief's body, a topic, a convention — inferring the event
from the path so the commit message has the same form as every other. It refuses two
documents of different events in one commit, because one commit is one event.

## The daemon is not a command

Nothing a person types names it. It exists so that a site is **reachable**: to answer a
peer's question about who is on a job, to hold a lease another site asked for, and to
take delivery of a message addressed to an actor here.

- A site that declares no `listen:` needs no daemon at all, and never gets one: on one
  machine everything above works with files.
- A site that does accept peers gets one started for it, and it stays until it is
  stopped — a site that accepted peers a moment ago and then left is a site whose actors
  cannot be written to. `thrum daemon stop` ends it.
- Where it accepts peers comes from `var/daemon.yaml`, which is never committed: how to
  reach this machine is its own business, and a site that says nothing accepts nobody.

Two commands are hidden, because they are for the tool itself, for an operator who wants
it under systemd, and for a system `sshd`:

```sh
thrum daemon run     # foreground; also what the tool runs detached for itself
thrum daemon exec    # the forced command a peer runs
thrum daemon stop
```

What it says goes to `var/daemon.log`, which is the only place to find out why it
refused something. One daemon per site, enforced by `var/daemon.lock`, which names the
process holding it; a lock left behind by a process that is gone is taken over by
whoever runs next.

## Reaching a peer

How one site reaches another is the site's own business and lives in
`var/config/<name>.yaml`, never in git:

```yaml
reach: build-01.internal:22     # or user@host:port, or unix:/run/thrum.sock
ssh_user: root                   # optional
key: /etc/thrum/id_ed25519       # optional; var/secrets/id_ed25519 is the default
```

Authentication is SSH's and nothing else: the daemon accepts a peer whose public key is in
`var/secrets/authorized_keys`, and refuses every other key. A peer may ask for exactly two
things — the forced command, and this workspace's own repositories, so that the records
and branches can travel by git. A path outside the workspace is refused, and there is no
shell.

The daemon can serve SSH itself, or a system `sshd` can be pointed at it:

```sshconfig
# authorized_keys on the serving side
command="thrum daemon exec",no-port-forwarding,no-agent-forwarding,no-pty ssh-ed25519 AAAA…
```

## What it refuses, on purpose

- A job id that has been used before; a label that is not declared, or that holds another
  job; opening a job without saying what it is for is allowed but writes a brief that
  says nothing.
- A resource declared without a kind: a kind is never guessed. A name already declared as
  another kind, because a resource name is unique across every kind and the tooling is the only
  writer of those files.
- Overwriting occupancy that names another actor; taking over is explicit.
- Removing a resource whose directory exists.
- A lease for a job the workspace does not know or one that has closed, for an occupied
  unit, for a unit above the resource's capacity, or renewal or release by a job that does
  not hold it.
- An artifact path outside `job/<job-id>/`, or an absolute one: a record names this job's
  payload and not the machine.
- Ownership taken from an owner by anybody but that owner: an owner is who the work was
  promised to.
- Publishing a version twice, reusing a withdrawn version number, or moving a tag.
- Replacing a payload without being asked; importing one whose records have not arrived.
- Writing a handoff over an existing handoff, or for a job that has already closed; closing a job that has already closed, which is refused before anything is written.
- A handoff whose `next` disagrees with its status: `none` belongs to `done`, and every other stretch leaves a baton.
- Sending to a name nobody has seen: the roster is what makes a typo an error rather than
  a message into an empty mailbox.
- Committing two documents of different events together.
- `git clean` with `-x`, or with `-d`: the first would delete `.bare/` and the source
  history with it, the second would demolish the workspace's own empty directories.
- Repairing anything in `verify`. It reports; a wrong repair costs more than a stale line.

## Where it writes

Under `--root`, and nowhere else: `doc/`, `handoff/` and `store/` are the versioned half,
`var/` is this site's runtime — including the actor roster and the mailboxes — and
`job/<job-id>/` is a job's payload. `AGENTS.md` and `.agents/` are the unversioned skeleton
the tooling materialises, beside the empty directories and the ignore rules; the rules that
keep them out of `git status` — and therefore out of `clean` — are written with the rest of
the list, in both copies.

`var/secrets/` holds machine-local credentials, including the daemon's own host key and
its `authorized_keys`. Prefer keeping it outside the workspace; it is gitignored either
way, and it never belongs in a handoff or a message.

## Tests

Each package tests its own rules. `internal/validate` has the checks, `internal/format`
the formats, and `e2e` drives the built command through a whole job's life — create,
declare, open, occupy, lease, hand off, publish, materialise, export, close, collect —
and through actors reaching each other, asserting after every step that the workspace
still passes `thrum verify`. The daemon's tests run two sites against each other over a
real SSH channel, including a message with an attachment.
