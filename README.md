# MultiAgentWorkspace

A development repository whose product is a workspace template. The template defines a workspace built on **git worktree** for parallel agent operation: the source code lives in its own repository, and everything else falls into three planes — coordination, assets, and runtime — each with its own lifecycle rule. The workspace is edited by people as much as by agents, so every name and every file stays readable and editable in place; nothing requires a helper tool to be understood.

## This repository

** This is the development repository, not a workspace created from the template.**

| Path | Role |
| --- | --- |
| `README.md` | The design document for a workspace |
| `workspace-template/` | The template from which a workspace is created |
| `scripts/` | Development tooling that creates and maintains workspaces |

A workspace is created from `workspace-template/` by that tooling. Creation is not a plain file copy: `.bare/` and `store/` are bare repositories, and `var/`, `jobs/`, and `worktrees/` are empty directories that must be created.

Development-side files are not part of the design; the two layouts are independent.

## The designed workspace

The rest of this document describes a workspace created from the template, not this repository.

### Workspace tree

The directory name of a workspace is chosen when it is created; `workspace/` below is an example.

```text
workspace/
    .git/                            # Coordination repository: versions doc/ and handoffs/
    .gitignore                       # Ignore rules for the coordination repository
    .bare/                           # Code repository (bare), checked out into worktrees/

    # ── Coordination plane: versioned, identified by job-id, small
    doc/
        conventions/                 # Rules every agent must read; numbering is the order
        tasks/<job-id>/              # brief.md
        topics/<topic>/              # Reference knowledge shared across jobs
        assets/<asset-name>/         # Manifests anchoring store/ and external data
        templates/                   # brief.md, handoff.md, manifest.md
        archive/                     # Retired documents, original layout preserved
        worktrees.yaml               # Declared labels: purpose and one-off flag
    handoffs/<job-id>/<started-utc>.md  # Evidence: append-only, never edited

    # ── Asset plane: durable bytes, content-addressed, never checked out
    store/                           # Asset repository (bare)

    # ── Runtime plane: unversioned, regenerable or disposable
    var/
        locks/                       # Exclusion
        leases/                      # Capacity occupancy: GPUs, ports, other limited resources
        cache/                       # Workspace-wide regenerable caches
        env/<label>/                 # Per-worktree dependency environments
        state/<service>/             # Stateful test services, resettable
        config/                      # Machine-local configuration overrides
        secrets/                     # Machine-local secrets; prefer outside the workspace
    jobs/<job-id>/                   # logs/, tmp/, cache/, data/, ckpt/, out/
    worktrees/<label>/               # A working directory for an area of work; passed between jobs
```

### Repositories

| Repository | Path | Holds | Branches |
| --- | --- | --- | --- |
| Coordination | `workspace/.git/` | `doc/` and `handoffs/` | One branch (`main`), never branched |
| Code | `workspace/.bare/` | Source code, checked out under `worktrees/` | One branch per job |
| Assets | `workspace/store/` | Durable data, published as tags | Never branched |

Each plane gets its own repository because each has a different lifecycle. The code repository is bare so that no checkout duplicates it, and the asset repository is bare for the same reason.

### Version control

- **Only `doc/` and `handoffs/` are tracked by the coordination repository.** `.bare/`, `store/`, `var/`, `jobs/`, and `worktrees/` are listed in `.gitignore` **and** duplicated in `.git/info/exclude`, so that one bad edit to a tracked file cannot expose the source history.
- **The coordination repository never branches.** It is the single shared, always-current source of truth; only the code repository uses one branch per job.
- **Never run a bare `git clean` at the workspace root.** `git clean -xdf` would delete `.bare/` and, with it, the entire source history; use the guarded clean operation supplied by the tooling instead.
- **Keep a backup of the code repository outside the workspace.** Because `.bare/` lives inside the coordination repository's working tree, its safety depends on the ignore rules above, and an external mirror is the only real recovery path.
- **Commit one coordination event at a time.** `store/`, `var/`, and `jobs/` never appear in a commit; artifacts under `jobs/` are referenced from `handoffs/` by raw checksum instead.
- **Assets are published as tags.** A blob that no tag reaches is regenerable by definition and may be collected.

