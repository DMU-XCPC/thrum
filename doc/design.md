# The design of a workspace

This is the design of a workspace: what it is made of, what rules hold it together, and
why. It is the authority for everything else in this repository — the tooling implements
it, and where an implementation and this document disagree, this document is right.

It describes **a workspace created from the template**, not this repository. The
operations a workspace needs its tooling to provide are in [`tooling.md`](tooling.md),
which is derived from this document and adds no rule of its own.

Four words are used throughout, and they are defined at the start because everything else
is built on them.

## The designed workspace

The rest of this document describes a workspace created from the template, not this repository. Four words are used throughout. An **actor** is a person or an agent acting through the tooling, identified by one name for as long as it is working rather than by one run of one process; which actors exist, and where each of them is, is the tooling's knowledge, and this workspace writes down names and nothing else about them. A **job** is one unit of work, and it is what holds everything at runtime: the working directory it occupies, its leases, its payload. An actor holds none of those. A **workspace** is one project's shared thing — the code repository, the asset repository, and the coordination plane — while a **site** is one machine's copy of it, with its own checkouts, working directories, payloads and runtime state. A project may have more than one workspace; a workspace has one or more sites. Records travel between sites through git, and `job/`, `worktree/` and `var/` never leave the machine on their own — the one exception is a payload deliberately bundled for another site.

### Workspace tree

The directory name of a workspace is chosen when it is created; `workspace/` below is an example.

```text
workspace/
    .git/                            # Coordination repository: versions doc/ and handoff/
    .gitignore                       # Ignore rules, reproduced from the template; never tracked
    AGENTS.md                        # The entry point for an actor; from the template; never tracked
    .agents/skills/                  # The workspace's own rules, read on demand; never tracked
    .bare/                           # Code repository (bare), checked out into worktree/

    # ── Coordination plane: versioned, identified by job-id, small
    doc/
        convention/                  # Rules every agent must read; numbering is the order
        task/<job-id>/               # brief.md
        topic/<topic>/               # Reference knowledge shared across jobs
        asset/<asset-name>/          # Manifests anchoring store/ and external data
        template/                    # brief.md, handoff.md, manifest.md
        archive/                     # Retired documents and closed jobs, original layout preserved
        resource/                    # Declared resources, one file per kind
    handoff/<job-id>/<started-utc>.md # Evidence: append-only, never edited

    # ── Asset plane: durable bytes, content-addressed, never checked out
    store/                           # Asset repository (bare)

    # ── Runtime plane: unversioned; holds no decision
    var/
        lease/                       # One unit of a declared resource, held until it expires
        occupy/<job-id>.yaml         # Who is on a job at this site, and since when
        state/<service>/             # Data of a shared stateful dependency
        env/<label>/                 # Per-worktree dependency environments
        cache/                       # Workspace-wide regenerable caches
        config/                      # The site-local half of resource declarations
        site                         # Which site this machine is, by name
        secrets/                     # Machine-local credentials; prefer outside the workspace
    job/<job-id>/                    # log/, tmp/, cache/, data/, state/, ckpt/, out/
    worktree/<label>/                # A working directory for an area of work; passed between jobs
```

### Repositories

| Repository | Path | Holds | Branches |
| --- | --- | --- | --- |
| Coordination | `workspace/.git/` | `doc/` and `handoff/` | One branch (`main`), never branched |
| Code | `workspace/.bare/` | Source code, checked out under `worktree/` | One branch per job |
| Assets | `workspace/store/` | Durable data, published as tags | Never branched |

Each plane gets its own repository because each has a different lifecycle. The code repository is bare so that no checkout duplicates it, and the asset repository is bare for the same reason.

### Version control

- **Only `doc/` and `handoff/` are tracked by the coordination repository.** `.bare/`, `store/`, `var/`, `job/`, and `worktree/` are listed in `.gitignore` **and** duplicated in `.git/info/exclude`, so that one bad edit to a tracked file cannot expose the source history.
- **The coordination repository never branches.** It is the single shared, always-current source of truth; only the code repository uses one branch per job.
- **Never run a bare `git clean` at the workspace root.** `git clean -xdf` would delete `.bare/` and, with it, the entire source history; use the guarded clean operation supplied by the tooling instead.
- **Keep a backup of the code repository outside the workspace.** Because `.bare/` lives inside the coordination repository's working tree, its safety depends on the ignore rules above, and an external mirror is the only real recovery path.
- **Commit one coordination event at a time.** `store/`, `var/`, and `job/` never appear in a commit; artifacts under `job/` are referenced from `handoff/` by raw checksum instead.
- **Push when records have to travel, not whenever you commit.** The coordination repository is read by the actors that need it, and nothing requires a fetch in order to start work: who is on a job right now is the tooling's answer and a site-local note, and nothing in the shared line depends on it. What has to cross is the moments after which another site depends on them — a handoff, a close, a promotion, a job moving — and pushing then costs nothing extra, because the small commits have been sitting locally all along. That is a floor and not a ceiling: the travel moments are also when a remote copy becomes a backup, so a project that wants its coordination history off the machine sooner simply pushes more often.
- **Assets are published as tags.** A blob that no tag reaches is regenerable by definition and may be collected.
- **Two kinds of file are written differently.** `doc/convention/`, `doc/topic/`, briefs, handoffs and the archive are written by actors, in an editor. `doc/resource/`, `doc/asset/` and everything under `var/` are written only by the tooling, because each carries an invariant a hand would break — a name that has to stay unique, a hash that has to keep matching the bytes it names, an occupancy with an expiry. In a multi-site workspace the second kind is never edited by hand, and every commit goes through the tooling, which is where the coordination lock lives.

### Commit messages

The code repository's commit conventions are the project's own business, and nothing here constrains them. The coordination repository has one commit per coordination event, and its subject line names the event:

```text
<kind> <subject>: <summary>
```

| Kind | Subject | Example |
| --- | --- | --- |
| `init` | omitted | `init: created from the template` |
| `brief` | job id | `brief add-retry-to-uploader: scope narrowed to the uploader only` |
| `handoff` | job id | `handoff add-retry-to-uploader: the flaky timeout remains` |
| `close` | job id | `close add-retry-to-uploader: acceptance criteria hold after four handoffs` |
| `topic` | topic name | `topic upload-protocol: added the retry contract` |
| `convention` | file name without `.md` | `convention 03-naming: numbered conventions are never reused` |
| `asset` | `name@version` | `asset uploader-baseline@2026-10-09: published from a production sample` |
| `resource` | omitted | `resource: declared the uploader and spike-gc worktrees, the latter one-off` |
| `template` | file name without `.md` | `template brief: added the question a brief has to answer` |
| `retire` | path relative to `doc/`, without the extension | `retire topic/upload/old-retry: superseded by topic/upload/retry` |

- One message describes one event, matching the commit granularity above.
- A message never restates the document it commits; it says what changed. The document is the content.
- The summary is plain language, and a body is written only when it adds something.

## Workflow

### Handing a workspace to an actor

Whoever starts an actor gives it two facts: the workspace root and a job id. Everything else
it derives from the workspace, because anything pushed into a prompt is a copy that goes
stale, while the workspace is where the current answer already is.

An actor also works under a **name**, and the name is what the workspace records — a handoff's
`agent`, an occupancy's `occupant`, a job's `owner`. One name holds for as long as that actor is
long as that actor is working: every command of one stretch records the same one. A later session
may use a different name, and the roster simply stops hearing from the old one. A name is not a
permission: nothing is refused for being unknown, and the first time one is used it exists.

What the workspace carries to meet it is an **entry point** at its root and the **skill** that
entry point names: the workspace's own rules, written once — how a name is made, where a thing
goes, what a record has to say, and in what order to ask — together with whatever the tooling has
to say about itself, such as that it is still being built. They are the tooling's, reproduced
from the template the tooling carries; what they state is what is true of every workspace,
because this document is in none of them, and what they say about the tooling changes with it
rather than with the workspace, which is why it has no other home. A rule this project decides for
itself belongs to `doc/convention/`, and what is true only of one job belongs to its brief;
neither is restated there, and the entry point says where each is. A project that wants its own
agent guidance writes it where its own guidance already goes: in `doc/convention/`, read in number
order before the work starts.

The entry point is not the code repository's `AGENTS.md`. That one sits inside a worktree, next
to the code, is versioned with it, and belongs to the project; a workspace never writes it and
never reads it. The one at the workspace root is unversioned skeleton, on the same footing as
the ignore rules: the tooling's, restored rather than merged, and never carried by git.

Nothing depends on the entry point being loaded automatically, and no rule is enforced by
whether it was. It is there to be read. An actor that never sees it is given the same two facts
and finds the same records, and the refusals it meets are the same refusals.

### Opening a job

A job begins when an actor writes down what it is: an id, a goal, what is out of scope, the acceptance criteria, the paths it may touch, and who owns it. Opening produces the brief at `doc/task/<job-id>/brief.md`, the branch `job/<job-id>`, and a labelled working directory, and it is one commit — unless the label it needs has not been declared yet, because declaring a resource is a different event with a commit of its own.

**A label is free when the code repository's `git worktree list` shows no job branch in it**, after the cleaning the drift rules already call for: an idle working directory is clean and detached. It is occupied when it shows one, and an occupied label is never taken from under the job holding it — the job that wants it ends the stretch as `blocked`, naming that job. Wanting a different area is not a conflict at all: that is a new declaration, and its own commit.

