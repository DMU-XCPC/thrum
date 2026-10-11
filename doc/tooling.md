# The interface

This is what a workspace's tooling provides, and how it is reached: the operations, what each
one does and refuses, the interfaces they are asked through, and the commands that carry them.
The design is [`design.md`](design.md) and it is the authority; this document adds no rule of
its own, and the code that makes it is described in [`implementation.md`](implementation.md).

## The daemon

The previous section says why a long-running process exists and why there is no hub. This one is what that process is.

### It is optional, and that is load-bearing

Every operation in this document works with no daemon running: the command-line program does its work locally and returns. The daemon is started when a question about the present has to be answered, when peers have to be reached, or when an actor has to be sent something, and the program talks to it when it is there. A site with somebody watching never needs it.
### Interfaces

- **Local**: a socket under `var/`, so the command-line program on the same site can ask and tell. When the socket is not there, the command does the work itself.
- **Remote**: SSH, with no authentication model of its own. The machines that hold resources are already reachable over SSH — that is what reaching a resource always meant — so the daemon rides on what exists rather than inventing a trust model. A peer runs one command per request, over the same channel it would use to use the resource; a message is one of those requests, with its attachments behind it.
- **`serve` is therefore nearly free**: the repositories are files at paths the roster declares, and a peer fetches them over SSH like any other remote. Where the sites already share an external git remote, none of it is needed. **It is the daemon's own listener that serves them** — a peer asks it for `git-upload-pack` or `git-receive-pack` — because a request through a system sshd runs one forced command and carries the protocol rather than git's. A site that accepts peers through an sshd it already runs therefore answers questions and takes messages through it, and serves its repositories only where its own listener is reachable.
### The wire

A peer is reached over SSH, and every request is one verb with a JSON argument list. These names
are the daemon's own, and they are not yet one shape — some say noun and verb, some are bare —
so this is what they are today rather than what they ought to be:

| Verb | Asks |
| --- | --- |
| `status` | Is this site's daemon there, and what does it hold |
| `roster` | The names this site has seen |
| `roster.learn` | The names the caller has seen, merged into this site's roster |
| `job` | Whether this site holds a job, and what its claim says |
| `occupy`, `vacate` | Write or clear this site's claim on a job |
| `holds` | Whether this site says a resource lives here |
| `lease.take`, `lease.renew`, `lease.release` | The units of a resource, taken and given back where it lives |
| `msg.deliver` | Put a message into one of this site's mailboxes |
| `exec` | Run one of the git verbs allowed inside a served repository |

The names are half one shape and half another: `lease.take` and `msg.deliver` say noun and verb,
while `job`, `occupy`, `holds` and `roster` are bare. Renaming them is a wire change and is not
done here, because **a rename breaks a pair of sites that are not the same version** — an old
site asking with the old name is refused by a new one, and the other way round — and nothing
versions this wire. The sites are one collective's own machines and are updated together, which
is the assumption this rests on rather than a guarantee it makes.

**Renaming one of these breaks a pair of sites that are not the same version** — an old site
asking with the old name is refused by a new one, and the other way round. Nothing here versions
the wire: the sites are one collective's own machines, and they are updated together, which is
the assumption this rests on rather than a guarantee it makes.

### What it answers

| Question | Asked of | Answer |
| --- | --- | --- |
| Take this unit of this resource until then | the resource's home site | written, or refused because it is held |
| Who is on this job | the site that holds the job | its claim, and whether it holds the job at all. A peer that does not answer means *cannot be established*, never *nobody is on it* |
| This job reached `done` | the actors who were waiting on it | a message left in each of their mailboxes, at whichever site they are |
| Who exists, and where | here first, then any peer this site has not heard from | the actor roster, which the daemons keep current between them: each site tells the others what it has seen, merging by when each name was last heard from, so a name stays known while the site that has it is quiet. A site that was asked and did not answer means *cannot be established*, never *nobody by that name* — and a site the roster declares that this machine has no address for cannot be asked at all, which is the same thing |
| Send this actor a message | the recipient's site, found here first and by asking otherwise | written into its mailbox, or refused because that site cannot be reached. **The gate is the site, not the actor**: the mailbox is a directory there, so a message goes in whether or not the recipient is working, and nothing waits for it. A name every site that could be asked has never seen is refused as unknown; a name a silent site might have is refused because *where it is cannot be established*, which is not the same refusal |
### Configuration

