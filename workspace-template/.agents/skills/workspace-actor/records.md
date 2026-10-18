# The records you write

How must a record look? Two record types are yours: the **brief**, which states a job's intent, and
the **handoff**, which records a stretch of work. Both are YAML front-matter plus a Markdown body,
and both are shared histories — the brief in the line, the handoff in the ledger — so neither may
contain a secret, and neither may name an absolute or machine-local path.

`doc/template/` carries a copy-me file for each. `thrum job open` and `thrum job handoff` write them
correctly; this file is what you need when you edit one by hand, and when you have to judge whether
a record you are reading is well formed.

## Common to every record in the workspace

- **Front-matter is delimited by `---` and must be valid YAML; the body is Markdown.** A pure-YAML
  file — a site's declaration, an actor's, a repository's, a worktree's — has neither: its top
  level is the record.
- **Field names are lower snake case**, and every `status` is a closed enumeration of lower-case
  values. There is no free-form status.
- **A field that names a path says which root it is relative to**, and a path is never absolute.
- **One record per file.** Nothing in either shared history is a file that every actor writes.
- **Git already records who committed and when, so neither is duplicated** — with two exceptions:
  where the meaning depends on the time, as in a handoff's start and end or a retirement notice; and
  where it depends on which actor rather than which committer, as with a handoff's `agent`.
- **No secrets**, in `doc/`, in `handoff/`, or in any code repository: all three are permanent.
- **Every document that can be retired has exactly one H1 title**, on its first line after any
  front-matter — that is what gives a retirement notice a defined position. Conventions, topics and
  manifests can be retired and therefore have one; briefs and handoffs cannot, and their identity
  lives in their front-matter instead.

`thrum verify` checks every mechanical consequence of the above and of the formats below. It is the
authority on what is well formed; run it rather than reasoning about it.

## Brief — `doc/task/<job-id>/brief.md`

```yaml
---
job: add-retry-to-uploader
status: active                   # active | done | abandoned; being blocked is derived
repo: code                       # the code repository this job works in
branch: job/add-retry-to-uploader
touches: ["src/upload/**", "tests/upload/**"]
owner: flash-9c2e                # optional; absent means unowned
depends_on: [fix-ci-timeouts]    # optional
keep: false                      # optional; true protects this job's payload
---
```

| Field | Required | Meaning |
| --- | --- | --- |
| `job` | always | must equal the containing directory name |
| `status` | always | `active` while it is still meant to happen, then `done` or `abandoned` |
| `repo` | always | the code repository the job works in; must be a declared repository |
| `branch` | always | the branch in that repository that implements it |
| `touches` | optional | paths this job expects to touch, relative to the job's repository root — **a description for the next reader, never a lock, a claim or a mechanism** |
| `owner` | optional | who is responsible; absent means nobody is |
| `depends_on` | optional | job ids that must reach `done` before this work proceeds |
| `keep` | optional, default false | true keeps this job's payload out of collection |

Body sections, in this order, all required, and `None.` is a legal answer to any of them:

```markdown
## Goal
## Scope
## Acceptance criteria
```

Additional sections may follow. A plan, when one is needed, is one of them — never a separate file.

The whole body can be written at the open with `job open --body <file|->` (or `work --body`), or
generated from `--goal`, `--scope` and `--criteria`; the body is what `job show` prints and what
`verify` reads the three sections from.

### Two states are not stored

*Started or not* and *blocked or not* are derived, and `SKILL.md` says where from. The consequence
for a record is narrow: **only `status` is a decision rather than a reading**, it lives in the
brief, and it changes only when the job ends.

### Ownership, and whose brief it is

A job has one owner or none. Opening sets it, an owner may reassign it, and an owner may give it up
— which is what makes a job claimable, because there is nobody left to ask. An owner who has become
unreachable rather than unwilling is given up or reassigned by a person on their behalf; otherwise
the job is stuck forever. `actors.md` has the rest.

Ownership is not liveness, and it is not the body. **Only the actor currently working a job changes
its `## Goal`, `## Scope`, `## Acceptance criteria` or the sections after them**, because that is
the understanding that is current. An owner who reassigns or gives up the job does not thereby gain
the body.

