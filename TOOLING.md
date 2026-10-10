# Workspace tooling

`README.md` is the design and the authority. This file is derived from it: the operations a workspace needs its tooling to provide. Nothing here adds a rule, and where the two disagree the design wins.

## What needs the tooling at all

An operation belongs here when doing it by hand is unsafe or impossible:

- **two actors must not both succeed at it** — taking a lease, occupying a job, claiming an unowned job, publishing a version, declaring a name;
- **it spans several things at once** — opening a job touches a brief, the code repository and a working directory, and has to leave all three consistent or none;
- **it computes something a hand gets wrong** — content hashes, tags, git bundles, a file name from a timestamp;
- **it deletes something a hand would delete too much of** — the guarded clean;
- **it answers a question no file can** — who is alive, and who is on what.

Everything else is a file plus a commit, and an actor does it in an editor. That is not an oversight: the workspace has to stay readable and editable without a helper.

**The command surface is not part of the design.** One program with subcommands, several programs, or a library all satisfy this list. What matters is that each operation exists and refuses what it must.

## Shape

The operations above are the interface. This section is about the processes that provide them and the topology they form, because one machine and several machines are different problems.

### One site

Everything above is a command-line program and nothing else. No long-running process is required, and that is a constraint rather than an accident: the simplest case — one machine, somebody watching — must work with no infrastructure at all. `var/occupy/` is a note, and "who is working" is answerable by looking around.

### Several sites

A file cannot do three things, which is why a long-running process exists at all:

- **liveness** — no file can tell a working actor from a dead one, and this document says so more than once;
- **waking** — bringing an actor back when the job it waits on reaches `done` needs something listening;
- **exclusion across machines** — the invariants that one working tree gives for free have to be held by something.

So each site may run the same daemon, in the same role. **There is no hub and no privileged node**, and the reasons are structural:

- **The roster is a declaration, not a service.** Which sites exist is `doc/resource/site.yaml`, so it travels by git like every other record, is auditable, and needs no quorum to be readable. It also closes the last machine fact this design was missing: a site's `capacity` is how many jobs that host runs at once.
- **A resource is leased where it is, never where it is used.** A lease is taken at the resource's home site — the machine that physically holds the thing — so exclusion stays site-local and needs no consensus. This costs no new exposure, because reaching the resource already meant reaching that machine.
- **Transport is symmetric.** Peers serve their repositories to each other, so an external git remote becomes useful rather than necessary. Payload bundles travel directly between peers.
- **Liveness is gossip, not a registry.** Each daemon knows its own actors and learns the others' from its peers.

### Why this topology

The state in this design falls into three kinds, and the topology follows from where each kind can live:

| Kind of state | Example | Home |
| --- | --- | --- |
| Permanent decisions and evidence | handoffs, briefs, conventions, manifests | the shared line — git |
| Site-local runtime | occupancy, this machine's leases, environments, caches | `var/`, tooling-managed |
| **Cross-site runtime** | *nothing, once resources have home sites* | — |

The third row is the one that forces a service in most designs. Pinning each resource to a home site is what empties it, and that is the whole reason this topology can be symmetric.

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
- **etcd** — if a central service is ever wanted, this is the primitive set it would need: leases with a TTL, watches for waking, compare-and-swap. It should be used rather than reimplemented.

### Degradation

- A peer that is unreachable takes only its own resources out of reach. Work at every other site continues.
- With the roster unreadable, each site keeps working from the last roster it saw. A control plane that is down must not stop work that needs no control.
- If exclusion cannot be established for a cross-site resource, the claim is **refused, not assumed** — the same answer this design gives everywhere else.

## The daemon

The previous section says why a long-running process exists and why there is no hub. This one is what that process is.

### It is optional, and that is load-bearing

Every operation in this document works with no daemon running: the command-line program does its work locally and returns. The daemon is started when liveness has to be answerable and when peers have to be reached, and the program talks to it when it is there. A site with somebody watching never needs it.

### Lifecycle