Two things it cannot read from the workspace: the root it was started in, and the name of the site it is on. The second comes from `var/site`, a one-line site-local file, because a site that cannot name itself cannot find its own half of a resource declaration — and every peer it talks to needs to know which roster entry it is. Everything else it reads from the workspace: the site roster is a declaration, where it accepts peers is its own file, and git is the directory service. There is no discovery protocol.
### Failure

- **A peer is unreachable** — its resources are out of reach; every other site carries on.
- **The roster cannot be read** — keep working from the last one seen.
- **A claim cannot be established** — refused, never assumed.
- **A message cannot be delivered** — the send is refused and says so. Nothing is queued, because a workspace that assumes delivery loses messages quietly.
- **The daemon is absent** — everything local still works, including messages between actors on this machine, because a mailbox is a directory; asking about another site, and sending to one, fails loudly.
### What it must not do

Everything in *What the tooling must not do* below, and two more: it does not hold a job's payload except while carrying a bundle through — message attachments are its own business and live only under `var/` — and it introduces no rule of its own. It exists so that "nobody is watching" and "several machines" can be true, and for nothing else.

## Workspace

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `create` | Instantiates a workspace from the template: the two bare repositories, the empty runtime directories, the ignore rules in both `.gitignore` and `.git/info/exclude`, the entry point and the skill an actor is handed, this site's own name in `var/site`, and the first coordination commit. | Never a plain file copy. The repositories have to be initialised, not duplicated. |
| `update` | Brings the unversioned skeleton up to date with the template the tooling carries: the empty directories, the ignore rules in both copies, and the entry point and skill an actor is handed. Reports the copy-me files under `doc/template/` that differ, and writes them only when a person asks, leaving them uncommitted to be recorded. | Never touches a versioned file on its own initiative, and never removes a directory. Restores a rule the workspace has lost, and keeps a rule the tooling no longer has until `--narrow` says otherwise |
| `verify` | Runs the whole validation list, including the unversioned skeleton — a directory the template defines that is not there, a file the template carries that is not there, and an ignore rule the tooling carries that this workspace has lost in either copy — and the values the conventions declare, since a policy nobody can read is a policy nobody has. | Reports; it does not repair. |
| `status` | Prints the current picture: the task board — each brief's status with its latest handoff — which labels this site has materialised and what each holds, which leases are held, which payloads are collectible, and where a directory exists with no declaration. | Reads only. | `--json` is the same picture for whatever asked, as `{"report": …, "actors": […], "sites": […]}` — the report, and the two questions about the present that no report answers.
| `clean` | The guarded clean of untracked files. | Never a bare `git clean` at the workspace root, which would delete `.bare/` and the entire source history with it. |
| `join` | Makes this machine another site of a workspace that already exists: takes the shared line from the coordination repository, and fills in what a repository cannot carry — the ignore rules, the empty directories, the entry point and skill an actor is handed, the site's own name, and its own copies of the code and asset repositories. | Commits nothing: joining does not begin a history, and a second first commit would be a second workspace. Refuses a site the roster does not declare, rather than half-joining one. |
| *every record write* | Begins by bringing this site up to date from the shared line, and ends by putting the event in it or taking it back out. | The tip of the shared branch is the only piece of exclusion state a coordination write needs: an event is in the line if and only if its parent was. A commit that lost a race was never in the line, so redoing it rewrites nothing that has been seen; a commit whose file moved as well is refused and taken back out, because two versions of one file cannot both be in one line. Nothing is merged, ever |
| `sync` | Publishes what this site has recorded, and fetches what has to arrive. It brings the site up to date from the shared line first and puts this site's unpublished records on top of it, which is the same step every record write takes — a site that recorded while it was alone has a backlog, and sync is where a backlog is published. At a job move the branch goes too. | A floor rather than a ceiling: publishing more often only makes the backup fresher, and nothing requires a fetch in order to start work. Refuses rather than merging when this site's unpublished records do not go together with what was published elsewhere — naming the file and the way out — and says so when the remote cannot be reached rather than reporting it as a failure to push. Offers the way out as well: taking this site's unpublished records back out of the working copy, which keeps what they wrote. |

## Actors