### Who is on the job right now is not here

Whether an actor is working cannot be observed, so it is recorded nowhere. What is here is the
`owner`; what a reader can add is the newest handoff, with its `agent` and its `ended`. `actors.md`
has the reasoning.

## Handoff — `handoff/<job-id>/<started-utc>.md`

The file name is the **start** of the stretch in UTC, with `:` replaced by `-`
(`2026-10-09T02-10-00Z.md`). It labels the work, not the write.

```yaml
---
job: add-retry-to-uploader
repo: code
agent: flash-9c2e
branch: job/add-retry-to-uploader
base: 4f2a1c9d8e3b7f01c2a4e6b8d0f2a4c6e8b0d2f4
head: 9be77d05a1c3e5f7b9d1e3f5a7c9b1d3e5f7a9c1
worktrees: [uploader]
status: partial
started: 2026-10-09T02:10:00Z
ended: 2026-10-09T03:05:12Z
next: Fix the flaky timeout in tests/upload/test_retry.py, then run make test-upload.
touches: ["src/upload/**", "tests/upload/**"]
metrics: { tests_passed: 128, coverage: 0.83 }
artifacts:
  - path: job/add-retry-to-uploader/out/coverage.html
    sha256: 3f9c1d8b2a4e6f0c8a2e4b6d8f0a2c4e6b8d0f2a4c6e8b0d2f4a6c8e0b2d4f6a
    bytes: 184223
assets: [uploader-baseline@2026-10-09]
---
```

| Field | Required | Value |
| --- | --- | --- |
| `job` | always | must equal the containing directory name |
| `repo` | always | the repository its commits are in; must equal the brief's |
| `agent` | always | the actor that wrote it |
| `branch` | always | must equal `job/` + the job id, in that repository |
| `base` | always | the commit the work started from; must resolve in that repository |
| `head` | always | the commit at handoff time, equal to `base` if nothing was committed |
| `status` | always | `done` \| `partial` \| `blocked` \| `aborted` |
| `started` | always | RFC 3339 UTC, matching the file name |
| `ended` | always | RFC 3339 UTC |
| `next` | always | the baton; `none` exactly when `status` is `done` |
| `worktrees` | optional | labels the work used |
| `supersedes` | when correcting | a sibling file name |
| `blocked_by` | when `blocked` | job ids it is waiting on |
| `touches` | optional | what was actually touched, for comparison against the brief |
| `metrics` | optional | counts, coverage, durations — the source of performance history |
| `artifacts` | optional | a list of `path`, `sha256` and `bytes`, with each `path` under `job/<job-id>/`, relative to the workspace root |
| `assets` | optional | `name@version`, for what this stretch published |

`status` means exactly this:

- **`done`** — the brief's acceptance criteria are met and nothing remains.
- **`partial`** — progress was made and work remains, with no external blocker.
- **`blocked`** — something outside this job's control is missing; `blocked_by` says what.
- **`aborted`** — no usable progress: a crash, a wrong approach, a superseded plan.

Body sections, in this order, all required, `None.` legal in any:

```markdown
## What changed
## What was verified
## What remains
## Open questions
```

"What was verified" is the load-bearing one: *I changed it* and *I proved it works* are different
claims, and this is the section that separates them. "Open questions" is the most useful thing you
can hand over and the easiest to drop.

### Lifecycle, and the rules that are not negotiable

1. **Write one whenever you stop** — to hand over, for the day, because you are blocked, because
   your context is nearly full, or because the job is finished. Not before. Work that runs for many
   rounds without stopping needs nothing in between.
2. **Commit before handing off.** `head` has to name an existing commit, so the commit comes first
   and the handoff second.
3. **A gap is information.** When work stops without a handoff, whoever continues reads the working
   directory and the branch instead, and knows the last stretch ended in the middle rather than on
   purpose.
4. **Never edit a handoff once it has been read downstream.** A correction is a new file carrying
   `supersedes`; chains are allowed, cycles are not. A correcting record may cover no work at all.