- **One per site.** A second refuses to start, by the same atomic discipline the rest of this design uses for occupancy: create a lock under `var/`, and lose if it is already there.
- **Restarting is safe and loses nothing that matters.** The daemon holds almost no state of its own, because the workspace is the state: it re-reads the roster and the records when it starts, and the leases it honours are files. What a restart does lose is its supervision of running actors — and an actor that dies with its supervisor leaves exactly the stale occupancy this design already expects. Nothing pretends otherwise.
- **Starting needs no network.** It reads the roster from the workspace, begins listening, and reports ready; peers are found later, or never.

### State

| Where | What |
| --- | --- |
| In memory | which actors it supervises and which job each is on, the last roster it read, which peers it can currently reach |
| On disk, under `var/` | `occupy/`, which it writes and clears; its lock; its socket |
| Nowhere | job records; payloads except while carrying them; any decision about who should take which job |

### Interfaces

- **Local**: a socket under `var/`, so the command-line program on the same site can ask and tell. When the socket is not there, the command does the work itself.
- **Remote**: SSH, with no authentication model of its own. The machines that hold resources are already reachable over SSH — that is what reaching a resource always meant — so the daemon rides on what exists rather than inventing a trust model. A peer runs one command per request, over the same channel it would use to use the resource.
- **`serve` is therefore nearly free**: the repositories are files at paths the roster declares, and a peer fetches them over SSH like any other remote. Where the sites already share an external git remote, none of it is needed.

### What it answers

| Question | Asked of | Answer |
| --- | --- | --- |
| Take this unit of this resource until then | the resource's home site | written, or refused because it is held |
| Who is on this job | every peer, in parallel | the site whose actor holds it, if any. Sites are few, and the broadcast doubles as a liveness check |
| This job reached `done` | broadcast to every peer | each peer wakes any actor of its own that was waiting on it |
| Who is alive, and on what | this site's own daemon | the registry it holds, which no file can replace |

### Configuration

It needs two things and no more: the workspace root, which is where it was started, and the name of the site it is on. The second comes from `var/site`, a one-line site-local file, because a site that cannot name itself cannot find its own half of a resource declaration — and every peer it talks to needs to know which roster entry it is. Everything else it reads from the workspace: the roster is a declaration, and git is the directory service. There is no discovery protocol.

### Failure

- **A peer is unreachable** — its resources are out of reach; every other site carries on.
- **The roster cannot be read** — keep working from the last one seen.
- **A claim cannot be established** — refused, never assumed.
- **The daemon is absent** — everything local still works, and everything that needed a peer fails loudly.

### What it must not do

Everything in *What the tooling must not do* below, and two more: it does not hold payloads except while carrying them, and it introduces no rule of its own. It exists so that "nobody is watching" and "several machines" can be true, and for nothing else.

## Stack

An implementation decision, not a design rule: nothing in `README.md` depends on any of it, and a workspace has to stay usable if the tool were rewritten in something else tomorrow.

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

**This choice closes one open question and one worry.** Embedding the template means the template version travels with the binary, which is where "how an instantiated workspace receives updates to the template" gets its answer. And because peers speak SSH, an embedded git server is not a thing that has to be built at all: the repositories are files at declared paths, and serving them is permitting a peer to read them.

## Workspace

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `create` | Instantiates a workspace from the template: the two bare repositories, the empty runtime directories, the ignore rules in both `.gitignore` and `.git/info/exclude`, this site's own name in `var/site`, and the first coordination commit. | Never a plain file copy. The repositories have to be initialised, not duplicated. |
| `verify` | Runs the whole validation list. | Reports; it does not repair. |
| `status` | Prints the current picture: the task board — each brief's status with its latest handoff — which labels this site has materialised and what each holds, which leases are held, which payloads are collectible, and where a directory exists with no declaration. | Reads only. |
| `clean` | The guarded clean of untracked files. | Never a bare `git clean` at the workspace root, which would delete `.bare/` and the entire source history with it. |
| `sync` | Pushes the coordination repository, and at a job move the branch too, at the moments records have to travel: a handoff, a close, a promotion, a job moving. Fetches when a record has to arrive. | A floor rather than a ceiling — pushing more often only makes the backup fresher. Nothing requires a fetch in order to start work. |

