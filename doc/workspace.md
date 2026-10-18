# The workspace

A **workspace** is one project's shared state: the records that say what is being worked on,
the code, the data the project publishes, and the runtime around it. It is a directory, and
every machine that works on the project has a copy — a **site**. The copies are held together
by **thrum** ([`thrum.md`](thrum.md)): a daemon per site, which finds actors, carries messages,
and keeps the shared histories converging.

An **actor** is a person or an agent working through thrum. A **job** is one unit of work, and
it is the only thing this design isolates.

## What a job isolates

One job owns one brief, one branch, one working directory, and one payload. Two jobs therefore
share none of the machinery that work actually collides over, and two actors can work at once
without stepping on each other by construction.

What the design deliberately does **not** do is claim to isolate *paths*. Two jobs may touch
the same file; they will do it on two branches, and the collision appears at integration, where
the project can see it and decide. The brief lists the paths a job expects to touch —
`touches` — and it is what it says: a description for whoever reads the brief next, not a lock,
not a claim, and not a mechanism. A previous version read exclusion into that field; the
reading is withdrawn.

There are **no leases and no resource registry** in this version. A shared scarce thing — a
borrowed machine, a database, a license — is the project's business until a later version gives
it a mechanism, and a file under `var/` that only pretends to arbitrate it is worse than
saying nothing.

## The layout

The directory name is chosen when the workspace is created; `workspace/` below is an example.

```text
workspace/
    .git/                            # the line repository: doc/, and nothing else
    .gitignore
    AGENTS.md                        # the entry point an actor reads; from thrum; never tracked
    .agents/skills/                  # the workspace's rules, read on demand; never tracked

    doc/
        actor/<name>.yaml            # who exists, and what kind
        site/<name>.yaml             # which machines exist, their keys and their reach
        repo/<name>.yaml             # which code repositories the project has
        convention/<nn>-<slug>.md    # rules every actor reads, in number order
        topic/<topic>/               # knowledge shared across jobs
        task/<job-id>/brief.md       # one job, in flight
        worktree/<label>.yaml        # a named area of work, its repository, and whether it is one-off
        archive/                     # retired documents, and closed jobs
        template/                    # copy-me files for the records an actor writes
    handoff/                         # the ledger repository: append-only evidence
        .git/
        <job-id>/<started>.md        # one file per stretch; never edited, never deleted

    repos/                           # bare repositories: no permanent checkout
        <name>.git/                  # one per declared code repository
        store.git/                   # published data, never checked out
    job/<job-id>/                    # a job's payload: log, tmp, cache, data, state, ckpt, out
    worktree/<label>/                # a checkout of one repository, passed between jobs
    var/                             # this site's runtime; it carries no decision
```

Three planes, each with its own rule for staying alive:

| Plane | Holds | Keyed by | Backed up |
| --- | --- | --- | --- |
| the line | `doc/` — the decisions | job id, name, topic | pushed to the other sites |
| the ledger | `handoff/` — the evidence | job id and stamp | pushed to the other sites |
| the work | `repos/` | repository and branch, asset tag | an external mirror; the code most of all |
| the runtime | `var/`, `job/`, `worktree/` | this machine | nothing; it is rebuilt |

## Records

Everything durable is a record: a file in one of the **two shared histories**, written by an
actor or by thrum, and committed as one event. A **decision** is a file in the line; **evidence**
is a file in the ledger. Nothing the fabric holds is a record.

| Record | Where | Written by |
| --- | --- | --- |
| actor | `doc/actor/<name>.yaml` | thrum, when a name is declared |
| site | `doc/site/<name>.yaml` | thrum, at invite and join |
| repo | `doc/repo/<name>.yaml` | thrum, when a code repository is added |
| brief | `doc/task/<job-id>/brief.md` | the actor on the job |
| handoff | `handoff/<job-id>/<started>.md` | the actor finishing a stretch |
| topic | `doc/topic/<topic>/` | an actor, when knowledge stops being job-specific |
| convention | `doc/convention/<nn>-<slug>.md` | the project, at its own pace |
| manifest | `doc/asset/<name>/<version>.md` | thrum, when data is published |
| worktree | `doc/worktree/<label>.yaml` | thrum, at the first `worktree add` of a label |

The brief and the handoff are two histories on purpose. A brief is edited as understanding
changes; a handoff is appended once and never touched. Keeping them apart lets the ledger, which
grows without bound, merge by the union of new files, and keeps the line — the part a person
edits — small.

### Brief — `doc/task/<job-id>/brief.md`

The brief states one job's intent. It is editable, and every change is one event, so the
history shows when the scope moved.

```yaml
---
job: add-retry-to-uploader
status: active                       # active | done | abandoned
repo: code                           # the code repository this job works in
branch: job/add-retry-to-uploader
owner: flash-9c2e                    # optional; absent means nobody is accountable
touches: ["src/upload/**"]           # optional, descriptive: what this job expects to touch
depends_on: [fix-ci-timeouts]        # optional
keep: false                          # optional: protect this job's payload from collection
---
```

The body has three sections, in this order, and `None.` is a legal answer to any of them:

