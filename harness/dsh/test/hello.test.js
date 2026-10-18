// Saying hello, driven by a child process a test owns.
//
// The one thing this cannot get wrong silently: an older thrum prints `actor` help and
// exits 0. Only the acknowledgement says the hello happened.

import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { PassThrough } from 'node:stream'
import test from 'node:test'

import { sayHello } from '../lib/hello.js'

class FakeChild extends EventEmitter {
  constructor() {
    super()
    this.stdout = new PassThrough()
    this.stderr = new PassThrough()
    this.killed = false
  }

  kill() {
    this.killed = true
  }
}

function fakeSpawn() {
  const children = []
  const calls = []
  const spawn = (command, args, options) => {
    const child = new FakeChild()
    children.push(child)
    calls.push({ command, args, options })
    return child
  }
  return { spawn, children, calls }
}

/** Write an answer and then exit, in the order a child process does. */
async function answer(child, text, code = 0, signal = null) {
  child.stdout.write(text)
  await new Promise((resolve) => setImmediate(resolve))
  child.emit('close', code, signal)
}

test('hello says where this actor is, with the actor in the environment too', async () => {
  const { spawn, children, calls } = fakeSpawn()
  const said = sayHello({ spawn, command: 'thrum', root: '/ws', actor: 'alice' })
  await answer(children[0], 'alice is here\n')

  assert.deepEqual(await said, { ok: true })
  assert.deepEqual(calls[0].args, ['--root', '/ws', '--actor', 'alice', 'actor', 'hello'])
  assert.equal(calls[0].options.env.THRUM_ACTOR, 'alice')
})

test('an older thrum that exits 0 with help did not say hello', async () => {
  const { spawn, children } = fakeSpawn()
  const said = sayHello({ spawn, command: 'thrum', root: '/ws', actor: 'alice' })
  await answer(
    children[0],
    'The roster is the tooling\'s, not a file in the workspace.\nUsage:\n  thrum actor [command]\n',
  )

  const result = await said
  assert.equal(result.ok, false)
  assert.match(result.reason, /no "alice is here" acknowledgement/)
})

test('a failing hello carries thrum\'s complaint', async () => {
  const { spawn, children } = fakeSpawn()
  const said = sayHello({ spawn, command: 'thrum', root: '/ws', actor: 'alice' })
  children[0].stderr.write('thrum: actor "alice" is not declared')
  await answer(children[0], '', 1)

  const result = await said
  assert.equal(result.ok, false)
  assert.match(result.reason, /status 1: thrum: actor "alice" is not declared/)
})

test('a missing executable is a reason, not a throw', async () => {
  const { spawn, children } = fakeSpawn()
  const said = sayHello({ spawn, command: 'thrum', root: '/ws', actor: 'alice' })
  children[0].emit('error', new Error('spawn thrum ENOENT'))

  const result = await said
  assert.equal(result.ok, false)
  assert.match(result.reason, /could not run thrum: spawn thrum ENOENT/)
})

test('a signal is named in the reason', async () => {
  const { spawn, children } = fakeSpawn()
  const said = sayHello({ spawn, command: 'thrum', root: '/ws', actor: 'alice' })
  await answer(children[0], '', null, 'SIGTERM')

  const result = await said
  assert.equal(result.ok, false)
  assert.match(result.reason, /signal SIGTERM/)
})

test('a hello that never answers times out and is killed', async () => {
  const { spawn, children } = fakeSpawn()
  const result = await sayHello({
    spawn,
    command: 'thrum',
    root: '/ws',
    actor: 'alice',
    timeoutMs: 20,
  })

  assert.deepEqual(result, { ok: false, reason: 'timed out after 20ms' })
  assert.equal(children[0].killed, true)
})