## Actors

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `registry` | Knows who exists, who is alive, and which job each one is on. | Not a file in the workspace: the workspace records names, and this is the running world. |
| `ask` | Answers *who is on this job right now*. | The file at `var/occupy/` is a claim; this is the answer. |
| `start` | Starts an actor on a job with two facts — the workspace root and the job id — and with the base environment variables that point `TMPDIR`, `XDG_CACHE_HOME` and `XDG_STATE_HOME` at the workspace. | An actor started without them writes outside the workspace, where nothing collects or reviews what it leaves. |
| `wake` | Brings an actor back when a job it is waiting on reaches `done`. | The rule is the workspace's — end the stretch as `blocked` — but waking something is not a thing a file can do. |

## Jobs

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `open` | Writes the brief, creates the branch, and materialises a labelled working directory. | One commit — unless the label has to be declared first, because that is a different event with a commit of its own. |
| `claim` | Sets `owner` on a job that has none. | The tooling settles simultaneous claims; nothing on disk can. |
| `reassign`, `relinquish` | Changes or clears `owner`, on the owner's decision, or on a person's when the owner has become unreachable. | One commit. A job owned by an actor who no longer exists would otherwise be stuck forever. |
| `close` | Sets the final `status`, moves the brief to `doc/archive/task/<job-id>/`, releases the label and any lease the job still holds. | One `close` commit. It merges nothing. |
| `add worktree` | Checks the job's branch out into a label, for the job to work in, or detached, for comparison or reproduction. | Refuses a label that already holds a job branch. A label that is clean and detached is free, once the cleaning the drift rules require has been done. |
| `release worktree` | Leaves the working directory clean and detached, and destroys it when the declaration says one-off. | |

## Occupancy — `var/occupy/`

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `occupy` | Writes `<job-id>.yaml` with the actor and the time a stretch starts. | The actor never writes this file itself; it goes through here. |
| `vacate` | Clears it when the stretch ends. | Left behind, it would claim someone is still working — the one lie the file must not tell. |
| `sweep` | Removes an entry whose job has left `doc/task/`. | The file is anchored by its job, so a file with no job is stale. |

## Resources and leases

| Operation | Does | Refuses, or guarantees |
| --- | --- | --- |
| `declare`, `remove` | Adds or removes an entry in `doc/resource/<kind>.yaml`. | One change per commit, serialised: it is the one file that holds several entries. Removal is refused while the resource is materialised, because pulling a working directory out from under its user is not something a permission makes safe. |
| `resources` | Lists what the project has declared, by kind. | |
| `take lease` | Picks a free unit up to the resource's `capacity` and writes the record with an expiry. | Refuses an occupied unit. Writes the unit quoted, or YAML reads `02` as octal. |
| `renew`, `release` | Extends a lease, or deletes the record. | Releasing early means deleting the file. |
| `leases` | Lists who holds what. | An expired record is free, and so is one whose job has closed. |

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
| `write handoff` | Writes `handoff/<job-id>/<started-utc>.md`, the time coming from the file name's form. | Refuses an existing file name: never overwriting is the minimum an append-only record owes. |
| `commit` | Stages the files of one coordination event and commits them with the message form. | One event per commit. Serialising concurrent commits is the tooling's. |
| `collect` | Deletes `tmp/`, `cache/` and `ckpt/` on close, and `log/`, `data/`, `state/` and `out/` once the retention window has passed. | Skips any job whose brief says `keep: true`. Idempotent, so two collectors cannot harm each other. A handoff keeps the raw hash of everything it names, so a collected artifact is a detectable absence rather than a silent one. |

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

Merging a job's branch, starting and stopping services, running tests, and deciding which actor takes which job. The first three are the project's business; the last belongs to whoever dispatches work. The workspace never assigns a job to an actor.

## Still open

- **How an instantiated workspace receives updates to the template.** Its ignore rules and its templates come from outside itself.
- **The actor registry's own shape** — what it stores and where it lives. It is deliberately not a file in the workspace.
- **Whether a stored copy of a peer's state is ever needed** for a site that is offline for a long time, and if so whose.
