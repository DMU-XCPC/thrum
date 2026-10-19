# A thrum workspace

You are an **actor** — a person or an agent — and this workspace knows you by a name. You were
given two facts: this workspace root, and a job id. Everything else is derived from here. Nothing
in this file is the authority for anything.

**Before your first command, read the skill.** `.agents/skills/workspace-actor/SKILL.md` is the
loop; the eight files beside it are the rules, one question each. Read SKILL.md now, and the file a
step names before you take that step — this file is only the way in.

**Set your name before your first command.** The records call you by it — a handoff's `agent`, a
job's `owner` — and one name holds for the whole of this stretch:

```sh
export THRUM_ACTOR=flash-9c2e     # or --actor <name> on every command
```

A name is lowercase letters, digits and hyphens. If you were given one, that is your name. **A name
that fits everybody names nobody**: `agent-novaphy` names the project, and a harness's or a model's
name is shared by every session running it. Your name has to pick **this actor** out of all of
them; if none was given, look at `thrum actor list` first and give yours something only this actor
has. An agent's is **the model you are running, in one short word, then exactly four random
characters** — `deepseek-flash` gives `flash-9c2e`; not the bare model (every session would share
it), and not a generic `agent` (it says nothing). `names.md` has the shape.

**A name is declared once, and then it exists.** If the name has never been declared, the first
command that writes anything refuses and says to declare it. Do that once:

```sh
thrum actor declare flash-9c2e --kind agent    # or --kind human
thrum actor hello                              # tell the fabric where your mail is
```

`kind` changes what a reader expects and nothing about what you may do. `thrum actor hello` is how
mail finds you: it is an address, not a claim about working, and it does not expire.

**Bind the name to this session with the harness's tool.** Look in your tool list for a tool
whose description says it wakes the session when the actor it works as receives a message, and call
it once with your name: it records which session this actor is and says hello. The workspace
deliberately does not name that tool and no file here lists it — a tool named after one harness is
not true of every workspace, and only the harness knows what it installed — but every bound
session should have it in the tool list. **Without it you lose only the nudge**: `thrum message
list` still has your mail, and the mailbox is the truth either way.

**If nobody gave you a job id**, there are two lawful ways in: ask for one, or open the job
yourself — `thrum work <new-id> --repo <name> --label <label> --goal '<what it is for>'`. Work that
begins as "read this and find out" is still a job: a stretch nobody can find is a stretch nobody
can continue.

**Work you were given is a new job.** Opening it is the ordinary path, not the exception: `thrum
work <new-id> --repo <name> --label <label> --goal '<what it is for>'` opens it, claims it and puts
you in a working directory; `--body <file|->` gives the brief its whole body instead of the three
summary sections. **A job works in one repository**: `--repo` names it when the job is opened, the
brief carries it afterwards, and `doc/repo/<name>.yaml` says which repositories exist. Continue an
existing job only when you were pointed at it — or when it has no owner and you take it up — never
because the board happens to hold something close to your task.

**A human is an actor too.** Nothing here assumes a session, a plugin or a particular harness. A
person at a keyboard sets the same name, declares it once, and follows the same loop.

**This file is the workspace's; the project's `AGENTS.md` inside the working directory is the
project's** — versioned with the code, and read beside this one. They govern different things: this
one says where work goes and how it is recorded, the project's says how the code is built and
delivered. `doc/convention/` is the project's own internal policy, read in number order; it has
nothing to do with how your harness wakes you.

**thrum and this format are under development.** A refusal is still the design being enforced, so
trust it first; but a command that fails unexpectedly, or a refusal that contradicts the records,
is a defect. Keep what it produced under `job/<job-id>/log/`, name it in your handoff, and say what
you expected — *When thrum is wrong* in `SKILL.md` has the rest.

**First command of a stretch: `thrum verify`** — with `thrum status` beside it. It reports and
never repairs, so it is always safe to run.

## What you may touch

Five things, and nothing else. Read this twice, because the failure it prevents is silent: work
done in the wrong place is work the next actor will not find, and no command will tell you.

| | |
| --- | --- |
| **the directory thrum names** | `thrum where --job <job-id>` prints it, and `thrum work <job-id>` takes the job up and puts you there. This, and only this, is where the code is edited. |
| **your job's payload** | `job/<job-id>/`: scratch, logs, a tool's output. `thrum env --job <job-id>` points `TMPDIR` and the cache and state directories at it, so a tool that respects them cannot write anywhere else. |
| **the records you write** | the brief's body, `handoff/<job-id>/…`, and the project's `doc/topic/` and `doc/convention/`. |
| **nothing under `var/`** | It is thrum's — *including making a directory there*. An entry a hand invents is invisible to `thrum status` and `verify`, is never collected, and never travels. |
| **nothing outside this root** | Not `~/.ssh/config`, not `~/.config`, not `/tmp`. A tool run without `thrum env` writes there, where nothing collects what it leaves. Secrets go in none of these files; the only ones inside a workspace are thrum's own, in `var/secrets/`. Reading is narrower than writing: the workspace holds what the task needs, and if you must look outside it — a tool's own documentation, a fact about this machine — say so in your handoff rather than going through the user's home for context (`~/.bash_history`, `~/.ssh/*`, another project's tree). |

**Reading is not checking out.** A document on another branch, the brief of a job that has closed,
a commit you are comparing against: read it with whatever you have at hand. A working directory is
made by thrum, under a declared label, and never by a `git worktree add` of your own.

## The daemon is not your concern

This site has a daemon — the endpoint other sites reach — and you never start, stop or watch it.
`thrum sync`, a message and `thrum actor hello` start it when it is not running, and it stops
itself when no neighbour answers for a few intervals. There is no service to manage and nothing to
supervise. If the site is deliberately not serving, its operator runs `thrum site leave`; it comes
back with `thrum site stay`. Neither is part of your loop.

## Where the answers are

| What you need | Where it is |
| --- | --- |
| what the job is for, and how it will be judged | `doc/task/<job-id>/brief.md` — the body's Goal, Scope and Acceptance criteria sections; `--body` writes them whole |
| which repository the job works in | the brief's `repo`, declared at `doc/repo/<name>.yaml` |
| where the last stretch stopped, and what to do first | the newest file in `handoff/<job-id>/`; its `next` is the baton |
| where to work | `thrum where --job <job-id>` prints it; `thrum work <job-id>` gets you there |
| everything about this job | `thrum job show <job-id>`, or `--json` for whatever asked |
| this project's internal rules, in number order | `doc/convention/` — read them before working |
| the rule behind a question the loop raises | the files beside `SKILL.md`: `actors.md`, `messages.md`, `names.md`, `placement.md`, `records.md`, `knowledge.md`, `assets.md`, `sites.md` |
| my mail, and any files it carried | `thrum message list`, `thrum message read <id> [--out <dir>]` |
| what is true right now | `thrum status`, `thrum job show <job-id>`, `thrum message list` |
| another site, and reaching it | `thrum sync`; the `sites.md` skill file |

`doc/` is **the line** and `handoff/` is **the ledger**: two shared histories, both versioned, and
the only places a durable record can live. The rest of the workspace is runtime, and is disposable.

Before you run anything, `eval "$(thrum env --job <job-id>)"`: it points `TMPDIR` and the cache and
state directories inside the payload, so a tool that respects them cannot write outside the
workspace. Where a host gives every command a clean `/tmp`, this is not advice but the only way the
next command can see what this one made.

Then read `.agents/skills/workspace-actor/SKILL.md`. It is the loop, and this file is only the way
in.
