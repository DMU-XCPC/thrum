# Workspace tooling

[`design.md`](design.md) is the design and the authority. This file is derived from it: the operations a workspace needs its tooling to provide. Nothing here adds a rule, and where the two disagree the design wins.

## What needs the tooling at all

An operation belongs here when doing it by hand is unsafe or impossible:

- **two actors must not both succeed at it** — taking a lease, occupying a job, claiming an unowned job, publishing a version, declaring a name;
- **it spans several things at once** — opening a job touches a brief, the code repository and a working directory, and has to leave all three consistent or none;
- **it computes something a hand gets wrong** — content hashes, tags, git bundles, a file name from a timestamp;
- **it deletes something a hand would delete too much of** — the guarded clean;
- **it answers a question no file can** — who is where, and who is on what, right now — and it carries a message from one actor to another;

Everything else is a file plus a commit, and an actor does it in an editor. That is not an oversight: the workspace has to stay readable and editable without a helper.

**The command surface is not part of the design.** One program with subcommands, several programs, or a library all satisfy this list. What matters is that each operation exists and refuses what it must.

## Shape

The operations above are the interface. This section is about the processes that provide them and the topology they form, because one machine and several machines are different problems.

### One site

Everything above is a command-line program and nothing else. No long-running process is required, and that is a constraint rather than an accident: the simplest case — one machine, somebody watching — must work with no infrastructure at all. `var/occupy/` is a note, and "who is working" is answerable by looking around. Actors on one machine can reach each other with no daemon either: a mailbox is a directory, and a roster is a file.

### Several sites

A file cannot do three things, which is why a long-running process exists at all:

- **questions about the present** — who exists and where, who is on a job, which peer can be reached: no file can answer these, because they are true only now;
- **carrying a message** — one actor telling another something needs something listening, and the recipient may be on another machine;
- **exclusion across machines** — the invariants that one working tree gives for free have to be held by something.

So each site may run the same daemon, in the same role. **There is no hub and no privileged node**, and the reasons are structural:

- **The roster is a declaration, not a service.** Which sites exist is `doc/resource/site.yaml`, so it travels by git like every other record, is auditable, and needs no quorum to be readable. It also closes the last machine fact this design was missing: a site's `capacity` is how many jobs that host runs at once.
- **A resource is leased where it is, never where it is used.** A lease is taken at the resource's home site — the machine that physically holds the thing — so exclusion stays site-local and needs no consensus. This costs no new exposure, because reaching the resource already meant reaching that machine.
- **Transport is symmetric, and nothing relays.** Peers serve their repositories to each other, so an external git remote becomes useful rather than necessary, and payload bundles travel directly between peers. A request goes to the site that can answer it and comes back: no daemon forwards for another, which is what keeps an answer from becoming a search. The consequence is deliberate too — a site that cannot be reached is **refused, not proxied**, so a full mesh is not required and a partial one is legitimate. Mutual reachability is required exactly between the sites that have to share a claim or a lease, because that is where exclusion has to hold.
- **The actor roster is a service, not a declaration.** Each daemon knows the names its own site has seen and learns the rest from its peers, because which actors exist is the tooling's knowledge and never a file. The sites themselves are the other case, and stay a declaration.

### Why this topology

The state in this design falls into three kinds, and the topology follows from where each kind can live:

| Kind of state | Example | Home |
| --- | --- | --- |
| Permanent decisions and evidence | handoffs, briefs, conventions, manifests | the shared line — git |
| Site-local runtime | occupancy, this machine's leases, environments, caches | `var/`, tooling-managed |
| **Cross-site runtime** | a message on its way to an actor, and the answer to *who exists, and where* | the daemons, and the mailboxes under `var/` |

Pinning each resource to a home site is what keeps the third row from swallowing the design: exclusion needs no agreement beyond the one machine that physically holds the thing, so nothing has to be agreed globally. What is left is small and does not want agreement either — a message, and the answer to who exists and where — so the daemons carry it and the topology stays symmetric.

Two lessons from prior art are worth recording, because both were learned the hard way elsewhere:

> **Symmetric transport and symmetric membership are cheap. Symmetric exclusion is expensive.**

Systems that tried to make exclusion symmetric paid for it: Cassandra's lightweight transactions use Paxos and are awkward to operate, and Riak largely gave exclusion up. Systems that kept exclusion local — one writer per object, chosen by a map — did not have to.

> **The data path should not need the control plane.**

A resource's home site answers for that resource even when the rest of the world is unreachable, and that failure is the same as the physical one: if the machine is gone, the thing on it was gone anyway.

What is borrowed, and from where:

- **Ceph** — a small cluster map plus a primary per object, with the data path independent of the map. Here the map is a git-tracked declaration, which is steadier than a monitor quorum.
- **Orleans and Akka Cluster Sharding** — a virtual entity that always exists, placed by the runtime, activated once. A job is that entity: it exists, it is materialised at a site, and one actor works it at a time.
- **SWIM, memberlist, Serf** — failure detection without a registry.
- **Fossil and `hg serve`** — a version-control system with a built-in server is a well-trodden path, so `serve` is not an invention.
- **Content-addressed caches** — anything whose bytes are named by their hash can be fetched from any peer that holds them, which is what `store/` already is.
- **etcd** — if a central service is ever wanted, this is the primitive set it would need: leases with a TTL, watches for telling one actor that something happened, compare-and-swap. It should be used rather than reimplemented.

### Degradation

- A peer that is unreachable takes only its own resources out of reach. Work at every other site continues.
- With the site roster unreadable, each site keeps working from the last one it saw. A control plane that is down must not stop work that needs no control.
- If exclusion cannot be established for a cross-site resource, the claim is **refused, not assumed** — the same answer this design gives everywhere else.

## The daemon

The previous section says why a long-running process exists and why there is no hub. This one is what that process is.

### It is optional, and that is load-bearing

Every operation in this document works with no daemon running: the command-line program does its work locally and returns. The daemon is started when a question about the present has to be answered, when peers have to be reached, or when an actor has to be sent something, and the program talks to it when it is there. A site with somebody watching never needs it.

### Lifecycle

- **One per site.** A second refuses to start, by the same atomic discipline the rest of this design uses for occupancy: create a lock under `var/`, and lose if it is already there.
- **Restarting is safe and loses nothing that matters.** The daemon holds almost no state of its own, because the workspace is the state: it re-reads the roster and the records when it starts, and the leases it honours are files. What a restart does lose is a message that was in flight, and nothing else: the roster and the mailboxes are files under `var/`, so they are still there when it returns. Nothing pretends otherwise.
- **Starting needs no network.** It reads the roster from the workspace, begins listening, and reports ready; peers are found later, or never.

### State

| Where | What |
| --- | --- |
| In memory | the names its site has seen and when each was last heard from, which job each says it is on, the last site roster it read, which peers it can currently reach |
| On disk, under `var/` | the actor roster; the mailboxes, with the bytes they carry; `occupy/`, which it writes and clears on an actor's behalf; its lock; its socket |
| Nowhere | job records; payloads except while carrying them; any decision about who should take which job |

### Interfaces

- **Local**: a socket under `var/`, so the command-line program on the same site can ask and tell. When the socket is not there, the command does the work itself.
- **Remote**: SSH, with no authentication model of its own. The machines that hold resources are already reachable over SSH — that is what reaching a resource always meant — so the daemon rides on what exists rather than inventing a trust model. A peer runs one command per request, over the same channel it would use to use the resource; a message is one of those requests, with its attachments behind it.
- **`serve` is therefore nearly free**: the repositories are files at paths the roster declares, and a peer fetches them over SSH like any other remote. Where the sites already share an external git remote, none of it is needed.

### What it answers

| Question | Asked of | Answer |
| --- | --- | --- |
| Take this unit of this resource until then | the resource's home site | written, or refused because it is held |
| Who is on this job | the site that holds the job | its claim, and whether it holds the job at all. A peer that does not answer means *cannot be established*, never *nobody is on it* |
| This job reached `done` | the actors who were waiting on it | a message left in each of their mailboxes, at whichever site they are |
| Who exists, and where | every peer, in parallel | the actor roster each of them keeps |
| Send this actor a message | the recipient's site | written into its mailbox, or refused because that site cannot be reached |

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

