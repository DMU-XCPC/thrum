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
being reachable — answering a lookup, taking a message, exchanging the line — so a command on
one machine needs none, and a command that has to reach another site says plainly when it
cannot.

`--json` on any reading command prints what a person sees as machine-readable data. Those keys
are the interface: a key is renamed only with whatever reads it, and a test fails when one
changes.

## Workspace

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum create <dir> --site <name> [--code <repo>]` | Instantiates a workspace from the template thrum carries: the repositories, the runtime directories, the entry point and skill an actor is handed, this site's declaration, and the first event. | Never a plain file copy: bare repositories have to be initialised, and a repository cannot carry an empty directory. |
| `thrum join <token>` | Makes this machine another site: proves the token, presents a key, takes the roster and the repositories, starts the daemon, and registers the key. | A site the roster does not declare is refused rather than half-joined. |
| `thrum update` | Brings the unversioned skeleton up to date: the ignore rules, the empty directories, the entry point and the skill. Reports differing copy-me files and writes them only when asked. | Never touches a versioned file on its own initiative, and never removes a directory. |
| `thrum verify` | Runs every mechanical check, and reads the values the conventions declare. | Reports; it never repairs. A wrong repair costs more than a stale line. |
| `thrum status` | The picture: the board, the sites, the payloads, and this actor's mail. | Reads only. |
| `thrum clean` | Removes what the line does not track and does not ignore. | Never `git clean -x` or `-d`: the first would delete the code repository and its history, the second the workspace's own empty directories. |

## Sites

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum site invite [--capacity <n>]` | Writes the new roster entry and prints a token that names the workspace and this site. | One event. The token expires, and it is the only secret that has to move by hand. |
| `thrum site list`, `thrum site show <name>` | The roster, and what each site is and where it is. | Reads only. |
| `thrum site reach <name> --at <host:port>` | Overrides where a peer listens. | Site-local, never committed; normally written for you at join. |
| `thrum site doctor` | For each peer: declared, addressed, key known, last answered, protocol understood. | Reads only, and useful precisely when something is unreachable. |
| `thrum site key rotate`, `thrum site key revoke` | Replaces a site's key, or withdraws it. | One event. A revoked key is refused by every peer. |
| `thrum site forget <name>` | Removes a site from the roster. | Refused while it is the last site, or while a job is materialised there; reports what it cannot clean up. |

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
| `thrum message send <to> [--subject <text>] [--body <file>\|-] [--attach <file>] [--job <id>] [--reply-to <id>] [--wait <dur>]` | Places the name and pushes the message to the site that takes it. | Refuses a name nobody has declared, and a site that cannot be reached. **The sender never queues**: `--wait` is a bound the caller chose, not a lifetime the fabric invented. |
| `thrum message list [--watch]`, `thrum message read <id>` | Reads this actor's mailbox at the site holding it, reached through the fabric. | Reads only. `--watch` keeps reporting arrivals, and prints the backlog before it starts. |
| `thrum message reply <id>` | Sends a message whose `reply_to` names the one answered. | A message is never retracted or edited; a correction is another message. |
| `thrum message purge <id>` | Removes a message and its attachments. | Manual on purpose: nothing collects messages, because nothing can know which still matter. |
| `thrum events [--json]` | The local event stream: a message arrived, a job's state changed, the line moved. | A hint for a plugin. A missed event is never a lost message. |

## Jobs

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum job open <id> --purpose <text> --label <label> [--goal <text>] [--scope <text>] [--criteria <text>] [--owner <actor>] [--touches <paths>] [--depends-on <ids>] [--keep] [--ephemeral]` | Writes the brief, creates `job/<id>`, and materialises a working directory under the label. `--purpose` is the label's own; `--goal`, `--scope` and `--criteria` are the brief's. | Refuses an id that has been used before — against `doc/task/` and `doc/archive/` both — before anything is written. One event, or none; declaring a label that does not exist yet is a second event of its own. |
| `thrum job list`, `thrum job show <id>` | The board, and one job in full. | Reads only. |
| `thrum job claim <id>` | Sets `owner` on a job that has none. | Refuses a job that already has an owner: taking ownership from somebody is not a claim. |
| `thrum job reassign <id> --to <actor>`, `thrum job relinquish <id>` | Changes or clears the owner. | Refused to anyone but the owner, or a person acting for an owner who is unreachable. |
| `thrum job handoff <id> --status <s> [--body <file>\|-] [--next <text>] [--artifact <path>] [--worktree <label>] [--supersedes <file>] [--blocked-by <id>]` | Writes `handoff/<id>/<started>.md`, a new file every time. | Refuses an existing file name, a brief that is not `active`, and a `next` that disagrees with the status: `none` belongs to `done`, and every other stretch leaves a baton. |
| `thrum job close <id> --status done\|abandoned` | Sets the final status, moves the brief to the archive, and releases the working directory. | Refuses a job that has already closed, before anything is written. It merges nothing. |
| `thrum work <id> --label <label> [--purpose <text>] [--ephemeral] [--goal <text>] [--owner <actor>]` | Open, claim and enter in one act, for the ordinary case of being handed work. `--purpose` is the label's own; `--goal` is the brief's. | As the operations it composes: a job that already has an owner is worked under that owner, not claimed. |

## Worktrees

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum worktree add <label> [--job <id>] [--purpose <text>] [--ephemeral]` | Declares the label if it is new, and checks a job's branch — or a detached head, for comparison — into it. | Refuses a label that already holds another job's branch. A label at rest is clean and detached. |
| `thrum worktree release <label>` | Leaves the directory clean and detached, and destroys it when it is one-off. | |
| `thrum worktree list`, `thrum worktree remove <label>` | What exists, and retiring a label. | Removing a label whose directory is materialised is refused. |

## The line

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum commit --summary <text> [-- <paths>…]` | Turns one hand-made change into one event in the local line. | One event per commit. Refuses an index holding changes the event did not make. |
| `thrum sync` | Exchange: fetch the peers, merge the line, publish. | A merge that needs a person names the file and stops. A name claimed twice is settled by the order every site derives, and the loser is told by name. Nothing already seen is rewritten. |
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

## The daemon

| Command | Does | Refuses, or guarantees |
| --- | --- | --- |
| `thrum daemon run` | Runs this site's daemon in the foreground. | One per site: a second refuses to start. Starting needs no network. |
| `thrum daemon status` | Whether it is up, what it is, and what it holds. | Reads only; works by asking the socket, and its absence is an answer too. |
| `thrum daemon stop` | Stops it. | Restarting loses nothing durable: the roster, the mail and the line are files. |

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
- **it has to reach another machine** — placing a name, sending a message, merging the line,
  carrying a payload.

Everything else is a file plus a commit, and an actor does it in an editor. That is not an
oversight: a workspace has to stay readable and editable without a helper.

## The refusals that hold everywhere

- **A name is used once.** Job ids, actor names, site names, asset versions and convention
  numbers are never reused, and `retire` does not free one.
- **One writer per file.** A file in the line has exactly one site allowed to write it; a write
  from anywhere else is refused rather than merged.
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
