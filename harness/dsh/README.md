# A DSH harness plugin for a thrum workspace

This wakes a DSH session when the actor it is working as receives a message.

It is **not part of the workspace format**. `doc/design.md` says how a particular agent or
team works is an agreement between that agent and whoever runs it, and this is one such
agreement: it is written against DSH, it lives outside any workspace, and a workspace does
not know it exists. Everything it uses is something the design already promises — the
`thrum` command line, `--json` output, the mailbox under `var/msg/<actor>/`, and the fact
that a message appears whole or not at all. Nothing here may become a reason for the
format to change; any other harness can have an equivalent.

## Why it is needed at all

Messages are files in a mailbox. Nothing pushes them at anybody: an actor finds out by
looking. A working agent looks (the skill's loop says so), but an **idle** agent does not,
and an actor that is waiting to be told something is exactly the actor a message is for.

So the plugin does one thing: it turns "a message arrived" into "this session has a new
turn to run".

## The two halves

```text
        thrum                            the plugin                      DSH
  ┌──────────────────┐            ┌────────────────────────┐     ┌──────────────────┐
  │ var/msg/<actor>/ │            │  feed: reads the stream│     │ agent.followup() │
  │  <id>.md         │──watch────▶│  registry: actor→session│────▶│ session/flush    │
  └──────────────────┘  JSONL     └────────────────────────┘     └──────────────────┘
                                              ▲
                                    tool call: thrum_watch(actor)
```

**The feed** is `thrum inbox --watch --json`, one message per line. It prints what is
already in the mailbox before it starts reporting arrivals, which is the only chance to see
what arrived while nobody was watching — nothing is re-delivered.

**The binding** is the hard half, and it is why there is a tool. A watcher's callback runs
outside any agent's asynchronous chain (DSH's initiator scope covers only the driver's own
work, and a host timer must explicitly opt out of it), so a plugin cannot ask "which agent
am I?" when a message lands. And the actor name is a host-process fact, not a per-session
one. So the session says who it is: the agent calls `thrum_watch(actor)` once, and the
plugin records `<root, actor> → sessionId`.

That call belongs where the agent already is at the start of a stretch, and the skill's
loop is where it goes — see *What this needs from the manual*, below.

## The tool

```text
thrum_watch(actor, root?)  →  { watching, root, mailbox }
```

- `actor` — the name this session works as. Required, and explicit: the agent chose it, so
  it is the one thing the plugin must not guess.
- `root` — the workspace. Omitted, it is the plugin's configured root, and failing that the
  session's own working directory, because the workspace root is where an actor starts.

Calling it twice rebinds, which is what a session that switched jobs wants.

## What it does when something goes wrong

**Nothing here may lose a message, and nothing here may stop work.** The mailbox is the
truth; this plugin is a convenience on top of it, and every failure is a missing nudge
rather than a missing message.

- `thrum` missing, or the watch process dying: log it, and let the tool's next call start a
  new one. The backlog is printed again on start, so nothing that arrived meanwhile is lost.
- The bound session being gone, busy, or unwritable: log it. The message stays where it is.
- A message that has already been announced is not announced again. The set of announced ids
  is kept next to the binding, capped, and pruned — a message id is unique forever, so the
  only cost of forgetting an old one is one repeated nudge.
- The plugin never inserts anything into the mailbox and never purges one. Reading is
  `thrum read`, and removing a message is the recipient's decision.

## State, and where it lives

One JSON file under the harness home (`$DSH_HOME`, else `~/.dsh`), holding the bindings and
the announced ids. **Not in the workspace**: the workspace is a shared thing whose `var/` is
the tooling's, and a harness's bookkeeping has no business in it.

## What a project has to add: one convention

The plugin cannot be useful if nobody calls the tool, and the actor is only told to call it
if the project says so. That sentence does **not** belong in the workspace material: the
tooling-owned skeleton states what is true of every workspace, and a tool named after one
harness's plugin is not that.

It belongs where a project's own agent policy already goes — a convention, which is
versioned, travels with the records, and is read in number order before work starts:

```markdown
# 01-waking-on-messages

Call `thrum_watch` with your actor name once at the start of a stretch, before you begin
work. Messages then arrive as ordinary turns instead of waiting for you to look. A session
that skips it is not wrong — it will find its mail the next time it checks — it is only
slower to hear.
```

A project without DSH writes the equivalent for whatever it runs, which is the point: this
plugin is one deployment's answer, not the format's.

## Installing it

It is an ordinary cordis plugin package: the entry exports `apply`, `name` and `inject`.

Three things about installing it are easy to get wrong, and all three were checked against
a real `dsh` boot rather than against cordis alone:

**It is an insert, not a patch.** A bare `- id: … / name: …` entry in a patch layer targets
an *existing* entry, so a new one is dropped with `patch: entry "thrum-mailbox" not found`.
A new plugin is appended with `insert:`, as above.

**`name` is a module specifier or a file, never a directory.** `file:///…/harness/dsh` fails
with `ERR_UNSUPPORTED_DIR_IMPORT` — the loader imports the entry as an ES module, and ES
modules do not do directory imports — and the harness reports it only as `failed to import`,
without the cause. Install the package into the profile (`dsh plugin --profile web add
/path/to/harness/dsh`) and name it, or point the name at the entry file itself
(`'…/harness/dsh/index.js'`).

**The profile has to provide `sessionController`.** The `web` profile does; `headless` and
`tui` do not, and there the plugin is not broken so much as never reached: cordis holds it at
`pending (waiting for service: sessionController)`, and the boot log says exactly that and
nothing else. There is no wake without a session to wake.

```yaml
- insert:
    - id: thrum-mailbox
      name: 'thrum-mailbox-dsh'   # installed into the profile; see below
      config:
        thrum: thrum        # the command to run, if it is not on PATH
        dshHome: ~/.dsh     # where the bindings are kept
```

Mount it where the sessions live (a profile's bundle or `cordis.patch.yml`), not per agent:
it holds the bindings for every actor on this host, and the tool it registers is what ties a
session to one of them.

## What a restart does

A watch lives in the host process, so a host that has restarted has none — while the session
that asked still believes it is arranged. The bindings are what say which mailboxes this host
is responsible for, so mounting picks them up again: every recorded `(workspace, actor)` whose
workspace is still there gets its watch back, and a recorded one whose workspace is not there
is skipped and reported rather than forgotten, because a workspace can live on a volume that
has not been mounted yet.

What that costs is one child process per binding, so a host that has been used for a long time
starts as many as there are mailboxes it has ever watched for. `thrum_watch` in a session is
what adds one, and nothing prunes them yet: a binding is dropped only if somebody writes the
file without it.
