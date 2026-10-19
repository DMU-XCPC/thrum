# Where a thing goes

Where does this thing belong? Nothing in a workspace is placed by taste: a thing's home follows from
what it is, and the same procedure answers every case, including the ones this file does not name.

## The procedure

```text
1. Source code, dependency locks, or a document that must change atomically with code?
       → a code repository: next to the code, or the project's own doc/ inside a worktree
2. A record of what a job did?
       → handoff/<job-id>/
3. A secret?
       → never in doc/, handoff/, or any git repository
4. Must be deletable object by object?
       → keep a reference here; the bytes live in an external system
5. Regenerable at acceptable cost?
       shared across jobs       → var/cache/
       private to one job       → job/<job-id>/cache/
       per worktree             → var/env/<label>/
6. Not regenerable?
       text a person reads in place   → doc/
       binary, or too large to read   → the store, anchored by a manifest in doc/asset/
7. True only of this machine, and not a decision?
       → var/, or job/<job-id>/ when it belongs to one job
8. Larger, or externally governed?
       → outside the workspace, with an immutable locator in doc/asset/
```

Ask the questions in order. The first one that answers is the home; there is no appeal to
convenience, and "I needed it nearby" is not one of the questions.

## The worktree holds source, and nothing else

**A working directory is made by thrum, under a declared label.** `thrum work <job-id> --repo <name>
--label <label>` takes a job up and is the ordinary way, `thrum job open <job-id> --repo <name>
--label <label>` opens one for somebody else, and `thrum worktree add <label> --repo <name>`
materialises a label on its own, detached, for comparison. The declaration at
`doc/worktree/<label>.yaml` is what says the label exists at all, and it names the repository the
label belongs to.

Checking a branch out by hand anywhere else, into `var/` or `job/` or your home directory, registers
a carrier thrum cannot see: `thrum status` and `verify` do not report it, `thrum clean` will not
remove it because `var/` is ignored, and `thrum where` and `thrum work` will **follow** it — because
"which worktree has this branch" is how thrum answers *where is this job*. A hand-made carrier
therefore does not sit harmlessly beside the workspace; it becomes the answer.

**To read something without working in it, read the blob.** A document on another branch, the brief
of a job that has closed, a commit you are comparing against: read it with whatever you have at hand.
Nothing has to be checked out, and nothing should be — *Reading is not checking out* in `SKILL.md`
has the reason.

Everything under `worktree/<label>/` is source, or a document that changes with the source. That is
the whole reason a label can be handed from one job to the next without inheriting the last one's
mess — and why a scratch file, a log, a cache or a downloaded dataset left there is not untidiness
but a broken handover.

A document that would go wrong if a single component were rewritten belongs next to the code, in the
worktree, so that the change and the document are reviewed together. A document that stays true
across a rewrite does not: it belongs in `doc/`, as a topic.

## What a job writes stays inside the workspace

Including the scratch of your own process. Anything written outside is invisible: not reviewed, not
collected, and gone when the machine is rebuilt.

| Instead of | Write to |
| --- | --- |
| the tool's default temp directory (`/tmp`, `$TMPDIR`) | `job/<job-id>/tmp/` |
| a cache under `$HOME` | `var/cache/`, or `job/<job-id>/cache/` when only this job can reuse it |
| a dependency environment inside the working directory | `var/env/<label>/` |
| a log beside whatever produced it | `job/<job-id>/log/` |

`eval "$(thrum env --job <job-id>)"` before you run anything: it sets `THRUM_ROOT`, `THRUM_ACTOR`,
`THRUM_JOB`, `TMPDIR`, `XDG_CACHE_HOME` and `XDG_STATE_HOME` to point inside the workspace. The base
variables belong to whoever starts the work; a tool with a variable of its own needs it set when you
invoke the tool — `PIP_CACHE_DIR`, `npm_config_cache`, `GOCACHE`, `CARGO_TARGET_DIR`,
`PYTHONPYCACHEPREFIX`, `GRADLE_USER_HOME` and so on.

A tool run without them has written outside the workspace, and the next actor cannot see what it left
behind.

## The payload is disposable by construction

```text
job/<job-id>/
    log/      # stdout and stderr worth keeping
    tmp/      # scratch, including redirected tool temp
    cache/    # regenerable; safe to delete at any time
    data/     # transient inputs only
    state/    # a private copy of a shared service's data, for destructive tests
    ckpt/     # resume state for a long run
    out/      # artifacts; not a delivery mechanism
```

`data/src/` is a working-tree snapshot copied from the site that holds the job by
`thrum sync --code`; it is a transient input like the rest of `data/`, replaced whole on every
sync and collected with the payload.

**It is not a repository, and you never clone into it.** A `.git` here is a mistake: the next
`sync --code` deletes the directory and unpacks the snapshot over it, and a clone would carry a
remote and a default branch the job does not own. The helper's `repos/` may be empty; that is
expected. If you need the branch — to edit, commit or push — that is the site that **holds** the
job and its `worktree/<label>/`, not the helper.

Nothing here is irreplaceable. `tmp/`, `cache/` and `ckpt/` are collected when the job closes;
`log/`, `data/`, `state/` and `out/` follow once the retention window has passed, which defaults to
14 days and is set by the `collect.window` value a convention may carry. A brief with `keep: true`
protects its payload from collection entirely.

**`out/` is a place to work, not a delivery mechanism.** A result that has to survive the job is
published to the store (see `assets.md`) and named in the handoff under `assets`. A handoff keeps the
raw `sha256` of every artifact it names, so a collected artifact is a detectable absence rather than
a silent one — but a hash cannot give the bytes back.

## Three planes, three lifecycles

| Plane | Holds | Versioned | Travels by git |
| --- | --- | --- | --- |
| The line — `doc/` | decisions | yes, in the line | yes |
| The ledger — `handoff/` | evidence, append-only | yes, in the ledger | yes |
| Assets — the store (`repos/store.git`) | durable bytes, content-addressed | yes, as tags | yes, as tags |
| Runtime — `var/`, `job/`, `worktree/` | working state | no | no |

**Nothing under `var/` is written by hand, and that includes making a directory there.** `var/` is
thrum's, and *What you may touch* in `AGENTS.md` is the rule. It matters here because placement is
how the mistake happens: working state you put under `var/` is state no actor after you finds, which
is the same failure as work outside the root.

`repos/`, `handoff/`, `var/`, `job/` and `worktree/` are ignored in `.gitignore` and
`.git/info/exclude`. Never run a bare `git clean` at the workspace root: `-x` would delete `repos/`,
the ledger and every history with them. `thrum clean` is the guarded version, and
`thrum clean --payloads` collects the payloads the retention window has released.
`thrum status` prints each payload's state (`in use`, `waiting`, `collectible`,
`kept`, `orphan`).
