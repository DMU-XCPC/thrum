# Sites: messages, payloads in transit, and the one line

What changes once there is a second machine? A workspace is one project's shared thing; a
**site** is one machine's copy of it. Most work never notices: records travel through the
coordination repository, and on one machine nothing needs to be running at all. Three things
only appear once there is a second machine, and each is worth knowing before it surprises you.

## The line is one line, and nothing is ever merged

Every operation that records something begins by taking what the other sites have published, and
ends by putting its own event into that line or taking it back out.

- **What it reads is what is already true** — which names are taken, what a job's status is, who
  owns it. A site that is alone in the world skips this, and a workspace with no remote cannot
  fall behind anything.
- **An event is published or withdrawn, never kept.** If the line moved while you were writing,
  your commit comes back out and is made again on top of what moved. It was never in the line, so
  nothing anybody has seen is rewritten.
- **If the same file moved as well, the event is refused.** Two versions of one file cannot both
  be in one line. Nothing is published, nothing is merged, and **what you wrote stays in the
  working tree, uncommitted, for you to look at and record again.** That is a refusal, not a
  loss: read what moved, decide how your event should read now, and record it.

What this buys is one line with no merges and nothing rewritten that has been seen — and it is
also what makes *a name is used once* hold **between sites** rather than only within one. A job
id or convention number taken at another site while your event was being written is a file that
moved, so the event is refused rather than duplicated. What it costs is a round trip per recorded
event at a site that has a remote.

Publish at the moments another site depends on the record: a handoff, a close, a promotion, a job
moving. That is a floor and not a ceiling — publishing more often only makes the backup fresher,
and nothing requires a fetch in order to start work. `thrum sync` does it, and it is also what
publishes a **backlog**: a site that recorded while it was alone has records the line does not, so
sync brings the site up to date and puts them on top rather than refusing them.

**When they do not go together.** Two sites can record while they are apart and find that the
same file was changed on both sides. Nothing is merged and nothing is dropped. `thrum sync`
refuses, names the file, and says the way out — the general mechanic `records.md` states, met
here between two machines. Read what the other site published (`git show origin/main:<path>`),
take your records back out of the working copy with `thrum sync --withdraw` — what they wrote
stays in the tree, uncommitted — put the file right, and record the event again. **Never `git
reset --hard`: the record it would destroy is a brief or a handoff, which is somebody's work and
the only copy.**

## Messages — `var/msg/<actor>/`

Actors reach each other through the tooling, not through the records. A message is **process
information**: not a record, never committed, never validated, and it does not travel by git.

```sh
thrum send --to agent-3 --subject "the timeout is explained" --body "…" [--file report.csv]
thrum inbox [--json]
thrum read <id> [--json] [--out dir]
thrum reply <id> --body "…"
thrum purge <id>
```

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | always | opaque and unique; what a reply and any other tool refers to |
| `from`, `to` | always | actor names |
| `site` | always | where the sender was when it sent it |
| `sent` | always | RFC 3339 UTC |
| `subject` | always | one line, for a person reading a list — nothing parses it |
| `reply_to` | when it answers one | the `id` being answered |
| `job` | when it is about one | a job id, as a topic marker and nothing more |
| `attachments` | when there are files | a list; each entry has a `name` and a `size`, and the bytes sit beside the message |

- **Delivered or refused, never queued.** When the recipient's site cannot be reached the send
  fails and says so, because a workspace that assumes delivery is a workspace that loses messages
  quietly.

- **The gate is the site, not the person.** A mailbox is a directory at the site that has the
  name, so a message goes in whenever that site answers — the actor does not have to be working,
  and nothing waits for it. A name a site that did not answer might have is **not** "nobody by
  that name": the send failed, and where the name is cannot be established. A name this site has
  seen is delivered to without asking anyone.
- **It cannot be retracted or edited, because it may already have started something.** Your own
  tooling may act on a message the moment it arrives. A correction is another message with
  `reply_to` set.
- **Attachments are bytes and nothing more.** Not assets: no content addressing, no tag, no
  manifest, never `store/`. Sending a file this way is transport, not the record.
- **Nothing collects messages.** `purge` is manual on purpose, because nothing can know which of
  them still matter.

A message is the right way to ask another job's owner about a path you need (see `SKILL.md`), and
the right way to tell someone a job they were waiting on has finished.

## Moving a job to another site

A job belongs to the workspace, not to the machine it happens to be running on. Moving one is
three acts, and all three are needed:

1. **Push the records.** The brief and every handoff are in the coordination repository, so they
   travel the way everything else there does.
