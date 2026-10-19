// The wiring: a session says who it is, a watch starts, a message arrives, a session wakes.
//
// This is the part with the subtle mistakes in it, so it is tested whole — with a fake
// harness and a fake thrum, and no process anywhere.

import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { mkdirSync, mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { PassThrough } from 'node:stream'
import test from 'node:test'

import { install } from '../lib/install.js'
import { Bindings, statePath } from '../lib/state.js'

// The tool refuses a root that is not a workspace, so the fixtures are workspaces: enough
// of one to be recognised, which is all this unit is about.
function workspace(name) {
  const dir = join(mkdtempSync(join(tmpdir(), 'thrum-ws-')), name)
  mkdirSync(join(dir, 'var'), { recursive: true })
  mkdirSync(join(dir, '.git'), { recursive: true })
  return dir
}
const WS = workspace('ws')
const OTHER = workspace('other')


const COALESCE = 250
const settle = () => new Promise((resolve) => setTimeout(resolve, COALESCE + 150))

class FakeChild extends EventEmitter {
  constructor() {
    super()
    this.stdout = new PassThrough()
    this.stderr = new PassThrough()
    this.stdin = new PassThrough()
    this.written = ''
    this.stdin.on('data', (chunk) => {
      this.written += chunk
    })
  }

  say(message) {
    this.stdout.write(`${JSON.stringify({ kind: 'message.delivered', actor: 'alice', message })}\n`)
  }

  kill() {
    this.emit('exit', 0, null)
  }
}

/** A harness, a thrum, and the sessions that can be woken. */
function world() {
  const w = {
    woken: [],
    logs: [],
    children: [],
    spawned: [],
    agents: new Map(),
    registered: null,
    tools: {},
    dispose: null,
    /** Make a session that can be woken, and remember what it was told. */
    session(sessionId, cwd = WS, header = {}) {
      const agent = {
        sessionId,
        session: { id: sessionId, header: { cwd, ...header } },
        followup(message) {
          w.woken.push({ sessionId, text: message.content[0].text, source: message.source.kind })
        },
      }
      w.agents.set(sessionId, agent)
      return agent
    },
    last() {
      return w.woken[w.woken.length - 1]
    },
  }

  w.ctx = {
    logger: { info: (message) => w.logs.push(message) },
    sessionController: {
      async resolveAgent(sessionId) {
        const agent = w.agents.get(sessionId)
        return agent ? { agent } : { error: { code: 'session/not-found' } }
      },
    },
    sessions: {
      async flush() { return true },
      get(sessionId) { return w.agents.get(sessionId)?.session },
    },
    tools: {
      register(definition) {
        w.tools[definition.name] = definition
        if (definition.name === 'thrum_watch') w.registered = definition
        return () => {}
      },
    },
    effect(callback) {
      w.dispose = callback()
    },
  }
  return w
}

function mount(overrides = {}, injected = {}) {
  const w = world()
  const home = mkdtempSync(join(tmpdir(), 'thrum-mailbox-'))
  w.home = home
  w.hellos = []
  const deps = {
    defineTool: (definition) => definition, // the real one only adds validation
    createUserMessage: ({ content, source }) => ({ id: 'm-nudge', role: 'user', content, source }),
    spawn: (command, args, options) => {
      const child = new FakeChild()
      w.children.push(child)
      w.spawned.push({ command, args, options })
      return child
    },
    sayHello: async (options) => {
      w.hellos.push(options)
      return { ok: true }
    },
    ...injected,
  }
  install(w.ctx, { dshHome: home, ...overrides }, deps)
  return w
}

test('a session says who it is, and a watch for that actor starts', async () => {
  const w = mount()
  w.session('s-1', `${WS}`)

  const result = await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })
  assert.deepEqual(result, { watching: 'alice', root: `${WS}`, mailbox: `${WS}/var/msg/alice` })

  assert.equal(w.spawned.length, 1)
  assert.deepEqual(w.spawned[0].args, ['--root', `${WS}`, 'watch'])
  assert.match(w.children[0].written, /"op":"watch","actor":"alice"/)

  assert.equal(w.hellos.length, 1, 'binding says hello for the actor')
  assert.equal(w.hellos[0].actor, 'alice')
  assert.equal(w.hellos[0].root, `${WS}`)
})

test('a hello that fails is logged and does not stop the watch', async () => {
  const w = mount({}, {
    sayHello: async () => ({ ok: false, reason: 'no current thrum' }),
  })
  w.session('s-1')

  const result = await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })
  assert.deepEqual(result, { watching: 'alice', root: `${WS}`, mailbox: `${WS}/var/msg/alice` })
  assert.equal(w.spawned.length, 1, 'the watch still starts')
  assert.match(w.logs.join('\n'), /could not say hello: no current thrum/)

  w.children[0].say({ id: 'm-1', from: 'bob', subject: 'local mail still wakes' })
  await settle()
  assert.equal(w.woken.length, 1, 'the mailbox is still the truth')
})

