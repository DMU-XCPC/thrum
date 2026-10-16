# Working on thrum

This is the development repository for the workspace format and for `thrum`, the fabric that
creates and operates a workspace. It is **not** a workspace: the two layouts are independent,
and nothing here was created from the template. The `doc/` here holds documents about the
design; a workspace's `doc/` holds records, and the two are unrelated beyond the name.

`README.md` has the reading order. What follows is what an agent working here has to know
before it changes anything.

## What is the authority

**`doc/workspace.md` and `doc/thrum.md` are the authority for everything**, and they are two
halves of one design: the workspace says what has to be true, and thrum says which parts of it
a distributed program makes true. Where an implementation and the design disagree, the design
is right.

**A change starts as a change to the design.** The implementation, the template and the tests
follow in the same change. So does the manual an actor is handed — `workspace-template/AGENTS.md`
and `.agents/skills/workspace-actor/` — because that manual is the only place the design's
operating half reaches a workspace, and nothing in a workspace can tell that it has fallen
behind. A change that never touched the design has moved the authority by accident, which is
the one thing this repository is arranged to prevent.

**The design names thrum.** The older documents said "the tooling" because the design came
first and the tool second; that distance is not worth keeping. Keep the split that remains: a
rule belongs to the design, a command to the design of the fabric, the code to `tool/`, and a
refusal to the code that makes it.

## This branch is the redesign

`master` holds the previous design (`doc/design.md`, `doc/tooling.md`,
`doc/implementation.md`). This branch replaces it with `doc/workspace.md` and `doc/thrum.md`.
**`tool/`, `workspace-template/` and `harness/` here still implement the previous design**, so
`make test` is expected to fail until the implementation is rebuilt from these two documents.
Do not treat the current code as evidence about the design.

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
rules it holds, and `tool/e2e` drives the built command through a whole job's life and through
two sites over a real channel. Every one of them is written against the previous design and is
rebuilt with it.

## Where things are

| Path | What it is |
| --- | --- |
| `doc/workspace.md` | The design of a workspace: jobs, records, the line. The authority. |
| `doc/thrum.md` | The fabric: sites, actors, messages, and sequencing the line. The authority. |
| `doc/README.md` | The reading order, and what changed and why. |
| `tool/` | The implementation — stale; it implements the previous design. |
| `workspace-template/` | What a workspace is created from, carried into the binary — stale. |
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

The manual an actor reads tells it to keep the evidence when thrum misbehaves — the command,
the output, what it left behind — and to write what it expected into its handoff, because a
workspace has no issue tracker. That report is the raw material for a change here: reproduce
it, make it a test, and if the design is what was wrong, change the design first. A defect that
only ever gets worked around is a defect that comes back.

## What not to do

Merging a job's branch, starting and stopping services, running tests, and deciding which actor
takes which job are the project's business or whoever dispatches work — thrum never does them,
and neither should a change here. `doc/workspace.md` has the same list from the workspace's
side.