### Commit messages

The code repository's commit conventions are the project's own business, and nothing here constrains them. The coordination repository has one commit per coordination event, and its subject line names the event:

```text
<kind> <subject>: <summary>
```

| Kind | Subject | Example |
| --- | --- | --- |
| `brief` | job id | `brief add-retry-to-uploader: scope narrowed to the uploader only` |
| `handoff` | job id | `handoff add-retry-to-uploader: attempt 2, the flaky timeout remains` |
| `topic` | path relative to `doc/` | `topic upload-protocol: added the retry contract` |
| `convention` | file name | `convention 03-naming: numbered conventions are never reused` |
| `asset` | `name@version` | `asset uploader-baseline@2026-10-09: published from a production sample` |
| `worktrees` | omitted | `worktrees: declared perf and spike-gc, the latter one-off` |
| `retire` | path relative to `doc/` | `retire topics/upload/old-retry: superseded by topics/upload/retry` |

- One message describes one event, matching the commit granularity above.
- A message never restates the document it commits; it says what changed. The document is the content.
- The summary is plain language, and a body is written only when it adds something.

## Placement

Artifacts are placed by a decision procedure, not by a directory list: a list is always overtaken by the next kind of data, a procedure is not.

```text
1. Source code, dependency locks, or documents that must change atomically with code?
       → the code repository (next to the code, or worktrees/<label>/doc/)
2. A record of what a job did?
       → handoffs/  (metrics in front-matter; indexes are derived, never maintained)
3. A secret?
       → never in doc/, handoffs/, or any git repository
4. Must be deletable object by object?
       → keep a reference here; the bytes live in an external system
5. Regenerable at acceptable cost?
       shared across jobs     → var/cache/
       private to one job     → jobs/<job-id>/cache/
       per worktree           → var/env/<label>/
6. Not regenerable?
       small text             → doc/
       large or binary        → store/, with a manifest in doc/assets/
7. Capacity, exclusion, or coordination state?
       → var/
8. Larger or externally governed?
       → outside the workspace, with an immutable locator in doc/assets/
```

### Assets

`store/` is a **bare git repository**, for the same reason `.bare/` is: git already provides content addressing, immutability, deduplication, delta compression between similar versions, integrity checking, transport, and backup. A hand-rolled object directory would reimplement all of that and lose the delta compression that makes comparing a dataset against a future version cheap.

Assets are never checked out permanently. A job materializes the version it needs into its own transient directory:

```bash
git -C store archive <ref> | tar -x -C jobs/<job-id>/data
```

Publishing a version is a tag, which makes it atomic: the blobs are written first, and the tag is what makes the dataset visible. Each asset is described by a small manifest in `doc/assets/<asset-name>/<version>.md`:

```yaml
asset: uploader-baseline
version: 2026-10-09
ref: uploader-baseline@2026-10-09
sha256: 3f9c…              # raw hash, for systems outside git
produced_by: { job: add-retry-to-uploader, commit: 9be77d0 }
```

Record the raw `sha256` as well as the ref: a git object id hashes `blob <len>\0<content>`, so it is not the hash of the file itself, and anything crossing the workspace boundary speaks in raw checksums.

### Boundary

> The workspace is a coordination plane, not a data lake.

Container images, model weights, terabyte-scale corpora, externally governed or licensed data, anything that must be deleted object by object, and secrets stay in dedicated external systems: a registry, an object store, a secret manager. The workspace keeps only an immutable locator for them, in the same manifest. "Everything unversioned must be anchored by something versioned" extends to: everything outside the workspace must be anchored by a locator inside it.

### Loss model

| Layer | Regenerable | Has history | Backup |
| --- | --- | --- | --- |
| `.bare/` | No | Yes | A mirror outside the workspace |
| `doc/`, `handoffs/` | No | Yes | Pushed to a remote |
| `store/` | **No** | **No** | **Pushed to a remote — the layer that most needs it** |
| `var/`, `jobs/`, `worktrees/` | Yes | No | None needed |

