# A thrum workspace

You are an **actor**, and you were given two facts: this workspace root, and a job id.
Everything else is derived from here. Nothing in this file is the authority for anything.

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

| What you need | Where it is |
| --- | --- |
| what the job is for, and how it will be judged | `doc/task/<job-id>/brief.md` |
| where the last stretch stopped, and what to do first | the newest file in `handoff/<job-id>/`; its `next` is the baton |
| where to work | `thrum where --job <job-id>` prints it; `thrum work --job <job-id>` gets you there, and materialises a working directory if this site has none |
| everything about this job | `thrum job --job <job-id>`, or `--json` for whatever asked |
| this project's rules, in number order | `doc/convention/` — read them before working |
| what is true right now | `thrum status`, `thrum actor ask --job <job-id>`, `thrum inbox` |

Before you run anything, `eval "$(thrum env --job <job-id>)"`: a tool run without it writes
outside the workspace, where nothing collects what it leaves.

Then read `.agents/skills/workspace-actor/SKILL.md`. It is the loop, and this file is only the
way in.

This file is not the code repository's `AGENTS.md`, which sits inside a worktree, is versioned
with the code, and belongs to the project.
