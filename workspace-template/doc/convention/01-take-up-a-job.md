# Take up a job

You are given two facts: the workspace root, and a job id. Everything else you derive.

1. **Read `doc/task/<job-id>/brief.md`.** Its Goal is what the job is for, its Scope is
   what is out of it, and its Acceptance criteria are how the work will be judged. A
   brief that says nothing useful is a job nobody can do: fix the brief first.
2. **Read the last handoff**, the newest file in `handoff/<job-id>/`. Its `next` is where
   the previous actor stopped, and it is where you start.
3. **Work in the working directory that holds `job/<job-id>`**, which is the one
   `git worktree list` shows that branch checked out in. Not the workspace root, and not
   a checkout of that branch anywhere else.
4. **Say you are on it**: `thrum occupancy occupy --job <job-id>`. If the job has no
   owner, claim it: `thrum claim --job <job-id>`. A job another actor is on is not yours
   to start — ask them, by message, and let them answer.
5. **Point your tools inside the workspace** before running anything:
   `eval "$(thrum env --job <job-id>)"`.
6. **When you stop, hand off.** That is `05-hand-over-what-you-learned.md`, and stopping
   without it is how work disappears.

The brief is the contract, not a suggestion. If the work turns out to need something the
brief excludes, change the brief — with the tooling, in its own commit — rather than
doing it quietly and leaving the record wrong.
