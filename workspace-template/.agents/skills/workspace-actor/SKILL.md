---
name: workspace-actor
description: Work one job in a thrum workspace as an actor — the workspace's own rules, the loop that ends in a handoff, and where each answer lives.
whenToUse: You are in a thrum workspace (a directory holding .bare/, store/, var/, job/ and worktree/), or you have been given a workspace root and a job id.
---

# Working a job

You are an **actor** — a person or an agent — and the workspace knows you by a name. You were
given two facts: the workspace root and a job id. Everything else is here or in the workspace.

This skill states **the workspace's own rules**: what is true of every workspace, and the part
of the design that is not in any of them. Two other kinds of rule live in the workspace, and
neither is restated here:

| A rule about | Lives in | Read it |
| --- | --- | --- |
| the workspace itself — names, placement, records, retirement | this skill | now |
| **this project's** policy, and any value a tool must act on | `doc/convention/` | before you work, in number order |
| **this job's** intent | `doc/task/<job-id>/brief.md` | first |

Where this skill and a convention disagree about a project's policy, the convention wins. Where
either disagrees with the brief about this job, the brief wins.

## Six things that explain the rest

Read these once. Nearly every rule below follows from them, which is why a situation this skill
does not name still has a right answer.

1. **Three planes, three lifecycles.** `doc/` and `handoff/` are decisions and evidence, versioned
   and shared. `store/` is durable bytes, content-addressed and never checked out. `var/`, `job/`
   and `worktree/` are working state: unversioned, machine-local, and disposable. Getting a thing
   into the wrong plane is the most common way to lose it.
2. **A job holds the runtime, and an actor does not.** A job owns a branch, a working directory, a
   payload, its leases. An actor is a name that picks one up, hands it over or works it from
   another machine. That is why a name is never a permission, and why nothing is assigned to you.
3. **The records are the truth, and the views are derived.** No board, no status file, no index is
   maintained by hand. A job's status is its brief's; whether it is blocked is its latest
   handoff's; where the work is, is what `thrum where --job <job-id>` prints. A derived thing that has been written
   down is a second version of the truth, and it will drift.
4. **The line is one line, and nothing is ever merged.** A record write takes what the other sites
   published first, and the event is published or withdrawn. A refusal is not a loss.
5. **A name is a permanent identifier.** It never encodes state that can change, and job ids,
   asset versions and convention numbers are never reused. So *used once* is enforceable rather
   than promised.
6. **The tooling refuses what must not happen, and the refusal says why.** A rule the tooling
   enforces is one less rule to carry: you meet it as a refusal at the moment it matters, rather
   than having to remember it.

## Before anything: your name

You are an actor, and the workspace records which actor did the work: a handoff's `agent`, an
occupancy's `occupant`, a job's `owner`. Set your name before your first command, and use it for
every command of this session:

```sh
export THRUM_ACTOR=claude-opus        # or --actor <name> on every command
```

- A slug: lowercase letters, digits and hyphens, like every other name here.
- If you were given a name, that is your name.
- If your commands do not share a shell, the variable does not reach them — pass
  `--actor <name>` on each one, with the same value.
- Without `--actor` or `$THRUM_ACTOR`, the tooling records `$USER`.
- A later session may use a different name; every command of one session uses the same one.

## The loop

1. **Locate.** `thrum where --job <job-id>` prints the directory holding `job/<job-id>`, which is
   where you work, and `thrum job --job <job-id>` says everything else about the job in one place.
   No directory here means the job is unstarted, already closed, or its work has moved to another
   site: `thrum job` and `ls handoff/<job-id>/` say which. The coordination repository is a
   different repository, whose own `git worktree list` shows only the workspace root — never work
   there.
2. **Read.** `doc/task/<job-id>/brief.md`, then the newest file in `handoff/<job-id>/`. Its `next`
   is the baton: what the last actor decided you do first, and usually the whole reason you are
   being asked.
3. **Read this project's rules.** `doc/convention/`, in number order. The order is the priority.
4. **Check your mail.** `thrum inbox` lists what other actors have sent you, and `thrum read <id>`
   reads one and takes any files it carried. A message is process information and is never in a
   handoff, so this is the only place it arrives — and it is often why the last actor stopped.
