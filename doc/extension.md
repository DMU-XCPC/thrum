# Extension interfaces: the event stream, the watch, and the plugin

This is for whoever writes a harness plugin. It is **not** the actor's manual: an actor's loop is
`message list`, `read`, `reply` and the mailbox, and none of the interfaces here appear in it.

The design's position (`doc/thrum.md`): waking is the harness's business. thrum delivers; a plugin
turns "a message arrived" into a new turn. The plugin is an optimization over delivery and never
the delivery path: **the mailbox is the truth; the stream is a hint.**

## The interfaces

| Interface | What it gives | State |
| --- | --- | --- |
| `thrum events --json` (and `--watch`) | one JSON event per line: a message delivered, a job's state changed, a shared history moved | design; the current CLI has it only as the hidden `daemon events` |
| `thrum message list --watch --json` | the mailbox's backlog first, then one JSON message per line as it arrives | current |
| `--json` on the read commands | the machine-readable form | current |
| the mailbox (`var/msg/<actor>/<id>.md`, `<id>/` for attachments) | the truth | current; a plugin never writes it |

A `message.delivered` event names the id, the sender, the time and the subject — never the body.
The body is what `thrum message read <id>` is for, and a session log is a worse place for it.

## The binding

The tool is the harness's. For DSH it is `thrum_watch(actor, root?)`; another harness ships its
own. The plugin records `(workspace, actor) → session`, calls `thrum actor hello` for the actor,
and subscribes for that actor. **The workspace does not name the tool and no convention lists
it**: the session finds it in the harness's tool list, and only the harness knows what it
installed.

## The wake

The nudge names the messages and their ids; the body arrives through `message read`. DSH's
`followup` puts the nudge in the session's inbox and `flush` makes it durable. Announced ids are
remembered, capped and pruned; a repeated nudge is the only cost of forgetting an old one.

## Failure and state

- The stream can die; the plugin restarts it with a backoff and prints the backlog on start.
- A failed `actor hello` is logged, not fatal: the mailbox is still the truth and the watch
  still starts. Until it succeeds, mail is only pushed to the actor's last known site.
- A missed event is never a lost message; the mailbox is the source.
- The daemon stops itself when no neighbour answers and starts again on the next command; the
  stream's reconnect covers it.
- The plugin's state lives in the harness's home (DSH: `$DSH_HOME`), never under the workspace's
  `var/`.
- The plugin never inserts a message and never purges one.

## What the current CLI provides

`thrum events [--json]` is the local event stream, and `thrum message list --watch [--json]`
prints the mailbox's backlog and then each arrival, one JSON object per line. The first event kind
is `message.delivered`; the job-state and history-moved kinds the design names are not emitted yet,
and no plugin depends on them.

## The DSH companion

`harness/dsh/` is one implementation: a DSH plugin that binds `(workspace, actor) → session`,
watches the mailbox, and nudges the session. It is outside the workspace format, it uses only the
interfaces above, and it is the reference for another harness's plugin. Its `README.md` has the
tool, the state file and the install.