5. **Never delete a handoff.**
6. **One handoff is one coordination event and one commit, in the ledger.** When a stretch ends the
   job, the close is a **second** event, in the line: they are separate repositories and cannot be
   one commit. Write the handoff first — a crash between them then leaves the job open with its
   final evidence written, which is the version somebody can act on.

If the file name already exists, the write fails. Never overwriting is the minimum an append-only
record owes.

### Never in a handoff

- Secrets, credentials or tokens: this file is versioned and permanent.
- Pasted logs: name a file under `job/` with `--artifact` instead, and its raw hash is recorded.
- A restatement of the brief: a handoff is evidence, not a copy of the intent.
- Plans beyond `next`: those belong in a new brief.
- The status of other jobs: that recreates shared mutable state.
- Absolute or machine-local paths.

### Derived views

There is no board to maintain. **The task board** is each brief's `status` with the status of the
newest file in its `handoff/` directory: the brief says whether the job is still meant to happen,
the last handoff says whether the last stretch got anywhere. **Performance history** is the `metrics`
fields aggregated across handoffs. **Provenance** runs from `artifacts` into `job/` and from `assets`
into the store. `thrum status` prints all of it.

**What the board's word for a job means.** For a job that has closed it is the brief's `status`. For
one still open it is what the **newest handoff** said — `done`, `partial`, `blocked`, `aborted` — or
`no handoff yet` when `handoff/<job-id>/` is empty. That last word is worth reading exactly: it
means *nothing has been recorded yet*, not *nobody is doing anything*. Whether an actor is on the
job is not recorded anywhere, and `actors.md` says why.

## Writing a record is a coordination event

Every durable fact is a file in one of the **two shared histories**, and every operation that records
one turns it into a single event — one commit. The brief lives in the **line** (`doc/`), with the
declarations, conventions, topics and manifests; the handoff lives in the **ledger** (`handoff/`),
which is append-only. `sites.md` has the mechanics of how they converge between sites; two
consequences a writer has to carry are here.

**A merge is never a conflict.** In the line it is because no two sites write one file: a brief has
the one site working its job, and a declaration has the site that made it. In the ledger it is
because there is nothing to merge — every handoff is a new file whose name carries the job and the
moment the stretch started, so two sites recording at once write two files and both are true. A
correction is another new file with `supersedes` set.

**A name claimed twice is settled, not merged.** Two sites can open a job with the same id while they
are apart. The events carry a clock, every site derives the same order from them, and the first
claim keeps the name. The other is void, its file is discarded, and its author is told by name on
the next exchange — so you record again under a name that is free rather than having your work
overwritten.

**On one machine, one writer at a time.** If another actor here is recording, your event waits for
theirs and then records on top. An event records exactly the paths it staged: a change that arrives
while an event is being recorded belongs to the next event, and a change you staged but did not
record is never swept into somebody else's event.

## Commit messages

One commit describes one coordination event, and its subject names the event:

```text
<kind> <subject>: <summary>
```

| Kind | Subject | Example |
| --- | --- | --- |
| `init` | omitted | `init: created from the template` |
| `actor` | actor name | `actor dana: declared as a human` |
| `site` | site name | `site build-02: joined, key registered` |
| `repo` | repository name | `repo code: declared the uploader service` |
| `worktree` | label | `worktree uploader: declared for uploader reliability` |
| `brief` | job id | `brief add-retry: scope narrowed to the uploader only` |
| `handoff` | job id | `handoff add-retry: the flaky timeout remains` |
| `close` | job id | `close add-retry: acceptance criteria hold after four handoffs` |
| `topic` | topic name | `topic upload-protocol: added the retry contract` |
| `convention` | file name without `.md` | `convention 03-naming: numbered conventions are never reused` |
| `asset` | `name@version` | `asset uploader-baseline@2026-10-09: published from a production sample` |
| `template` | file name without `.md` | `template brief: added the question a brief has to answer` |
| `retire` | path under `doc/`, without the extension | `retire topic/upload/old-retry: superseded by topic/upload/retry` |

A message says what changed, never restating the document it commits — the document is the content.
The summary is plain language, and a body is written only when it adds something.
`thrum commit --summary <text> -- <paths>` infers the event from the path and refuses two documents
of different events in one commit, because one commit is one event.
