// The plugin body, with every DSH-specific piece passed in.
//
// Keeping `defineTool` and the message factory as parameters is what lets the wiring —
// which is where the subtle mistakes live — be read and tested without a running harness.

import { existsSync } from 'node:fs'
import { join } from 'node:path'

import { watchMailbox } from './feed.js'
import { Bindings, statePath } from './state.js'
import { makeWaker } from './wake.js'

// Two messages that arrive together are one thing to be told about, so a nudge carries
// the batch and costs one turn instead of two. Short enough that nobody waits for it.
const COALESCE_MS = 250

const key = (root, actor) => `${root}\u0000${actor}`

/**
 * Mount the plugin.
 *
 * @param {object} ctx - the cordis context.
 * @param {object} config - `{thrum, root, dshHome, sourceKind}`.
 * @param {object} deps - `{defineTool, createUserMessage, spawn}`.
 */
export function install(ctx, config, deps) {
  const { defineTool, createUserMessage, spawn } = deps
  const command = config.thrum ?? 'thrum'

  const log = (message) => {
    if (ctx.logger?.info) ctx.logger.info(`thrum-mailbox: ${message}`)
    else console.error(`thrum-mailbox: ${message}`)
  }

  const bindings = Bindings.load(statePath(config.dshHome))
  const wake = makeWaker({
    sessionController: ctx.sessionController,
    sessions: ctx.sessions,
    createUserMessage,
    sourceKind: config.sourceKind,
    log,
  })

  /** One running watch per bound (root, actor). */
  const feeds = new Map()
  /** Messages waiting out the coalescing window, per binding. */
  const batches = new Map()

  const flushNudge = async (k) => {
    const batch = batches.get(k)
    if (!batch) return
    batches.delete(k)
    // What is already announced is skipped, not remembered as pending: a restart prints
    // the whole mailbox again, because nothing is re-delivered.
    const fresh = batch.messages.filter((m) => !bindings.announced(batch.root, batch.actor, m.id))
    if (fresh.length === 0) return
    if (await wake(batch.sessionId, fresh)) {
      for (const m of fresh) bindings.announce(batch.root, batch.actor, m.id)
    }
  }

  const onMessage = (root, actor, message) => {
    if (!message?.id) return
    const sessionId = bindings.sessionFor(root, actor)
    if (!sessionId) return // bound, but no session has said it works as this actor yet
    if (bindings.announced(root, actor, message.id)) return

    const k = key(root, actor)
    const batch = batches.get(k) ?? { root, actor, sessionId, messages: [], timer: null }
    // A restart inside the coalescing window can print a message that is also waiting
    // here, and one turn should not name it twice.
    if (batch.messages.some((m) => m.id === message.id)) return
    batch.sessionId = sessionId // a rebind wins, even mid-window
    batch.messages.push(message)
    batches.set(k, batch)
    if (!batch.timer) {
      batch.timer = setTimeout(() => {
        batch.timer = null
        void flushNudge(k).catch((error) => log(`could not wake a session: ${error}`))
      }, COALESCE_MS)
    }
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
      'in THRUM_ACTOR. Messages arrive as ordinary turns; this call only arranges that.',
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
      const root = args.root ?? config.root ?? agent?.session?.header?.cwd
      if (!root) {
        throw new Error(
          'thrum_watch needs a workspace: pass root, configure one, or start the session in the workspace',
        )
      }
      // The tool promises a wake, so the two things that would make one impossible are
      // refused here rather than logged later: a name thrum would never accept, and a
      // root that is not a workspace. "You will be woken" when nothing can arrive is the
      // one lie this tool must not tell.
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
      startFeed(root, args.actor)
      return { watching: args.actor, root, mailbox: `${root}/var/msg/${args.actor}` }
    },
  })

  ctx.effect(() => {
    const unregister = ctx.tools.register(tool)
    return () => {
      unregister()
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