## Document classification

### Coordination plane

| Location | Contents |
| --- | --- |
| `workspace/doc/conventions/` | Rules every agent must read, in order |
| `workspace/doc/tasks/<job-id>/` | The brief for one job: goal, scope, acceptance criteria, owned paths |
| `workspace/doc/topics/<topic>/` | Reference knowledge shared across jobs |
| `workspace/doc/assets/<asset-name>/` | Manifests anchoring stored and external data |
| `workspace/doc/templates/` | Copy-me templates for briefs, handoffs, and manifests |
| `workspace/doc/archive/` | Retired documents, original layout preserved and a notice added |
| `workspace/handoffs/<job-id>/` | Append-only records of what each attempt did |
| `workspace/doc/worktrees.yaml` | Declared labels: what each working directory is for, and whether it is one-off |
| `workspace/worktrees/<label>/doc/` | Design, API, and other documents tightly coupled to the source code |

### Asset plane

| Location | Contents |
| --- | --- |
| `workspace/store/` | Durable data, content-addressed and published as tags; never checked out |

### Runtime plane

| Location | Contents |
| --- | --- |
| `workspace/var/locks/` | Exclusion state |
| `workspace/var/leases/` | Capacity occupancy: GPUs, ports, other limited resources |
| `workspace/var/cache/` | Workspace-wide regenerable caches |
| `workspace/var/env/<label>/` | Per-worktree dependency environments |
| `workspace/var/state/<service>/` | Stateful test services, resettable between jobs |
| `workspace/var/config/` | Machine-local configuration overrides |
| `workspace/var/secrets/` | Machine-local secrets; prefer keeping them outside the workspace |
| `workspace/jobs/<job-id>/` | Transient payload: logs, scratch, per-job cache, data, checkpoints, artifacts |
| `workspace/worktrees/<label>/` | A labelled working directory for an area of work; durable unless the registry marks it one-off |

- Keep the `doc/` hierarchy organized by task and topic. The shape of the tree is the index; listings are read with `ls`, never maintained by hand.
- A file is a document only when it carries a decision. Content that is identical in every workspace is a template, not a document; content that changes with the shape of the tree is derived and is never written by hand.
- Start an agent by injecting its context: the workspace root, the job id, and the reading order (`conventions/` in numeric order, then its own `brief.md`). Do not rely on a file being auto-loaded — the source repository may ship its own `AGENTS.md`, which would shadow the workspace's inside a worktree.
- Retired documents move to `doc/archive/` rather than being deleted: a retired specification is often the only record of a decision.
- Files under `jobs/` are never versioned, and anything that must outlive a job belongs in `store/`.
- A worktree label names an area of work, never the job occupying it, and is created and removed through the tooling, by a person or an agent. `doc/worktrees.yaml` records what each label is for and whether it is one-off; the current occupant is never recorded there, because `git worktree list` already reports it — the checked-out branch is `job/<job-id>`, and a worktree at rest holds no job branch.
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
| Agent | `slug`, a stable worker identity rather than a process | `claude-opus` |

### Invariants

1. **One character set**: lowercase ASCII letters, digits, and hyphens. No spaces, no uppercase, no underscores, and no dots outside file extensions.
2. **Names are short and shell-safe**, because people type them.
3. **Job ids, asset versions, and convention numbers are taken forever.** Once used, none of them is reused, even after the job closes, the asset is withdrawn, or the convention is retired; a job id's uniqueness is checked against both `doc/tasks/` and `doc/archive/`. A worktree label is the deliberate exception: it is reused by design, so its meaning must stay fixed — a label is never repurposed for a different area of work.
4. **Names are reversible**: `job/` + job id ↔ job id; asset tag ↔ manifest path; handoff file name ↔ `started`; worktree label ↔ directory name.
5. **Names never describe state.** A job id carries no date, owner, or status; a worktree label carries no occupant.
6. **`@` and `/` are reserved separators** and never appear inside a slug.
7. **Every name is at most 64 characters**, so it is safe as a single path component.
8. **Time has two forms**: file contents use RFC 3339 (`2026-10-09T02:10:00Z`) and names use the hyphen form (`2026-10-09T02-10-00Z`). They convert mechanically.

