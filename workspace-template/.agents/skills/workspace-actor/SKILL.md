---
name: workspace-actor
description: Work one job in a thrum workspace as an actor — the workspace's own rules, the loop that ends in a handoff, and where each answer lives.
whenToUse: You are in a thrum workspace (a directory holding repos/, handoff/, var/, job/ and worktree/), or you have been given a workspace root and a job id.
---

# Working a job

You are an **actor** — a person or an agent — and the workspace knows you by a name. You were given
two facts: the workspace root and a job id. Everything else is here or in the workspace.

This skill states **the workspace's own rules**. Two other kinds of rule live in the workspace, and
neither is restated here:

| A rule about | Lives in | Read it |
| --- | --- | --- |
| the workspace itself — names, placement, records, actors | this skill | now |
| **this project's** policy, and any value thrum must act on | `doc/convention/` | before you work, in number order |
| **this job's** intent | `doc/task/<job-id>/brief.md` | first |

Where this skill and a convention disagree about a project's policy, the convention wins. Where
either disagrees with the brief about this job, the brief wins.

**The files beside this one are these rules in detail**, one question each, and they are not
optional reading when their question comes up: `actors.md`, `messages.md`, `names.md`,
`placement.md`, `records.md`, `knowledge.md`, `assets.md`, `sites.md`. The loop below names the file
at each step that reaches one.

## Seven things that explain the rest

Read these once. Nearly every rule below follows from them, which is why a situation this skill does
not name still has a right answer.

1. **Three planes, three lifecycles.** `doc/` and `handoff/` are decisions and evidence, versioned
   and shared. The store (`repos/store.git`) is durable bytes, content-addressed and never checked
   out. `var/`, `job/` and `worktree/` are working state: unversioned, machine-local, disposable.
   Getting a thing into the wrong plane is the most common way to lose it.
2. **A job holds the runtime, and an actor does not.** A job owns a branch, a working directory and
   a payload. An actor is a name that picks one up or hands it over. That is why a name is never a
   permission, and why nothing is assigned to you.
3. **The records are the truth, and the views are derived.** No board, no status file, no index is
   kept by hand. A job's status is its brief's; whether it is blocked is its newest handoff's; where
   the work is, is what `thrum where --job <job-id>` prints. A derived thing written down is a
   second version of the truth, and it drifts.
4. **Two shared histories, and both merge.** The **line** (`doc/`) holds the decisions, and no two
   sites write one file there: a brief has the one site working its job, and a name claimed by two
   sites at once is settled by an order every site derives the same way. The **ledger**
   (`handoff/`) holds the evidence and is append-only, so it merges by the union of new files — a
   handoff is a new file every time. Nothing that has been seen is ever rewritten — `sites.md` has
   the rest.
5. **A name is a permanent identifier.** It never encodes state that can change, and job ids,
   repository names, actor names, site names, asset versions and convention numbers are never
   reused. So *used once* is enforceable rather than promised.
6. **Everything you write is inside the workspace, or it is a mistake.** `var/` belongs to thrum,
   `job/<job-id>/` is the payload, and the only place code is edited is the directory thrum names —
   *What you may touch* in `AGENTS.md` is the whole of it.
7. **thrum refuses what must not happen, and the refusal says why.** A rule it enforces is one less
   rule to carry: you meet it as a refusal at the moment it matters, rather than having to remember
   it.

## Before anything: your name

You are an actor, and the workspace records which actor did the work: a handoff's `agent`, a job's
`owner`. Set your name before your first command:

```sh
export THRUM_ACTOR=agent-3f9c     # or --actor <name> on every command
```

- A slug: lowercase letters, digits and hyphens, like every other name here.
- If you were given a name, that is your name.
- If your commands do not share a shell, the variable does not reach them — pass `--actor <name>` on
  each one, with the same value.
- Without `--actor` or `$THRUM_ACTOR`, thrum records `$USER`.
- **It names this actor, and nothing that fits everybody.** A project's name, a task, an area of
  work, a machine, the harness and the model are names any actor here could take, and records under
  them read as one actor's — `names.md` has the rule and its reason.
- **A name must be declared before it can write anything:**
  `thrum actor declare <name> --kind agent|human`, once. `thrum actor hello` then says where your
  mail is now. There is no session and nothing to end — `actors.md` has both.

## What you may touch

Five things, and nothing else — the table is in `AGENTS.md`, and it is worth reading twice, because
the failure it prevents is silent. In one line each: the directory thrum names; the payload at
`job/<job-id>/`; the records you write; nothing under `var/`; nothing outside the workspace root.

## The loop

1. **Locate.** `thrum where --job <job-id>` prints the directory holding `job/<job-id>`, which is
   where you work, and `thrum job show <job-id>` says everything else about the job in one place. No
   directory here means the job is unstarted, already closed, or its work has moved to another site:
   `thrum job list` and `ls handoff/<job-id>/` say which. Never work in the workspace root, in
   `var/`, or in `job/<job-id>/`.