```markdown
## Goal
## Scope
## Acceptance criteria
```

`status` is the one field a person decides. Everything else a job can be in is read from
something already written: **started or not** from whether `handoff/<job-id>/` is empty, and
**blocked or not** from the last handoff, so a job that stops being blocked stops being blocked
the moment a later handoff says so and no field has to be reset.

### Handoff — `handoff/<job-id>/<started>.md`

One handoff covers one stretch of work. It is written when the work stops, never before, and it
is append-only: a correction is a new file with `supersedes` set, and a handoff is never
deleted.

```yaml
---
job: add-retry-to-uploader
repo: code
agent: flash-9c2e
branch: job/add-retry-to-uploader
base: 4f2a1c9d8e3b7f01c2a4e6b8d0f2a4c6e8b0d2f4
head: 9be77d05a1c3e5f7b9d1e3f5a7c9b1d3e5f7a9c1
status: partial                      # done | partial | blocked | aborted
started: 2026-10-09T02-10-00Z
ended: 2026-10-09T03-05-12Z
next: Fix the flaky timeout in tests/upload/test_retry.py, then make test-upload.
worktrees: [uploader]
blocked_by: []                       # present exactly when status is blocked
artifacts:                           # paths under job/<job-id>/
  - {path: job/add-retry-to-uploader/out/coverage.html, sha256: 3f9c1d…, bytes: 184223}
metrics: {tests_passed: 128, wall_seconds: 3312}
---
```

The body has four sections, in this order, and again `None.` is legal:

```markdown
## What changed
## What was verified
## What remains
## Open questions
```

`next` is the baton: it is `none` exactly when the status is `done`, and every other stretch
says what whoever continues does first. Both directions are refused when they are wrong.

A handoff records a stretch of work, so it is refused for a brief that is not `active`. When a
stretch ends the job, that is **two events in two repositories, and the order is fixed**: the
handoff first, in the ledger, and then the close, in the line. They cannot be one commit across
two repositories, and the design does not pretend otherwise. Handoff first is the recoverable
direction: a crash between them leaves the job open with its final evidence already written,
which is the version somebody can act on.

### Worktree — `doc/worktree/<label>.yaml`

A label names an area of work and the repository it belongs to. It is written by thrum at the
first `worktree add` of the label, so that the label means the same thing to every job that uses
it.

```yaml
label: uploader
repo: code
purpose: Uploader reliability work
ephemeral: false
```

| Field | Required | Meaning |
| --- | --- | --- |
| `label` | always | must equal the file name; the directory under `worktree/` |
| `repo` | always | the code repository whose branch is checked out there |
| `purpose` | always | what the area is for, in a person's words |
| `ephemeral` | optional, default false | the directory may be destroyed on release |

## Ownership, and who is on a job

- **`owner` is durable accountability**, in the brief: who the work was promised to, and who to
  ask when it is unclear. It changes only when somebody decides it should — the opener sets it,
  an owner may reassign it or give it up, and a person may do either on behalf of an owner who
  has become unreachable. An unowned job may be claimed by whoever takes it up.
- **Nobody claims a job, and nothing records who is on it now.** There is no occupancy file and
  no registration of *I am working this*. Whether an actor is working cannot be observed, so
  the design does not pretend to observe it. What can be read is what was written: the owner,
  and the newest handoff with its `agent` and its `ended`. The fabric adds only where that
  actor was last known to be — an address, not a claim.
- **Who worked a finished stretch is in the record**: the handoffs, each naming its `agent` and
  its start and end. That is the history, and it does not need a claim to exist.

## The shared histories, and the repositories

Two repositories hold everything durable, and they hold different things.

**The line** (`doc/`) is the decisions: actor, site, repository and worktree declarations,
conventions, topics, briefs, asset manifests. Several sites write it at once, and it merges rather than
conflicting because no two sites write one file — a brief has the one site working its job, a
declaration has the site that made it, and a name claimed twice is settled by a deterministic
order rather than merged. Nothing that has been seen is ever rewritten.

**The ledger** (`handoff/`) is the evidence: one file per stretch of work, appended once and
never edited or deleted. It is the history that grows without bound, and it needs no convergence
rule at all beyond the union of new files. A handoff's name carries the job and the moment the
stretch started, so two sites recording at once produce two files, and both are the truth about
their own stretch. A correction is a further new file with `supersedes` set, because the old one
is evidence of what was believed then.

Keeping them apart is not tidiness. The ledger is append-only and merges by union; the line is
edited in place and needs the name rule. One repository would have to carry both regimes, and the
half that grows forever would drag the half a person edits. [`thrum.md`](thrum.md) has the
mechanism.

**A handoff is checked against the line as well as itself**: its `job` must name a brief that
exists, live or archived. That is the one check that reads across the two, and `verify` is where
it lives.

The code repositories are a different history with a different rule: **one branch per job, and
one writer per branch** — the site holding that job. A job names its repository in its brief
(`repo`), and a worktree label names the one it checks out. Integration is the project's
business; `job/<id>` is never merged by the workspace, and a closed job is a hand-off to whoever
integrates, not an integration itself.

