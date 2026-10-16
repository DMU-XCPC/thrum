# The design

Two documents, read in this order:

1. [`workspace.md`](workspace.md) — what a workspace is: the jobs that isolate work, the
   records that say what happened, and where everything goes.
2. [`thrum.md`](thrum.md) — the fabric that holds the sites together: finding actors, carrying
   messages, and keeping the shared histories converging.
3. [`commands.md`](commands.md) — the operations and every command, and what each refuses.

They are two halves of one design, not two designs. The workspace says what has to be true;
thrum says which parts of that a distributed program makes true. Where they meet — a name, a
site, the line — each states its own half once and points at the other.

## What this replaces

These documents replace `design.md`, `tooling.md`, and `implementation.md`, which are on
`master`. `tool/` and `workspace-template/` here still implement the previous design; they are
stale and are rebuilt from these documents.

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
- **Two shared histories, and both merge.** The line (`doc/`) holds the decisions, and no two
  sites write one file there: a brief has the one site working its job, and a name claimed twice
  is settled by a deterministic order rather than merged. The ledger (`handoff/`) holds the
  evidence and is append-only, so it merges by pure union — a new file every time, and a
  correction a further new file. A project that would rather have one order can still name a
  single site to sequence the line.
- **A job is the only isolation unit.** `touches` is a description, not a claim; the previous
  reading of `owned_paths` as exclusion is withdrawn. Leases and resources are out of this
  version entirely.
- **The design names thrum.** The older documents said "the tooling" because the design came
  first and the tool second. That distance is not worth keeping.
