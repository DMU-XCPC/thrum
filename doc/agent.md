# Handing a workspace to an agent

This is for whoever starts an agent on a workspace: a person at a keyboard, a scheduler, a
harness with skills of its own. **It is not part of the workspace.** A workspace created
from the template carries records and rules, not instructions about how to talk to an agent,
and the design says why in one line: the code repository may ship an `AGENTS.md` of its own,
and it is not this workspace's.

So there are three kinds of rule, and they belong in three different places.

| A rule about | Belongs in | Written by |
| --- | --- | --- |
| the workspace as a design — true of every workspace | `design.md` in this repository | whoever changes the design |
| this project's policy, and any value a tool has to read | `doc/convention/` in the workspace | the project |
| how one job is to be done | that job's brief | whoever opens or takes up the job |
| how a particular agent, team or harness works | the agent's own configuration, or the code repository's `AGENTS.md` | whoever runs the agent |

A workspace **starts with no conventions at all**. `doc/convention/` is there and empty, and
it is the project's: a rule that belongs to this project, or a value a tool has to read, is
written there in number order. Everything below is what an agent can be left to derive, or
what the tooling refuses on its own — which is why none of it needs to be shipped into every
workspace as prose.

## Give it two facts

The workspace root, and a job id. Everything else is in the workspace, and pushing a copy
into a prompt is how a prompt goes stale.

| What it needs | Where it gets it |
| --- | --- |
| what the job is for, what is out of it, how it will be judged | `doc/task/<job-id>/brief.md` |
| where the last stretch stopped, and what to do first | the newest file in `handoff/<job-id>/`, whose `next` is the handover |
| where to work | the working directory `git worktree list` shows `job/<job-id>` checked out in |
| where its tools may write | `thrum env --job <job-id>` |
| what this project has decided | `doc/convention/`, in number order |
| what is true right now | `thrum status`, `thrum actor ask --job <job-id>`, `thrum inbox` |
| what the tooling will not let it do | the refusal it gets, which says why |

## What does not have to be agreed, because it is refused

A rule the tooling enforces is one less rule to remember, and a refusal is better than a
paragraph because it arrives at the moment it matters. An agent does not need to be told not
to: hand-edit `doc/resource/`, `doc/asset/` or anything under `var/`; overwrite a handoff;
commit a resource that is still materialised; take a lease somebody holds, or one for a job
that has closed; publish a version twice, or move a tag; claim a job another actor is on;
send to a name nobody has seen; commit two documents of different events together; run a
bare `git clean` at the root; or make two versions of one file and merge them. Each of those
is a refusal with a message, and most of them are also in the validation list that
`thrum verify` runs.

## What has to be agreed, because nothing can enforce it

These are the ones worth writing down for an agent, and they are the agent's and the user's
own agreement rather than the workspace's:

- **How much to do before handing over**, and what `done` has to have been verified against.
  `done` means the acceptance criteria hold, not that the agent ran out of ideas.
- **When to ask rather than decide.** Two decisions belong to the project rather than to a
  job: removing a resource that is in use, and retiring a convention. Everything else the
  agent may decide, and a decision it made should be in the brief or the handoff.
- **What a handoff has to say** for the next actor to pick the job up: what changed, what was
  verified and how, what remains, and what to do first.
- **How to say it.** A commit message says what changed rather than restating the document;
  a message to another actor is process information and is never committed.
- **How to report to the person who asked**, which is the one thing the workspace deliberately
  has no opinion about.

## The loop, in short

Read the brief, read the last handoff, work in the working directory that holds the job,
`eval "$(thrum env --job <job-id>)"` before running anything, say you are on it, and stop by
writing a handoff rather than by stopping. Close the job only when the acceptance criteria
hold. Publish a result that has to survive: `job/<job-id>/out/` is a place to work, and
anything left there is collected.
