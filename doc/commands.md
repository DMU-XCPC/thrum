# Commands and operations

This is how a workspace is reached: the operations, what each one does and what it refuses, and
the commands that carry them. It adds no rule of its own — [`workspace.md`](workspace.md) says
what has to be true of a workspace and [`thrum.md`](thrum.md) says what the fabric makes true of
it, and this is how to ask for either.

## How a command is reached

Every command takes the same two global flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--root` | `$THRUM_ROOT`, then the current directory | the workspace |
| `--actor` | `$THRUM_ACTOR`, then `$USER` | the name this command acts as |

An actor is a name, not a process ([`thrum.md`](thrum.md)): nothing is started and nothing is
registered, and every command works whether or not a daemon is running. What a daemon adds is
being reachable — answering a lookup, taking a message, exchanging the shared histories — so a
command on one machine needs none, and a command that has to reach another site says plainly when
it cannot.

`--json` on any reading command prints what a person sees as machine-readable data. Those keys
are the interface: a key is renamed only with whatever reads it, and a test fails when one
changes.

## Workspace

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum create <dir> --site <name> [--at <host:port>] [--code <name>=<path>]…` | Instantiates a workspace from the template thrum carries: the line, the ledger, one repository per `--code`, and the store — the runtime directories, the entry point and skill an actor is handed, this site's declaration, and the first event. | Never a plain file copy: bare repositories have to be initialised, and a repository cannot carry an empty directory. `--at` is the explicit declaration that records this site's address in the roster and in var/config; omit it and the roster stays empty — with `site discover on`, the LAN can find the site. |
| `thrum join <token>` | Makes this machine another site: proves the token to the inviting site's endpoint, takes the line, the ledger and the store through it, presents a key, and registers it. | A site the roster does not declare is refused rather than half-joined. `site invite` starts the inviting endpoint when needed. If the token carries no address, the machine asks the LAN for the inviting site first (the inviting site must have `site discover on`); the join records the address locally and starts no process. |
| `thrum update` | Brings the unversioned skeleton up to date: the ignore rules, the empty directories, the entry point and the skill. Reports differing copy-me files and writes them only when asked. | Never touches a versioned file on its own initiative, and never removes a directory. |
| `thrum verify` | Runs every mechanical check, including the two that read across boundaries — a handoff's `job` must name a brief in the line, and its `repo` must equal the brief's — and reads the values the conventions declare. | Reports; it never repairs. A wrong repair costs more than a stale record. |
| `thrum status` | The picture: the board, the sites, the payloads, and this actor's mail. | Reads only. |
| `thrum clean` | Removes what the shared histories do not track and do not ignore. | Never `git clean -x` or `-d`: the first would delete `repos/`, the ledger and their histories, the second the workspace's own empty directories. |

