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
and an **actor** — a person or an agent, known by a name rather than by a process — can pick one
up, hand it over, or work it from another machine.

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

# --code brings the project's own history in, so there is nothing to push by hand.
./.gocache/thrum create /tmp/demo --site build-01 --code <the project's repository>

# One command: the label is declared, the job opened and claimed, and the directory printed.
thrum work --job add-retry --label uploader --purpose 'uploader reliability' \
    --goal 'retry the uploader instead of failing it' \
    --scope 'the uploader only' \
    --criteria 'a flaky upload succeeds on the second attempt'
eval "$(thrum env --job add-retry)"   # what it tells you to run, before anything else
thrum job --job add-retry             # what the job is for, where it is, who is on it
thrum verify                          # every structural rule, in one command

thrum job handoff add-retry --status done --next none
thrum job close add-retry
```

A new workspace arrives ready to hand to an agent: it carries an entry point and the skill it
names, the record templates, an empty `doc/convention/` for whatever the project decides, and a
tool that refuses the things an agent would otherwise have to be told not to do. The entry point
is a map rather than a second copy of the rules — it says where each answer is, never what the
answer is — so the brief, the handoffs and the conventions stay the only places an intent or a
rule is stated.

Several sites of one workspace — the same records, on more than one machine — are joined
with `thrum join`, and reach each other directly over SSH; `thrum site` writes this machine's
own half of that. `doc/tooling.md` has every command and the interfaces they are asked
through.

## Repository layout

This is the development repository, not a workspace created from the template. The two
layouts are independent.

| Path | What it is |
| --- | --- |
| `doc/design.md` | The design of a workspace, and the authority for everything else here |
| `doc/tooling.md` | The interface: operations, refusals, and the commands — kept in step with the binary by a test |
| `doc/implementation.md` | How the tooling is built, and why |
| `tool/` | The implementation of those operations, including the one that creates a workspace |
| `workspace-template/` | The template that operation creates a workspace from |
| `harness/` | Companions for particular agent harnesses. **Not the format**: they consume what the design already promises, and a workspace neither ships nor knows about them |
| `Makefile`, `go.mod` | Build |

Creating a workspace is not a plain file copy. `.bare/` and `store/` have to be initialised
as bare repositories, and the empty directories — `doc/task/`, `var/lease/`, `job/`,
`worktree/` and the rest — have to be created rather than copied, because a repository
cannot carry an empty directory. What the template holds is the files that are copied
verbatim: the entry point an agent is handed and the skill it names, the record templates
under `doc/template/`, and the ignore rules, which are written into `.gitignore` and
`.git/info/exclude` together.

## Reading order

1. **`doc/design.md`** — what a workspace is made of and why, four definitions in, and the
   authority for every rule the tooling enforces.
2. **`doc/tooling.md`** — the interface: the operations, what each does and refuses, and every
   command. It adds no rules of its own.
3. **`doc/implementation.md`** — how the tooling is built: the stack, the record line and its
   lock, the daemon's process.
4. **`tool/README.md`** — where the code is and how to build and test it.

A behaviour change starts as a change to `doc/design.md`. Where an implementation and the
design disagree, the design is right.

## Tests

Each package tests the rules it holds. `tool/internal/validate` has the structural checks,
`tool/internal/format` the file formats, and `tool/e2e` drives the built command through a
whole job's life — create, declare, open, occupy, lease, hand off, publish, materialise,
export, close, collect — and through two actors reaching each other, asserting after every
step that the workspace still validates. The daemon's tests run two sites against each other
over a real SSH channel.