`repos/` holds every bare repository: one per declared code repository, and `store.git`. It is the
one directory that never has a permanent checkout — the line and the ledger keep theirs, and a
code repository is checked out under `worktree/` only while a job holds it. A site materialises
the repositories it works with; a declared repository that is not here is not an error, and
`status` says so.

Assets live in the store (`repos/store.git`) and are published as tags, created once and never
moved. A version that turns out to be wrong is **withdrawn**: its manifest is retired with a
notice, and the tag is deleted, so the bytes become collectible and the version number stays
taken.

## Moving a job to another site

A job belongs to the workspace, not to a machine. Moving one is three acts:

1. **The records travel by the shared histories.** The brief is in the line and the handoffs are
   in the ledger; both are already shared.
2. **The branch travels through the fabric.** The receiving site fetches `job/<id>` from the
   job's repository — `repo` in the brief.
3. **The payload travels as a bundle**, because git cannot carry it. Export runs after a
   handoff and never during work; it releases nothing, because stopping already released
   whatever the job held.

Import refuses unless the named handoff is present and the commit it names resolves, so there
is nothing to merge and nothing to diverge. If a payload is already there, import refuses and
says so; only an explicit replacement proceeds, and it reports every path it discarded.

A move changes nothing about ownership. The receiving actor may work under the owner's
authority, or the owner may give the job up first and let the receiver claim it.

## Payload

`job/<job-id>/` is everything a job produces on disk: `log/`, `tmp/`, `cache/`, `data/`,
`state/`, `ckpt/`, `out/`. Nothing in it is irreplaceable — whatever must survive is published
to the store and referenced from a handoff, or reproduced.

A payload becomes collectible once its brief says `done` or `abandoned`, unless `keep` is true.
`tmp/`, `cache/` and `ckpt/` go at the close; the rest after a retention window measured from
it. The window is policy, so it is stated once where a tool can read it and everyone sees the
same one, and a collected artifact is a *detectable* absence because the handoff kept its hash.

The value lives in a convention's one `values` block: `collect.window`, as `14d`, `36h` or a Go
duration. The default is 14 days. `thrum verify` checks the block (one per convention, dotted
keys, scalar values, one convention per key, and a window it can parse); `thrum status` reports
each payload as `in use`, `waiting` (with its collectible date), `collectible`, `kept` or
`orphan`; and `thrum clean --payloads` collects the collectible ones. A `keep` brief is never
collected, and a payload whose brief is still live is never touched.

## Where a thing goes

Placement is a procedure, not a list, because a list is overtaken by the next kind of data.

```text
1. Source code, or a document that must change atomically with it?  → a code repository
2. A record of what a job did?                                      → handoff/
3. A secret?                                                        → never in git
4. Regenerable?   shared → var/cache/   one job → job/<id>/cache/   → var/env/<label>/
5. Text a person reads in place?                                    → doc/
6. Binary, or too large to read?                                    → the store, with a manifest
7. State of the machine right now?                                  → var/
8. Larger, or externally governed?                                  → outside, with a locator
```

Everything above is also a prohibition: nothing under `worktree/` holds anything that is not
source or a document that changes with it, which is what lets a label be handed to the next job
without inheriting somebody else's mess.

## Naming

Names are identifiers, not descriptions: short, shell-safe, and never encoding state that can
change, because a name written into a record can never change again.

```text
slug  := [a-z0-9]+ ( "-" [a-z0-9]+ )*
stamp := YYYY "-" MM "-" DD "T" HH "-" MM "-" SS "Z"      # UTC, colons as hyphens
```

| Name | Form | Example |
| --- | --- | --- |
| job id | `slug` | `add-retry-to-uploader` |
| repository | `slug` | `code`, `web` |
| branch | `job/` + job id | `job/add-retry-to-uploader` |
| worktree label | `slug`, reused by design | `uploader` |
| actor, site, topic, asset | `slug` | `flash-9c2e` |
| asset version | `YYYY-MM-DD`, then `-N` on the same day | `2026-10-09` |
| handoff file | `stamp` + `.md` | `2026-10-09T02-10-00Z.md` |
| convention file | two digits + `-` + `slug` + `.md` | `01-placement.md` |

Job ids, repository names, actor and site names, asset versions, and convention numbers are taken
forever: once used, never reused. A worktree label is reused deliberately, so its meaning must
stay fixed. Names are reversible — `job/` + id, file name ↔ timestamp, label ↔ directory — so a
record can be checked against the path it lives at.

## What is out of this version

Deliberately absent, rather than forgotten:

- **leases and resources.** Nothing arbitrates a scarce shared thing yet.
- **path exclusion.** `touches` is a description; a conflict is the project's to see at
  integration.
- **a job spanning repositories.** A job works in one repository; work that spans them is two
  jobs, with `depends_on` between them.
- **more than one workspace per site.** One site, one workspace, for now.
- **discovery.** A site is declared and introduced; nothing finds one by itself.
- **deferred delivery.** A sender never queues, so nothing waits for an actor to appear. Making
  an operation wait, on purpose and with a bound, is a later version's business.