## Sites

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum site invite <name> [--at <host:port>]` | Writes the new roster entry and prints a token that names the workspace, this site and, when given, the new site's address. | One event. The token is one-time, and it is the only secret that has to move by hand. Omit `--at` and, with `site discover on` at the sites, the LAN finds the new site; omit it on `create` too and this machine is found the same way. |
| `thrum site announce [--at <host:port>] [--clear]` | Declares this site's address in its roster entry, or clears it, as one line event. | The explicit counterpart of `site reach`: it writes the shared roster, so this site runs it for itself. A peer sees it on the next sync; its own local view still wins. |
| `thrum site leave` | Leaves the workspace: clears this site's declared address, commits the departure, writes a site-local left flag, and stops the endpoint. | Refuses while this site holds a job's worktree, and when it has already left. The line, ledger, store, mail and code stay; `site stay` returns. |
| `thrum site stay [--at <host:port>]` | Returns a site that left: clears the left flag, writes the address locally and in the roster, commits the return, and starts the endpoint. | Refuses when the site has not left. `--at` defaults to the local view, then the old declared address. |
| `thrum site discover [on\|off]` | Turns this site's LAN announcement on or off. | A local setting, never committed; off by default, so nothing is broadcast unless asked for. The endpoint reads it when it starts, and the next command restarts it. |
| `thrum site list`, `thrum site show <name>` | The roster, and what each site is, where it is (declared, local, discovered) and its next hop. | Reads only; the next hop comes from the daemon's learned `var/routes.yaml`, and the discovered address from the LAN hint. |
| `thrum site reach <name> --at <host:port>` | Overrides where a peer listens. | Site-local, never committed; it wins over the roster's declared address and the LAN hint. The inviter's is written by the join and this site's by `create --at`. The host may be a name; it is resolved at each dial. |
| `thrum site routes` | The route table this daemon learned: each site, its next hop and its distance. | Reads `var/routes.yaml`, a hint that is never committed. A direct edge is one hop, then the fewest hops; a route expires when the neighbour stops answering. |
| `thrum site doctor` | For each peer: declared, addressed, key known, last answered, protocol understood. | Reads only, and useful precisely when something is unreachable. |
| `thrum site key rotate`, `thrum site key revoke` | Replaces a site's key, or withdraws it. | One event. A rotated key is proved by the previous one until the line carrying the new one reaches a peer; a revoked key is refused by every peer. |
| `thrum site forget <name>` | Removes a site from the roster. | Refused while it is the last site, or while a job is materialised there; reports what it cannot clean up. |

## Repositories

The code is one or more bare repositories under `repos/`, each declared in the line. A job names
the one it works in, and every label names the one it checks out.

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum repo add <name> [--about <text>] [--from <url\|path>]` | Declares a code repository in the line and materialises it at `repos/<name>.git`, from the given source or as an empty bare repository. | One event. Refuses a name that is taken or is not a slug. |
| `thrum repo list`, `thrum repo show <name>` | The declared repositories, and which this site has materialised. | Reads only. A declared repository that is not here is not an error. |
| `thrum repo remove <name>` | Retires the declaration. | Refused while a live brief names it or a label is bound to it. The name stays taken. |

`repos/` also holds `store.git`, the asset store. It is not declared: every workspace has one.

## Actors

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum actor declare <name> --kind human\|agent [--about <text>] [--home <site>]` | Declares the name: one event in the line. | Refuses a name that is taken or is not a slug. The declaring site is the home unless one is given. |
| `thrum actor hello [--as <name>]` | Tells the local daemon that this actor's mail is here now. | Writes nothing durable. It is an address, not a claim, and it does not expire. |
| `thrum actor list`, `thrum actor show <name>` | The register: every name that exists, its kind, and where it was last known to be. | Reads only. Nothing here says whether an actor is working, and nothing guesses. |
| `thrum actor retire <name>` | Moves the declaration to the archive with a notice. | Reports the live jobs it still owns; retiring does not take them away. The name stays taken. |

**A name must be declared before it can author a record or be sent anything.** A command that
finds its actor undeclared refuses and names `actor declare`, because a declaration is a
decision and nothing else should make one as a side effect.

## Messages

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum message send <to> [--subject <text>] [--body <file>\|-] [--attach <src[:name]>] [--job <id>] [--reply-to <id>] [--wait <dur>]` | Places the name, then hands the message to this site's endpoint (started on demand): a direct peer gets it pushed, a farther one gets it forwarded through the next hop. Attachments are arbitrary bytes; the message's total is ten megabytes, each file carries its name, size, sha256 and MIME, a duplicate name is refused rather than overwriting, and `src:name` renames one. | Refuses a name nobody has declared, a site with no route, a duplicate attachment name, and a total over ten megabytes (put the larger payload in the store with `asset ingest`/`asset publish`). **The sender never queues**, and a relay holds nothing durable: `--wait` is a bound the caller chose, not a lifetime the fabric invented. |
| `thrum message list [--watch]`, `thrum message read <id> [--out <dir>] [--force]` | Reads this actor's mailbox at the site holding it, reached through the fabric; `read --out` writes the attachments, refusing an existing file unless `--force`. | Reads only. `--watch` is an extension interface for plugins (`doc/extension.md`); an actor reads the list itself. |
| `thrum message reply <id>` | Sends a message whose `reply_to` names the one answered. | A message is never retracted or edited; a correction is another message. |
| `thrum message purge <id>` | Removes a message and its attachments. | Manual on purpose: nothing collects messages, because nothing can know which still matter. |
| `thrum events [--json]` | The local event stream: a message arrived, a job's state changed, a shared history moved. An extension interface for plugin authors (`doc/extension.md`), not part of an actor's loop. | A hint, never the delivery path: the mailbox is the truth and a missed event is never a lost message. Binding a name to a session is the harness's tool, not this command. |