An actor is a name: nothing is started, and nothing is bound to a process. A session of that
name has two ends, and both are the actor's to state — `actor online` and `actor offline` — and
nothing expires: what a name says about itself is a statement made at a time, and a name that has
never said either way reads as exactly that. Which names exist, and where each was last heard from, is the tooling's knowledge — deliberately not a file in the workspace.

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `roster` | Knows which names this site has seen, when each was last heard from, and which job each says it is on. | The workspace records names and claims; this is what the tooling has seen, and it is never used to refuse anything |
| `ask` | Answers *who is on this job right now*, from the site that holds it. | The file at `var/occupy/` is a claim, and this reports the one that counts; a query, so it answers rather than refuses, including when the answer is that nobody can say |
| `env` | Prints the base environment variables that point a job's tools at the workspace: `THRUM_ROOT`, `THRUM_ACTOR`, `THRUM_JOB`, `TMPDIR`, `XDG_STATE_HOME` and `XDG_CACHE_HOME`, as shell `export` lines to evaluate. | An actor that runs a tool without them writes outside the workspace, where nothing collects or reviews what it leaves. The tool-specific variables a tool has of its own are set when it is invoked, not here |

## Messages

Actors reach each other through the tooling rather than through the records: a message is process information, never committed and never validated. The format is in [`design.md`](design.md); what matters here is that `id` and `subject` are separate, that a reply names what it answers, and that a message may carry bytes.

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `message send` | Writes a message into another actor's mailbox, at this site or at the recipient's. | Delivered or refused, never queued: a recipient whose site cannot be reached is an error, not a backlog |
| `message list`, `message read` | Lists a mailbox and reads one message — and can keep reporting what arrives, for a harness that has to be woken rather than to ask. | Process information, not records: never committed, never collected, kept until somebody removes it |
| `message reply` | Sends a message whose `reply_to` names the one being answered. | A message cannot be retracted or edited, because it may already have started something; a correction is another message |
| `attach` | Carries bytes with a message. | Not an asset: no content addressing, no tag, no manifest, and never `store/`. Anything that has to survive is published as an asset and referred to by name |
| `message purge` | Removes a message and its attachments from a mailbox. | Manual on purpose: nothing collects messages, because nothing can know which of them still matter |

## Jobs

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `open` | Writes the brief, creates the branch, and materialises a labelled working directory. | One commit — unless the label has to be declared first, because that is a different event with a commit of its own. |
| `job claim` | Sets `owner` on a job that has none. | The tooling settles simultaneous claims; nothing on disk can. |
| `reassign`, `job relinquish` | Changes or clears `owner`, on the owner's decision, or on a person's when the owner has become unreachable. | One commit. A job owned by an actor who no longer exists would otherwise be stuck forever. |
| `close` | Sets the final `status`, moves the brief to `doc/archive/task/<job-id>/`, releases the label and any lease the job still holds. | One `close` commit. It merges nothing. A brief that has already closed is refused, before anything is written. |
| `add worktree` | Checks the job's branch out into a label, for the job to work in, or detached, for comparison or reproduction. | Refuses a label that already holds a job branch. A label that is clean and detached is free, once the cleaning the drift rules require has been done. |
| `release worktree` | Leaves the working directory clean and detached, and destroys it when the declaration says one-off. | |

## Occupancy — `var/occupy/`

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `occupy` | Writes `<job-id>.yaml` with the actor and the time a stretch starts — **at the site that holds the job**, which is the site whose working directory has its branch. | The actor never writes this file itself; it goes through here. A job no site holds is refused, and two sites holding one job is refused rather than picked between. Silence from a peer that might hold it is refused too: not answering is not the same as being free |
| `vacate` | Clears it when the stretch ends. | Left behind, it would claim someone is still working — the one lie the file must not tell. |
| `sweep` | Removes an entry whose job has left `doc/task/`, and one at a site that no longer holds the job. | The file is anchored by its job, and a claim follows the working directory: a site that released the job has no business holding a claim on it |

## Resources and leases

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `declare`, `remove` | Adds or removes an entry in `doc/resource/<kind>.yaml`. | One change per commit, serialised: it is the one file that holds several entries. A name already used by another kind is refused, because a name is unique across every kind. Removal is refused while the resource is materialised, because pulling a working directory out from under its user is not something a permission makes safe. |
| `resources` | Lists what the project has declared, by kind. | |
| `take lease` | Picks a free unit up to the resource's `capacity` and writes the record with an expiry. | Refuses an occupied unit. Writes the unit quoted, or YAML reads `02` as octal. |
| `renew`, `release` | Extends the lease a job holds, or deletes the record. | A lease is renewed or released by the job that holds it, and by no other. Releasing early means deleting the file. |
| `leases` | Lists what each job holds. | An expired record is free, and so is one whose job has closed. |

