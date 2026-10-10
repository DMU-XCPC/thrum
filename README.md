# MultiAgentWorkspace

A workspace template and the tooling that creates it, for running many agents on one
project at the same time.

The workspace is built on `git worktree`. The source code lives in its own repository, and
everything else falls into three planes, each with its own lifecycle rule:

- **coordination** — the records that say what is being worked on and what happened, in git
  beside the code, so that they are reviewed and versioned with it;
- **assets** — the data a project publishes, in its own repository, identified by name and
  version rather than by path;
- **runtime** — working directories, payloads, leases and mailboxes, on the machine that has
  them, never in git.

A **job** is one unit of work, and it is what holds the runtime state: a working directory
of its own, its leases, its payload. Two jobs run side by side without touching each other,
and an **actor** — a person or an agent, known by a stable name — can pick one up, hand it
over, or work it from another machine.

People edit the workspace as much as agents do, so every name and every file stays readable
in place, and nothing requires a helper tool to be understood.

The tooling is **`thrum`**: one static Go binary, no service to install, and — on a single
machine — nothing running at all.

**Status.** The design is complete and the tooling implements it end to end. There are no
releases yet, and no license has been chosen.

## Requirements

- Go, to build the tooling (see `go.mod` for the version)
- `git` 2.x, which the tooling drives rather than reimplements
- no cgo; the binary is built with `CGO_ENABLED=0`

## Build and test

```sh
make build      # writes .gocache/thrum
make test       # every package, plus the end-to-end fixtures
make vet fmt tidy
```

The build and module caches are pinned under `.gocache/`, and the module proxy is pinned in
the `Makefile`, so a build behaves the same whatever the surrounding environment looks like.
Nothing has to be installed to try the tool: the binary lands beside the caches.

## Try it

```sh
make build
./.gocache/thrum create /tmp/demo --site build-01
cd /tmp/demo

# The code repository starts empty, and a job branches from the project's history, so the
# project needs a first commit before anything can be opened.
git clone --quiet .bare /tmp/demo-clone
cd /tmp/demo-clone && echo '# demo' > README.md
git add README.md && git -c user.email=you@example -c user.name=you commit -m 'project: first commit'
git push --quiet origin HEAD:refs/heads/main && cd /tmp/demo

thrum resource declare --kind worktree --name uploader --purpose 'uploader reliability'
thrum open --job add-retry --label uploader --owner me \
    --goal 'retry the uploader instead of failing it' \
    --scope 'the uploader only' \
    --criteria 'a flaky upload succeeds on the second attempt'

eval "$(thrum env --job add-retry)"   # point your tools inside the workspace
thrum status                          # the board, the working directories, the claims
thrum verify                          # every structural rule, in one command

thrum handoff --job add-retry --status done --next none
thrum close --job add-retry
```

A new workspace arrives ready to hand to an agent: it carries the record templates, an empty
`doc/convention/` for whatever the project decides, and a tool that refuses the things an
agent would otherwise have to be told not to do.

Several sites of one workspace — the same records, on more than one machine — are joined
with `thrum join`, and reach each other directly over SSH. `tool/README.md` has the whole
command surface and the configuration that goes with it.

## Repository layout

This is the development repository, not a workspace created from the template. The two
layouts are independent.

| Path | What it is |
| --- | --- |
| `doc/design.md` | The design of a workspace, and the authority for everything else here |
| `doc/tooling.md` | The operations a workspace needs its tooling to provide, derived from the design |
| `tool/` | The implementation of those operations, including the one that creates a workspace |
| `workspace-template/` | The template that operation creates a workspace from |
| `Makefile`, `go.mod` | Build |

Creating a workspace is not a plain file copy. `.bare/` and `store/` have to be initialised
as bare repositories, and the empty directories — `doc/task/`, `var/lease/`, `job/`,
`worktree/` and the rest — have to be created rather than copied, because a repository
cannot carry an empty directory. What the template holds is the files that are copied
verbatim: the ignore rules, and the record templates under `doc/template/`.

## Reading order

1. **`doc/design.md`** — what a workspace is made of and why, four definitions in, and the
   authority for every rule the tooling enforces.
2. **`doc/tooling.md`** — the operations a workspace needs, and what each one is for. It
   adds no rules of its own.
3. **`doc/agent.md`** — handing a workspace to an agent: the two facts it is given, what it
   derives, and which rules the tooling refuses on its own so nobody has to be told.
4. **`tool/README.md`** — building the tooling, its command surface, how sites reach each
   other, and what it refuses.

A behaviour change starts as a change to `doc/design.md`. Where an implementation and the
design disagree, the design is right.

## Tests

Each package tests the rules it holds. `tool/internal/validate` has the structural checks,
`tool/internal/format` the file formats, and `tool/e2e` drives the built command through a
whole job's life — create, declare, open, occupy, lease, hand off, publish, materialise,
export, close, collect — and through two actors reaching each other, asserting after every
step that the workspace still validates. The daemon's tests run two sites against each other
over a real SSH channel.
