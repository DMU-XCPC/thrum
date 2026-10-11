# Claims: who is on a job, and who holds what

Two different things are both called "who has it", and they answer different questions. Neither
is a lock, and saying so is not a disclaimer — it is what makes each one usable.

| | Occupancy | Lease |
| --- | --- | --- |
| Answers | who is on this job **right now** | which job holds **one unit of a declared resource** until when |
| Lives at | `var/occupy/<job-id>.yaml`, at the site that holds the job | `var/lease/<resource>.<unit>.yaml`, at the resource's home site |
| Belongs to | a job | a job |
| Ends when | the stretch ends | it expires, its job closes, or the job holding it releases it |

## Occupancy — `var/occupy/`

`thrum occupancy occupy --job <job-id>` writes the claim; `thrum occupancy vacate --job <job-id>`
clears it. In the ordinary flow it is `thrum work --job <job-id>` that writes it, because taking a
job up is the opening, the claim and the working directory in one act; `occupy` is the same claim
said on its own. The file holds an `occupant` and a `since`, and it is written and cleared by the
tooling on your behalf, never by hand.

**It is a claim, not a lock, because nothing on disk can tell a working actor from a stopped
one.** A name stays behind after the actor has stopped, and the tooling does not pretend
otherwise.

**It is kept at the site that holds the job** — the site whose working directory has
`job/<job-id>` checked out. That is the anchor, rather than the actor's name, and the reason is
that a job is a record and records travel by git, while an actor is at a machine. Two actors at
two sites can each believe they are on one job; a name cannot arbitrate between them, but a
working directory can, because there is only one of it, and only where it is can the work
actually be done.

What follows:

- A job no site holds cannot be claimed, because there is nothing to claim.
- Two sites holding one job is refused rather than picked between.
- A peer that does not answer means **cannot be established**, never *nobody is on it*.
- **Taking a job over is a decision, not a field edit.** If `occupant` names somebody else, ask
  the tooling or the job's owner. `--take-over` exists; using it is how you claim you know
  something the file does not.

Who has held the job, and since when, comes from the record instead: the current claim, and the
handoffs for every finished stretch, each of which already names its `agent` and its start and
end.

## Resources — `doc/resource/`

Anything the workspace shares that is not reachable by its own name — a working directory, a
borrowed machine, a stateful dependency, a GPU — is declared here, one file per kind:

```yaml
# doc/resource/worktree.yaml
worktree:
  - name: uploader
    purpose: Uploader reliability work
    ephemeral: false
  - name: spike-gc
    purpose: One-off look at GC pauses
    ephemeral: true
```

| Field | Read by the tool | Required | Meaning |
| --- | --- | --- | --- |
| `name` | yes | always | unique across every kind; the name leases use, and the directory under `worktree/` |
| `capacity` | yes | only above 1 | how many holders at once; omitted means 1, which means exclusive |
| `ephemeral` | yes | `worktree` entries | true when the directory exists for a single use and may be destroyed on release |
| `purpose` | no | always | what the resource is for, in a person's words |

- **The declaration is a recipe, not an inventory.** The code repository's `git worktree list` says what this site has
  materialised; the file says what the project intends to exist. A declared entry with no
  directory is normal — not yet materialised here, or removed and expected back. **A directory
  with no entry is the error**, and the tooling reports it.
- **Only the tooling writes these files**, one change per commit, because a registry is the one
  file that holds several entries and two hands appending to it collide.
- **Removing a resource whose directory exists is refused**, not permitted: pulling a working
  directory out from under whatever is using it is not a thing a permission makes safe.
- **A field the tooling does not read is a field for the reader.** A kind a project adds may
  bring fields of its own, with the same understanding.

**This half must be publishable.** `doc/` is a git repository with remotes, so everything in it
should be something you would be content to see published. Whatever a resource needs beyond that
— how to reach it, which credential to use, who to contact — belongs in its site-local half at
`var/config/<name>.yaml`, which is never committed and never leaves the machine. A worktree is
the example of a kind with nothing local: that is why it is the only kind complete on its own.

**Which site holds a resource is read from the machines, not declared.** A declaration travels by
git, so every site has every one; what a machine actually has does not travel, and cannot be
written into the declaration without publishing it. A site says it holds a resource in one of two
ways, both local: the resource is materialised there, or its own `var/config/<name>.yaml` says so.

## Leases — `var/lease/<resource>.<unit>.yaml`

```yaml
# var/lease/gpu.2.yaml
resource: gpu
unit: "2"
job: sweep-learning-rate
since: 2026-10-09T10:00:00Z
expires: 2026-10-09T12:00:00Z
```

| Field | Required | Meaning |
| --- | --- | --- |
| `resource` | always | must name an entry in `doc/resource/` |
| `unit` | always | from `1` to the resource's `capacity` |
| `job` | always | the job that holds it — a lease always belongs to a job |
| `since` | always | when it was taken |
| `expires` | always | when it becomes reclaimable |

One file per unit. The dot separates the two because a slug never contains a dot, so the name is
unambiguous and reversible. **The unit is quoted**, or YAML would read `2` as a number and `01` as
octal.

```sh
thrum lease take --resource gpu --job <job-id> --ttl 2h   # --unit picks one; empty takes the first free
thrum lease list
thrum lease renew   --resource gpu --unit 2 --job <job-id>
thrum lease release --resource gpu --unit 2 --job <job-id>
```

**The job holds it, not you.** Who is working that job is the claim at `var/occupy/`, which
changes with every stretch; a name in the lease as well would be a second record of one fact. So
every act on a lease names the job: taking it, renewing it, releasing it.

- **Take a lease before you start, not once you are stuck.** Exclusion has to be settled *before*
  acting, which is why a lease is persistent state rather than a check.
- **Expiry is reclaim that works without anyone remembering.** A lease is held across commands
  rather than for the life of one process, so nothing can release it when the actor working it
  stops. A record whose `expires` has passed is free; taking it means overwriting the record, and
  releasing early means deleting the file.
- **A lease dies with its job.** A lease whose job is `done` or `abandoned` is as free as an
  expired one — otherwise a finished job would keep its unit out of reach until the expiry passed.
- **A lease whose `resource` names nothing in `doc/resource/` is not valid.** That is how
  "everything unversioned must be anchored by something versioned" is checked mechanically, and
  it is the reason the declarations exist at all.

### Leased where the thing is

A lease names something on a machine, so it is taken **at the resource's home site**, never at the
site that wants it. That is not a limitation: reaching the resource already meant reaching that
machine, so exclusion stays site-local and nothing central is involved.

- Two sites that both claim a resource are refused rather than picked between: a resource with
  two homes is one the workspace has nothing to say about.
- A site that cannot be reached is refused, not proxied — so **mutual reachability is required
  exactly between the sites that have to share a claim or a lease**, and a full mesh is not.

## When reality does not match the registries

They and the filesystem are two authorities, so drift is expected rather than exceptional.

- **A directory with no entry in `doc/resource/`: report it, do not repair it.** The entry may be
  missing because someone is declaring it right now, and a wrong repair costs more than a stale
  line. The reverse is not drift at all — a declared entry with no directory is simply not
  materialised here.
- **An expired lease is free**, and taking it overwrites the record.
- **A working directory that is dirty or still on a branch was released badly.** Clean it before
  working in it: an idle directory is clean and detached.