## Assets

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `ingest` | Writes bytes into `store/` and computes the raw `sha256` and `bytes`. | A git object id is not the file's hash, so both are recorded. |
| `publish` | Writes the tag and the manifest. | One `asset` commit. The tag is what makes the version visible, and a tag is never moved. |
| `materialise` | Extracts a version into `job/<job-id>/data/`. | |
| `withdraw` | Moves the manifest to `doc/archive/asset/<name>/<version>.md` with a notice, and deletes the tag. | One `retire` commit. The version number stays taken. |
| `store` | Reports blobs that no tag reaches. | Those are regenerable by definition and may be collected. |

## Records, commits and payload

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `write handoff` | Writes `handoff/<job-id>/<started-utc>.md`, the time coming from the file name's form, and the body from `--body <file>|-` when it is given — its four sections are checked before anything is written — or from the template when it is not. | Refuses an existing file name: never overwriting is the minimum an append-only record owes. Refuses a brief that is not `active`, because a handoff covers a stretch of work, and refuses a `next` that disagrees with the status: `none` belongs to `done`, and every other stretch leaves a baton. |
| `commit` | Stages the files of one coordination event and commits them with the message form, and an event commits exactly the paths it staged. | One event per commit. Serialising concurrent commits on one machine is the tooling's: a second writer waits rather than failing. Refuses when the index holds changes the event did not make, rather than recording them under this event's name. |
| `collect` | Applies the retention policy to the payloads of closed jobs: the cheap half goes at the close, and the rest once the window has passed. The window is a policy value, so it is read from the conventions — `collect.window` — and `--window` overrides it once. | Never collects a payload whose brief says `keep: true`, and never makes a record false: every artifact a handoff names keeps its raw hash, so a collected artifact is a detectable absence. A window the conventions state but the tooling cannot read is an error rather than a default. |

## Bundles

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `export` | Writes `<job-id>-<stamp>.tar.gz` holding `bundle.yaml` and the payload. | After a handoff, never during work. Written outside the workspace, and never inside `job/<job-id>/` itself. Releases nothing, because stopping already released whatever the job held. |
| `import` | Unpacks a payload into `job/<job-id>/`. | Refuses unless the named handoff is present and the commit it names resolves. Refuses when the payload is already there; only an explicit replacement proceeds, reporting every path it discarded. Commits nothing. |

## Across sites

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `coordinate` | Holds the invariants that one working tree gives for free: that a change to the coordination plane is serialised, that one job is worked by one actor, and that a name is used once. | A cross-site claim is refused rather than assumed when it cannot be established. Exclusion is settled at the resource's home site, so no consensus and no hub are involved. |
| `serve` | Serves this site's repositories to its peers. | Read and write for the repositories, authenticated per peer. Sites that need no peers do not run it. |
| `peers` | Reports the roster this site currently believes, and which of those sites it can reach. | Read-only, and useful precisely when something is unreachable. |

## What the tooling must not do

Merging a job's branch, starting and stopping services, running tests, running an actor's harness, and deciding which actor takes which job. The first four are the project's business; the last belongs to whoever dispatches work. The workspace never assigns a job to an actor.

## Commands

Four commands are groups, and each one's subcommands are the operations named above:
`actor list`, `actor show`, `actor online`, `actor offline`; `worktree add`, `worktree release`, `worktree list`,
`worktree prune`; `job list`, `job show`, `job occupy`, `job vacate`, `job claim`,
`job relinquish`;
`lease take`, `lease renew`, `lease release`, `lease list`; `resource declare`,
`resource remove`, `resource list`; `asset ingest`, `asset publish`, `asset materialise`,
`asset withdraw`, `asset store`, `asset prune`; `bundle export`, `bundle import`;
`site listen`, `site reach`, `site key`, `site show`; and the hidden `daemon run`,
`daemon exec`, `daemon stop`. Messages are one group: `message send`, `message list` (with
`--watch`), `message read`, `message reply` and `message purge`.

Every command is one of the operations above, and every one of them takes the same two
global flags: `--root` (the workspace, defaulting to `$THRUM_ROOT`, then the current
directory) and `--actor` (the name to act as, defaulting to `$THRUM_ACTOR`, then `$USER`).
`thrum <command> --help` has the flags in full; this is the map.