### What the rules make checkable

The naming rules are also the validator for the formats defined later:

- a brief's `job` equals its directory name;
- a handoff's `branch` equals `job/` plus its directory name;
- a handoff's `started` equals its file name converted;
- a manifest's `asset` and `version` equal its directory and file names;
- an asset tag equals `asset` + `@` + `version`;
- every label in `doc/worktrees.yaml` has a directory under `worktrees/`, and the reverse.

### Out of scope

Branch names owned by the project itself (`main`, release branches), the name of the workspace directory, and lease instance ids — the last of these belongs to the runtime state whose format is still open.

## File formats

Every structured file in a workspace is either YAML front-matter plus a Markdown body, or — when there is nothing to say in prose — pure YAML with a comment header: the front-matter is what tools and agents read, the body is what people read. The formats are defined here so that an agent can write a well-formed file without inspecting an example.

### Common rules

- Front-matter is delimited by `---` and must be valid YAML; the body is Markdown. A pure-YAML file has neither: its top level is the record itself.
- Every document has exactly one H1 title, on its first line after any front-matter. This is what gives a retirement notice a defined position.
- Field names are lower snake case, and every `status` is a closed enumeration of lower-case values.
- A field that names a path says which root it is relative to. Paths are never absolute.
- No secrets in any versioned file: `doc/`, `handoffs/`, and the code repository are permanent.
- Git already records authorship and commit times, so they are not duplicated — except where the meaning of a record depends on the time, as in a handoff's attempt boundaries or a retirement notice.
- One record per file. Nothing in the coordination plane is a file that every agent writes.

### Retirement notice

A retired document moves to `doc/archive/`, keeping its relative path, and gains a notice immediately after its title. The notice answers the only question a reader has when arriving from a stale link: is this still true, and where should I look instead.

```markdown
# Retry policy

> Retired 2026-10-09: the retry policy moved into the uploader itself.
> Superseded by `topics/uploader/retry.md`.
```

When nothing replaces it, say so rather than leaving the line out:

```markdown
# Batch mode

> Retired 2026-10-09: batch mode was removed from the product.
> No replacement.
```

`Retired <date>:` is a fixed prefix and the second line has two fixed forms, so `grep -l '^> Retired'` lists every retired document with a tool everyone already has. The date uses the same calendar form as an asset version, because a retirement is a person's decision and does not need a time of day.

- **A notice, not front-matter.** Recording the same facts in front-matter as well would create a second version of the truth that can drift; front-matter alone would not be readable in place.
- **The replacement is a `doc/`-relative path in backticks, not a link.** The document has already moved, so a relative link written before the move is fragile, while a root-relative path is independent of position.
- **The date is recorded, the person is not.** The date says how stale the reasoning is; git already knows who, and the useful answer to "who do I ask" is the replacement path.
- **Reviving a document moves it back and removes the notice.** The notice states the document's current status, not a permanent scar; the fact that it was once retired is in git history.
- **Retirement is not closure.** The brief of a finished job is the record of finished work, not a document that lost its authority; its `status` already says so, and it carries no notice.
- **Numbers are never reused, so `conventions/` may have gaps.** A retired `01-placement.md` moves to `doc/archive/conventions/01-placement.md` and its number stays taken, because reusing it would break every reference. A gap means "there was one, and it is archived".

### Topics

A topic is durable knowledge about the subject matter: how something works, why it is the way it is, and what has to keep being true. It is branch-independent — it lives in the coordination plane, so it is always current — and it outlives the components it describes.

A topic is not:

| Not a topic | Where it belongs |
| --- | --- |
| An API reference or a component design document | Next to the code, in the worktree, so that the change and the document are reviewed together |
| A rule or an obligation | `conventions/` — a convention instructs, a topic describes |
| A plan or a status | `tasks/<job-id>/brief.md` |
| An investigation log | `handoffs/` — a topic is what survives the investigation, written deliberately afterwards |
| A restatement of the code's comments | Nowhere; delete it |

