# Actors: who you are, and who owns a job

Who is this, and who is on the job? An **actor** is a person or an agent, and thrum treats the two
the same. What makes a name an actor is not a process, a session or a connection: it is a
declaration in the line, and a place where mail is taken.

| | **Standing** | **Where** |
| --- | --- | --- |
| what | this name is a real actor, and what kind | the site that last took mail for it |
| where | `doc/actor/<name>.yaml`, in the line | the fabric |
| lifetime | until retired | until the actor says hello somewhere else |

## Declaring a name

```yaml
# doc/actor/dana.yaml
actor: dana
kind: human              # human | agent
about: Works on the uploader
home: build-01           # where mail waits until this actor says hello somewhere else
registered: 2026-10-16T09:12:00Z
```

```sh
thrum actor declare dana --kind human --about 'Works on the uploader'
thrum actor declare flash-9c2e --kind agent    # an agent that names itself
thrum actor hello                    # my mail is here now
thrum actor list                     # every name that exists, and where each was last known
thrum actor show dana
thrum actor retire dana              # when the actor is finished with
```

- **A name is declared once.** It is unique in the workspace and never reused. Retiring moves the
  declaration to `doc/archive/actor/` with a notice, and the name stays taken, because a handoff
  names it.
- **Declaration is one event, and nothing else creates an actor.** A name must exist before it can
  author a record or be sent anything; a command that finds the name undeclared refuses and says to
  declare it. Nothing makes an actor as a side effect.
- **`kind` says what a reader should expect** — a human reads their mail, an agent may bind its name
  to a session and be woken — and never what is permitted.
- **`home` is where mail waits** when the fabric has never seen the actor say hello. The site that
  declared it is the home unless another is named.

## There is no session

**Nothing in thrum says whether an actor is working.** That cannot be observed, so it is not
modelled: there is no `online`, no `offline`, no ttl, no `present`/`gone`, and no list that thins
itself. A name has a kind and a place, and that is the whole of it.

`thrum actor hello` is the only thing that moves an actor: *my mail is here now*. It is a plain CLI
command, the same for a person at a keyboard and for an agent's wrapper. A plugin may run it on the
actor's behalf, but nothing depends on a plugin existing — `messages.md` has that half.

**An agent that wants its mail as turns binds its name to its session once, at the start** — one
call to the harness's own tool, which the harness's tool list offers; the workspace does not name
it. That is not a session: it records which running session is this actor, and it says hello on the
actor's behalf. Skip it and you lose only the nudge; `messages.md` has the contract and
the design's `doc/extension.md` is the plugin author's side.

**Nothing ages out.** A place is corrected by saying hello somewhere else, and by a delivery that
arrives where the actor no longer is; by nothing else. A daemon may drop a cached address to save
memory, which costs one question and nothing more.

**A human is not a degenerate agent.** A message to a human lands in the mailbox at their site and
waits until they run `thrum message list`. No wake is required, expected, or invented. An agent is
delivered to the same way.

## Ownership: who a job was promised to

A job has one `owner` in its brief, or none.

- **Opening sets it**, and it may name somebody other than whoever does the work.
- **An owner may reassign it or give it up** — `thrum job reassign <id> --to <actor>`,
  `thrum job relinquish <id>`. Giving it up makes the job unowned, and therefore claimable by
  whoever takes it up.
- **An unowned job may be claimed** by the next actor to take it up (`thrum job claim <id>`),
  because there is nobody left to ask.
- **An owner who has become unreachable** is given up or reassigned by a person on their behalf: a
  job owned by an actor who no longer exists would otherwise be stuck forever.
- **Ownership is not permission.** An actor may work a job owned by somebody else under that
  owner's authority. The owner is who the work was promised to, and who to ask when the brief is
  unclear.
- **Ownership is not the body.** Reassigning or giving up a job does not hand over its text. Only
  the actor currently working a job changes its `## Goal`, `## Scope`, `## Acceptance criteria` or
  the sections after them, because that is the understanding that is current.

## Who is on a job right now

**Nothing records it, and nothing guesses.** Whether an actor is working cannot be observed, so the
design does not pretend to. What can be read is what was written: the `owner`, and the newest
handoff with its `agent` and its `ended`. The fabric adds only where that actor was last known to
be — an address, not a claim.

A job whose newest handoff is old is a job whose last actor has not written anything. That is
exactly what it looks like, and reading it as anything more would be a guess.

If two actors do work one job, the handoffs show it. The record is the evidence, and the next actor
reads it rather than a claim file.

## One name, and it is yours

Never act as another actor, and never watch a name that is not yours: a record under a name you
took over makes somebody else's work read as yours. If a name you reach for is already declared,
it is somebody's — ask them.