## Stack

An implementation decision, not a design rule: nothing in `design.md` depends on any of it, and a workspace has to stay usable if the tool were rewritten in something else tomorrow.

**Pure Go, with cgo disabled.** One statically linked binary per platform, built with `CGO_ENABLED=0`, so it can be dropped onto a machine that has nothing but `git` and work. Cross-compilation is then free, which matters because the tool is installed rather than copied and there is no package manager to lean on.

The parts:

| Concern | Choice |
| --- | --- |
| Command surface | `cobra`, one command per operation, with completion |
| Structured files | `gopkg.in/yaml.v3`, used **only** on a front-matter block |
| Git | the real `git`, always as a subprocess, with its version checked by `verify`: the usage here is unusual enough that git has to stay the authority |
| Peer channel | `golang.org/x/crypto/ssh`, client and server, authenticated from `authorized_keys`; the serving side exposes a forced command with a small verb set rather than a shell |
| Local IPC | JSON over a unix socket at `var/daemon.sock`, with `var/daemon.lock` held by the one daemon |
| Atomicity | create-with-`O_EXCL` for locks, and write-temporary-then-`rename` for every replacement |
| Static assets | `go:embed` for `workspace-template/` |
| Tests | `go test`, golden files for every format, and one test that runs the whole validation list against a fixture workspace |
| Packaging | `goreleaser`, plus a systemd unit for the daemon |

**Front matter is edited textually.** The body of a document is copied through byte for byte and only the delimited front-matter block is decoded and re-encoded. That is why a YAML library without perfect comment round-tripping is good enough here: what must survive is the prose, and the prose never passes through the parser.

**This choice closes one open question and one worry.** Embedding the template is what makes an update derivable: the current template is in the binary, so what a workspace is missing is found by comparing the two rather than remembered, and `update` is what acts on the comparison. And because peers speak SSH, an embedded git server is not a thing that has to be built at all: the repositories are files at declared paths, and serving them is permitting a peer to read them.

## Workspace

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `create` | Instantiates a workspace from the template: the two bare repositories, the empty runtime directories, the ignore rules in both `.gitignore` and `.git/info/exclude`, the entry point and the skill an actor is handed, this site's own name in `var/site`, and the first coordination commit. | Never a plain file copy. The repositories have to be initialised, not duplicated. |
| `update` | Brings the unversioned skeleton up to date with the template the tooling carries: the empty directories, the ignore rules in both copies, and the entry point and skill an actor is handed. Reports the copy-me files under `doc/template/` that differ, and writes them only when a person asks, leaving them uncommitted to be recorded. | Never touches a versioned file on its own initiative, and never removes a directory. Restores a rule the workspace has lost, and keeps a rule the tooling no longer has until `--narrow` says otherwise |
| `verify` | Runs the whole validation list, including the unversioned skeleton — a directory the template defines that is not there, a file the template carries that is not there, and an ignore rule the tooling carries that this workspace has lost in either copy — and the values the conventions declare, since a policy nobody can read is a policy nobody has. | Reports; it does not repair. |
| `status` | Prints the current picture: the task board — each brief's status with its latest handoff — which labels this site has materialised and what each holds, which leases are held, which payloads are collectible, and where a directory exists with no declaration. | Reads only. |
| `clean` | The guarded clean of untracked files. | Never a bare `git clean` at the workspace root, which would delete `.bare/` and the entire source history with it. |
| `join` | Makes this machine another site of a workspace that already exists: takes the shared line from the coordination repository, and fills in what a repository cannot carry — the ignore rules, the empty directories, the entry point and skill an actor is handed, the site's own name, and its own copies of the code and asset repositories. | Commits nothing: joining does not begin a history, and a second first commit would be a second workspace. Refuses a site the roster does not declare, rather than half-joining one. |
| *every record write* | Begins by bringing this site up to date from the shared line, and ends by putting the event in it or taking it back out. | The tip of the shared branch is the only piece of exclusion state a coordination write needs: an event is in the line if and only if its parent was. A commit that lost a race was never in the line, so redoing it rewrites nothing that has been seen; a commit whose file moved as well is refused and taken back out, because two versions of one file cannot both be in one line. Nothing is merged, ever |
| `sync` | Publishes what this site has recorded, and fetches what has to arrive. It brings the site up to date from the shared line first and puts this site's unpublished records on top of it, which is the same step every record write takes — a site that recorded while it was alone has a backlog, and sync is where a backlog is published. At a job move the branch goes too. | A floor rather than a ceiling: publishing more often only makes the backup fresher, and nothing requires a fetch in order to start work. Refuses rather than merging when this site's unpublished records do not go together with what was published elsewhere, and says so when the remote cannot be reached rather than reporting it as a failure to push. |

