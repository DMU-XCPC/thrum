# A thrum workspace

You are an **actor**. You were given two facts: this workspace root, and a job id. Everything
else is derived from here. Nothing in this file is the authority for anything.

**If nobody gave you a job id**, there are two lawful ways in: ask for one, or open the job
yourself — `thrum work --job <new-id> --purpose '<what it is for>'`, with the person's own words as
the brief's goal. Work that begins as "read this and find out" is still a job: the records are
where the work has to be visible, and a stretch nobody can find is a stretch nobody can continue.

**Set your actor name before your first command.** A handoff, an occupancy, a lease and a job's
owner all record which actor did the work:

```sh
export THRUM_ACTOR=claude-opus     # one slug, for every command in this session
```

If you were given a name, that is your name. Without `--actor` or `$THRUM_ACTOR`, your work is
recorded under `$USER`.

**A host that gives every command a new shell loses the export**: pass `--actor <name>` on each
command instead, with the same value.

**The name is yours, not the name of what you are running in.** `deepseek-harness`, `dsh` and
`claude-code` name a harness — the thing hosting you, which changes while the work goes on, and
which hosts several actors at once, so one of those names would merge their handoffs, occupancies
and leases into one. Name the actor: `dana`, `agent-3`, `claude-opus`.

**This file is the workspace's; the project's `AGENTS.md` inside the working directory is the
project's** — versioned with the code, and read beside this one. They govern different things: this
one says where work goes and how it is recorded, the project's says how the code is built and
delivered.

**This format and `thrum` are under development.** A refusal is still the design being enforced,
so trust it first; but a command that fails unexpectedly, or a refusal that contradicts the
records, is a defect. Keep what the tool produced under `job/<job-id>/log/`, name it in your
handoff, and say what you expected — *When the tooling is wrong* in `SKILL.md` has the rest.

**First command of a stretch: `thrum verify`** — with `thrum status` beside it. It reports and
never repairs, so it is always safe to run, and it is where a workspace that is incoherent, or a
skeleton older than the tooling, is found before any work is built on it. If it reports skeleton
drift, `thrum update --dry-run` says what the tooling has now.

## What you may touch

Five things, and nothing else. This is worth reading twice, because the failure it prevents is
silent: work done in the wrong place is work the next actor will not find, and no command will
tell you.

| | |
| --- | --- |
| **the directory the tooling names** | `thrum where --job <job-id>` prints it and `thrum work --job <job-id>` takes the job up and puts you there. This, and only this, is where the code is edited. |
| **your job's payload** | `job/<job-id>/`: scratch, logs, a tool's output. `thrum env --job <job-id>` points `TMPDIR` and the cache and state directories at it, so a tool that respects them cannot write anywhere else. |
| **the records you write** | the brief's body, `handoff/<job-id>/…`, and the project's `doc/topic/` and `doc/convention/`. |
| **nothing under `var/`** | It is the tooling's — *including making a directory there*. An entry a hand invents is invisible to `thrum status` and `verify`, is never collected, and never travels; work left inside one is work the next actor will not find. |
| **nothing outside this root** | Not `~/.ssh/config`, not `~/.config`, not `/tmp`. A tool run without `thrum env` writes there, where nothing collects what it leaves. Secrets go in none of these files: the only ones inside a workspace are the tooling's own, in `var/secrets/`. Reading is narrower than writing: the workspace holds what the task needs, and if you must look outside it — a tool's own documentation, a fact about this machine — say so in your handoff rather than going through the user's home for context (`~/.bash_history`, `~/.ssh/*`, another project's tree). |

**Reading is not checking out.** A document on another branch, the brief of a job that has
closed, a commit you are comparing against: `git -C .bare show <ref>:<path>` prints it, and
`git -C .bare ls-tree` or `git -C .bare log` say what there is to read. A working directory is
made by the tooling, under a declared label — never by a `git worktree add` of your own, and
never inside `var/`.

## Where the answers are

| What you need | Where it is |
| --- | --- |
| what the job is for, and how it will be judged | `doc/task/<job-id>/brief.md` |
| where the last stretch stopped, and what to do first | the newest file in `handoff/<job-id>/`; its `next` is the baton |
| where to work | `thrum where --job <job-id>` prints it; `thrum work --job <job-id>` gets you there, and materialises a working directory if this site has none |
| everything about this job | `thrum job --job <job-id>`, or `--json` for whatever asked |
| this project's rules, in number order | `doc/convention/` — read them before working |
| the rule behind a question the loop raises | `.agents/skills/workspace-actor/` — `SKILL.md` is the loop; the files beside it answer one question each: `placement.md` (where a thing goes), `records.md` (how a record looks), `names.md`, `knowledge.md`, `assets.md`, `claims.md`, `sites.md` |
| what is true right now | `thrum status`, `thrum actor ask --job <job-id>`, `thrum inbox` |

Before you run anything, `eval "$(thrum env --job <job-id>)"`: it points `TMPDIR` and the cache
and state directories inside the payload, so a tool that respects them cannot write outside the
workspace. Where a host gives every command a clean `/tmp`, this is not advice but the only way
the next command can see what this one made.

Then read `.agents/skills/workspace-actor/SKILL.md`. It is the loop, and this file is only the
way in.
