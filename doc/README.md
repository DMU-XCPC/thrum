# The design

Two documents, read in this order:

1. [`workspace.md`](workspace.md) — what a workspace is: the jobs that isolate work, the
   records that say what happened, and where everything goes.
2. [`thrum.md`](thrum.md) — the fabric that holds the sites together: finding actors, carrying
   messages, and putting the shared line in one order.

They are two halves of one design, not two designs. The workspace says what has to be true;
thrum says which parts of that a distributed program makes true. Where they meet — a name, a
site, the line — each states its own half once and points at the other.

## What this replaces

These documents replace `design.md`, `tooling.md`, and `implementation.md`, which are on
`master`. `tool/` and `workspace-template/` here still implement the previous design; they are
stale and are rebuilt from these documents.

## What changed, and why

- **An actor has standing and location, and nothing else.** Standing — that this name is a real
  actor, and what kind — is a declaration in the line. Location — which site is serving it now
  — is the fabric's, it is soft, and nothing durable depends on it. There is no session, no
  presence to state, no begin and end, and no roster of the inactive, because a list of the
  absent answers nothing.
- **The daemon is central and is never an authority.** It is a cache and a channel. Its memory
  may be lost, and it may be wrong, without changing a decision, because no decision is stored
  there. This is what makes a distributed daemon safe to have rather than something whose
  concurrency has to be perfect.
- **Messages are transient and pushed.** They are not records and never go into git. A message
  is placed by name through the fabric and pushed to the site that is serving the actor; if no
  site is, the send says so rather than inventing a queue.
- **The line is sequenced.** One declared site orders shared writes, so the history is linear
  by construction rather than by every writer repeating a fetch, rebase, and retry protocol.
- **A job is the only isolation unit.** `touches` is a description, not a claim; the previous
  reading of `owned_paths` as exclusion is withdrawn. Leases and resources are out of this
  version entirely.
- **The design names thrum.** The older documents said "the tooling" because the design came
  first and the tool second. That distance is not worth keeping.