The owner need not be the actor that does the work, and it can be reassigned or given up. While a job has an owner, that is who the next actor asks when the brief is unclear. An owner that gives the job up makes it unowned, and an unowned job may be claimed by whoever takes it up, without asking anyone. What the owner is for is accountability: a job with nobody behind it is a job nobody has promised anything about, and that is a state worth being able to reach deliberately.

### Doing the work

A handoff is what ends a stretch of work, and it is written when the work stops — not between rounds. Until then, work.

1. **Locate.** Your context is two facts: the workspace root and a job id. `git -C .bare worktree list` shows which label has `job/<job-id>` checked out, and that is where you work. No label may have it — the job may be unstarted, already closed, or its work may have moved to another site — and `handoff/<job-id>/` is what tells you which, because it is empty only in the first case.
2. **Read.** The brief at `doc/task/<job-id>/brief.md`, then the last file in `handoff/<job-id>/`; its `next` is the baton whoever stopped last left you.
3. **Take what you need.** If the work needs a unit of a declared resource, take its lease before starting rather than once you are stuck.
4. **Work.** Stay inside `owned_paths`, commit to `job/<job-id>`, and keep everything that is not source outside the worktree.
5. **Commit, then hand off.** `head` must point at an existing commit, so commit first, then write `handoff/<job-id>/<started-utc>.md`, and commit that as one coordination event.
6. **Leave the baton.** `next` says what whoever continues does first. Release whatever you took.

### Where your tools write

Everything a job writes stays inside the workspace, because anything outside it is invisible: not reviewed, not collected, and gone when the machine is rebuilt. That includes the scratch of the agent process itself, not only the tools it runs.

| Instead of | Write to |
| --- | --- |
| the tool's default temp directory (`/tmp`, `$TMPDIR`) | `job/<job-id>/tmp/` |
| a cache under `$HOME` | `var/cache/`, or `job/<job-id>/cache/` when only this job can reuse it |
| a dependency environment inside the working directory | `var/env/<label>/` |
| a log beside whatever produced it | `job/<job-id>/log/` |

The working directory holds source and the documents that change with it, and nothing else. That is not tidiness: it is what lets a label be handed to the next job without inheriting someone else's mess.

**How.** Point the tools there before running them. `TMPDIR`, `XDG_CACHE_HOME` and `XDG_STATE_HOME` cover most of it, and a tool with its own variable has one — `PIP_CACHE_DIR`, `npm_config_cache`, `GOCACHE`, `CARGO_TARGET_DIR`, `PYTHONPYCACHEPREFIX`, `GRADLE_USER_HOME` and so on. The base variables belong to whoever starts the work — a shell, a scheduler, the person at the keyboard — and the tooling can print them for a job; the tool-specific ones are set when the tool is invoked. A job that runs a tool without redirecting it has written outside the workspace, and the next actor cannot see what it left behind.

### Closing a job

**A job closes once.** Closing a brief that is already `done` or `abandoned` is refused, and nothing is written before the refusal is known — a refused operation that has already changed a record has changed a record, which is exactly what a workspace must not do.

Close when the acceptance criteria hold, or as `abandoned` when they will not. One commit, and it does three things: the brief's `status` is set to `done` or `abandoned`, the brief moves to `doc/archive/task/<job-id>/`, and the payload becomes collectible — `tmp/`, `cache/` and `ckpt/` at once, the rest once the retention window has passed. The move is what keeps `doc/task/` a list of work in flight rather than a list of everything ever done, and it is the only reason `doc/archive/` is mentioned when a job id's uniqueness is checked.

Only the brief moves. `handoff/<job-id>/` stays where it has always been, because an append-only log that never changes its address is worth more than a tidy archive.

Closing also releases the working directory and any lease the job still holds.

Closing merges nothing. The branch `job/<job-id>` stays in the code repository, and merging it is the project's business: a closed job is a hand-off to whoever integrates, not an integration itself.

Before closing, look for something to promote — work that is no longer specific to this job belongs in `doc/topic/`.

### Moving a job to another site

A job belongs to the workspace, not to the machine it happens to be running on. Moving one is three acts, and only the last is this document's business:

1. **Push the records.** The brief and every handoff are in the coordination repository, so they travel the way everything else there does.
2. **Push the branch.** `job/<job-id>` lives in the code repository, and the receiving site has to be able to fetch it.
3. **Carry the payload.** Nothing else moves it, so it travels as a bundle — the one part of a job that git cannot carry.

A move does not by itself change who owns the job. The receiving actor may work it under the owner's authority, or the owner may give it up first and let the receiver claim it; both are ordinary, and neither is implied by the move.

**Export** runs after a handoff and never during work: the bundle says which handoff it belongs to, and the receiver's first act is to check that it already has it. Export releases nothing, because stopping already released whatever the job held — a job that has left this site holds no lease here. The bundle is written outside the workspace, and never inside `job/<job-id>/` itself.

**Import** refuses unless the records are already local: the named handoff must be present, and the commit it names must resolve in the code repository. That single check replaces every merge rule the workspace would otherwise need. There is nothing to append and nothing to diverge, because the versioned half arrives through git and only the payload arrives in the bundle.

If `job/<job-id>/` is already present, import refuses and says so. Only an explicit instruction to replace it proceeds, discarding what is there and reporting every path it discarded: a payload is disposable by construction, so nothing that is lost was not disposable, but nothing is removed silently either.

Import commits nothing, because it writes nothing versioned. The payload's presence is anchored by the job itself, which is a versioned record.

### One line, published or withdrawn

The records are one line, and that is a property of how an event is recorded rather than a rule everybody is asked to keep. It has three parts.

**A site brings itself up to date before it writes.** Every operation that records something begins by taking what the others have published, so what it reads — which names are taken, what a job's status is, who owns it — is what is already true. A site that is alone in the world skips this, and a workspace with no remote cannot fall behind anything.

**An event is published or withdrawn, never kept.** If the line moved while the event was being written, the event's commit comes back out and is made again on top of what moved: it was never in the line, so nothing anybody has seen is rewritten. If the same file moved as well, there is no way to put both in one line, and the event is refused: nothing is published, nothing is merged, and what it wrote is still in the working tree for the actor to look at and record again.

**Records made while a site is alone are published when it is not.** They are put on top of what was published meanwhile, by the same rule: they were never in the line. If they cannot go together with it, nothing is published and nothing is merged — a record is somebody's work, and the one that cannot go in is worth more than the tidiness of resolving it — and the site says so, including what to do about it: the record is taken back out of the working copy, what it wrote stays in the working tree, and the event is recorded again.

**On one machine, one writer at a time.** Two actors recording at once is a race the tooling settles rather than reports: the second waits for the first, and an event records exactly the paths it changed rather than whatever happens to be in the working tree when it commits. A change that arrives while an event is being recorded therefore belongs to the next event rather than to this one, which is what keeps one commit one event when two hands are at work on one machine.

What this buys is one line with no merges and nothing rewritten that has been seen, and it is also what makes *a name is used once* hold between sites rather than only within one: a job id, an asset version or a convention number taken at another site while this event was being written is a file that moved, so the event is refused and taken back out. What it costs is a round trip per recorded event at a site that has a remote, and it is why a workspace whose sites are never connected is still a workspace with one line — its own.

### Joining a site

A site is added in two places, and the order matters: the workspace has to know the site exists before the site knows the workspace does.

1. **Declare it, from a site that is already in the workspace.** `doc/resource/site.yaml` is what every site reads to decide who exists, so the declaration comes first: one commit, and one push. **An empty roster declares nobody**, so a workspace whose roster has never named a site is one no second site can join until somebody names the first — the same rule, read strictly rather than leniently.
2. **Join from the new machine, pointing at the coordination repository.** Joining fetches the shared line, checks out, and fills in what a repository cannot carry — the ignore rules, the empty directories, the entry point and the skill an actor is handed, the site's own name, and its own copies of the code and asset repositories. **It commits nothing**: joining does not begin a history, because a second first commit would be a second workspace, with its own idea of which names are taken and which jobs are done, and the two would never agree because neither is wrong. A site the roster does not declare is refused here rather than half-joined.
3. **Enter the reach, on both sides.** How to reach a machine is that machine's own business, so each side writes its own `var/config/<peer>.yaml`; a pair that has to share a claim or a lease has to be able to reach each other, and nothing relays.
4. **Exchange keys.** SSH is the whole authentication model: the new site's public key goes into each peer's `authorized_keys`, and theirs into its own.
5. **Let it accept peers, if it should.** A site that accepts none runs no daemon and declares no reach, and everything that needs only its own machine still works.

**A full mesh is not required.** Each site enters the reach it needs and nothing more, so a workspace can be a partial graph, a star, or a pair. What is required is mutual reachability between the sites that must share exclusion — because a claim or a lease is taken where the thing is, and a site that cannot be reached is refused rather than proxied. That refusal is the same answer this document gives everywhere else, and it is why nothing has to route.

### What you decide alone

Everything inside the brief: how to implement it, what to verify, when to hand off, what `next` says, and closing the job once its acceptance criteria hold. You also decide alone to take and release leases, to declare a resource, to declare or retire a topic, to publish or withdraw an asset, to claim a job that has no owner, to give up a job you own, and to send another actor a message.

