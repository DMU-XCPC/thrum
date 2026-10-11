# A thrum workspace

You are an **actor**. You were given two facts: this workspace root, and a job id. Everything
else is derived from here. Nothing in this file is the authority for anything.

**Set your actor name before your first command.** A handoff, an occupancy, a lease and a job's
owner all record which actor did the work:

```sh
export THRUM_ACTOR=claude-opus     # one slug, for every command in this session
```

If you were given a name, that is your name. Without `--actor` or `$THRUM_ACTOR`, your work is
recorded under `$USER`.

**This format and `thrum` are under development.** A refusal is still the design being enforced,
so trust it first; but a command that fails unexpectedly, or a refusal that contradicts the
records, is a defect. Keep what the tool produced under `job/<job-id>/log/`, name it in your
handoff, and say what you expected — *When the tooling is wrong* in `SKILL.md` has the rest.

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
workspace.

Then read `.agents/skills/workspace-actor/SKILL.md`. It is the loop, and this file is only the
way in.

This file is not the code repository's `AGENTS.md`, which sits inside a worktree, is versioned
with the code, and belongs to the project.