## Actors

An actor is a name: nothing is started, and nothing is bound to a process. Which names exist, and where each was last heard from, is the tooling's knowledge — deliberately not a file in the workspace.

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `roster` | Knows which names this site has seen, when each was last heard from, and which job each says it is on. | The workspace records names and claims; this is what the tooling has seen, and it is never used to refuse anything |
| `ask` | Answers *who is on this job right now*, from the site that holds it. | The file at `var/occupy/` is a claim, and this reports the one that counts; a query, so it answers rather than refuses, including when the answer is that nobody can say |
| `env` | Prints the base environment variables that point a job's tools at the workspace. | An actor that runs a tool without them writes outside the workspace, where nothing collects or reviews what it leaves |

## Messages

Actors reach each other through the tooling rather than through the records: a message is process information, never committed and never validated. The format is in [`design.md`](design.md); what matters here is that `id` and `subject` are separate, that a reply names what it answers, and that a message may carry bytes.

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `send` | Writes a message into another actor's mailbox, at this site or at the recipient's. | Delivered or refused, never queued: a recipient whose site cannot be reached is an error, not a backlog |
| `inbox`, `read` | Lists a mailbox and reads one message — and can keep reporting what arrives, for a harness that has to be woken rather than to ask. | Process information, not records: never committed, never collected, kept until somebody removes it |
| `reply` | Sends a message whose `reply_to` names the one being answered. | A message cannot be retracted or edited, because it may already have started something; a correction is another message |
| `attach` | Carries bytes with a message. | Not an asset: no content addressing, no tag, no manifest, and never `store/`. Anything that has to survive is published as an asset and referred to by name |
| `purge` | Removes a message and its attachments from a mailbox. | Manual on purpose: nothing collects messages, because nothing can know which of them still matter |


## Jobs

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `open` | Writes the brief, creates the branch, and materialises a labelled working directory. | One commit — unless the label has to be declared first, because that is a different event with a commit of its own. |
| `claim` | Sets `owner` on a job that has none. | The tooling settles simultaneous claims; nothing on disk can. |
| `reassign`, `relinquish` | Changes or clears `owner`, on the owner's decision, or on a person's when the owner has become unreachable. | One commit. A job owned by an actor who no longer exists would otherwise be stuck forever. |
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
| `write handoff` | Writes `handoff/<job-id>/<started-utc>.md`, the time coming from the file name's form. | Refuses an existing file name: never overwriting is the minimum an append-only record owes. Refuses a brief that is not `active`, because a handoff covers a stretch of work, and refuses a `next` that disagrees with the status: `none` belongs to `done`, and every other stretch leaves a baton. |
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

## Still open

- **How a site discovers the others.** Today the roster is a git declaration, each site enters its own reach by hand, and keys are exchanged once; that is enough for a workspace whose sites are known to each other. A discovery protocol is not designed, and is not needed for a site to be added reliably — `join` and the roster are the whole of it.
- **Whether a site ever needs its records to be reconciled rather than published or withdrawn.** Today the answer is no, by construction: an event that cannot go into the line is taken back out and re-recorded. The cost is a round trip per event at a site with a remote.
- **Whether a stored copy of a peer's state is ever needed** for a site that is offline for a long time, and if so whose.