5. **Take it up.** `thrum work --job <job-id>` is `open`, `claim` and `occupy` in the order they
   have to happen, and it ends by printing the working directory and the environment line to run:
   it opens the job when it is not open yet, declares the label when you name a new one, claims
   the job, and materialises a working directory when this site has none. It is a claim and not a
   lock, so if it names somebody else, ask that actor or the job's owner rather than reaching for
   `--take-over` — see `claims.md`. `thrum occupancy vacate --job <job-id>` ends the stretch.
6. **Point your tools inside the workspace.** `eval "$(thrum env --job <job-id>)"` before running
   anything. A tool run without it writes outside, where nothing collects what it leaves.
7. **Take what you need first.** A lease for a unit of a declared resource, before you start rather
   than once you are stuck: `thrum lease take --resource <name> --job <job-id>`.
8. **Work.** Stay inside the brief's `owned_paths`, and commit to `job/<job-id>`.
9. **Commit, then hand off.** `head` has to name an existing commit, so the commit comes first.
   Then `thrum handoff --job <job-id> --status <done|partial|blocked|aborted> --next '<first thing
   next>'`. One handoff is one coordination event, and therefore one commit.
10. **Close, or leave the baton.** `thrum close --job <job-id>` only when the acceptance criteria
    hold, and `thrum close --abandoned` when they will not. Closing merges nothing: the branch stays
    in the code repository for whoever integrates. Before closing, look for something to promote to
    `doc/topic/`.

## If you are opening the job rather than taking it up

Opening writes the brief, creates the branch and materialises a labelled working directory, and
it is one commit — unless the label has not been declared yet, because declaring a resource is a
different event with a commit of its own. **`thrum work` does this and takes the job up in one
act** when you are the one who will work it; `thrum open` is the same act without taking it up,
which is what opening a job for somebody else looks like.

```sh
thrum open --job add-retry-to-uploader --label uploader \
    --goal 'retry the uploader instead of failing it' \
    --scope 'the uploader only' \
    --criteria 'a flaky upload succeeds on the second attempt' \
    --owner <actor> --owned-paths 'src/upload/**,tests/upload/**'
```

- **The job id is taken forever**, and checked against both `doc/task/` and `doc/archive/`.
- **The label names the area of work, never the job.** It must already be declared;
  `--create-label` declares it, as a second commit — or `thrum work --purpose '<what it is for>'`
  declares it in the same act, with `--ephemeral` when the label is one-off. A label is free when
  `thrum worktree list` shows no job branch in it, and it is never taken from under the job
  holding it — that job ends the stretch as `blocked` instead.
- **`--owner` may name somebody else.** The owner is who the work is promised to, not necessarily
  who does it. Leaving it out makes the job unowned, and therefore claimable by whoever takes it
  up.
- **`--depends-on` is checked before work starts**, not after it fails.
- **`--keep` protects this job's payload** from collection after it closes; without it, the
  payload is disposable by construction.
- `--owned-paths` and `--depends-on` are comma-separated. `--brief <file>` (or `-`) reads the
  body from a file when the three flags are not enough.

The brief's body belongs to whoever is working the job; the `owner` field is the owner's to
change.

## Seeing the state of things

`thrum status` prints every derived view at once — each brief's status with its latest handoff,
which labels this site has materialised and what each holds, which leases are held, which
payloads are collectible, and where a directory exists with no declaration. Also: `thrum actor
ask --job <job-id>` for who is on a job, `thrum inbox` for your mailbox, and `thrum verify` for
every mechanical check — it reports and never repairs, so it is always safe to run when you are
unsure whether the workspace is still coherent.

## What to read, and when

| When you are about to | Read |
| --- | --- |
| put a file somewhere, or wonder if it belongs in the worktree | `placement.md` |
| write or edit a brief or a handoff, or judge one you are reading | `records.md` |
| name anything — a job, a label, a convention, an asset version | `names.md` |
| write a convention, start or promote a topic, retire a document | `knowledge.md` |
| keep a result that has to outlive the job | `assets.md` |
| take a lease, declare a resource, or find a job or resource claimed | `claims.md` |
| touch another site, send a message, carry a payload, or meet a refusal | `sites.md` |

## When the tooling is wrong

**This format and `thrum` are under development**, and there are no releases yet. A refusal is
still the design being enforced, so trust it first — but a command that fails unexpectedly, a
refusal that contradicts the records, or a file the tooling cannot read is a defect, and it is
not yours to patch from inside a job.