The boundary with code and API documentation is not the subject, because a topic may well discuss an API. It is **what makes the document wrong**. A signature list goes stale the moment a signature changes, so it belongs with the code; an explanation of the contract that the signature has to satisfy stays true while the implementation is rewritten, so it is a topic. This is the placement rule applied to the workspace rather than to one file: *would this document become wrong if a single component were rewritten?* A topic must stay true across versions, which is exactly why it has no home on any branch.

```text
doc/topics/<topic>/
    overview.md          # required: what this topic covers
    <name>.md            # further documents, named freely and never numbered
    <subdir>/            # optional, to organise this topic's own documents
```

- **The first component is the topic; everything below it is that topic's own organisation.** Topics do not nest: in `topics/uploader/retry/backoff.md` the topic is `uploader`, and `retry/` is a grouping inside it.
- **`overview.md` is required.** It says what the topic covers, and it does not enumerate its own files; `ls` does that.
- **Documents inside a topic are never numbered.** Numbering is for conventions, where the order carries priority.
- **Promotion is deliberate.** Knowledge becomes a topic when it stops being specific to one job.
- **Retiring a whole topic** moves the directory to `doc/archive/topics/<topic>/` and adds a notice to `overview.md` alone; the rest are retired by association, so one retirement gives one grep hit rather than one per file.

### Brief — `doc/tasks/<job-id>/brief.md`

The brief states the intent of one job, and it is the only mutable document in the coordination plane.

```yaml
---
job: add-retry-to-uploader
status: active                   # open | active | blocked | done | abandoned
branch: job/add-retry-to-uploader
owned_paths: ["src/upload/**", "tests/upload/**"]
owner: claude-opus               # optional
depends_on: [fix-ci-timeouts]    # optional
keep: false                      # optional; true protects this job's payload from collection
---
```

| Field | Required | Meaning |
| --- | --- | --- |
| `job` | Always | Must equal the containing directory name |
| `status` | Always | The lifecycle of the job itself; one of the five values above |
| `branch` | Always | The branch in the code repository that implements this job |
| `owned_paths` | Always | Paths this job touches exclusively, relative to the code repository root; may be empty for analysis-only jobs |
| `owner` | Optional | Who is responsible; absent while the job is unassigned |
| `depends_on` | Optional | Job ids that must finish first |
| `keep` | Optional, default `false` | When true, the job's payload under `jobs/` is never collected |

`owned_paths` is the cheapest conflict avoidance there is: a conflict prevented at allocation costs nothing, and one resolved at merge time costs a great deal. An attempt that touches anything outside its `owned_paths` must say so in its handoff.

The brief also decides the fate of the job's payload: the payload becomes collectible once `status` is `done` or `abandoned`, unless `keep` is true. That decision lives here rather than in `jobs/` because a marker stored inside the payload cannot protect the payload.

The body has three sections, in this order, with additional sections allowed after them:

```markdown
## Goal
## Scope
## Acceptance criteria
```

A brief is edited as understanding changes, and every edit is committed, so the history shows when the scope moved. This is the opposite of a handoff, which is immutable once read. Its `status` is the one field in the coordination plane that a person has to keep current. A plan, when one is needed, is one of the additional sections rather than a separate file.

The brief's statuses are not the same enumeration as a handoff's: a job is `abandoned` when it will not be done, while an attempt is `aborted`; a job is `blocked` while its latest attempt is blocked.

### Handoff — `handoffs/<job-id>/<started-utc>.md`

A handoff records one attempt. It lives at `handoffs/<job-id>/<started-utc>.md`, where the timestamp is the attempt's start time in UTC with `:` replaced by `-`. The directory already carries the job id, so the file name only has to identify the attempt — and it uses the start time rather than the time of writing, because it labels the occupancy, not the write.

One file per attempt, carrying machine-readable front-matter and a human-readable body. There is no `.md`/`.json` pair; two files drift apart.

Attempts on one job are serialized: a job owns one branch, and the next attempt reads the previous handoff before starting. If the target file name already exists, the writer fails rather than overwrites — never overwriting is the minimum requirement of an append-only record.