2. **Read.** `doc/task/<job-id>/brief.md`, then the newest file in `handoff/<job-id>/`. Its `next`
   is the baton: what the last actor decided you do first, and usually the whole reason you are
   being asked. Anything not in your working tree — a document on another branch, a closed job's
   brief, a commit you are comparing against — is read, not checked out.
3. **Read this project's rules.** `doc/convention/`, in number order. The order is the priority. An
   empty directory means this project has written no policy yet: say that in your handoff rather
   than inventing a rule for it.
4. **Check your mail.** `thrum message list`, and `thrum message read <id>` for one. A message is
   process information and is never in a handoff, so this is the only place it arrives — and it is
   often why the last actor stopped. `messages.md` has sending and reading.
5. **Take it up.** `thrum work <job-id> --repo <name> --label <label>` opens the job when it is not open yet,
   claims it when it has no owner, and materialises a working directory when this site has none; it
   ends by printing the directory and the environment line to run. There is no claim to take and
   none to vacate: whether an actor is working cannot be observed, and `actors.md` says what stands
   in its place. A job owned by somebody else is worked under that owner's authority; if the brief
   is unclear, ask them.
6. **Point your tools inside the workspace.** `eval "$(thrum env --job <job-id>)"` before running
   anything. A tool run without it writes outside, where nothing collects what it leaves.
7. **Work.** Stay inside the paths the brief's `touches` describes, and commit to `job/<job-id>`.
   `touches` is a **description, not a lock**: it says what you expect to touch, written where
   everyone can read it, and touching something outside it is allowed — say so in your handoff,
   because the point of the field is that the next reader can see what you expected. A collision
   between two jobs is a merge conflict in two branches at integration, which is the project's to
   see, not a permission for you to obtain. Keep everything that is not source outside the
   worktree: scratch, logs and a tool's output go in `job/<job-id>/`, which is what `thrum env`
   points `TMPDIR` at. `placement.md` says where a thing goes; `records.md` has the two records you
   write.
8. **Commit, then hand off.** `head` has to name an existing commit, so the commit comes first.
   Then `thrum job handoff <job-id> --status <done|partial|blocked|aborted> --next '<first thing
   next>'` — one handoff is one coordination event, and therefore one commit. `records.md` has the
   shape, the sections, and what is refused.
9. **Close, or leave the baton.** `thrum job close <job-id> --status done` only when the acceptance
   criteria hold, and `--status abandoned` when they will not. It is a second event, in the line:
   the handoff that ends the stretch comes first, in the ledger, and a crash between them leaves
   the job open with its final evidence written. Closing merges nothing: the branch stays in its
   repository for whoever integrates. Before closing, look for something to promote — a fact
   that outlives the job and does not depend on how it was solved — to `doc/topic/`.

## If you are opening the job rather than taking it up

Opening writes the brief, creates the branch and materialises a labelled working directory, and it
is one event — unless the label has not been declared yet, because declaring a label is a different
event of its own. **`thrum work` does this and takes the job up in one act** when you are the one
who will work it; `thrum job open` is the same act without taking it up, which is what opening a job
for somebody else looks like.

```sh
thrum job open add-retry-to-uploader --repo code --label uploader \
    --purpose 'uploader reliability' \
    --goal 'retry the uploader instead of failing it' \
    --scope 'the uploader only' \
    --criteria 'a flaky upload succeeds on the second attempt' \
    --owner <actor> --touches 'src/upload/**,tests/upload/**'
```

`--purpose` is the **label's** own, and `--goal`, `--scope` and `--criteria` are the **brief's**.

- **The job id is taken forever**, and checked against both `doc/task/` and `doc/archive/`.
- **The label names the area of work, never the job**, and it is declared at
  `doc/worktree/<label>.yaml` at the first `thrum worktree add` of that label. A label is free when
  `thrum worktree list` shows no job branch in it, and it is never taken from under the job holding
  it.
- **`--owner` may name somebody else.** The owner is who the work is promised to, not necessarily
  who does it. Leaving it out makes the job unowned, and therefore claimable by whoever takes it up.
- **`--depends-on` is checked before work starts**, not after it fails.
- **`--keep` protects this job's payload** from collection after it closes.
- `--touches` and `--depends-on` are comma-separated. `--brief <file>` (or `-`) reads the body from
  a file when the three flags are not enough.

The brief's body belongs to whoever is working the job; the `owner` field is the owner's to change.

## Seeing the state of things

