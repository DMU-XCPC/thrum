// The plugin body, with every DSH-specific piece passed in.
//
// Keeping `defineTool` and the message factory as parameters is what lets the wiring —
// which is where the subtle mistakes live — be read and tested without a running harness.

import { existsSync } from 'node:fs'
import { join } from 'node:path'

import { watchMailbox } from './feed.js'
import { sayHello as sayHelloWith } from './hello.js'
import { Bindings, statePath } from './state.js'
import { makeWaker } from './wake.js'

// Two messages that arrive together are one thing to be told about, so a nudge carries
// the batch and costs one turn instead of two. Short enough that nobody waits for it.
const COALESCE_MS = 250
// A wake can fail because the session is busy, being written, or briefly gone. The
// message is still in the mailbox, but a long-lived watch would never report it again, so
// a failed batch is retried with a backoff instead of being dropped.
const WAKE_RETRY_MS = 2000
const MAX_WAKE_RETRY_MS = 60000
const MAX_WAKE_ATTEMPTS = 5

const key = (root, actor) => `${root}\u0000${actor}`

/**
 * Mount the plugin.
 *
 * @param {object} ctx - the cordis context.
 * @param {object} config - `{thrum, root, dshHome, sourceKind, coalesceMs, helloTimeoutMs, wakeRetryMs, maxWakeAttempts}`.
 * @param {object} deps - `{defineTool, createUserMessage, spawn, sayHello?}`.
 */