### When you are unsure

**You do not know whether someone else is touching a file.** `owned_paths` is a declaration, and you can read everyone's. You may touch a path outside your own when no other live brief claims it — live meaning the brief is still in `doc/task/`, since closing both sets the status and moves it out — and you say so in your handoff. When one does claim it, that is not yours to decide: ask that job's owner — which is a message — and if the owner is an agent, stop and record `blocked` with `blocked_by` naming it. If that job has no owner, there is nobody to ask — you may claim it and decide, which is precisely what an unowned job is for.

**Reality does not match the resource registries.** They and the filesystem are two authorities, so drift is expected rather than exceptional.

- A directory with no entry in `doc/resource/`: **report it, do not repair it.** The entry may be missing because someone is declaring it right now, and a wrong repair costs more than a stale line. The reverse is not drift at all — a declared entry with no directory is simply not materialised here.
- An expired lease is free, and taking it overwrites the record.
- A working directory that is dirty or still on a branch was released badly. Clean it before working in it: an idle directory is clean and detached.

**You want to change something that is not yours to change.** Two decisions belong to the project rather than to a job, and this document deliberately gives them no owner: removing a resource that is in use, and retiring a convention. The first is refused mechanically while the resource is occupied, which is a better guard than a permission. The second is an ordinary retirement whose discipline is the notice and the reason, not an approval — a project that wants approval says so in a convention, which is what conventions are for.

Abandoning a job is not in that category: it is the owner's decision, because the owner is who the work was promised to. An actor that wants to abandon a job it does not own asks the owner, and when the owner is an agent, ends the stretch as `blocked` with `blocked_by` naming the job. An unowned job is the exception: nobody has been promised anything, so whoever has claimed it may abandon it.

**You cannot proceed at all.** Stop and record it instead of spinning: `blocked` with `blocked_by` costs one handoff, while a spinning actor costs a context window and tells whoever continues nothing.

**The tooling itself is wrong.** A command that fails, a refusal that contradicts the records, or a file the tooling cannot read is a defect rather than something the actor did — and the workspace has no issue tracker to put it in. Keep the evidence instead of working around it in silence: what was run, what came back, and whatever it left behind, under `job/<job-id>/` and named in the handoff by `artifacts`, so the raw hash outlives the file. It is not a blocked stretch — `blocked_by` names jobs, and a defect is not one — so the status is the one the work actually reached, and what should change is written among the handoff's open questions. A workaround nobody wrote down is a workaround nobody can remove. The format and its tooling are young, and this is the one place a report of that kind has a home.

### Outside the workflow

Merging branches, starting and stopping services, running tests, running an actor's harness, and deciding which actor takes which job. The first four are the project's business and the last belongs to whoever dispatches work; the workspace never assigns a job to an actor.

Conventions and topics change on a slower loop than jobs do, and an actor edits them like any other document. Resource declarations change on the same slow loop but not by the same hand: only the tooling writes them.

## Placement

Artifacts are placed by a decision procedure, not by a directory list: a list is always overtaken by the next kind of data, a procedure is not.

```text
1. Source code, dependency locks, or documents that must change atomically with code?
       → the code repository (next to the code, or the project's own doc/ inside a worktree)
2. A record of what a job did?
       → handoff/  (metrics in front-matter; indexes are derived, never maintained)
3. A secret?
       → never in doc/, handoff/, or any git repository
4. Must be deletable object by object?
       → keep a reference here; the bytes live in an external system
5. Regenerable at acceptable cost?
       shared across jobs     → var/cache/
       private to one job     → job/<job-id>/cache/
       per worktree           → var/env/<label>/
6. Not regenerable?
       text a person reads in place  → doc/
       binary, or too large to read  → store/, with a manifest in doc/asset/
7. State of a declared resource — capacity in use, exclusion, or another runtime fact?
       → var/
8. Larger or externally governed?
       → outside the workspace, with an immutable locator in doc/asset/
```

Everything above is also a prohibition: nothing under `worktree/` holds anything that is not source, or a document that changes with the source. Keeping that true is what lets a label be handed from one job to the next. The stronger form of the same rule covers everything a job runs: nothing it writes lands outside the workspace at all.

### Assets

`store/` is a **bare git repository**, for the same reason `.bare/` is: git already provides content addressing, immutability, deduplication, delta compression between similar versions, integrity checking, transport, and backup. A hand-rolled object directory would reimplement all of that and lose the delta compression that makes comparing a dataset against a future version cheap.

Assets are never checked out permanently. A job materializes the version it needs into its own transient directory:

```bash
git -C store archive <ref> | tar -x -C job/<job-id>/data
```

Publishing a version is a tag, which makes it atomic: the blobs are written first, and the tag is what makes the dataset visible. A tag is never moved and never deleted except by withdrawal: it is the version's identity, and moving one would silently repoint every manifest and every handoff that names it. Each asset is described by a small manifest in `doc/asset/<asset-name>/<version>.md`, whose body is defined with the format further down:

```yaml
---
asset: uploader-baseline
version: 2026-10-09
location: store
ref: uploader-baseline@2026-10-09
sha256: 3f9c1d8b2a4e6f0c8a2e4b6d8f0a2c4e6b8d0f2a4c6e8b0d2f4a6c8e0b2d4f6a
bytes: 10737418240
produced_by: { job: add-retry-to-uploader, commit: 9be77d05 }
---
```

Record the raw `sha256` as well as the ref: a git object id hashes `blob <len>\0<content>`, so it is not the hash of the file itself, and anything crossing the workspace boundary speaks in raw checksums.

### Boundary

> The workspace is a coordination plane, not a data lake.

Container images, model weights, terabyte-scale corpora, externally governed or licensed data, anything that must be deleted object by object, and secrets stay in dedicated external systems: a registry, an object store, a secret manager. The workspace keeps only an immutable locator for them, in the same manifest. "Everything unversioned must be anchored by something versioned" extends to: everything outside the workspace must be anchored by a locator inside it.

### Loss model

| Layer | Regenerable | History in git | Backup |
| --- | --- | --- | --- |
| `.bare/` | No | Yes | A mirror outside the workspace |
| `doc/`, `handoff/` | No | Yes | Pushed to a remote |
| `store/` | **No** | **Yes** | **Pushed to a remote — the layer that most needs it** |
| `var/`, `job/`, `worktree/` | Yes | No | None needed |

The ignore rules (`.gitignore` and `.git/info/exclude`) are not in the table, because they are in no repository at all: they are reproduced from the template when a workspace is created, so recovering them means recovering the template rather than restoring a backup.

## Document classification

### Coordination plane

| Location | Contents |
| --- | --- |
| `workspace/doc/convention/` | Rules every agent must read, in order |
| `workspace/doc/task/<job-id>/` | The brief for one job: goal, scope, acceptance criteria, owned paths |
| `workspace/doc/topic/<topic>/` | Reference knowledge shared across jobs |
| `workspace/doc/asset/<asset-name>/` | Manifests anchoring stored and external data |
| `workspace/doc/template/` | Copy-me templates for briefs, handoffs, and manifests |
| `workspace/doc/archive/` | Retired documents, each with a notice, and closed jobs, which carry none |
| `workspace/handoff/<job-id>/` | Append-only records of work done and what remains |
| `workspace/doc/resource/` | Declared resources, one file per kind: `worktree` here, other kinds as a project adds them |

### Asset plane

| Location | Contents |
| --- | --- |
| `workspace/store/` | Durable data, content-addressed and published as tags; never checked out |

### Runtime plane

| Location | Contents |
| --- | --- |
| `workspace/var/lease/` | One file per held unit of a declared resource |
| `workspace/var/occupy/` | One file per job somebody says they are on, kept at the site that holds the job; stale once the job has left `doc/task/` or this site no longer holds it |
| `workspace/var/state/<service>/` | Data of a shared stateful dependency |
| `workspace/var/env/<label>/` | Per-worktree dependency environments |
| `workspace/var/cache/` | Workspace-wide regenerable caches |
| `workspace/var/config/` | The site-local half of resource declarations; never committed |
| `workspace/var/site` | Which site this machine is, by name; never committed |
| `workspace/var/secrets/` | Machine-local credentials; prefer keeping them outside the workspace |
| `workspace/job/<job-id>/` | Transient payload: logs, scratch, per-job cache, data, checkpoints, artifacts |
| `workspace/worktree/<label>/` | A labelled working directory for an area of work; durable unless the registry marks it one-off |

Documents that change with the source — a component design, an API reference, a plan for one module — are in none of these planes. They live in the code repository, next to the code, and are versioned by it.

- Keep the `doc/` hierarchy organized by task and topic. The shape of the tree is the index; listings are read with `ls`, never maintained by hand.
- A file earns its place only when it carries a decision. Content that is identical in every workspace is a template, not a document; content that changes with the shape of the tree is derived and is never written by hand.
- Start an agent by giving it two facts: the workspace root and a job id. Everything else it derives — the working directory from the code repository's `git worktree list`, the rules from `doc/convention/`. Do not rely on a file being auto-loaded: the entry point at the workspace root and the code repository's `AGENTS.md` are two different files, and it is the second that a worktree holds.
- Retired documents move to `doc/archive/` rather than being deleted: a retired specification is often the only record of a decision.
- Files under `job/` are never versioned, and anything that must outlive a job belongs in `store/`.
- A worktree label names an area of work, never the job occupying it, and is created and removed through the tooling, by an actor. `doc/resource/worktree.yaml` records what each label is for and whether it is one-off; the current occupant is never recorded there, because the code repository's `git worktree list` already reports it — the checked-out branch is `job/<job-id>`, and a worktree at rest holds no job branch.
- A job may hold several worktrees, but only one of them can carry the job's branch: git refuses to check out the same branch twice. The others are detached checkouts, used for comparison or reproduction.