`thrum status` prints every derived view at once — each brief's status with its newest handoff,
which labels this site has materialised and what each holds, which payloads are collectible, and
this actor's mail. Also: `thrum job show <job-id>` for one job, `thrum actor list` for the register,
`thrum site doctor` for the fabric, and `thrum verify` for every mechanical check — it reports and
never repairs, so it is always safe to run when you are unsure whether the workspace is coherent.

## What to read, and when

| When you are about to | Read |
| --- | --- |
| declare a name, say hello, own or claim a job, or ask who is on one | `actors.md` |
| send, read, reply to or purge a message | `messages.md` |
| put a file somewhere, or wonder if it belongs in the worktree | `placement.md` |
| write or edit a brief or a handoff, or judge one you are reading | `records.md` |
| name anything — a job, a repository, a label, a site, an actor, a convention, an asset version | `names.md` |
| write a convention, start or promote a topic, retire a document | `knowledge.md` |
| keep a result that has to outlive the job | `assets.md` |
| touch another site, carry a payload, or meet a refusal | `sites.md` |

## When thrum is wrong

**This format and thrum are under development**, and there are no releases yet. A refusal is still
the design being enforced, so trust it first — but a command that fails unexpectedly, a refusal that
contradicts the records, or a file thrum cannot read is a defect, and it is not yours to patch from
inside a job.

Keep the evidence rather than working around it in silence:

- **Write down what happened** under `job/<job-id>/log/`: the exact command, its whole output, and
  anything it left behind.
- **Name it in your handoff** with `--artifact`, so its raw `sha256` is recorded. The file is
  collected with the payload; the hash is not, so the loss becomes detectable rather than silent.
- **Say it in the handoff.** A defect is not a job, so `blocked_by` cannot name it: write what you
  ran, what you expected, what came back, and what you think should change, under
  `## Open questions` or `## What remains`.
- **Choose the status the work actually reached** — `partial` if you got somewhere despite it,
  `aborted` if it produced nothing usable. Never `blocked` for a defect, because there is no job to
  wait on.
- **Tell whoever runs you.** The workspace has no issue tracker, and how you report back is between
  the two of you.

A workaround nobody wrote down is a workaround nobody can remove.

## Ending a stretch

Write a handoff whenever you stop — to hand over, for the day, because you are blocked, because your
context is nearly full, or because the job is finished — and not before. Work that runs for many
rounds without stopping needs nothing in between. `records.md` has the front-matter, the four
required sections, and the rules that are not negotiable.

Two of the four sections are easy to lose and matter most: **what was verified** separates *I
changed it* from *I proved it works*, and **open questions** is the most useful thing you can hand
over.

Never paste a log into a handoff — name a file under `job/` with `--artifact` and its raw `sha256`
is recorded. Never put a secret in one: it is versioned and permanent. Never edit one that somebody
downstream has read: a correction is a new handoff carrying `supersedes`.

Look at your mail once more before you hand off: a message that arrived mid-stretch is not in the
handoff, so whoever continues will not know it was ever sent.

## Stopping without spinning

Stop and record it rather than spinning. `--status blocked --blocked-by <job-id>` costs one handoff;
an actor that spins costs a context window and tells whoever continues nothing.

- A job in `depends_on` is satisfied only when its brief says `done` — and its brief may have moved
  to `doc/archive/task/`, because finishing moves it. An `abandoned` dependency is **not** satisfied:
  the premise it stood for is gone, and only the job's owner decides what that means.
- **`aborted`** is for a stretch that produced nothing usable: a crash, a wrong approach, a
  superseded plan. It is not the same as `blocked`, and neither is `abandoned`, which is a decision
  about the whole job.
- **You cannot proceed at all:** stop and record it. That is one handoff.

## What you decide alone, and what you do not

**Alone:** everything inside the brief — how to implement it, what to verify, when to hand off, what
`next` says, and closing the job once its acceptance criteria hold. Also: declaring a topic or an
asset; retiring a document; claiming a job that has no owner; giving up a job you own; and sending
another actor a message.

**Ask instead:** retiring a convention. It belongs to the project rather than to any job, and its
discipline is the notice and the reason, not an approval step.

**Ask the owner:** abandoning a job you do not own, and anything about a job they own that the brief
leaves unclear. The owner is who the work was promised to.

**Between you and whoever runs you:** how much to do before handing over, and how you report back.
The workspace deliberately has no opinion about either.

## What you do not have to remember

thrum refuses what must not happen, and the refusal says why. You do not need to be told not to
hand-edit `doc/actor/`, `doc/site/`, `doc/asset/` or anything under `var/`; not to overwrite a
handoff; not to publish a version twice or move a tag; not to take a job another actor owns; not to
commit two documents of different events together; not to write a file another site is the writer
of; not to run a bare `git clean` at the workspace root.

`thrum help` lists the whole surface, and `thrum verify` runs every mechanical check. Run `thrum
verify` when you are unsure whether the workspace is coherent: it reports and never repairs, so it
is always safe to run.