2. **Push the branch.** `job/<job-id>` lives in the code repository, and the receiving site has to
   be able to fetch it.
3. **Carry the payload.** Nothing else moves it, so it travels as a bundle — the one part of a
   job git cannot carry.

**A move does not by itself change who owns the job.** The receiving actor may work it under the
owner's authority, or the owner may give it up first and let the receiver claim it. Both are
ordinary, and neither is implied by the move.

### Bundles

A bundle is a transport artifact, not a workspace file: written outside the workspace, nothing in
the workspace ever refers to it, and it is expected to be deleted once unpacked. Its name is a
convenience and not an identity — a file that has been through a chat client comes back renamed —
so what a receiver trusts is the header inside it.

```text
bundle.yaml
job/<job-id>/log|data|ckpt|out/…
```

| Field | Required | Meaning |
| --- | --- | --- |
| `job` | always | the job this payload belongs to |
| `handoff` | always | the handoff it corresponds to, by name |
| `exported` | always | RFC 3339 UTC |
| `site` | optional | where it came from, for whoever finds the file later |

**`handoff` is the whole check.** If the receiving site has that file, it has the records the
payload belongs to; if it does not, the bundle arrived ahead of the history it depends on, and the
answer is to fetch rather than to guess.

```sh
thrum bundle export --job <job-id> --out /somewhere/outside/the/workspace
thrum bundle import <bundle> [--replace]
```

- **Export runs after a handoff and never during work** — the bundle says which handoff it belongs
  to. It releases nothing, because stopping already released whatever the job held. The bundle is
  written outside the workspace, and never inside `job/<job-id>/` itself.
- **Import refuses unless the records are already local**: the named handoff must be present, and
  the commit it names must resolve in the code repository. That single check replaces every merge
  rule the workspace would otherwise need — there is nothing to append and nothing to diverge,
  because the versioned half arrives through git and only the payload arrives in the bundle.
- **Import refuses when `job/<job-id>/` is already present** and says so. Only `--replace`
  proceeds, discarding what is there and reporting every path it discarded: a payload is
  disposable by construction, so nothing lost was not disposable, but nothing is removed silently
  either.
- **Import commits nothing**, because it writes nothing versioned.

### What a bundle never carries

`tmp/` and `cache/` are absent by construction — the two directories that exist only to be thrown
away are the two that never need to travel. Nothing under `var/` is carried either, and each
absence has its own reason rather than one shared reason:

| Not carried | Why not |
| --- | --- |
| `var/env/<label>/` | a materialised environment is **not portable**: a virtualenv embeds absolute paths, a dependency tree may hold native builds, a compiler cache belongs to one architecture. Copying one imports a machine's assumptions and produces failures that look like code defects. |
| `var/state/<service>/` | service data belongs to the site. What a job needs from it is either the recipe that resets it — versioned, so it travels by git — or a private copy, which belongs in `job/<job-id>/state/` and is therefore carried. |
| `var/lease/` | occupancy of a resource on one machine. A job that has stopped holds none. |
| `var/cache/` | regenerable and machine-specific. |
| `var/config/`, `var/secrets/` | site-local by definition; that is what they are for. |

The environment variables that point a job's tools at the workspace name paths **inside** it, so
they read the same on every site: whoever starts the work supplies them on each site, and there is
nothing for a bundle to carry.

A bundle carries whatever the job wrote — logs and inputs included — so it is exactly as
publishable as the payload was, and no more.

## Joining a site, briefly

Adding a machine is two places, and the order matters: the workspace has to know the site exists
before the site knows the workspace does.

1. **Declare it**, from a site already in the workspace: `doc/resource/site.yaml` is what every
   site reads to decide who exists. One commit, one push.
2. **Join from the new machine**: `thrum join <dir> --remote <url> --site <name>`. It takes the
   shared line and fills in what a repository cannot carry — the ignore rules, the empty
   directories, the entry point and this skill, the site's own name, and its own copies of the
   code and asset repositories. **It commits nothing**, because a second first commit would be a
   second workspace with its own idea of which names are taken, and the two would never agree.
3. **Enter the reach on both sides.** How to reach a machine is that machine's own business, so
   each side writes its own half with `thrum site reach <peer> <address>` (and `thrum site listen
   --at <address>` if it accepts peers), and public keys are exchanged once: `thrum site key` prints
   the line that goes into a peer's `var/secrets/authorized_keys`. `thrum site show` says what this
   machine has. SSH is
   the whole authentication model. A site that accepts no peers runs no daemon and declares no
   reach, and everything that needs only its own machine still works.
