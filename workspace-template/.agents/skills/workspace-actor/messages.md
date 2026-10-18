# Messages: reaching another actor

A message is **temporary work state**. It is not a record, it is never committed, and nothing
durable is derived from it. It exists so that one actor can tell another something now.

```sh
thrum message send dana --subject 'the retry branch is ready' --body '…'
thrum message send dana --subject 'the log' --attach job/add-retry/out/run.log
thrum message send dana --subject 'the payload' --attach build/out.tar.zst:payload.tar.zst
thrum message list                   # my mailbox, wherever I am
thrum message read <id>              # prints the text and the attachments' names and paths
thrum message read <id> --out ./inbox
thrum message read <id> --out ./inbox --force
thrum message reply <id> --body '…'
thrum message purge <id>
```

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | always | opaque and unique; what a reply and any other tool names |
| `from`, `to` | always | actor names |
| `sent` | always | RFC 3339 UTC |
| `subject` | always | one line, for a person reading a list; nothing parses it |
| `reply_to` | when it answers one | the `id` being answered |
| `job` | when it is about one | a job id, as a topic marker and nothing more |
| `attachments` | when there are files | each has a name, a size, a sha256 and a MIME type; the bytes sit beside the message |

**Attachments are arbitrary bytes, and a message carries at most ten megabytes of them.** The
bytes are exact — base64 is only the wire format — and each file keeps its name, size, sha256 and
detected MIME. A duplicate name is refused rather than silently overwriting; `--attach
<src>:<name>` renames one; a directory is tarred first. Anything larger, or anything that has to
outlive the message, goes to the store — `asset ingest` then `asset publish` — and the message
carries the name.

## It is pushed, and the sender never queues

The fabric places `to` — the site that last took mail for that actor, or its `home` if it has never
said hello — and pushes the message there, directly or through a relay.

- **If the name is not declared, or no route reaches the site, the send fails and says which.**
  Nothing waits on the sending side for an actor to appear. `--wait <dur>` asks for a bounded wait,
  which is a bound the caller chose, not a lifetime the fabric invented.
- **A stale place costs one retry.** The receiving site answers `not here` with the actor's current
  site, and the sender retries there; a place is a hint, corrected by `actor hello`.
- **Once a site accepts a message, it holds it.** It survives a daemon restart, and it waits for a
  human who reads it a day later exactly as it waits for an agent that polls in a second.
- **`purge` is how a message goes away.** Nothing collects messages, because nothing can know which
  of them still matter.

## What a message is, and is not

- **Whole or not at all.** Attachments are written before the message that names them, so a reader
  never sees a message whose bytes have not arrived.
- **It cannot be retracted or edited**, because it may already have started something. A correction
  is another message with `reply_to` set.
- **Attachments are bytes, not assets.** They are not content-addressed, tagged or manifested, and
  they are never in the store. Anything that has to survive is published and referred to by name —
  `assets.md`.
- **It is never in a handoff.** A handoff is evidence and it is permanent; a message is process
  information and it is not. Look at your mail before you hand off, because whoever continues will
  not see it otherwise.

## Binding, waking, and reading

thrum delivers, and it does not wake anybody. Waking is a fact about a harness — a session's turn,
a terminal, a bell — and this design knows none of them.

**A harness with a thrum plugin gives you a tool for it.** That tool binds this session to your
name, says hello, and turns a delivered message into a new turn. If you see it in your tool list,
call it once at the start of the stretch. The workspace does not name it and no file here lists
it: only the harness knows what it installed, and a tool named after one harness is not true of
every workspace.

- **The plugin** watches delivery (through the extension interfaces in the design's `doc/extension.md` — an
  actor never needs them) and nudges the session. It is an optimization over delivery, never the
  delivery path.
- **A human** runs `thrum message list`, or their shell does.
- **Without the tool** nothing about delivery changes: the mailbox is the truth, and
  `thrum message list` finds what arrived while nobody was listening. You are only slower to hear.

If your harness can run a subagent and the mail is more than a quick read — a batch, a long
thread, attachments — a one-shot, read-only triage subagent is a good way to keep this session's
context clean. It reads with the same actor name (`--actor <name>`), returns a digest of each
message's id, sender, subject, request, deadline, and whether a reply is needed, and does not
reply, send, purge, or claim work. You remain the actor who decides and answers; the mailbox and
the name do not move to the helper.

## When to send one

- To ask a job's owner about their job, when the brief or the intent is unclear.
- To tell somebody that a job they were waiting on has reached `done`. Nothing does this for you: a
  message is how a waiter hears.
- To hand over a small payload (≤ 10 MB) that the next stretch needs and that does not belong in
  the store.
- Never for anything that has to outlive the stretch. That is a handoff, a topic, or an asset.