## Naming

Names are identifiers, not descriptions. They are typed by people as often as by tools, so they stay short and shell-safe; they are parsed by tools, so they follow one grammar; and they never encode state that can change, because a name that has been written into a record can never change again.

### Atoms

```text
slug  := [a-z0-9]+ ( "-" [a-z0-9]+ )*
stamp := YYYY "-" MM "-" DD "T" HH "-" MM "-" SS "Z"    # UTC, colons replaced by hyphens
```

### Names

| Name | Form | Example |
| --- | --- | --- |
| Job id | `slug` | `add-retry-to-uploader` |
| Branch (code repository) | `job/` + job id | `job/add-retry-to-uploader` |
| Worktree label | `slug`, chosen for the area of work | `uploader` |
| Asset | `slug` | `uploader-baseline` |
| Asset version | `YYYY-MM-DD`, then `-N` for a further version on the same day | `2026-10-09`, `2026-10-09-2` |
| Asset tag | asset `@` version | `uploader-baseline@2026-10-09` |
| Handoff file | `stamp` + `.md` | `2026-10-09T02-10-00Z.md` |
| Convention file | two digits + `-` + `slug` + `.md` | `01-placement.md` |
| Topic | `slug` | `upload-protocol` |
| Topic document | `slug` + `.md` | `retry.md` |
| Actor | `slug`, one name for as long as that actor works | `claude-opus` |

### Invariants

1. **One character set**: lowercase ASCII letters, digits, and hyphens. No spaces, no uppercase, no underscores, and no dots outside file extensions.
2. **Names are short and shell-safe**, because people type them.
3. **Job ids, asset versions, and convention numbers are taken forever — within the workspace.** Once used, none of them is reused, even after the job closes, the asset is withdrawn, or the convention is retired; a job id's uniqueness is checked against both `doc/task/` and `doc/archive/`. Two things sit deliberately outside this rule: a worktree label, which is reused by design, so its meaning must stay fixed and it is never repurposed for a different area of work; and a topic name, because topics are a knowledge tree rather than a register.
4. **Names are reversible**: `job/` + job id ↔ job id; asset tag ↔ manifest path; handoff file name ↔ `started`; worktree label ↔ directory name.
5. **Names never describe state.** A job id carries no date, owner, or status; a worktree label carries no occupant.
6. **`@` and `/` are reserved separators** and never appear inside a slug.
7. **Every name is at most 64 characters**, so it is safe as a single path component.
8. **Time has two forms**: file contents use RFC 3339 (`2026-10-09T02:10:00Z`) and names use the hyphen form (`2026-10-09T02-10-00Z`). They convert mechanically.

### What the rules make checkable

The naming rules exist so that the formats can be checked by machine, and the checks themselves are collected in one list in `File formats` rather than repeated here. What this chapter contributes is the reason they work: a name is reversible, so a record can be checked against the path it lives at, and it never encodes state, so a check on it cannot go stale.

### Out of scope

Branch names owned by the project itself (`main`, release branches), the name of the workspace directory, and the name of a bundle, which is a transport artifact rather than a workspace name.

## File formats

Every structured file in a workspace is either YAML front-matter plus a Markdown body, or — when there is nothing to say in prose — pure YAML with a comment header: the front-matter is what tools and agents read, the body is what people read. The formats are defined here so that an agent can write a well-formed file without inspecting an example.

### Common rules

- Front-matter is delimited by `---` and must be valid YAML; the body is Markdown. A pure-YAML file has neither: its top level is the record itself.
- Every document that can be retired has exactly one H1 title, on its first line after any front-matter. That is what gives a retirement notice a defined position. Conventions, topics and manifests can be retired and therefore have one; a brief and a handoff cannot, and their identity lives in their front-matter instead.
- Field names are lower snake case, and every `status` is a closed enumeration of lower-case values.
- A field that names a path says which root it is relative to. Paths are never absolute.
- No secrets in any versioned file: `doc/`, `handoff/`, and the code repository are permanent.
- Git already records who committed and when, so neither is duplicated — with two exceptions: where the meaning of a record depends on the time, as in a handoff's start and end or a retirement notice; and where it depends on which actor rather than which committer, as with a handoff's `agent`.
- One record per file. Nothing in the coordination plane is a file that every agent writes — which is why a resource registry, the one file that holds several entries, is written only by the tooling, and that tooling serialises its own changes.

### Validation

Everything mechanical that a record has to satisfy, in one list: a checker has one place to read, and a writer has one place to look.

- a brief's `job` equals its directory name, its `status` is in its enumeration, and its `owner`, when present, is an actor name;
- a handoff's `branch` equals `job/` plus its directory name, and its `started` equals its file name converted;
- a handoff's `status` is in its enumeration, `next` is `none` exactly when that status is `done`, and `blocked_by` is present exactly when it is `blocked`;
- a handoff's `base` and `head` resolve in the code repository, and its `supersedes` names an existing sibling without forming a cycle;
- every `artifacts[]` path lies under `job/<job-id>/`, and every `artifacts[].sha256` is 64 hexadecimal digits;
- a handoff's body holds its four sections, and a brief's its three;
- a lease's `resource` names an entry in `doc/resource/`, and its `unit` is no greater than that entry's `capacity`;
- a handoff's `agent` is an actor name;
- a manifest's `asset` and `version` equal its directory and file names, and its tag equals `asset` + `@` + `version`;
- a bundle's `handoff` names a file that exists;
- every directory under `worktree/` has an entry in `doc/resource/worktree.yaml` — but not the reverse, since a declared entry with no directory is simply not materialised here;
- resource names are unique across every kind;
- no two convention files share a number;
- every document that can be retired has exactly one H1 title.

Actor names are checked for form only: whether an actor exists is the tooling's knowledge, not a file's.

### Retirement notice

A retired document moves to `doc/archive/`, keeping its relative path, and gains a notice immediately after its title. The notice answers the only question a reader has when arriving from a stale link: is this still true, and where should I look instead.

```markdown
# Retry policy

> Retired 2026-10-09: the retry policy moved into the uploader itself.
> Superseded by `topic/uploader/retry.md`.
```

When nothing replaces it, say so rather than leaving the line out:

```markdown
# Batch mode

> Retired 2026-10-09: batch mode was removed from the product.
> No replacement.
```

`Retired <date>:` is a fixed prefix and the second line has two fixed forms, so `grep -l '^> Retired'` lists every document that carries a notice, with a tool everyone already has. A retired topic is found through its `overview.md`, the only one of its files that carries one. The date uses the same calendar form as an asset version, because a retirement is a person's decision and does not need a time of day.

- **A notice, not front-matter.** Recording the same facts in front-matter as well would create a second version of the truth that can drift; front-matter alone would not be readable in place.
- **The replacement is a `doc/`-relative path in backticks, not a link.** The document has already moved, so a relative link written before the move is fragile, while a root-relative path is independent of position.
- **The date is recorded, the person is not.** The date says how stale the reasoning is; git already knows who, and the useful answer to "who do I ask" is the replacement path.
- **Reviving a document moves it back and removes the notice.** The notice states the document's current status, not a permanent scar; the fact that it was once retired is in git history.
- **Retirement is not closure.** The brief of a finished job is the record of finished work, not a document that lost its authority; its `status` already says so, and it carries no notice.
- **Numbers are never reused, so `convention/` may have gaps.** A retired `01-placement.md` moves to `doc/archive/convention/01-placement.md` and its number stays taken, because reusing it would break every reference. A new convention therefore takes the highest number in use plus one, never the first free one, because filling a gap would reuse a number that a record may already name. A gap means "there was one, and it is archived".

### Topics

A topic is durable knowledge about the subject matter: how something works, why it is the way it is, and what has to keep being true. It is branch-independent — it lives in the coordination plane, so it is always current — and it outlives the components it describes.

A topic is not:

| Not a topic | Where it belongs |
| --- | --- |
| An API reference or a component design document | Next to the code, in the worktree, so that the change and the document are reviewed together |
| A rule or an obligation | `convention/` — a convention instructs, a topic describes |
| A plan or a status | `task/<job-id>/brief.md` |
| An investigation log | `handoff/` — a topic is what survives the investigation, written deliberately afterwards |
| A restatement of the code's comments | Nowhere; delete it |

The boundary with code and API documentation is not the subject, because a topic may well discuss an API. It is **what makes the document wrong**. A signature list goes stale the moment a signature changes, so it belongs with the code; an explanation of the contract that the signature has to satisfy stays true while the implementation is rewritten, so it is a topic. This is the placement rule applied to the workspace rather than to one file: *would this document become wrong if a single component were rewritten?* A topic must stay true across versions, which is exactly why it has no home on any branch.

```text
doc/topic/<topic>/
    overview.md          # required: what this topic covers
    <name>.md            # further documents, named freely and never numbered
    <subdir>/            # optional, to organise this topic's own documents
```

