# A DSH harness plugin for a thrum workspace

This wakes a DSH session when the actor it is working as receives a message.

It is **not part of the workspace format**. The design says the plugin is an optimization over
delivery and that waking is the harness's business (`doc/thrum.md`); how a particular agent or
team works is an agreement between that agent and whoever runs it, and this is one such
agreement: it is written against DSH, it lives outside any workspace, and a workspace does
not know it exists. Everything it uses is something the design already promises — the
`thrum` command line, `--json` output, the event stream, the mailbox under `var/msg/<actor>/`,
and the fact that a message appears whole or not at all. Nothing here may become a reason for
the format to change; any other harness can have an equivalent.

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
  │ thrum messages   │            │  feed: reads the stream│     │ agent.followup() │
  │  --json (JSONL)  │──stream───▶│  registry: actor→session│────▶│ session/flush    │
  └──────────────────┘            └────────────────────────┘     └──────────────────┘
                                              ▲
                                    tool call: thrum_watch(actor)
```

**The feed** is `thrum message list --watch --json`, one message per line, the backlog first. A
message names the id, the sender and the subject, never the body, so the nudge says what arrived and
`thrum message read` is where the body is. The stream is a hint — a missed event is never a lost
message — and the backlog a starting stream prints is what arrived while nobody was watching.

**The binding** is the hard half, and it is why there is a tool. A watcher's callback runs
outside any agent's asynchronous chain (DSH's initiator scope covers only the driver's own
work, and a host timer must explicitly opt out of it), so a plugin cannot ask "which agent
am I?" when a message lands. And the actor name is a host-process fact, not a per-session
one. So the session says who it is: the agent calls `thrum_watch(actor)` once, and the
plugin records `<root, actor> → sessionId` and runs `thrum actor hello` for it.

That call is the actor's initialization step. The workspace manual states the obligation —
*Binding, waking, and reading* in `.agents/skills/workspace-actor/messages.md` — and the tool is
the harness's: the session sees `thrum_watch` in its tool list, and the workspace does not name it.

## The tool

```text
thrum_watch(actor, root?)  →  { watching, root, mailbox }
```

- `actor` — the name this session works as. Required, and explicit: the agent chose it, so
  it is the one thing the plugin must not guess.
- `root` — the workspace. Omitted, it is the plugin's configured root, and failing that the
  session's own working directory, because the workspace root is where an actor starts.

Calling it says hello for the actor (`thrum actor hello`), so the fabric knows where to push, and
subscribes to `thrum message list --watch --json` for that name. Calling it twice rebinds, which is what a session that
switched jobs wants.

The session must be an ordinary one. A subagent session's delivery belongs to DSH's subagent
routing, and `sessionController` refuses to resolve it for a follow-up; the tool refuses the
call at bind with that reason instead of promising a wake it cannot deliver. The actor must
already be declared: the tool says hello, and hello refuses an undeclared name.

## What it does when something goes wrong

**Nothing here may lose a message, and nothing here may stop work.** The mailbox is the
truth; this plugin is a convenience on top of it, and every failure is a missing nudge
rather than a missing message.

- `thrum` missing, or the watch process dying: log it, and let the tool's next call start a
  new one. The backlog is printed again on start, so nothing that arrived meanwhile is lost.
- `thrum actor hello` failing, timing out, or not acknowledging (`<actor> is here`): log it
  and start the watch anyway. The mailbox is still the truth; until hello succeeds, mail is
  only pushed to the actor's last known site. An older `thrum` that exits 0 while printing
  `actor` help is reported as a hello that was not acknowledged, so the configured `thrum`
  must be the current CLI.
- The bound session being gone, busy, or unwritable: log it. The message stays where it is.
- A message that has already been announced is not announced again. The set of announced ids
  is kept next to the binding, capped, and pruned — a message id is unique forever, so the
  only cost of forgetting an old one is one repeated nudge.
- The plugin never inserts anything into the mailbox and never purges one. Reading is
  `thrum message read`, and removing a message is the recipient's decision.

## State, and where it lives

One JSON file under the harness home (`$DSH_HOME`, else `~/.dsh`), holding the bindings and
the announced ids. **Not in the workspace**: the workspace is a shared thing whose `var/` is
the tooling's, and a harness's bookkeeping has no business in it.

## How the tool reaches the session

The tool is part of the harness's surface. When this plugin is installed, the DSH session sees
`thrum_watch` in its tool list, and the workspace does not name it: a tool named after one harness
is not true of every workspace, and `doc/convention/` is the project's internal policy, not a
registry of harness plugins. The workspace manual states the obligation — bind the name if your
harness can wake you — and stops there; the harness supplies the answer.

A project that runs DSH installs this plugin; another harness ships its own equivalent. Neither is
a reason for the workspace format to change. `doc/extension.md` is the interface both sides use.

## Installing it

It is a DSH bundle as well as a cordis plugin package: the entry exports `apply`, `name` and
`inject`, and `cordis.patch.yml` beside it carries the row that mounts it, so installing the
package into a profile is the whole installation:

```sh
dsh plugin --profile web add /path/to/harness/dsh
```

What follows is mounting it by hand instead — in a patch layer of your own, or where the
package is not installed. Three things about that are easy to get wrong, and all three were
checked against a real `dsh` boot rather than against cordis alone:

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

The `thrum` named by `config.thrum`, or found on PATH, must be the current CLI: binding runs
`thrum actor hello`, and an older binary can exit 0 while printing `actor` help. The plugin
logs that as an unacknowledged hello and keeps watching; it does not fall back to an older
spelling. Point the row at the current binary when PATH has an old one.

## What a restart does

A watch lives in the host process, so a host that has restarted has none — while the session
that asked still believes it is arranged. The bindings are what say which mailboxes this host
is responsible for, so mounting picks them up again: every recorded `(workspace, actor)` whose
workspace is still there gets its watch back, and a recorded one whose workspace is not there
is skipped and reported rather than forgotten, because a workspace can live on a volume that
has not been mounted yet.

A host that is killed outright leaves its watchers behind: they survive their parent and are
adopted by init, then die on the first message they try to report, because the pipe they were
writing to has no reader. That costs one idle process per binding until the next message
arrives, and it cannot cost a wake — every watcher holds its own watch on the mailbox, so the
host that mounted afterwards reports the message whichever orphan is still around.

What that costs is one child process per binding, so a host that has been used for a long time
starts as many as there are mailboxes it has ever watched for. `thrum_watch` in a session is
what adds one, and nothing prunes them yet: a binding is dropped only if somebody writes the
file without it.

## Two hosts, one home

Two hosts can share one harness home — the web app and something else on one machine, or two
profiles — and each of them binds actors at its own moments. A save is therefore
read-merge-write under a lock, not "write back what was loaded at boot": a binding this
process has never seen is kept, and a binding it knows is its own, which is what lets the cap
on announced ids actually evict. Without that, the second host's save erased the first host's
bindings, and an erased binding is a session that stops being woken with nothing said. The
lock is a directory (creating one is atomic), the merge is what does the work, and a lock left
behind by a host that died is taken away after five seconds rather than trusted.