#### Front-matter

| Field | Required | Value | Meaning |
| --- | --- | --- | --- |
| `job` | Always | string | Must equal the containing directory name |
| `attempt` | Always | integer from 1 | The nth attempt; one more than the number of `.md` files already in the directory |
| `agent` | Always | string | Stable identity of the writer |
| `branch` | Always | string | Branch in the code repository |
| `base` | Always | commit id | The commit the attempt branched from |
| `head` | Always | commit id | The commit at handoff time; equal to `base` if nothing was committed |
| `status` | Always | `done` \| `partial` \| `blocked` \| `aborted` | See below |
| `started` | Always | RFC 3339 UTC | Must match the file name |
| `ended` | Always | RFC 3339 UTC | Time of handoff |
| `next` | Always | string or `none` | The baton; must be `none` when `status` is `done` |
| `worktrees` | Optional | list of labels | The labelled working directories the attempt worked in |
| `supersedes` | When correcting | file name | A sibling record this one replaces |
| `blocked_by` | When `blocked` | list | What the attempt is waiting on |
| `owned_paths` | Optional | list, relative to the code repository root | Paths actually touched, for comparison against the brief |
| `metrics` | Optional | map of numbers | Counts, coverage, durations; the source of performance history |
| `artifacts` | Optional | list of `{path, sha256, bytes}` | Paths relative to the workspace root; `sha256` is the raw file hash |
| `assets` | Optional | list of `name@version` | Assets published to `store/` by this attempt |

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

1. **Commit before handing off.** `head` must point at an existing commit, so the order is: commit first, then write the handoff.
2. **Every attempt writes one**, failed attempts included. An attempt that crashed without writing leaves a gap, and the gap is itself information.
3. **Never edit a handoff once it has been read downstream.** Corrections are new files carrying `supersedes`; chains are allowed, cycles are not. A correcting record may be a degenerate attempt with no code change.
4. **Never delete a handoff.**
5. Writing a handoff is one coordination event, and therefore one commit.

#### Validation

A handoff is well formed when `job` equals the directory name; `started` equals the file name; `attempt` equals the number of `.md` files in the directory; `status` is in the enumeration; `next` is present and is `none` exactly when `status` is `done`; `blocked_by` is present exactly when `status` is `blocked`; `base` and `head` resolve in the code repository; `supersedes` names an existing sibling without forming a cycle; and every `artifacts[].sha256` is 64 hexadecimal digits.

#### Derived views

- **The latest attempt's status** is the front-matter of the last file in `handoffs/<job-id>/`; the job's own status is its brief's. Together they are the task board, and no separate board is maintained.
- **Performance history** is the `metrics` fields aggregated across handoffs, so no shared mutable table is needed either.
- **Artifact provenance** runs from `artifacts` into `jobs/`, and from `assets` into `store/`.

Artifacts under `jobs/` are subject to the retention policy: their `sha256` records what was lost but cannot recover it. Anything that must outlive the retention window is published to `store/` first and referenced under `assets`.

#### Example

````markdown
---
job: add-retry-to-uploader
attempt: 2
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
  - path: jobs/add-retry-to-uploader/out/coverage.html
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
- Pasted logs: point at a file under `jobs/` with `artifacts` instead.
- A restatement of the brief: a handoff is evidence, not a copy of the intent.
- Plans beyond `next`: those belong in a new brief.
- The status of other jobs: that would recreate shared mutable state.
- Absolute or machine-local paths: paths are relative to the workspace root (`artifacts`) or to the code repository root (`owned_paths`).

### Asset manifest — `doc/assets/<asset-name>/<version>.md`

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
| `sha256` | Always | The raw hash of the content |
| `bytes` | Always | The size of the content |
| `produced_by` | Optional | The job and commit that produced it; absent for externally sourced data |
| `source` | Optional | Where external data came from |
| `license` | Optional | The terms that govern reuse |

The body has one required section, `## Description`, with additional sections allowed after it.