- **The first component is the topic; everything below it is that topic's own organisation.** Topics do not nest: in `topic/uploader/retry/backoff.md` the topic is `uploader`, and `retry/` is a grouping inside it.
- **`overview.md` is required.** It says what the topic covers, and it does not enumerate its own files; `ls` does that.
- **Documents inside a topic are never numbered.** Numbering is for conventions, where the order carries priority.
- **Promotion is deliberate.** Knowledge becomes a topic when it stops being specific to one job.
- **Retiring a whole topic** moves the directory to `doc/archive/topic/<topic>/` and adds a notice to `overview.md` alone; the rest are retired by association, so one retirement gives one grep hit rather than one per file.

### Convention — `doc/convention/<nn>-<slug>.md`

A convention is a rule an agent must read. A topic describes; a convention instructs. They
are read in number order, so the order is the priority, and the naming rules are what keep a
retired number from being used again.

A convention carries no front-matter. It states a rule rather than a record of state, and
the one thing it has to say about itself — its number — is in its name. It has exactly one
H1 title, so that it can be retired like any other document, and the retirement notice goes
under that title.

The body is the rule, in the imperative, with the reason it exists: a rule whose reason is
missing is a rule that will be deleted by accident.

**A convention is where a project's policy reaches the tooling.** Some rules need a value a
tool has to act on — how long a job's payload is kept, for instance — and the value belongs
with the rule that uses it rather than in a configuration file of its own, because everyone
in the workspace should see the same one and a change to it should be auditable. So a
convention may carry one fenced block, and its keys name the tool and the value:

```values
collect.window: 14d
```

- **The fence opens at the left margin.** An indented fence is what a fence inside a list
  item looks like, which is how an example of one gets written — and an example must not
  declare anything.
- **The keys are the tooling's**, so they are dotted: `collect.window`, not `window`. A key
  no tool reads is a key for the reader, and is allowed: a project may record a value it
  intends a later version to read.
- **Values are scalars.** A mapping or a list in a values block is a document in the wrong
  place.
- **One block per convention, and one convention per key.** Two conventions declaring one
  key would have the workspace saying two things at once, and which of them a tool acted on
  would depend on the order the files were read in.
- **A value the tooling reads but cannot parse is an error rather than a default.** A policy
  nobody can read is a policy nobody has, and falling back quietly would hide the mistake
  that made it unreadable.

Validation checks all of it, along with the file name and the one title.

**A workspace starts with no conventions at all.** `doc/convention/` exists and is empty,
and everything in it is the project's: a rule that belongs to this project, or a value a tool
has to read. A new convention takes the highest number in use plus one, and the tooling never
writes or rewrites one.

**What an agent is told is not this.** The rules here are the workspace's, and an agent
reaches them through the workspace itself — through `doc/convention/`, through the brief,
through the entry point and the skill at the workspace root, and through the refusals it
gets. How a particular agent or team works is an agreement between that agent and whoever
runs it, and where it is written down is their business — the code repository may ship an
`AGENTS.md` of its own, and it is not this workspace's.

### Brief — `doc/task/<job-id>/brief.md`

The brief states the intent of one job. It is a document like any other — editable, and committed whenever it changes — and its `status` is the one field in it that somebody has to decide rather than read.

```yaml
---
job: add-retry-to-uploader
status: active                   # active | done | abandoned; blocked is derived
branch: job/add-retry-to-uploader
owned_paths: ["src/upload/**", "tests/upload/**"]
owner: claude-opus               # optional; absent means unowned
depends_on: [fix-ci-timeouts]    # optional
keep: false                      # optional; true protects this job's payload from collection
---
```

| Field | Required | Meaning |
| --- | --- | --- |
| `job` | Always | Must equal the containing directory name |
| `status` | Always | The lifecycle of the job itself: `active` while it is still meant to happen, then `done` or `abandoned`. Being blocked is not stored — it is read from the latest handoff |
| `branch` | Always | The branch in the code repository that implements this job |
| `owned_paths` | Always | Paths this job touches exclusively, relative to the code repository root; may be empty for analysis-only jobs |
| `owner` | Optional | Who is responsible. Absent means nobody is — never assigned, or given up — and an unowned job may be claimed by whoever takes it up |
| `depends_on` | Optional | Job ids that must reach `done` before this job's work proceeds |
| `keep` | Optional, default `false` | When true, the job's payload under `job/` is never collected |

`owned_paths` is the cheapest conflict avoidance there is: a conflict prevented at allocation costs nothing, and one resolved at merge time costs a great deal. Work that touches anything outside those paths must say so in the handoff. The patterns are written for a reader rather than for a matcher — this document defines no wildcard dialect, because the check they support is a person reading another brief.

**Ownership is a decision, not liveness.** A job has one owner or none, and that changes only when somebody decides it should: the opener sets it, an owner may reassign it, and an owner may give it up. Giving it up is what makes a job claimable — the next actor to take it up writes itself in without asking, because there is nobody left to ask. When an owner has become unreachable rather than willing, a person gives the job up or reassigns it on their behalf, since a job owned by an actor who no longer exists would otherwise be stuck forever. The sequence of handoffs remains the record of every actor that has worked on it, which is why `owner` lives here and `agent` lives there.

**Whose brief it is.** The body belongs to whoever is on the job: only the actor currently working it changes `## Goal`, `## Scope`, `## Acceptance criteria`, or the sections after them, because that is the understanding that is current. Ownership is a separate matter — an owner may reassign or give up the `owner` field, and does not thereby gain the body. The remaining fields change with the acts this document already assigns: opening, claiming, giving up, and closing.

**Occupancy is not a brief field.** Who is on a job right now changes with every stretch, and the coordination repository is a shared, permanent line: putting that churn in it would make the line something everybody has to synchronise just to know who is busy. So the claim lives where it belongs — site-locally, at `var/occupy/<job-id>.yaml`, with an `occupant` and a `since`, written and cleared by the tooling and read through it — and the two questions get two answers. *Who is on this job right now* comes from the tooling, and it is one answer rather than several, because the claim is kept at **the site that holds the job**: the site whose working directory has `job/<job-id>` checked out. **What has a home here is not the job but the actor working it.** A job is a record, and records travel by git; the actor is at a machine, and the machine is where the job's working directory is. That is why the anchor is where the job is materialised rather than the actor's name: an actor is present somewhere, but two actors at two sites can each believe they are on one job, and a name cannot arbitrate between them. A working directory can, because there is only one of it, and only where it is can the work actually be done.

That is the same rule a resource gets, and for the same reason — one place answers, so nothing has to be agreed on — and it is why a claim is consistent across sites without any consensus.

A job no site holds cannot be claimed, because there is nothing to claim; two sites holding one job is refused rather than picked between, exactly as two homes for a resource are; and a peer that does not answer means *cannot be established*, never *nobody is on it*. It remains a claim rather than a lock: nothing on disk can say whether the actor is still working, and this document does not pretend otherwise. *Who has held it, and since when* comes from the record: the file for the current stretch, and the handoffs for every finished one, since each of those already names its `agent` and its start and end.

That also makes the site-local file a note rather than a lock. A name stays behind after the actor has stopped, and nothing on disk can tell an actor who is working from one who is not. Taking a job over is therefore a decision rather than a field edit: an actor that finds another name in `occupant` asks the tooling, or the owner — it does not overwrite the file and call the job its own.

`depends_on` is checked before work starts rather than after it fails. A dependency is satisfied only when its brief says `done`, and that brief may be in `doc/task/` or in `doc/archive/`, since finishing moves it. An `abandoned` dependency is not satisfied: the premise it stood for is gone, and only the owner — or whoever claims the job, if it has none — can decide what that means. An actor that finds a dependency unmet does not wait for it — it ends the stretch as `blocked` with `blocked_by` naming the job. Telling it again when that job finishes is the tooling's business, not the workspace's: the tooling sends a message, and the actor it is addressed to reads it when it next looks.

The brief also decides the fate of the job's payload: the payload becomes collectible once `status` is `done` or `abandoned`, unless `keep` is true. That decision lives here rather than in `job/` because a marker stored inside the payload cannot protect the payload.

The body has three sections, in this order, with additional sections allowed after them:

```markdown
## Goal
## Scope
## Acceptance criteria
```

All three must be present, and `None.` is a legal body — the same rule the handoff uses, for the same reason: a section that can be quietly left out is a section nobody can rely on.

A brief is edited as understanding changes, and every edit is committed, so the history shows when the scope moved. This is the opposite of a handoff, which is immutable once read. Its `status` is the one field in the coordination plane that a person decides rather than reads, and it changes only when the job ends — every other state a job can be in is read from something else. A plan, when one is needed, is one of the additional sections rather than a separate file.

Three of the states a job can be in are not stored at all. **Started or not** is a fact, read from `handoff/<job-id>/`, which stays empty until the first handoff records something — which is why there is no `open`. **Blocked or not** is a fact too: a job is blocked exactly while its latest handoff is `blocked`, so a job that stops being blocked stops being blocked the moment a later handoff says so, and no field has to be remembered and reset. What the brief stores is what somebody has to decide: `active` while the job is still meant to happen, and `done` or `abandoned` once it will not be worked any further. A brief that is still in `doc/task/` is therefore always `active`, and that is the whole of what *live* means elsewhere in this document.