## Jobs

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum job open <id> --repo <name> --purpose <text> --label <label> [--goal <text>] [--scope <text>] [--criteria <text>] [--body <file>\|-] [--owner <actor>] [--touches <paths>] [--depends-on <ids>] [--keep] [--ephemeral]` | Writes the brief, creates `job/<id>`, and materialises a working directory under the label, in the named repository. `--purpose` is the label's own; `--goal`, `--scope` and `--criteria` are the brief's summary sections; `--body` supplies the whole body from a file or stdin, sections included. | Refuses an id that has been used before — against `doc/task/` and `doc/archive/` both — and a repository that is not declared, before anything is written. One event, or none; declaring a label that does not exist yet is a second event of its own. |
| `thrum job list`, `thrum job show <id>` | The board, and one job in full. | Reads only. |
| `thrum job claim <id>` | Sets `owner` on a job that has none. | Refuses a job that already has an owner: taking ownership from somebody is not a claim. |
| `thrum job reassign <id> --to <actor>`, `thrum job relinquish <id>` | Changes or clears the owner. | Refused to anyone but the owner, or a person acting for an owner who is unreachable. |
| `thrum job handoff <id> --status <s> [--body <file>\|-] [--next <text>] [--artifact <path>] [--worktree <label>] [--supersedes <file>] [--blocked-by <id>]` | Writes `handoff/<id>/<started>.md` into the ledger — a new file every time. | Refuses an existing file name, a brief that is not `active`, and a `next` that disagrees with the status: `none` belongs to `done`, and every other stretch leaves a baton. Write it before the close that ends the job; the two are separate events in separate repositories. |
| `thrum job close <id> --status done\|abandoned` | Sets the final status, moves the brief to the archive, and releases the working directory. | Refuses a job that has already closed, before anything is written. It is the second of two events when it ends a stretch — the handoff comes first — and it merges nothing. |
| `thrum work <id> --repo <name> --label <label> [--purpose <text>] [--ephemeral] [--goal <text>] [--body <file>\|-] [--owner <actor>]` | Open, claim and enter in one act, for the ordinary case of being handed work. `--purpose` is the label's own; `--goal` is the brief's summary; `--body` is the brief's whole body when it is opened. | As the operations it composes: a job that already has an owner is worked under that owner, not claimed. |

## Worktrees

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum worktree add <label> --repo <name> [--job <id>] [--purpose <text>] [--ephemeral]` | Declares the label if it is new, and checks a job's branch — or a detached head, for comparison — into it. | Refuses a label that already holds another job's branch, and a `--repo` that disagrees with the job's brief. A label at rest is clean and detached. |
| `thrum worktree release <label>` | Leaves the directory clean and detached, and destroys it when it is one-off. | |
| `thrum worktree list`, `thrum worktree remove <label>` | What exists, and retiring a label. | Removing a label whose directory is materialised is refused. |

