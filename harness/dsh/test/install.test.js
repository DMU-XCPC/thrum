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
  }

  say(message) {
    this.stdout.write(`${JSON.stringify(message)}\n`)
  }

  kill() {
    this.emit('exit', 0, null)
  }
}

/** A harness, a thrum, and the sessions that can be woken. */
function world() {
  const w = {
    woken: [],
    children: [],
    spawned: [],
    agents: new Map(),
    registered: null,
    dispose: null,
    /** Make a session that can be woken, and remember what it was told. */
    session(sessionId, cwd = WS) {
      const agent = {
        sessionId,
        session: { id: sessionId, header: { cwd } },
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
    logger: { info: () => {} },
    sessionController: {
      async resolveAgent(sessionId) {
        const agent = w.agents.get(sessionId)
        return agent ? { agent } : { error: { code: 'session/not-found' } }
      },
    },
    sessions: { async flush() { return true } },
    tools: {
      register(definition) {
        w.registered = definition
        return () => {}
      },
    },
    effect(callback) {
      w.dispose = callback()
    },
  }
  return w
}

function mount(overrides = {}) {
  const w = world()
  const deps = {
    defineTool: (definition) => definition, // the real one only adds validation
    createUserMessage: ({ content, source }) => ({ id: 'm-nudge', role: 'user', content, source }),
    spawn: (command, args, options) => {
      const child = new FakeChild()
      w.children.push(child)
      w.spawned.push({ command, args, options })
      return child
    },
  }
  install(w.ctx, { dshHome: mkdtempSync(join(tmpdir(), 'thrum-mailbox-')), ...overrides }, deps)
  return w
}

test('a session says who it is, and a watch for that actor starts', async () => {
  const w = mount()
  w.session('s-1', `${WS}`)

  const result = await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-1') })
  assert.deepEqual(result, { watching: 'alice', root: `${WS}`, mailbox: `${WS}/var/msg/alice` })

  assert.equal(w.spawned.length, 1)
  assert.deepEqual(w.spawned[0].args, ['--root', `${WS}`, '--actor', 'alice', 'message', 'list', '--watch', '--json'])
  assert.equal(w.spawned[0].options.env.THRUM_ACTOR, 'alice')
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

  // Rebinding is what a session that switched jobs does: the watch for that workspace is
  // replaced, and the messages it reports go to the session that is bound now.
  w.session('s-3', `${WS}`)
  let oldStopped = false
  w.children[0].kill = () => {
    oldStopped = true
  }
  await w.registered.execute({ actor: 'alice' }, { agent: w.agents.get('s-3') })
  assert.ok(oldStopped, 'rebinding should stop the watch the old session was using')

  const rebindFeed = w.children.at(-1)
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

  assert.equal(w.spawned.length, 2, 'both recorded mailboxes are watched again')
  assert.deepEqual(
    w.spawned.map((s) => s.args[3]).sort(),
    ['alice', 'bob'],
    'and each is watched for the actor it was bound for',
  )

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