The brief's statuses are still not the handoff's: a job is `abandoned` when it will not be done at all, while a handoff is `aborted` when one stretch produced nothing usable.

### Handoff — `handoff/<job-id>/<started-utc>.md`

A handoff covers one stretch of work. It lives at `handoff/<job-id>/<started-utc>.md`, where the timestamp is when that work started, in UTC with `:` replaced by `-`. The directory already carries the job id, so the file name only has to identify the stretch — and it uses the start time rather than the time of writing, because it labels the work, not the write.

One file per handoff, carrying machine-readable front-matter and a human-readable body. There is no `.md`/`.json` pair; two files drift apart.

One job is worked by one actor at a time, and keeping that true is the tooling's business: nothing on disk can enforce it, because nothing on disk can tell a working actor from a dead one. The site-local record at `var/occupy/<job-id>.yaml` states the claim rather than enforcing it: whoever continues reads the previous handoff before starting, and the tooling records who is on the job. What the files give is narrower but real — a job owns one branch, and that branch has at most one carrying worktree per site, which is what makes the code repository's `git worktree list` a reliable answer to where the work is. And if the target file name already exists, the writer fails rather than overwrites: never overwriting is the minimum requirement of an append-only record.

#### Front-matter

| Field | Required | Value | Meaning |
| --- | --- | --- | --- |
| `job` | Always | string | Must equal the containing directory name |
| `agent` | Always | string | The actor that wrote it |
| `branch` | Always | string | Branch in the code repository |
| `base` | Always | commit id | The commit the work started from |
| `head` | Always | commit id | The commit at handoff time; equal to `base` if nothing was committed |
| `status` | Always | `done` \| `partial` \| `blocked` \| `aborted` | See below |
| `started` | Always | RFC 3339 UTC | Must match the file name |
| `ended` | Always | RFC 3339 UTC | Time of handoff |
| `next` | Always | string or `none` | The baton; `none` exactly when `status` is `done` |
| `worktrees` | Optional | list of labels | The labelled working directories the work used |
| `supersedes` | When correcting | file name | A sibling record this one replaces |
| `blocked_by` | When `blocked` | list of job ids | The jobs it is waiting on |
| `owned_paths` | Optional | list, relative to the code repository root | Paths actually touched, for comparison against the brief |
| `metrics` | Optional | map of numbers | Counts, coverage, durations; the source of performance history |
| `artifacts` | Optional | list of `{path, sha256, bytes}` | Paths under `job/<job-id>/`, relative to the workspace root; `sha256` is the raw file hash |
| `assets` | Optional | list of `name@version` | Assets published to `store/` by this work |

`status` means exactly this:

- **`done`** — the brief's acceptance criteria are met and nothing remains.
- **`partial`** — progress was made and work remains, with no external blocker.
- **`blocked`** — something outside this job's control is missing; `blocked_by` says what.
- **`aborted`** — no usable progress: a crash, a wrong approach, a superseded plan.

#### Body

Four sections, with fixed names and order, so that any handoff can be parsed blind. Additional sections may follow them.

```markdown
## What changed
## What was verified
## What remains
## Open questions
```

All four must be present, and `None.` is a legal body. The last two matter most: "verified" separates *I changed it* from *I proved it works*, and "open questions" is the most useful thing to hand over and the easiest to drop.

#### Lifecycle

**Only a job that is still meant to happen is worked.** A handoff records a stretch of work, so one is refused for a brief whose `status` is not `active` — the same rule a lease already follows. A correction is the exception in form rather than in substance: it covers no work at all, and a correction to a record on a finished job is written by hand and committed like any other document.

**A stretch that is not finished leaves a baton.** `next` is `none` exactly when the status is `done`, and every other stretch says what whoever continues does first — a stretch that stops without saying so tells whoever continues nothing, which is the one thing a handoff is for. Both directions are refused when they are wrong, not only reported later.

1. **Write one whenever you stop**, and not before: stopping to hand over, stopping for the day, stopping because you are blocked, stopping because your context is nearly full, or stopping because the job is finished. Work that runs for many rounds without stopping needs nothing in between.
2. **Commit before handing off**, because `head` must point at an existing commit: commit first, then write the handoff.
3. **A gap is information.** When work stops without a handoff — a crash — whoever continues reads the working directory and the branch instead, and knows the previous stretch ended in the middle rather than on purpose.
4. **Never edit a handoff once it has been read downstream.** Corrections are new files carrying `supersedes`; chains are allowed, cycles are not. A correcting record may cover no work at all.
5. **Never delete a handoff.**
6. **Writing a handoff is one coordination event, and therefore one commit.** The last handoff may also close the job in the same commit, because finishing is a single event — and that commit takes the `close` kind, since the ending is the more consequential of the two.

#### Derived views

- **The latest handoff's status** is the front-matter of the last file in `handoff/<job-id>/`; the job's own status is its brief's. Together they are the task board, and no separate board is maintained: the brief says whether the job is still meant to happen, and the last handoff says whether the last stretch got anywhere or was blocked.
- **Performance history** is the `metrics` fields aggregated across handoffs, so no shared mutable table is needed either.
- **Artifact provenance** runs from `artifacts` into `job/`, and from `assets` into `store/`.

Artifacts under `job/` are subject to the retention policy: their `sha256` records what was lost but cannot recover it. Anything that must outlive the retention window is published to `store/` first and referenced under `assets`.

#### Example

````markdown
---
job: add-retry-to-uploader
agent: claude-opus
branch: job/add-retry-to-uploader
base: 4f2a1c9d8e3b7f01c2a4e6b8d0f2a4c6e8b0d2f4
head: 9be77d05a1c3e5f7b9d1e3f5a7c9b1d3e5f7a9c1
worktrees: [uploader]
status: partial
started: 2026-10-09T02-10-00Z
ended: 2026-10-09T03-05-12Z
supersedes: 2026-10-09T01-40-00Z.md
next: Fix the flaky timeout in tests/upload/test_retry.py, then run make test-upload.
owned_paths: ["src/upload/**", "tests/upload/**"]
metrics: { tests_passed: 128, coverage: 0.83, wall_seconds: 3312 }
artifacts:
  - path: job/add-retry-to-uploader/out/coverage.html
    sha256: 3f9c1d8b2a4e6f0c8a2e4b6d8f0a2c4e6b8d0f2a4c6e8b0d2f4a6c8e0b2d4f6a
    bytes: 184223
assets: [uploader-baseline@2026-10-09]
---

## What changed

Retry is bounded by `max_attempts`, and the backoff is jittered.

## What was verified

`make test-upload` passes 128 tests. The flaky case in `test_retry.py` reproduced
3 times in 20 runs and is not yet fixed.

## What remains

The flaky timeout; see `next`.

## Open questions

Whether the timeout belongs to the test or to the uploader.
````

#### Never in a handoff

- Secrets, credentials, or tokens: this file is versioned and permanent.
- Pasted logs: point at a file under `job/` with `artifacts` instead.
- A restatement of the brief: a handoff is evidence, not a copy of the intent.
- Plans beyond `next`: those belong in a new brief.
- The status of other jobs: that would recreate shared mutable state.
- Absolute or machine-local paths: paths are relative to the workspace root (`artifacts`) or to the code repository root (`owned_paths`).

### Asset manifest — `doc/asset/<asset-name>/<version>.md`

A manifest anchors one version of one dataset. It is the only part of an asset that is versioned: the bytes live in `store/` or outside the workspace, and the claim about them lives here.

```yaml
---
asset: uploader-baseline
version: 2026-10-09
location: store
ref: uploader-baseline@2026-10-09
sha256: 3f9c1d8b2a4e6f0c8a2e4b6d8f0a2c4e6b8d0f2a4c6e8b0d2f4a6c8e0b2d4f6a
bytes: 10737418240
produced_by: { job: add-retry-to-uploader, commit: 9be77d05 }
license: internal
---
```

| Field | Required | Meaning |
| --- | --- | --- |
| `asset` | Always | Must equal the containing directory name |
| `version` | Always | Must equal the file name without `.md` |
| `location` | Always | `store`, or an external locator such as `s3://…` or `registry+https://…` |
| `ref` | When `location` is `store` | The tag that publishes this version |
| `sha256` | When the content is one blob, or lives outside git | The raw hash of the content |
| `bytes` | Same as `sha256` | The size of the content |
| `produced_by` | Optional | The job and commit that produced it; absent for externally sourced data |
| `source` | Optional | Where external data came from |
| `license` | Optional | The terms that govern reuse |

The body has one required section, `## Description`, with additional sections allowed after it.

The identity fields — `location`, `ref`, and the checksum fields whenever they are present — freeze on publication. Correcting a descriptive field in place is allowed, but changing identity means a new version. Note that `sha256` is not the git object id: git hashes `blob <len>\0<content>`. Both are recorded, because anything crossing the workspace boundary speaks in raw checksums.

**What the checksums are computed over.** A version is a git object, which may be one blob or a whole tree; `ref` names it either way, and git verifies it either way. `sha256` and `bytes` exist for what leaves git's care — a single file, or a copy held somewhere else — so a version whose content is a directory simply leaves them out. Computing one over a tar stream would only be stable for as long as nothing about the tar ever changed, which is not a promise worth making.