The identity fields — `location`, `ref`, `sha256`, `bytes` — freeze on publication. Correcting a descriptive field in place is allowed, but changing identity means a new version. Note that `sha256` is not the git object id: git hashes `blob <len>\0<content>`. Both are recorded, because anything crossing the workspace boundary speaks in raw checksums.

### Job payload — `jobs/<job-id>/`

The payload is everything a job produces on disk: its scratch space, its caches, its logs, its inputs, its checkpoints and its outputs. It is unversioned, scoped to one job, and — by construction — disposable.

```text
jobs/<job-id>/
    logs/     # stdout and stderr worth keeping
    tmp/      # scratch; safe to delete at any time
    cache/    # regenerable; safe to delete at any time
    data/     # transient inputs only
    ckpt/     # resume state for a long run
    out/      # artifacts; not a delivery mechanism
```

Nothing here is irreplaceable. Whatever must survive is either reproducible or has been published to `store/` and referenced from a handoff, and that is what makes collection safe.

**Collection.** A payload becomes collectible once its brief's `status` is `done` or `abandoned`, unless the brief sets `keep: true`. Keying the rule on the brief's status rather than on the presence of a directory keeps it independent of where closed jobs are filed. The subdirectories differ in what they cost to lose:

| Subdirectory | Collected |
| --- | --- |
| `tmp/`, `cache/`, `ckpt/` | On close |
| `logs/`, `data/`, `out/` | After the retention window, measured from the close |

The window is a workspace setting, and `keep: true` is the only per-job override. Collection never makes a record false: a handoff keeps the raw `sha256` of every artifact it names, so a collected artifact is a detectable absence rather than a silent one. That is also why `out/` is not a delivery mechanism — a result that has to survive is published to `store/` and referenced under `assets`.

### Worktree registry — `doc/worktrees.yaml`

The labelled working directories that exist, and what each is for. It is pure YAML, with no prose body, because there is nothing to say about it that is not a field. Two authorities share this job and are reconciled rather than trusted blindly: `git worktree list` is the authority on what physically exists and which branch each worktree holds, and this file is the authority on intent — the purpose of a label and whether it is one-off.

```yaml
# The working directories that exist, and what each is for.
worktrees:
  - label: uploader
    purpose: Uploader reliability work
    ephemeral: false
  - label: perf
    purpose: Performance experiments
    ephemeral: false
  - label: spike-gc
    purpose: One-off look at GC pauses
    ephemeral: true
```

| Field | Required | Meaning |
| --- | --- | --- |
| `label` | Always | The directory name under `worktrees/` |
| `purpose` | Always | What the working directory is for, in a person's words |
| `ephemeral` | Always | `true` when the worktree exists for a single use and may be destroyed on release without further checks; `false` when it is durable and cleaned for reuse |

Labels are created and removed through the tooling, by a person or an agent, and each change is one coordination event and therefore one commit. Removing a worktree removes its entry: this file is a snapshot of current intent, and git's history is the ledger of what once existed. An entry whose directory is gone is stale, and a directory with no entry is undeclared; the tooling reports both rather than tolerating either.

### Templates

`doc/templates/` holds one copy-me file per record type defined in this chapter, named after it: `brief.md`, `handoff.md`, `manifest.md`. A template is a convenience and never the authority — this chapter is. When a format changes, its template changes in the same commit.

### Not yet defined

The following are left open deliberately rather than guessed at:

- **Runtime state under `var/`** — the formats of `locks/`, `leases/`, `state/` and `config/`. These belong with the deferred tooling, together with the mechanism that maintains them.
- **`cache/`, `env/<label>/` and `secrets/` have no record format at all.** They are directories of files, described by no schema; saying so here is a statement that nothing is missing rather than an omission.
- **The set and format of `doc/conventions/`** — which conventions exist, and how they are written.
- **The retention window** that collection measures from a job's close. It is a workspace setting, and the file that holds workspace settings is itself part of the deferred runtime.
- **The startup context injected into an agent**, discussed together with the conventions because both are pushed to every agent.
- **The tooling** — where the runtime operations live, and how an instantiated workspace receives updates to the template.