## The line

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum commit --summary <text> [-- <paths>…]` | Turns one hand-made change into one event in the line. Hand edits are to `doc/`; a handoff belongs to the ledger and is written by `job handoff`. | One event per commit. Refuses an index holding changes the event did not make. |
| `thrum sync` | Exchange the line, the ledger and the store with the reachable peers; this site's endpoint is started on demand, so no separate command is needed. | A peer that does not answer is reported and left for the next sync, so no full mesh is required. The endpoint stops itself when no peer answers for a few intervals; the next command starts it again. No repository has a remote to edit. A merge that needs a person names the file and stops. A name claimed twice is settled by the order every site derives, and the loser is told by name. Nothing already seen is rewritten. |
| `thrum retire <path> --why <text>` | Moves a document to the archive with its notice. | One event. A number once used is never used again. |

## Assets

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum asset ingest <path>` | Writes bytes into the store and computes the raw `sha256` and size. | A git object id is not the file's hash, so both are recorded. |
| `thrum asset publish <name> <version>` | Writes the tag and the manifest. | Refuses a version already used, or a tag that would move. One event. |
| `thrum asset materialise <name> <version>` | Extracts a version into `job/<id>/data/`. | |
| `thrum asset withdraw <name> <version>` | Retires the manifest with a notice and deletes the tag. | One `retire` event. The version number stays taken. |
| `thrum asset store`, `thrum asset prune` | Reports blobs no tag reaches, and collects them. | Those are regenerable by definition. |

## Bundles

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum bundle export <job>` | Writes the payload, and the handoff it belongs to, as one archive. | After a handoff, never during work. Written outside the workspace, and never inside `job/<id>/`. |
| `thrum bundle import <file>` | Unpacks a payload into `job/<id>/`. | Refuses unless the named handoff is present and the commit it names resolves. Refuses when a payload is already there; only an explicit replacement proceeds, reporting every path it discarded. Commits nothing. |

## Other

| Command | Does |
| --- | --- |
| `thrum where [--job <id>]` | Prints the working directory a job is in, or the workspace root. |
| `thrum env --job <id>` | Prints the environment variables that keep a job's tools inside the workspace. |
| `thrum version` | Prints what this is, for a pair of sites that have to agree before they talk. |

## What needs thrum at all

An operation belongs here when doing it by hand is unsafe or impossible:

- **two actors must not both succeed at it** — declaring a name, opening a job, publishing a
  version, claiming a job id;
- **it touches several things at once** — opening a job writes a brief, a branch and a
  directory, and has to leave all three consistent or none;
- **it computes something a hand gets wrong** — content hashes, tags, git bundles, a file name
  from a timestamp;
- **it deletes something a hand would delete too much of** — the guarded clean;
- **it has to reach another machine** — placing a name, sending a message, merging a shared
  history, carrying a payload.

Everything else is a file plus a commit, and an actor does it in an editor. That is not an
oversight: a workspace has to stay readable and editable without a helper.

## The refusals that hold everywhere

- **A name is used once.** Job ids, repository names, actor names, site names, asset versions and
  convention numbers are never reused, and `retire` does not free one.
- **One writer per file in the line; the ledger is append-only.** A file in the line has exactly
  one site allowed to write it, and the ledger takes new files and nothing else; a write outside
  those rules is refused rather than merged.
- **Nothing is assumed.** A claim that cannot be established is refused, never granted: an
  unreachable site is not a site that said no, and an actor that cannot be placed is not an
  actor that is absent.
- **A refusal writes nothing.** A refused operation leaves every record exactly as it found it,
  which is why closing an already-closed job is refused before anything is written.
- **Nothing is merged that has been seen.** A merge only ever adds; a name claimed twice is
  settled, and the loser records again rather than having its work rewritten.

## What thrum must not do

Merge a job's branch, start or stop a service, run a test, run an actor's harness, or decide
which actor takes which job. The first four are the project's business and the last belongs to
whoever dispatches work. thrum never assigns a job to an actor, and it never guesses whether an
actor is working.