**Withdrawing a version.** When a version turns out to be wrong, or must not be used, its manifest is retired like any other document, in one `retire` commit: it moves to `doc/archive/asset/<name>/<version>.md` with a notice, and the tag in `store/` is deleted, which makes the bytes collectible. The version number stays taken. Withdrawal is a retirement rather than a deletion, so a handoff that names the version keeps a resolvable reference and the reason is on record.

### Job payload — `job/<job-id>/`

The payload is everything a job produces on disk: its scratch space, its caches, its logs, its inputs, its checkpoints and its outputs. It is unversioned, scoped to one job, and — by construction — disposable.

```text
job/<job-id>/
    log/      # stdout and stderr worth keeping
    tmp/      # scratch, including redirected tool temp
    cache/    # regenerable; safe to delete at any time
    data/     # transient inputs only
    state/    # a private copy of a shared service's data, for destructive tests
    ckpt/     # resume state for a long run
    out/      # artifacts; not a delivery mechanism
```

Nothing here is irreplaceable. Whatever must survive is either reproducible or has been published to `store/` and referenced from a handoff, and that is what makes collection safe.

**Collection.** A payload becomes collectible once its brief's `status` is `done` or `abandoned`, unless the brief sets `keep: true`. Keying the rule on the brief's status rather than on the presence of a directory keeps it independent of where closed jobs are filed. The subdirectories differ in what they cost to lose:

| Subdirectory | Collected |
| --- | --- |
| `tmp/`, `cache/`, `ckpt/` | On close |
| `log/`, `data/`, `state/`, `out/` | After the retention window, measured from the close |

The window defaults to 14 days from the close, and `keep: true` is the only per-job override. There is no size limit by default. The value is policy rather than machine fact — everyone in the workspace should see the same one, and a change to it should be auditable — so a project that wants a different one states it where a tool can read it, in a convention beside the rule that uses it, rather than in a configuration file of its own. Collection never makes a record false: a handoff keeps the raw `sha256` of every artifact it names, so a collected artifact is a detectable absence rather than a silent one. That is also why `out/` is not a delivery mechanism — a result that has to survive is published to `store/` and referenced under `assets`.

### Resources

Anything the workspace shares that is not reachable by its own name — a working directory, a borrowed machine, a stateful dependency — is declared in `doc/resource/`, one file per kind. Those declarations are what lease names refer to: a name that resolves to no entry is a name with no referent.

```text
doc/resource/worktree.yaml    # the one kind this document specifies
doc/resource/host.yaml        # a kind a project adds
```

The field sets are fixed here rather than declared in the workspace: this chapter is the schema, exactly as it is for briefs, handoffs and manifests.

**Only `worktree` is specified**, because it is the only kind a single-machine workspace materialises. A kind is whatever a project needs it to be — a borrowed machine, a shared service, a site — declared as a file named after it, holding entries that carry the same core fields. A `site`, for instance, means the ordinary thing by `capacity`: how many jobs that host runs at once.

```yaml
# Working directories the project has declared, and what each is for.
worktree:
  - name: uploader
    purpose: Uploader reliability work
    ephemeral: false
  - name: spike-gc
    purpose: One-off look at GC pauses
    ephemeral: true
```

| Field | The tool reads it | Required | Meaning |
| --- | --- | --- | --- |
| `name` | yes | Always | Unique across every kind; the name leases use, and the directory under `worktree/` |
| `capacity` | yes | Only above 1 | How many holders at once; omitted means `1`, which means exclusive |
| `ephemeral` | yes | `worktree` entries | `true` when the working directory exists for a single use and may be destroyed on release without further checks |
| `purpose` | no | Always | What the resource is for, in a person's words |

Only the first three change what the workspace does; `purpose` is there for whoever reads the file. A kind a project adds may bring fields of its own, with the same understanding: **a field the tooling does not read is a field for the reader.** Elsewhere this document calls a worktree's `name` its label.

**This half must be publishable.** `doc/` is a git repository with remotes, so everything in it should be something you would be content to see published. Whatever a resource needs beyond that — how to reach it, which credential to use, who to contact, the ways it bites — belongs in its site-local half at `var/config/<name>.yaml`, which is never committed and never leaves the machine. A resource with nothing local has no such file, and a worktree is the example: that is why `worktree` is the only kind complete on its own.

**Which site holds a resource is the machine's own business, and it is read from the machines rather than declared.** A declaration travels by git, so every site has every one of them; what a machine actually has does not travel, and cannot be written into the declaration without publishing it. A site therefore says it holds a resource in one of two ways, both of them local: the resource is materialised there, or this site's own half at `var/config/<name>.yaml` says so — which is how a borrowed machine or a shared dependency, with nothing on disk, says where it is. That is the fact home-site leasing is settled from, and asking each site in parallel is what keeps that answer local too.

**The declaration is a recipe, not an inventory.** The code repository's `git worktree list` says what this site has materialised; the file says what the project intends to exist. A declared entry with no directory is therefore normal — not yet materialised here, or removed and expected back. A directory with no entry is the error, and the tooling reports it.

**Only the tooling writes these files.** Declaring or removing a resource goes through it, one change per commit, and in a multi-site workspace they are not edited by hand — which is what keeps two sites from colliding over a list they both append to. Removing a resource whose directory exists is refused rather than permitted: pulling a working directory out from under whatever is using it is not a thing a permission makes safe.

### Lease — `var/lease/<resource>.<unit>.yaml`

A lease records that one job holds one unit of a declared resource until a stated time. It is the only runtime record with a mechanism behind it; everything else under `var/` is an agreement about where files go.

```yaml
# var/lease/gpu.2.yaml
resource: gpu
unit: "2"
job: sweep-learning-rate
since: 2026-10-09T10:00:00Z
expires: 2026-10-09T12:00:00Z
```

| Field | Required | Meaning |
| --- | --- | --- |
| `resource` | Always | Must name an entry in `doc/resource/` |
| `unit` | Always | Which unit, from `1` to the resource's `capacity`; an exclusive resource has only unit `1` |
| `job` | Always | The job that holds it; a lease always belongs to a job |
| `since` | Always | When it was taken |
| `expires` | Always | When it becomes reclaimable |

One file per unit, named `<resource>.<unit>.yaml`. The dot separates the two because a resource name is a slug and a slug never contains a dot, so the name is unambiguous and reversible. The unit is quoted because YAML would otherwise read `2` as a number and `01` as octal.

**The job is what holds it.** Which actor happens to be working that job is the claim at `var/occupy/` — site-local, and changing with every stretch — so a name written here as well would be a second record of one fact, and the two would disagree the first time a stretch ended without releasing. Everything done to a lease is therefore done by the job that holds it: taking it, renewing it, releasing it.

**Expiry is reclaim that works without anyone remembering.** A lease is held across commands rather than for the life of one process, so nothing can release it when the actor working it stops. A record whose `expires` has passed is free; taking it means overwriting the record, and releasing early means deleting the file.

**A lease dies with its job**, so a lease whose job is `done` or `abandoned` is as free as an expired one. Without that rule, a finished job would keep its unit out of reach until the expiry passed.

A lease whose `resource` names nothing in `doc/resource/` is not valid. That is how *everything unversioned must be anchored by something versioned* is checked mechanically, and it is the reason the declarations exist at all.

### Bundle — `<job-id>-<stamp>.tar.gz`

A bundle carries one job's payload between sites. It is a transport artifact rather than a workspace file: it is written outside the workspace, nothing in the workspace ever refers to it, and it is expected to be deleted once unpacked. Its name is a convenience and not an identity — a file that has been through a chat client comes back renamed — so what a receiver trusts is the header inside it.

```text
bundle.yaml
job/<job-id>/log|data|ckpt|out/…
```

The payload paths are workspace-relative, so importing is an unpack, and `tmp/` and `cache/` are absent by construction: the two directories that exist only to be thrown away are the two that never need to travel.

| Field | Required | Value | Meaning |
| --- | --- | --- | --- |
| `job` | Always | job id | The job this payload belongs to |
| `handoff` | Always | file name | The handoff it corresponds to, by name |
| `exported` | Always | RFC 3339 UTC | When the bundle was written |
| `site` | Optional | string | Where it came from, for whoever finds the file later |

`handoff` is the whole check. If the receiving site has that file, it has the records the payload belongs to; if it does not, the bundle arrived ahead of the history it depends on, and the answer is to fetch rather than to guess.

**Nothing under `var/` is carried**, and each absence has its own reason rather than one shared reason:

| Not carried | Why not |
| --- | --- |
| `var/env/<label>/` | A materialised environment is **not portable**: a virtualenv embeds absolute paths, a dependency tree may hold native builds, a compiler cache belongs to one architecture. Copying one between sites imports a machine's assumptions and produces failures that look like code defects. |
| `var/state/<service>/` | Service data belongs to the site. Whatever a job needs from it is either the recipe that resets it — versioned, so it travels by git — or a private copy, which belongs in `job/<job-id>/state/` and is therefore carried. |
| `var/lease/` | Occupancy of a resource on this machine. A job that has stopped holds none. |
| `var/cache/` | Regenerable and machine-specific. |
| `var/config/`, `var/secrets/` | Site-local by definition; that is what they are for. |

The environment variables that point a job's tools at the workspace name paths **inside** it, so they read the same on every site: whoever starts the work supplies them on each site, and the tooling can print them, and there is nothing for a bundle to carry.

