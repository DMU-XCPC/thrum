# The design

Four documents, read in this order:

1. [`workspace.md`](workspace.md) — what a workspace is: the jobs that isolate work, the
   records that say what happened, and where everything goes.
2. [`thrum.md`](thrum.md) — the fabric that holds the sites together: finding actors, carrying
   messages, and keeping the shared histories converging.
3. [`commands.md`](commands.md) — the operations and every command, and what each refuses.
4. [`extension.md`](extension.md) — the interfaces a harness plugin uses: the event stream, the
   watch, the binding and its state. Not the actor's manual.

They are parts of one design, not separate designs. The first two are its two halves, the third is
how you ask for either, and the fourth is the plugin author's side of the binding.

## What this replaces

These documents replace `design.md`, `tooling.md`, and `implementation.md`, which are on
`master`. `tool/` and `workspace-template/` are built from these documents, and the fixtures test
the design they describe.

## What changed, and why

- **An actor has standing and a last-known place, and nothing else.** Standing — that this name
  is a real actor, and what kind — is a declaration in the line. Where it is, is the fabric's:
  the last site that took mail for it, corrected by `actor hello` or by a delivery that finds it
  is not there. There is no session, no presence to state, no begin and end, and **nothing ages
  out**, because whether an actor is working cannot be observed. An actor is not necessarily an
  agent: a human is one.
- **The daemon is central and is never an authority.** It is a cache and a channel. Its memory
  may be lost, and it may be wrong, without changing a decision, because no decision is stored
  there. This is what makes a distributed daemon safe to have rather than something whose
  concurrency has to be perfect.
- **Messages are transient and pushed.** They are not records and never go into git. A message
  is placed by name through the fabric and pushed to the actor's last-known place, where it
  waits to be read; the sender never queues, so a send that cannot be placed says so instead of
  inventing a backlog.
- **The binding is the harness's step, not thrum's.** An agent that wants mail as turns calls its
  harness's tool once at the start, which records which session is this actor and says hello for
  it. The workspace manual states the obligation; the harness's tool list carries the tool, and
  the workspace does not name it. thrum never wakes anybody, and a session that skips the binding
  loses only the nudge.
- **Two shared histories, and both merge.** The line (`doc/`) holds the decisions, and no two
  sites write one file there: a brief has the one site working its job, and a name claimed twice
  is settled by a deterministic order rather than merged. The ledger (`handoff/`) holds the
  evidence and is append-only, so it merges by pure union — a new file every time, and a
  correction a further new file. A project that would rather have one order can still name a
  single site to sequence the line.
- **A job is the only isolation unit.** `touches` is a description, not a claim; the previous
  reading of `owned_paths` as exclusion is withdrawn. Leases and resources are out of this
  version entirely.
- **Bare repositories live under `repos/`, and there may be more than one.** The code is one or
  more repositories, each declared in the line and materialised at `repos/<name>.git`; a job names
  the one it works in, and a worktree label names the one it checks out. The store is
  `repos/store.git`. The line and the ledger keep their git directory with their checkout, because
  they have one.
- **The design names thrum.** The older documents said "the tooling" because the design came
  first and the tool second. That distance is not worth keeping.