Keep the evidence rather than working around it in silence:

- **Write down what happened** under `job/<job-id>/log/`: the exact command, its whole output,
  and anything it left behind.
- **Name it in your handoff** with `--artifact`, so its raw `sha256` is recorded. The file is
  collected with the payload; the hash is not, so the loss becomes detectable rather than silent.
- **Say it in the handoff.** A defect is not a job, so `blocked_by` cannot name it: write what
  you ran, what you expected, what came back, and what you think should change, under
  `## Open questions` or `## What remains`.
- **Choose the status the work actually reached** — `partial` if you got somewhere despite it,
  `aborted` if it produced nothing usable. Never `blocked` for a defect, because there is no job
  to wait on.
- **Tell whoever runs you.** The workspace has no issue tracker, and how you report back is
  between the two of you.

A workaround nobody wrote down is a workaround nobody can remove.

## Ending a stretch

Write a handoff whenever you stop — to hand over, for the day, because you are blocked, because
your context is nearly full, or because the job is finished — and not before. Work that runs for
many rounds without stopping needs nothing in between. `records.md` has the front-matter, the four
required sections, and the rules that are not negotiable.

The four sections exist because two of them are easy to lose: **what was verified** separates *I
changed it* from *I proved it works*, and **open questions** is the most useful thing you can hand
over.

Never paste a log into a handoff — name a file under `job/` with `--artifact` and its raw `sha256`
is recorded. Never put a secret in one: it is versioned and permanent. Never edit one that
somebody downstream has read: a correction is a new handoff carrying `supersedes`.

Look at your mail once more before you hand off: a message that arrived mid-stretch is not in the
handoff, so whoever continues will not know it was ever sent.

## Stopping without spinning

Stop and record it rather than spinning. `--status blocked --blocked-by <job-id>` costs one
handoff; an actor that spins costs a context window and tells whoever continues nothing.

- A job in `depends_on` is satisfied only when its brief says `done` — and its brief may have
  moved to `doc/archive/task/`, because finishing moves it. An `abandoned` dependency is **not**
  satisfied: the premise it stood for is gone, and only the job's owner decides what that means.
- You need a path another **live** brief claims — live meaning still in `doc/task/`, since closing
  moves it out. That is not yours to decide: ask that job's owner, and if the owner is an agent,
  end the stretch as `blocked` naming it. If no live brief claims it, you may touch it, and you
  say so in your handoff. If the other job has no owner there is nobody to ask, so you may claim
  it — which is precisely what an unowned job is for.
- **`aborted`** is for a stretch that produced nothing usable: a crash, a wrong approach, a
  superseded plan. It is not the same as `blocked`, and neither is `abandoned`, which is a
  decision about the whole job.
- **You cannot proceed at all:** stop and record it. That is one handoff.

## What you decide alone, and what you do not

**Alone:** everything inside the brief — how to implement it, what to verify, when to hand off,
what `next` says, and closing the job once its acceptance criteria hold. Also: taking and
releasing leases; declaring a resource, topic or asset; claiming a job that has no owner; giving
up a job you own; and sending another actor a message.

**Ask instead:** removing a resource that is in use, and retiring a convention. Both belong to the
project rather than to any job — the first is refused mechanically while the resource is occupied,
and the second is an ordinary retirement whose discipline is the notice and the reason.

**Ask the owner:** abandoning a job you do not own, and any path another live brief claims. The
owner is who the work was promised to.

**Between you and whoever runs you:** how much to do before handing over, and how you report back.
The workspace deliberately has no opinion about either.

## What you do not have to remember

The tooling refuses what must not happen, and the refusal says why. You do not need to be told not
to hand-edit `doc/resource/`, `doc/asset/` or anything under `var/`; not to overwrite a handoff;
not to take a lease somebody holds, or one for a job that has closed; not to publish a version
twice or move a tag; not to claim a job another actor is on; not to commit two documents of
different events together; not to run a bare `git clean` at the workspace root; not to make two
versions of one file and merge them.

`thrum help` lists the whole surface, and `thrum verify` runs every mechanical check. Run
`thrum verify` when you are unsure whether the workspace is still coherent: it reports and never
repairs, so it is always safe to run.