A bundle carries whatever the job wrote — logs and inputs included — so it is exactly as publishable as the payload was, and no more.

### Templates

`doc/template/` holds one copy-me file per record type that an actor writes by hand, named after it: `brief.md`, `handoff.md`, `manifest.md`. The tooling-written records — resources, leases, bundles — have no template, because nobody writes one from scratch. A template is a convenience and never the authority — this chapter is. When a format changes, its template changes in the same commit.

**The template is what the tooling carries, and a workspace materialises it once.** The ignore rules and the copy-me files come from the tooling rather than from git, and the empty directories cannot be carried by a repository at all. So the two can drift — the tooling learns to ignore something new, or grows a directory — and nothing in a repository would notice, because none of it is in one.

**Nothing is stored to make that checkable.** The tooling carries the template, so what a workspace is missing is derived by comparing the two, rather than remembered in a version number that would go stale on every site separately. `verify` reports the difference, and one operation brings it up to date.

**What may be written follows from who owns it, and that splits the template in two.** The unversioned skeleton — the empty directories, the ignore rules in both `.gitignore` and `.git/info/exclude`, and the entry point and skill an actor is handed — belongs to the tooling, so bringing it up to date loses nothing. `doc/template/` belongs to the project: it is versioned, in the shared line, and a workspace may keep a different one on purpose, so a difference there is reported and written only when a person asks for it, and what is written is left uncommitted to be recorded as the event it is. Adopting it at one site is enough: from there it travels like any other record.

**A map, not a second source of truth.** What the entry point and the skill state is what is true of every workspace, and they never restate a record: the brief, the handoff and the conventions stay the only places a project's rule, a job's intent or a job's evidence is stated. And because the skeleton is the tooling's, a map that has fallen behind is not repaired by editing it — the same comparison that restores a lost ignore rule replaces it whole. What keeps it from falling behind is the same discipline the templates get: a change here that changes what an actor has to do changes the manual in the same commit, because the manual is the only place any of it reaches a workspace.

**Widening what is ignored happens on its own; narrowing it is a decision.** A rule the tooling has and the workspace has lost is restored, because a rule that is gone is a file that could be committed. A rule the workspace has and the tooling does not is kept and reported until somebody decides otherwise. A machine that needs rules of its own uses git's own `core.excludesFile`, which is that machine's business and never travels.

## Runtime

`var/` holds what a workspace needs while it runs, and nothing it needs to remember. It carries no decision, so losing it loses coordination rather than knowledge: a lease is taken again, an environment is rebuilt, a cache is recomputed. Most of it is a **materialisation rather than a recipe**: the environment under `env/<label>/` is built from the project's own setup, and it is never portable — a virtualenv embeds absolute paths, a dependency tree may hold native builds, a compiler cache belongs to one architecture — so it is rebuilt per site rather than copied between them. The two entries that are not produced by building anything are `lease/`, which is occupancy, and `config/`, which is the site-local half of a declaration.

**Exclusion is the one thing in it with a mechanism.** A lease is the only way to say *this unit of this resource is mine until then*. It covers exclusion as well as capacity, because an exclusive resource is simply one whose `capacity` is `1`, which is the default. A lease is site-local — it names something on this machine — so what it can constrain is a resource that lives here. That is not a limitation once resources are understood to have a home: a resource reachable from several sites is leased at the site that holds it rather than at the site that wants it, so exclusion stays on the machine the thing is physically on and nothing central is involved. The site holding it is asked rather than assumed, because only that machine knows. Two sites that both claim a resource are refused rather than picked between: a resource with two homes is one the workspace has nothing to say about.

There is deliberately no separate lock, because every other contention a workspace can have is cheaper to settle without persistent state:

| Contention | How it settles |
| --- | --- |
| Two agents editing one registry file | Read, change, and write only if the file is unchanged since the read; otherwise retry |
| Two agents declaring the same name | Creating the directory is atomic, and fails for whoever is second |
| One commit picking up another agent's files | Stage only the files of this event, which is what one coordination event per commit already requires |
| Two commits, or two asset tags, at once | Git already locks its own index and refs |
| Two collections running at once | Deletion is idempotent |

A lease cannot be replaced that way, and that is the entire reason it exists: whether a GPU is free is a question that has to be answered *before* acting, and the answer has to outlive the command that asked it.

**The rest of `var/` is mostly a naming agreement.** Each entry keeps something out of a worktree or out of a job's payload, or keeps what the tooling needs in order to answer a question about right now:

| Directory | Holds | Rule |
| --- | --- | --- |
| `state/<service>/` | The data of a shared stateful dependency | The acquirer resets it; reset the content, never the identity; never collected |
| `env/<label>/` | A worktree's dependency environment | Keyed by label, not by job: it belongs to the directory, not to whoever is using it |
| `cache/` | Regenerable caches shared across jobs | Safe to delete at any time |
| `occupy/<job-id>.yaml` | The actor who says they are on a job, kept at the site that holds that job | Written and cleared by the tooling on the actor's behalf, read through it; a claim, not a lock. It is stale once the job leaves `doc/task/` or this site no longer holds it |
| `config/<name>.yaml` | The site-local half of a resource declaration | Never committed; declared by this site, for this site |
| `site` | Which site this machine is, by name | A one-line site-local fact: a site that cannot name itself cannot find its own half of a declaration |
| `secrets/` | Machine-local credentials | Prefer outside the workspace; never in git, never in a handoff |
| `daemon.lock`, `daemon.sock`, `daemon.log` | The daemon's own: the process holding this site, the socket commands reach it on, and what it has said | Written and cleared by the daemon; the log is the only place it explains a refusal |
| `daemon.yaml` | Where this site accepts peers, if it accepts any | Never committed: how to reach this machine is this machine's own business, and a site that says nothing accepts nobody |
| `actors.yaml` | The actor roster: which names this site has seen, and when each was last heard from | Written by the tooling, never committed. A name is what an actor is, so this is the whole of what a name has |
| `msg/<actor>/` | That actor's mailbox: one file per message, with any attachments beside it | Written by the tooling on delivery, never committed, never collected, and never in git |


### Messages

An actor is a name, and actors reach each other through the tooling rather than through the records. A message is **process information**: it is not a record, it is never committed, it does not travel by git, and nothing about it is validated. It exists so that one actor can tell another something now — that a job is free, that the job it was waiting on is done, that the file it wanted is ready.

```text
var/msg/<actor>/<id>.md          # one message
var/msg/<actor>/<id>/<file>      # its attachments, if any
```

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | Always | Opaque and unique. It is what a reply and any other tool refers to, and it deliberately carries nothing — the subject is a separate field, because a title is for a person and an identity is for a machine |
| `from`, `to` | Always | Actor names |
| `site` | Always | Where the sender was when it sent it |
| `sent` | Always | RFC 3339 UTC |
| `subject` | Always | One line, for a person reading a list |
| `reply_to` | When it answers one | The `id` of the message being answered |
| `job` | When it is about one | A job id, as a topic marker and nothing more |
| `attachments[]` | When there are files | Each name and its size; the bytes sit beside the message |

- **It is delivered or refused, and never queued.** The sender's tooling hands the message to the recipient's site; when that site cannot be reached the send fails and says so, because a workspace that assumes delivery is a workspace that loses messages quietly. Once accepted, the message is in the recipient's mailbox and stays there until somebody removes it.
- **It cannot be retracted or edited, because it may already have started something.** An agent's own tooling may act on a message the moment it arrives, so a correction is another message with `reply_to` set.
- **It appears whole, or not at all.** Its attachments are written first and the record that names them last, each of them by a rename, so a tool watching a mailbox never sees a message whose bytes have not arrived. That is what lets a harness be woken by a mailbox rather than made to poll it.
- **Attachments are bytes and nothing more.** They are not assets: no content addressing, no tag, no manifest, and never `store/`. Sending a file this way is the transport, not the record — anything that has to survive is published as an asset and referred to by name.
- **Nothing parses `subject`, and nothing depends on `id` being readable.** Tools refer to `id`; people read `subject`.

The workspace does not manage processes, evict caches or read secrets. It only says where those things go.

**A shared service** is the one case with rules worth stating twice, because the workspace owns the directory and not the process: starting it, stopping it and resetting it are the project's business. Two rules follow. Reset **when acquiring**, not when releasing, because a crash cannot be trusted to release anything cleanly — so a dirty state left by a dead occupant is cleared by whoever comes next. And a destructive test must not use the shared instance at all; it belongs in a per-job copy under `job/<job-id>/state/`.

## Not yet defined

The following are left open deliberately rather than guessed at:

- **Whether the tooling assembles a startup context for one job.** The workspace's half is settled twice over: an actor is given two facts, the root and a job id, and the workspace carries an entry point and a skill saying where everything else is derived from, including the rules. The base environment variables are not part of it either: the tooling prints them for a job. What is left open is whether the tooling should gather a job's own context — the brief, the baton, the working directory, the environment — and hand it over itself, rather than leaving the reading to the actor.
- **How a site discovers the others.** Today the roster is a git declaration, each site enters its own reach by hand, and keys are exchanged once; that is enough for a site to be added reliably, which is what `Joining a site` describes. A discovery protocol is deliberately not designed, and nothing depends on one existing.
