# Messages: reaching another actor

A message is **temporary work state**. It is not a record, it is never committed, and nothing
durable is derived from it. It exists so that one actor can tell another something now.

```sh
thrum message send dana --subject 'the retry branch is ready' --body '…'
thrum message send dana --subject 'the log' --attach job/add-retry/out/run.log
thrum message list                   # my mailbox, wherever I am
thrum message read <id>
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
| `attachments` | when there are files | each has a name and a size; the bytes sit beside the message |

## It is pushed, and the sender never queues

The fabric places `to` — the site that last took mail for that actor, or its `home` if it has never
said hello — and pushes the message there.

- **If the name is not declared, or that site cannot be reached, the send fails and says which.**
  Nothing waits on the sending side for an actor to appear. `--wait <dur>` asks for a bounded wait,
  which is a bound the caller chose, not a lifetime the fabric invented.
- **Once a site accepts a message, it holds it.** It survives a daemon restart, and it waits for a
  human who reads it a day later exactly as it waits for an agent that polls in a second.
- **`purge` is how a message goes away.** Nothing collects messages, because nothing can know which
  of them still matter.

## What a message is, and is not

- **Whole or not at all.** Attachments are written before the message that names them, so a reader
  never sees a message whose bytes have not arrived.
- **It cannot be retracted or edited**, because it may already have started something. A correction
  is another message with `reply_to` set.
- **Attachments are bytes and nothing more.** They are not assets: no content addressing, no tag,
  no manifest, never the store. Anything that has to survive is published and referred to by name —
  `assets.md`.
- **It is never in a handoff.** A handoff is evidence and it is permanent; a message is process
  information and it is not. Look at your mail before you hand off, because whoever continues will
  not see it otherwise.

## A wake is an agent's business; a human reads

thrum delivers, and it does not wake anybody. Waking is a fact about a harness — a session's turn, a
terminal, a bell — and this design knows none of them.

- **An agent** may run a plugin that binds a name to a session, keeps the place current with
  `thrum actor hello`, and turns `thrum events` into a turn.
- **A human** runs `thrum message list`, or their shell does.

The plugin is an optimization over delivery, never the delivery path. A harness without one loses
only the nudge; the mailbox is still the truth.

## When to send one

- To ask a job's owner about their job, when the brief or the intent is unclear.
- To tell somebody that a job they were waiting on has reached `done`. Nothing does this for you: a
  message is how a waiter hears.
- Never for anything that has to outlive the stretch. That is a handoff, a topic, or an asset.