export function install(ctx, config, deps) {
  const { defineTool, createUserMessage, spawn, sayHello = sayHelloWith } = deps
  const command = config.thrum || 'thrum'
  const coalesceMs = config.coalesceMs ?? COALESCE_MS
  const wakeRetryMs = config.wakeRetryMs ?? WAKE_RETRY_MS
  const maxWakeAttempts = config.maxWakeAttempts ?? MAX_WAKE_ATTEMPTS

  const log = (message) => {
    if (ctx.logger?.info) ctx.logger.info(`thrum-mailbox: ${message}`)
    else console.error(`thrum-mailbox: ${message}`)
  }

  const bindings = Bindings.load(statePath(config.dshHome || undefined))
  const wake = makeWaker({
    sessionController: ctx.sessionController,
    sessions: ctx.sessions,
    createUserMessage,
    sourceKind: config.sourceKind,
    log,
  })

  /** One running watch per bound (root, actor). */
  const feeds = new Map()
  /** Messages waiting out the coalescing window or a retry, per binding. */
  const batches = new Map()

  const schedule = (k, delay) => {
    const batch = batches.get(k)
    if (!batch || batch.timer || batch.flushing) return
    batch.timer = setTimeout(() => {
      batch.timer = null
      void flushNudge(k).catch((error) => fail(k, error.message))
    }, delay)
  }

  const fail = (k, reason) => {
    const batch = batches.get(k)
    if (!batch) return
    batch.attempts += 1
    if (batch.attempts >= maxWakeAttempts) {
      log(
        `could not wake ${batch.sessionId} after ${batch.attempts} attempts (${reason}); ` +
          'the messages are still in the mailbox',
      )
      return
    }
    const delay = Math.min(wakeRetryMs * 2 ** (batch.attempts - 1), MAX_WAKE_RETRY_MS)
    log(`could not wake ${batch.sessionId} (${reason}); retrying in ${delay}ms`)
    schedule(k, delay)
  }

  const flushNudge = async (k) => {
    const batch = batches.get(k)
    if (!batch || batch.flushing || batch.cancelled) return
    if (bindings.sessionFor(batch.root, batch.actor) !== batch.sessionId) {
      // A rebind or unwatch won the race; the new owner will see the backlog.
      stopBatch(k)
      return
    }
    // What is already announced is skipped, not remembered as pending: a restart prints
    // the whole mailbox again, because nothing is re-delivered.
    batch.messages = batch.messages.filter((m) => !bindings.announced(batch.root, batch.actor, m.id))
    const fresh = batch.messages
    if (fresh.length === 0) {
      batches.delete(k)
      return
    }
    batch.flushing = true
    let ok = false
    try {
      ok = await wake(batch.sessionId, fresh)
    } catch (error) {
      batch.flushing = false
      fail(k, error.message)
      return
    }
    batch.flushing = false
    if (batch.cancelled || batches.get(k) !== batch) return
    if (!ok) {
      fail(k, 'the wake was not acknowledged')
      return
    }
    for (const m of fresh) bindings.announce(batch.root, batch.actor, m.id)
    const announced = new Set(fresh.map((m) => m.id))
    batch.messages = batch.messages.filter((m) => !announced.has(m.id))
    batch.attempts = 0
    if (batch.messages.length > 0) schedule(k, coalesceMs)
    else batches.delete(k)
  }

  const onMessage = (root, actor, message) => {
    if (!message?.id) return
    const sessionId = bindings.sessionFor(root, actor)
    if (!sessionId) return // bound, but no session has said it works as this actor yet
    if (bindings.announced(root, actor, message.id)) return

    const k = key(root, actor)
    const batch = batches.get(k) ?? {
      root,
      actor,
      sessionId,
      messages: [],
      timer: null,
      flushing: false,
      attempts: 0,
    }
    // A restart inside the coalescing window can print a message that is also waiting
    // here, and one turn should not name it twice.
    if (batch.messages.some((m) => m.id === message.id)) return
    batch.sessionId = sessionId // a rebind wins, even mid-window
    // New mail is a reason to try a session that had already exhausted its retries.
    if (batch.attempts >= maxWakeAttempts) batch.attempts = 0
    batch.messages.push(message)
    batches.set(k, batch)
    if (!batch.flushing) schedule(k, coalesceMs)
  }

  const stopBatch = (k) => {
    const batch = batches.get(k)
    if (batch) {
      batch.cancelled = true
      if (batch.timer) clearTimeout(batch.timer)
    }
    batches.delete(k)
  }

  const startFeed = (root, actor) => {
    const k = key(root, actor)
    feeds.get(k)?.stop()
    feeds.set(
      k,
      watchMailbox({
        spawn,
        command,
        root,
        actor,
        onMessage: (message) => onMessage(root, actor, message),
        onError: (error) => log(error.message),
      }),
    )
  }

  const tool = defineTool({
    name: 'thrum_watch',
    description:
      'Have this session woken when the actor it works as receives a message in a thrum ' +
      'workspace. Call it once, before you start working, with the same actor name you set ' +
      'in THRUM_ACTOR. Messages arrive as ordinary turns; this call only arranges that. ' +
      'Only an ordinary session can be bound: a subagent session belongs to the harness.',
    parameters: {
      actor: {
        type: 'string',
        required: true,
        description: 'The actor name this session works as, as THRUM_ACTOR carries it.',
      },
      root: {
        type: 'string',
        description: 'The workspace root. Defaults to this session working directory.',
      },
    },
    output: {
      // The harness's value-schema DSL is narrower than JSON Schema: an object node takes
      // `properties` and an explicit `additionalProperties`, and refuses `required`. That
      // is only visible against the real `defineTool`, which is why one test asks it.
      schema: {
        type: 'object',
        additionalProperties: false,
        properties: {
          watching: { type: 'string' },
          root: { type: 'string' },
          mailbox: { type: 'string' },
        },
      },
      render: (_args, value) => [
        { type: 'text', text: `this session will be woken for ${value.watching} in ${value.root}` },
      ],
    },
    async execute(args, exec) {
      const agent = exec.agent
      const sessionId = agent?.sessionId ?? agent?.session?.id
      if (!sessionId) {
        throw new Error('thrum_watch needs the session it was called from, and the call carried none')
      }
      // A subagent session belongs to DSH's subagent routing. sessionController refuses to
      // resolve it for a follow-up, so this tool cannot wake it: refusing at bind is what
      // keeps "you will be woken" from being a promise nothing can keep.
      if (agent?.session?.header?.origin === 'subagent') {
        throw new Error(
          'thrum_watch only binds an ordinary session: this session is a subagent, and DSH ' +
            'owns its delivery. Call thrum_watch from the session that should be woken.',
        )
      }
      const root = args.root ?? (config.root || undefined) ?? agent?.session?.header?.cwd
      if (!root) {
        throw new Error(
          'thrum_watch needs a workspace: pass root, configure one, or start the session in the workspace',
        )
      }
      // The tool promises a wake, so what would make one impossible is refused here rather
      // than logged later: a session DSH will not resolve for a follow-up, a name thrum
      // would never accept, and a root that is not a workspace. "You will be woken" when
      // nothing can arrive is the one lie this tool must not tell.
      if (!isSlug(args.actor)) {
        throw new Error(
          `${JSON.stringify(args.actor)} is not an actor name: an actor name is a slug, ` +
            'lowercase letters, digits and hyphens, up to 64 characters',
        )
      }
      if (!isWorkspace(root)) {
        throw new Error(`${root} is not a thrum workspace: there is no var/ and no .git there`)
      }
      bindings.bind(root, args.actor, sessionId)
      const pending = batches.get(key(root, args.actor))
      if (pending) pending.sessionId = sessionId // a rebind wins, even with a batch waiting
      startFeed(root, args.actor)
      // Start the watch before saying hello: hello can be slow or time out, and the watch
      // must not wait for it. The mailbox's backlog covers anything that arrived meanwhile;
      // hello only changes which site mail is pushed to from now on.
      let hello
      try {
        hello = await sayHello({
          spawn,
          command,
          root,
          actor: args.actor,
          timeoutMs: config.helloTimeoutMs,
        })
      } catch (error) {
        hello = { ok: false, reason: error.message }
      }
      if (!hello?.ok) {
        log(`watching ${args.actor}, but could not say hello: ${hello?.reason ?? 'no answer'}`)
      }
      return { watching: args.actor, root, mailbox: `${root}/var/msg/${args.actor}` }
    },
  })

  const unwatch = defineTool({
    name: 'thrum_unwatch',
    description:
      'Stop this session being woken for an actor in a thrum workspace. Use it when the ' +
      'session switches jobs or stops working as that actor; the mailbox and its messages ' +
      'stay where they are.',
    parameters: {
      actor: {
        type: 'string',
        required: true,
        description: 'The actor name this session was bound to.',
      },
      root: {
        type: 'string',
        description: 'The workspace root. Defaults to this session working directory.',
      },
    },
    output: {
      schema: {
        type: 'object',
        additionalProperties: false,
        properties: {
          unwatched: { type: 'boolean' },
          root: { type: 'string' },
          actor: { type: 'string' },
        },
      },
      render: (_args, value) => [
        {
          type: 'text',
          text: value.unwatched
            ? `this session will no longer be woken for ${value.actor} in ${value.root}`
            : `this session was not watching ${value.actor} in ${value.root}`,
        },
      ],
    },
    async execute(args, exec) {
      const agent = exec.agent
      const sessionId = agent?.sessionId ?? agent?.session?.id
      if (!sessionId) {
        throw new Error('thrum_unwatch needs the session it was called from, and the call carried none')
      }
      if (agent?.session?.header?.origin === 'subagent') {
        throw new Error(
          'thrum_unwatch only works for an ordinary session: this session is a subagent, ' +
            'and DSH owns its delivery.',
        )
      }
      const root = args.root ?? (config.root || undefined) ?? agent?.session?.header?.cwd
      if (!root) {
        throw new Error(
          'thrum_unwatch needs a workspace: pass root, configure one, or start the session in the workspace',
        )
      }
      if (!isSlug(args.actor)) {
        throw new Error(
          `${JSON.stringify(args.actor)} is not an actor name: an actor name is a slug, ` +
            'lowercase letters, digits and hyphens, up to 64 characters',
        )
      }
      // Unwatching does not need the workspace to still exist: a binding on a volume that
      // is gone is exactly the kind a session may want to retire.
      const bound = bindings.sessionFor(root, args.actor)
      if (bound === undefined) return { unwatched: false, root, actor: args.actor }
      if (bound !== sessionId) {
        throw new Error(
          `${args.actor} in ${root} is bound to another session; only that session can unwatch it`,
        )
      }
      const k = key(root, args.actor)
      feeds.get(k)?.stop()
      feeds.delete(k)
      stopBatch(k)
      bindings.unbind(root, args.actor)
      return { unwatched: true, root, actor: args.actor }
    },
  })

  ctx.effect(() => {
    const unregisterWatch = ctx.tools.register(tool)
    const unregisterUnwatch = ctx.tools.register(unwatch)
    // A watch lives in this process, so a host that has restarted has none — while the
    // session that asked still believes it is arranged. The bindings are what say which
    // mailboxes this host is responsible for, so they are picked up again here. A root that
    // is not there is skipped rather than forgotten: a workspace can live on a volume that
    // has not been mounted yet, and a binding costs nothing to keep.
    let carried = 0
    let absent = 0
    for (const { root, actor } of bindings.entries()) {
      if (!isWorkspace(root)) {
        absent++
        continue
      }
      startFeed(root, actor)
      carried++
    }
    if (carried > 0 || absent > 0) {
      log(
        `watching ${carried} mailbox(es) carried over from before` +
          (absent > 0 ? `, ${absent} skipped because their workspace is not there` : ''),
      )
    }
    return () => {
      unregisterWatch()
      unregisterUnwatch()
      for (const feed of feeds.values()) feed.stop()
      feeds.clear()
      for (const batch of batches.values()) if (batch.timer) clearTimeout(batch.timer)
      batches.clear()
    }
  }, 'thrum-mailbox')
}

// The two checks above. A slug is what thrum accepts as an actor name, and the workspace
// check is deliberately shallow: enough to tell a workspace from any other directory,
// without reimplementing thrum's own reading of it.
const SLUG = /^[a-z0-9]+(-[a-z0-9]+)*$/

function isSlug(name) {
  return typeof name === 'string' && name.length <= 64 && SLUG.test(name)
}

function isWorkspace(root) {
  return existsSync(join(root, 'var')) && existsSync(join(root, '.git'))
}
