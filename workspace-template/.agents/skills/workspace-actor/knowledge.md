# Durable knowledge: conventions, topics, retirement

What outlives a job, and where does it go? Jobs come and go. Three things outlive them, and each
has a different job to do.

| | Instructs or describes | Lives in | Belongs to |
| --- | --- | --- | --- |
| **Convention** | instructs — a rule you must follow | `doc/convention/` | the project |
| **Topic** | describes — how something works, and what stays true | `doc/topic/` | the project |
| **Code document** | either, but it goes stale with a component | the code repository, in a worktree | the project |

A plan or a status is none of these: it belongs in a brief. An investigation log is none of
these either: it belongs in a handoff, and a topic is what survives it, written deliberately
afterwards.

## Conventions — `doc/convention/<nn>-<slug>.md`

A convention is a rule you must read. They are read **in number order**, and the order is the
priority.

- **No front-matter.** A convention states a rule rather than a record of state, and the one
  thing it has to say about itself — its number — is in its name.
- **Exactly one H1 title**, on the first line, so a retirement notice has a defined position.
- **The body is the rule in the imperative, with the reason it exists.** A rule whose reason is
  missing is a rule that will be deleted by accident. This file is written that way on purpose.
- **A workspace starts with none.** `doc/convention/` exists and is empty, and everything in it
  is the project's. thrum never writes or rewrites one. A new convention takes the highest
  number in use plus one (see `names.md`).

### The values block

Some rules need a value a tool has to act on — how long a job's payload is kept, for instance.
The value belongs beside the rule that uses it rather than in a configuration file of its own,
because everyone in the workspace should see the same one and a change to it should be
auditable. So a convention may carry **one fenced block**, whose keys name thrum and the
value:

````markdown
```values
collect.window: 14d
```
````

- **The fence opens at the left margin.** An indented fence is how an example of one gets
  written inside a list item, and an example must not declare anything.
- **Keys are thrum's, so they are dotted**: `collect.window`, not `window`. A key no tool
  reads is a key for the reader, and is allowed.
- **Values are scalars.** A mapping or a list in a values block is a document in the wrong place.
- **One block per convention, and one convention per key.** Two conventions declaring one key
  would have the workspace saying two things at once, and which one a tool acted on would depend
  on the order the files were read in.
- **A value thrum reads but cannot parse is an error, not a default.** A policy nobody can
  read is a policy nobody has, and falling back quietly would hide the mistake.

`thrum verify` checks all of it: the file name, the one title, the block, and the values.

## Topics — `doc/topic/<topic>/`

A topic is durable knowledge about the subject matter: how something works, why it is the way it
is, and what has to keep being true. It is branch-independent — it lives in the line, so it is
always current — and it outlives the components it describes.

```text
doc/topic/<topic>/
    overview.md          # required: what this topic covers
    <name>.md            # further documents, named freely and never numbered
    <subdir>/            # optional, to organise this topic's own documents
```

- **The first path component is the topic; everything below it is that topic's own
  organisation.** Topics do not nest: in `topic/uploader/retry/backoff.md` the topic is
  `uploader`, and `retry/` is a grouping inside it.
- **`overview.md` is required**, says what the topic covers, and does not enumerate its own
  files — `ls` does that.
- **Documents inside a topic are never numbered.** Numbering is for conventions, where the order
  carries priority.
- **Promotion is deliberate.** Knowledge becomes a topic when it stops being specific to one job.

### Is this a topic? The test that decides it

The boundary with code documentation is not the subject — a topic may well discuss an API. It is
**what makes the document wrong**:

> Would this document become wrong if a single component were rewritten?

A signature list goes stale the moment a signature changes, so it belongs with the code. An
explanation of the contract the signature has to satisfy stays true while the implementation is
rewritten, so it is a topic.

The other question is whose question it answers:

> Would only this job, or this stretch, ever need this?

If yes, it is not a topic. A log of what you tried, a decision you made for this job, a status, or
a note for whoever continues **this** work belongs in the handoff. A topic is written for the next
job that needs the same understanding: if there is no next job, there is no topic.

| Not a topic | Where it belongs |
| --- | --- |
| An API reference or a component design document | next to the code, in the worktree, reviewed with the change |
| A rule or an obligation | `doc/convention/` — a convention instructs, a topic describes |
| A plan or a status | `task/<job-id>/brief.md` |
| An investigation log | `handoff/` — a topic is what survives the investigation |
| A decision, a log or a status about this job only | `handoff/` — the next job is not the audience |
| A restatement of the code's comments | nowhere; delete it |

## Retirement

A retired document **moves to `doc/archive/`, keeping its relative path**, and gains a notice
immediately after its title. It is not deleted: a retired specification is often the only record
of a decision.

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

- `Retired <date>:` is a fixed prefix and the second line has two fixed forms, so
  `grep -l '^> Retired'` lists every retired document with a tool everyone already has.
- **The replacement is a `doc/`-relative path in backticks, not a link.** The document has
  already moved, so a relative link written before the move is fragile, while a root-relative
  path is independent of position.
- **A retired topic is found through its `overview.md`**, the only one of its files that carries
  a notice.
- **The date is recorded, the person is not.** The date says how stale the reasoning is; git
  already knows who, and the useful answer to "who do I ask" is the replacement path.
- **Reviving a document moves it back and removes the notice.** The notice states the document's
  current status, not a permanent scar; that it was once retired is in git history.
- **Retirement is not closure.** The brief of a finished job is the record of finished work, not
  a document that lost its authority; its `status` already says so, and it carries no notice.
- **Numbers are never reused, so `convention/` may have gaps** — see `names.md`.
- **Retiring a whole topic** moves the directory to `doc/archive/topic/<topic>/` and adds a
  notice to `overview.md` alone; the rest are retired by association, so one retirement gives one
  grep hit rather than one per file.

Retirement is an ordinary act with a discipline — the notice and the reason — and not an approval
step. A project that wants approval says so in a convention, which is what conventions are for.

## Closing a job promotes what outlives it

Before closing, look for something to promote: work that is no longer specific to this job
belongs in `doc/topic/`. This is the moment knowledge either becomes durable or is lost, and it
is easy to skip because the job is finished and the answer is already in your context.
