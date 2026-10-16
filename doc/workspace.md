# The workspace

A **workspace** is one project's shared state: the records that say what is being worked on,
the code, the data the project publishes, and the runtime around it. It is a directory, and
every machine that works on the project has a copy — a **site**. The copies are held together
by **thrum** ([`thrum.md`](thrum.md)): a daemon per site, which finds actors, carries messages,
and keeps the shared line converging.

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
    .git/                            # the line: doc/ and handoff/, nothing else
    .gitignore
    AGENTS.md                        # the entry point an actor reads; from thrum; never tracked
    .agents/skills/                  # the workspace's rules, read on demand; never tracked

    doc/
        actor/<name>.yaml            # who exists, and what kind
        site/<name>.yaml             # which machines exist, their keys and their reach
        convention/<nn>-<slug>.md    # rules every actor reads, in number order
        topic/<topic>/               # knowledge shared across jobs
        task/<job-id>/brief.md       # one job, in flight
        worktree/<label>.yaml        # a named area of work, and whether it is one-off
        archive/                     # retired documents, and closed jobs
        template/                    # copy-me files for the records an actor writes
    handoff/<job-id>/<started>.md    # evidence: append-only, one file per stretch

    .bare/                           # the code repository, bare, checked out into worktree/
    store/                           # published data, bare, never checked out
    job/<job-id>/                    # a job's payload: log, tmp, cache, data, state, ckpt, out
    worktree/<label>/                # a labelled working directory, passed between jobs
    var/                             # this site's runtime; it carries no decision
```

Three planes, each with its own rule for staying alive:

| Plane | Holds | Keyed by | Backed up |
| --- | --- | --- | --- |
| the line | `doc/` and `handoff/` | job id, name, topic | pushed to the other sites |
| the work | `.bare/` and `store/` | branch, asset tag | an external mirror; the code most of all |
| the runtime | `var/`, `job/`, `worktree/` | this machine | nothing; it is rebuilt |

## Records

Everything durable is a record: a file in the line, written by an actor or by thrum, and
committed as one event. Nothing the fabric holds is a record.

| Record | Where | Written by |
| --- | --- | --- |
| actor | `doc/actor/<name>.yaml` | thrum, when a name is declared |
| site | `doc/site/<name>.yaml` | thrum, at invite and join |
| brief | `doc/task/<job-id>/brief.md` | the actor on the job |
| handoff | `handoff/<job-id>/<started>.md` | the actor finishing a stretch |
| topic | `doc/topic/<topic>/` | an actor, when knowledge stops being job-specific |
| convention | `doc/convention/<nn>-<slug>.md` | the project, at its own pace |
| manifest | `doc/asset/<name>/<version>.md` | thrum, when data is published |
| worktree | `doc/worktree/<label>.yaml` | thrum, at the first `worktree add` of a label |

### Brief — `doc/task/<job-id>/brief.md`

The brief states one job's intent. It is editable, and every change is one event, so the
history shows when the scope moved.

```yaml
---
job: add-retry-to-uploader
status: active                       # active | done | abandoned
branch: job/add-retry-to-uploader
owner: agent-3f9c                    # optional; absent means nobody is accountable
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
agent: agent-3f9c
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

A handoff records a stretch of work, so it is refused for a brief that is not `active`. A
stretch that ends the job may close it in the same event, which then takes the `close` kind,
because the ending is the more consequential of the two.

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

## The line, and the repositories

Everything in the table under *Records* is one **line**: the shared history that every durable
coordination fact goes into. Several sites write it at once, and it is the one shared thing. It
merges rather than conflicting because no two sites write one file — a handoff is a new file
every time, a brief has the one site working its job, and a name claimed twice is settled by a
deterministic order rather than merged. Nothing that has been seen is ever rewritten.
[`thrum.md`](thrum.md) has the mechanism.

The code repository is a different history with a different rule: **one branch per job, and one
writer per branch** — the site holding that job. Integration is the project's business;
`job/<id>` is never merged by the workspace, and a closed job is a hand-off to whoever
integrates, not an integration itself.

Assets are published as tags, created once and never moved. A version that turns out to be
wrong is **withdrawn**: its manifest is retired with a notice, and the tag is deleted, so the
bytes become collectible and the version number stays taken.

## Moving a job to another site

A job belongs to the workspace, not to a machine. Moving one is three acts:

1. **The records travel by the line.** The brief and every handoff are already shared.
2. **The branch travels through the fabric.** The receiving site fetches `job/<id>`.
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
to `store/` and referenced from a handoff, or reproduced.

A payload becomes collectible once its brief says `done` or `abandoned`, unless `keep` is true.
`tmp/`, `cache/` and `ckpt/` go at the close; the rest after a retention window measured from
it. The window is policy, so it is stated once where a tool can read it and everyone sees the
same one, and a collected artifact is a *detectable* absence because the handoff kept its hash.

## Where a thing goes

Placement is a procedure, not a list, because a list is overtaken by the next kind of data.

```text
1. Source code, or a document that must change atomically with it?  → the code repository
2. A record of what a job did?                                      → handoff/
3. A secret?                                                        → never in git
4. Regenerable?   shared → var/cache/   one job → job/<id>/cache/   → var/env/<label>/
5. Text a person reads in place?                                    → doc/
6. Binary, or too large to read?                                    → store/, with a manifest
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
| branch | `job/` + job id | `job/add-retry-to-uploader` |
| worktree label | `slug`, reused by design | `uploader` |
| actor, site, topic, asset | `slug` | `agent-3f9c` |
| asset version | `YYYY-MM-DD`, then `-N` on the same day | `2026-10-09` |
| handoff file | `stamp` + `.md` | `2026-10-09T02-10-00Z.md` |
| convention file | two digits + `-` + `slug` + `.md` | `01-placement.md` |

Job ids, asset versions, and convention numbers are taken forever: once used, never reused. A
worktree label is reused deliberately, so its meaning must stay fixed. Names are reversible —
`job/` + id, file name ↔ timestamp, label ↔ directory — so a record can be checked against the
path it lives at.

## What is out of this version

Deliberately absent, rather than forgotten:

- **leases and resources.** Nothing arbitrates a scarce shared thing yet.
- **path exclusion.** `touches` is a description; a conflict is the project's to see at
  integration.
- **more than one workspace per site.** One site, one workspace, for now.
- **discovery.** A site is declared and introduced; nothing finds one by itself.
- **deferred delivery.** A sender never queues, so nothing waits for an actor to appear. Making
  an operation wait, on purpose and with a bound, is a later version's business.