test('volatile Config references are read as values at mount', async () => {
  const ref = (value) => ({ get: () => value })
  const home = mkdtempSync(join(tmpdir(), 'thrum-mailbox-'))
  const w = mount({
    thrum: ref('thrum'),
    root: ref(`${WS}`),
    dshHome: ref(home),
    sourceKind: ref('thrum-mailbox'),
    coalesceMs: ref(10),
    helloTimeoutMs: ref(50),
    wakeRetryMs: ref(20),
    maxWakeAttempts: ref(2),
  })
  w.session('s-1')

  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })
  assert.equal(w.hellos.length, 1)
  assert.equal(w.hellos[0].root, `${WS}`)
  assert.equal(w.hellos[0].timeoutMs, 50)
  assert.deepEqual(w.spawned[0].args, ['--root', `${WS}`, 'watch'])
  assert.equal(w.spawned[0].command, 'thrum')
})

test('a failed wake is retried, and the message is announced only after it succeeds', async () => {
  const w = mount({ wakeRetryMs: 20, maxWakeAttempts: 3, coalesceMs: 10 })
  w.session('s-1')
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })

  let flushes = 0
  w.ctx.sessions.flush = async () => {
    flushes += 1
    return flushes > 1
  }

  w.children[0].say({ id: 'm-retry', from: 'bob', subject: 'retry me' })
  await new Promise((resolve) => setTimeout(resolve, 250))

  assert.ok(flushes >= 2, 'the wake should have been retried')
  assert.equal(w.woken.length, 2, 'each attempt queues the nudge; the second succeeds')
  assert.equal(Bindings.load(statePath(w.home)).announced(WS, 'alice', 'm-retry'), true)
})

test('a wake that keeps failing is logged and stays unannounced', async () => {
  const w = mount({ wakeRetryMs: 10, maxWakeAttempts: 2, coalesceMs: 10 })
  w.session('s-1')
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })

  w.ctx.sessions.flush = async () => false
  w.children[0].say({ id: 'm-stuck', from: 'bob', subject: 'stuck' })
  await new Promise((resolve) => setTimeout(resolve, 200))

  assert.match(w.logs.join('\n'), /could not wake .* after 2 attempts/)
  assert.equal(Bindings.load(statePath(w.home)).announced(WS, 'alice', 'm-stuck'), false)
})

test('thrum_unwatch stops the watch and removes only this session\'s binding', async () => {
  const w = mount()
  w.session('s-1')
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })
  let stopped = false
  w.children[0].kill = () => { stopped = true }

  const unwatch = w.tools.thrum_unwatch
  assert.deepEqual(
    await unwatch.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') }),
    { unwatched: true, root: `${WS}`, actor: 'alice' },
  )
  assert.equal(stopped, true)
  assert.equal(Bindings.load(statePath(w.home)).sessionFor(WS, 'alice'), undefined)

  assert.deepEqual(
    await unwatch.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') }),
    { unwatched: false, root: `${WS}`, actor: 'alice' },
  )

  await w.registered.execute({ actor: 'bob' }, { agent: w.agents.get('s-1') })
  w.session('s-2')
  await assert.rejects(
    () => unwatch.execute({ actor: 'bob' }, { agent: w.agents.get('s-2') }),
    /bound to another session/,
  )
})

test('a workspace feed stays while another actor remains', async () => {
  const w = mount()
  w.session('s-1')
  w.session('s-2')
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })
  await w.registered.execute({ actor: 'bob' }, { agent: w.agents.get('s-2') })
  assert.equal(w.spawned.length, 1, 'one process serves both actors in one workspace')

  let stopped = false
  w.children[0].kill = () => { stopped = true }
  await w.tools.thrum_unwatch.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })
  assert.equal(stopped, false, 'unwatching one actor must not stop the workspace feed')

  const status = await w.tools.thrum_watch_status.execute({})
  assert.equal(status.bindings.length, 1)
  assert.equal(status.bindings[0].actor, 'bob')
  assert.equal(status.bindings[0].feed, 'alive')

  await w.tools.thrum_unwatch.execute({ actor: 'bob' }, { agent: w.agents.get('s-2') })
  assert.equal(stopped, true, 'the last unwatch stops the workspace feed')
})