| Command | What it is for |
| --- | --- |
| `actor` | Say which names exist, where each was last heard from, and what they are on |
| `asset` | Put durable data in the store, publish it, and take it out again |
| `bundle` | Carry a job's payload to another site, and take one in |
| `clean` | Remove what the coordination repository neither tracks nor ignores |
| `close` | Close a job: set its final status, archive the brief, release the working directory |
| `collect` | Apply the retention policy to the payloads of closed jobs |
| `commit` | Commit one coordination event, for a document written by hand |
| `create` | Create a workspace from the template |
| `env` | Print the environment variables that keep a job's tools inside the workspace |
| `handoff` | Record a stretch of work |
| `job` | The board, one job in full, and who is on it |
| `join` | Make this machine another site of a workspace that already exists |
| `lease` | Take, renew, release and list the units of a declared resource |
| `message` | Send another actor a message, read your own, and keep the mailbox in order |
| `open` | Open a job: the brief, the branch, and a labelled working directory |
| `resource` | Declare or remove the resources a workspace shares |
| `site` | Say how this machine is reached, and how it reaches the others |
| `status` | Print the derived views: the task board, worktrees, leases, occupancy, payloads |
| `sync` | Publish the records this site has made, and fetch what has arrived |
| `update` | Bring this workspace up to date with the template the tooling carries |
| `verify` | Run every mechanical check and report what fails |
| `where` | Print the working directory a job is in, or the workspace root |
| `work` | Take up a job: the label, the job, the claim, and the directory |
| `worktree` | Manage the labelled working directories |

Two commands are hidden, because they are for the tool itself, for an operator who wants it
under systemd, and for a system `sshd`: `thrum daemon run`, `thrum daemon exec` and
`thrum daemon stop`.

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
- Committing two documents of different events together, and recording an event when the index
  holds changes that event did not make.
- Publishing a backlog that does not go together with what was published elsewhere: `sync`
  refuses rather than merging, and says so when the remote cannot be reached instead of
  reporting it as a failure to push.
- Recording an event when the index holds changes that event did not make — checked **before**
  the event writes anything, so a refusal never leaves half an event behind.
- `git clean` with `-x`, or with `-d`: the first would delete `.bare/` and the source
  history with it, the second would demolish the workspace's own empty directories.
- Repairing anything in `verify`. It reports; a wrong repair costs more than a stale line.

## When the records do not go together

Two sites can record something while they are apart and find that their records cannot both
be in one line — the same file, changed on both sides. Nothing is merged and nothing is
dropped: the record that cannot go in stays local, and `sync` refuses with the file named and
the way out in the message.

The way out is three steps, and the first one is the only one that has to be typed carefully:

```sh
git show origin/main:doc/resource/gpu.yaml    # read what the other site published
thrum sync --withdraw                         # take this site's records out of the working
                                              # copy; what they wrote stays in the tree
# put the file right, then record the event again
thrum commit --summary "both declarations" -- doc/resource/gpu.yaml
thrum sync
```

`--withdraw` is safe because the records it takes out were **never in the line**, so nothing
anybody has seen is rewritten, and every file they wrote stays where it is. What is not safe is
the command that looks easiest: **`git reset --hard origin/main` would delete the record**, and
a record in question is a brief or a handoff — somebody's work, and the only copy of it.

## Where it writes

Under `--root`, and nowhere else: `doc/`, `handoff/` and `store/` are the versioned half,
`var/` is this site's runtime — including `var/records.lock`, the advisory lock a record write
holds so that two actors on one machine wait for each other — and — including the actor roster and the mailboxes — and
`job/<job-id>/` is a job's payload. `AGENTS.md` and `.agents/` are the unversioned skeleton
the tooling materialises, beside the empty directories and the ignore rules; the rules that
keep them out of `git status` — and therefore out of `clean` — are written with the rest of
the list, in both copies.

`var/secrets/` holds machine-local credentials, including the daemon's own host key and
its `authorized_keys`. Prefer keeping it outside the workspace; it is gitignored either
way, and it never belongs in a handoff or a message.

## Fetching a peer's repositories

A site with a daemon answers for three repositories and no others: the workspace root (the
records), the code repository, and the store. Nothing in this tool fetches them — a peer uses
its own `ssh` as the transport, exactly as it would for any other remote, and git asks the
daemon for `git-upload-pack <path>` over that channel:

```sh
# the key is the workspace's own, and the path is the site's, as the roster declares it
export GIT_SSH_COMMAND="ssh -i /path/to/this/site/var/secrets/id_ed25519 -p <port> \
  -o StrictHostKeyChecking=accept-new"
git ls-remote "ssh://<user>@<host><that-site's-workspace-root>"
```

Nothing needs an external git remote for this: a second site can be brought into being from a
peer's daemon alone, by joining with that URL as `--remote`. `join` and `sync` take it like any
other remote URL, and the only thing that has to be arranged is that the key in the peer's
`var/secrets/authorized_keys` is the one the client offers.
