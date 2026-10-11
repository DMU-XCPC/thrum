# Working on thrum

This is the development repository for the workspace format and for `thrum`, the tool that
creates and operates a workspace. It is **not** a workspace: the two layouts are independent,
and nothing here was created from the template. The `doc/` here holds documents about the
tooling; a workspace's `doc/` holds records, and the two are unrelated beyond the name.

`README.md` has the reading order. What follows is what an agent working here has to know
before it changes anything.

## What is the authority

**`doc/design.md` is the authority for everything.** The tooling implements it, and where an
implementation and the design disagree, the design is right. `doc/tooling.md` is the
interface — the operations, what each does and refuses, and every command, kept in step with
the binary by a test in `tool/e2e`; `doc/implementation.md` is how the tooling is built, and
neither adds a rule of its own.

**A behaviour change starts as a change to `doc/design.md`.** The implementation, the templates
and the tests follow in the same change. So does the manual an actor is handed —
`workspace-template/AGENTS.md` and `.agents/skills/workspace-actor/` — because that manual is the
only place the design's operating half reaches a workspace, and nothing in a workspace can tell
that it has fallen behind. A behaviour change that never touched the design has moved the
authority by accident, which is the one thing this repository is arranged to prevent.

**The design never names the tool.** It says "the tooling", because a workspace has to stay
usable if the tool were rewritten tomorrow. Keep it that way, and keep the same split
everywhere else: a rule belongs to the design, an interface to `tooling.md` — commands
included, since a command is how an operation is reached — the code to `implementation.md`,
and a refusal to the code that makes it.

## Build and test

```sh
make build      # writes .gocache/thrum
make test       # every package, plus the end-to-end fixtures
make vet fmt tidy
```

`make` is the supported way to build: the caches and the module proxy are pinned in the
`Makefile`, so a build behaves the same whatever the surrounding environment looks like.
`gofmt -l tool/` should be empty, and `make vet` should be quiet.

`make test` runs the Go packages, and then `harness/` — the companions for particular harnesses,
which are a different language and a different runtime. Building the format must not require
node, so that half is skipped where node is absent rather than failing; run `make test-harness`
to run it on its own.

The tests are part of the specification rather than an afterthought: each package tests the
rules it holds, `tool/internal/validate` holds the structural checks, `tool/internal/format`
the file formats, and `tool/e2e` drives the built command through a whole job's life — create,
declare, open, occupy, lease, hand off, publish, materialise, export, close, collect — plus two
actors at two sites over a real SSH channel.

## Where things are

| Path | What it is |
| --- | --- |
| `doc/design.md` | The design of a workspace. The authority. |
| `doc/tooling.md` | The interface: the operations, the refusals, and every command. |
| `doc/implementation.md` | How the tooling is built: the stack, the record line, the daemon. |
| `tool/` | The implementation. |
| `workspace-template/` | What a workspace is created from, carried into the binary. |
| `harness/` | Companions for particular agent harnesses, and **not the format**. They read the CLI, `--json` output and the documented layout; they must never be a reason for the design to change, and a workspace neither ships nor knows about them. `harness/dsh/` is one. |
| `template.go` | The `go:embed` for that template — at the repository root because `go:embed` cannot name a path outside the directory its own source file lives in. |

## Two things that bite

**The template is payload, not policy.** `workspace-template/.gitignore` is the ignore list a
*workspace* gets. Here, git applies it to `workspace-template/**`, so a rule added to it can
silently make a template file uncommittable in this repository — while in the workspace that
same rule is required, or `thrum clean` would delete the file. That is why the rules keeping
the skeleton's own files out of `git status` live in `SkeletonFiles()`, in code, and not in
that file.

**`workspace-template/AGENTS.md` is not addressed to you.** It is the entry point an actor in a
materialised workspace reads, and it sits where that actor will find it. This file is the one
written for whoever works here.

## What comes back from a workspace

The manual an actor reads tells it to keep the evidence when the tooling misbehaves — the command,
the output, what it left behind — and to write what it expected into its handoff, because a
workspace has no issue tracker. That report is the raw material for a change here: reproduce it,
make it a test, and if the design is what was wrong, change the design first. A defect that only
ever gets worked around is a defect that comes back.

## What not to do

Merging a job's branch, starting and stopping services, running tests, and deciding which actor
takes which job are the project's business or whoever dispatches work — the tooling never does
them, and neither should a change here. `doc/tooling.md` has the same list from the
workspace's side.