test('thrum_watch_status declares the object rows it returns', async () => {
  const w = mount()
  const status = w.tools.thrum_watch_status
  const items = status.output.schema.properties.bindings.items
  assert.equal(items.type, 'object')
  assert.deepEqual(Object.keys(items.properties).sort(), [
    'actor',
    'announced',
    'feed',
    'root',
    'sessionId',
  ])
  assert.deepEqual(Array.from(status.output.render({}, { bindings: [] })), [
    { type: 'text', text: '0 binding(s)' },
  ])
})

test('a message wakes the session bound to that actor, naming the message', async () => {
  const w = mount()
  w.session('s-1')
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })

  w.children[0].say({ id: 'm-1', from: 'bob', sent: '2026-10-09T02:10:00Z', subject: 'the timeout' })
  await settle()

  assert.equal(w.woken.length, 1)
  assert.equal(w.woken[0].sessionId, 's-1')
  assert.equal(w.woken[0].source, 'thrum-mailbox')
  assert.match(w.woken[0].text, /m-1/)
  assert.match(w.woken[0].text, /the timeout/)
})

test('messages that arrive together are one turn, and one already told is not told again', async () => {
  const w = mount()
  w.session('s-1')
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })

  w.children[0].say({ id: 'm-1', from: 'bob', subject: 'first' })
  w.children[0].say({ id: 'm-2', from: 'bob', subject: 'second' })
  await settle()
  assert.equal(w.woken.length, 1, 'two messages in one window are one thing to be told about')
  assert.match(w.woken[0].text, /2 messages/)
  assert.match(w.woken[0].text, /m-1/)
  assert.match(w.woken[0].text, /m-2/)

  // A watch restarts and prints the whole mailbox again: nothing is re-delivered, so the
  // backlog is the only chance to see it — and what was already announced is skipped.
  w.children[0].say({ id: 'm-1', from: 'bob', subject: 'first' })
  await settle()
  assert.equal(w.woken.length, 1, 'a message already announced should not wake a session again')

  w.children[0].say({ id: 'm-3', from: 'bob', subject: 'third' })
  await settle()
  assert.equal(w.woken.length, 2)
  assert.match(w.woken[1].text, /m-3/)
})

test('the binding is per workspace and per actor, and a session can rebind', async () => {
  const w = mount()
  w.session('s-1', `${WS}`)
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })

  w.session('s-2', `${OTHER}`)
  await w.registered.execute({ actor: 'alice', root: `${OTHER}` }, { agent: w.agents.get('s-2') })
  assert.equal(w.spawned.length, 2, 'the same actor in another workspace is another watch')

  w.children[1].say({ id: 'm-other', from: 'bob', subject: 'elsewhere' })
  w.children[0].say({ id: 'm-here', from: 'bob', subject: 'here' })
  await settle()

  assert.deepEqual(
    w.woken.map((n) => n.sessionId).sort(),
    ['s-1', 's-2'],
    'each workspace wakes the session bound to it',
  )

  // Rebinding is what a session that switched jobs does: the workspace's feed stays, and
  // the messages it reports go to the session that is bound now.
  w.session('s-3', `${WS}`)
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-3') })
  assert.equal(w.spawned.length, 2, 'the workspace feed is reused, not replaced')
  assert.equal(w.hellos.length, 3, 'a rebind says hello again')

  const rebindFeed = w.children[0]
  rebindFeed.say({ id: 'm-later', from: 'bob', subject: 'later' })
  await settle()
  assert.equal(w.last().sessionId, 's-3')
})

test('a call with no session, and a session with no workspace, are refused with a reason', async () => {
  const w = mount()
  await assert.rejects(() => w.registered.execute({ actor: 'alice' }, {}), /session it was called from/)

  w.session('s-1', undefined)
  await assert.rejects(
    () => w.registered.execute({ actor: 'alice' }, { agent: { sessionId: 's-1', session: {} } }),
    /needs a workspace/,
  )
  assert.equal(w.hellos.length, 0)
  assert.equal(w.spawned.length, 0)
})

test('a bad actor name and a non-workspace root are refused before hello or bind', async () => {
  const w = mount()
  w.session('s-1')

  await assert.rejects(
    () => w.registered.execute({ actor: 'Alice' }, { agent: w.agents.get('s-1') }),
    /not an actor name/,
  )
  await assert.rejects(
    () => w.registered.execute({ actor: 'alice', root: '/definitely/not/a/workspace' }, {
      agent: w.agents.get('s-1'),
    }),
    /not a thrum workspace/,
  )
  assert.equal(w.hellos.length, 0)
  assert.equal(w.spawned.length, 0)
  assert.equal(Bindings.load(statePath(w.home)).sessionFor(WS, 'alice'), undefined)
})

test('a subagent session is refused before anything is bound or watched', async () => {
  const w = mount()
  w.session('s-child', `${WS}`, { origin: 'subagent', parentSession: 's-parent' })

  await assert.rejects(
    () => w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-child') }),
    /only binds an ordinary session/,
  )
  assert.equal(w.hellos.length, 0)
  assert.equal(w.spawned.length, 0)
  assert.equal(Bindings.load(statePath(w.home)).sessionFor(WS, 'alice'), undefined)
})

test('unwatch retires a binding whose workspace is gone', async () => {
  const home = mkdtempSync(join(tmpdir(), 'thrum-mailbox-'))
  const recorded = Bindings.load(statePath(home))
  recorded.bind('/definitely/not/a/workspace', 'ghost', 's-3')
  const w = mount({ dshHome: home })
  w.session('s-3')

  assert.deepEqual(
    await w.tools.thrum_unwatch.execute({ actor: 'ghost', root: '/definitely/not/a/workspace' }, {
      agent: w.agents.get('s-3'),
    }),
    { unwatched: true, root: '/definitely/not/a/workspace', actor: 'ghost' },
  )
  assert.equal(
    Bindings.load(statePath(home)).sessionFor('/definitely/not/a/workspace', 'ghost'),
    undefined,
  )
})

test('unloading the plugin stops every watch', async () => {
  const w = mount()
  w.session('s-1')
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })

  let stopped = false
  w.children[0].kill = () => {
    stopped = true
  }
  w.dispose()
  assert.ok(stopped, 'an unloaded plugin should leave no watch running')
})

// A watch lives in the host process and dies with it, while the binding it was made for
// does not. A host that mounts with bindings already recorded has to pick them up again,
// or the session that asked to be woken is never woken and nothing says so.
test('a host that mounts with bindings recorded starts watching them again', async () => {
  const home = mkdtempSync(join(tmpdir(), 'thrum-mailbox-'))
  const recorded = Bindings.load(statePath(home))
  recorded.bind(WS, 'alice', 's-1')
  recorded.bind(OTHER, 'bob', 's-2')

  const w = mount({ dshHome: home })
  w.session('s-1', `${WS}`)
  w.session('s-2', `${OTHER}`)

  assert.equal(w.spawned.length, 2, 'both recorded workspaces are watched again')
  assert.deepEqual(
    w.spawned.map((s) => s.args[1]).sort(),
    [WS, OTHER].sort(),
    'one feed per workspace, not one per actor',
  )
  assert.equal(w.hellos.length, 0, 'mounting carries the watch back, not a new hello')

  // And the wake works from there, without the tool being called again in this process.
  w.children[0].say({ id: 'm-1', from: 'carol', sent: '2026-10-11T00:00:00Z', subject: 'hello' })
  await settle()
  assert.equal(w.woken.length, 1, 'the session the binding names is woken')
})

// A binding whose workspace is not there is skipped, not watched: there is no mailbox to
// watch, and a workspace on an unmounted volume is not a binding to throw away.
test('a binding whose workspace is missing is skipped', () => {
  const home = mkdtempSync(join(tmpdir(), 'thrum-mailbox-'))
  const recorded = Bindings.load(statePath(home))
  recorded.bind('/definitely/not/a/workspace', 'alice', 's-1')

  const w = mount({ dshHome: home })
  assert.equal(w.spawned.length, 0)
})

test('thrum_watch_status reports the binding and its feed', async () => {
  const w = mount()
  w.session('s-1', `${WS}`)
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })

  const status = w.tools.thrum_watch_status
  assert.ok(status, 'the status tool is registered')
  const result = await status.execute({})
  assert.equal(result.bindings.length, 1)
  assert.equal(result.bindings[0].actor, 'alice')
  assert.equal(result.bindings[0].feed, 'alive')
  assert.equal(result.bindings[0].announced, 0)
})

test('mounting forgets a binding whose session the harness no longer holds', () => {
  const home = mkdtempSync(join(tmpdir(), 'thrum-mailbox-'))
  const recorded = Bindings.load(statePath(home))
  recorded.bind(WS, 'alice', 's-gone')
  recorded.bind(WS, 'bob', 's-live')

  mkdirSync(join(home, 'sessions', 'ws'), { recursive: true })
  mkdirSync(join(home, 'sessions', 'ws', 's-live'))
  const w = mount({ dshHome: home })
  // The mount already pruned s-gone; bob stays and is watched.
  assert.equal(Bindings.load(statePath(home)).sessionFor(WS, 'alice'), undefined)
  assert.equal(Bindings.load(statePath(home)).sessionFor(WS, 'bob'), 's-live')
  assert.equal(w.spawned.length, 1)
})
