// The feed, driven by a child process a test owns.

import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { PassThrough } from 'node:stream'
import test from 'node:test'

import { watchWorkspace } from '../lib/feed.js'

/** A child that looks enough like the real one, and can be told what to say. */
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
    this.killed = false
  }

  say(line) {
    this.stdout.write(`${line}\n`)
  }

  kill() {
    this.killed = true
    this.emit('exit', 0, null)
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

const quietly = () => new Promise((resolve) => setTimeout(resolve, 50))

test('a watch runs one process per workspace and says which actors it watches', () => {
  const { spawn, calls, children } = fakeSpawn()
  const watch = watchWorkspace({ spawn, command: 'thrum', root: '/ws', onMessage: () => {} })
  watch.watch('alice')

  assert.equal(calls.length, 1)
  assert.equal(calls[0].command, 'thrum')
  assert.deepEqual(calls[0].args, ['--root', '/ws', 'watch'])
  assert.match(children[0].written, /"op":"watch","actor":"alice"/)
  assert.equal(watch.size(), 1)
  assert.equal(watch.has('alice'), true)
  assert.equal(watch.running(), true)
  watch.stop()
  assert.equal(watch.running(), false)
})

test('a delivery names its actor, and a line that is not JSON is reported', async () => {
  const { spawn, children } = fakeSpawn()
  const seen = []
  const errors = []
  const watch = watchWorkspace({
    spawn,
    command: 'thrum',
    root: '/ws',
    onMessage: (actor, message) => seen.push([actor, message.id]),
    onError: (e) => errors.push(e),
  })

  children[0].say(JSON.stringify({ kind: 'message.delivered', actor: 'alice', message: { id: 'm-1' } }))
  children[0].say('not json at all')
  children[0].say('')
  children[0].say(JSON.stringify({ kind: 'message.delivered', actor: 'bob', message: { id: 'm-2' } }))
  await quietly()
  watch.stop()

  assert.deepEqual(seen, [
    ['alice', 'm-1'],
    ['bob', 'm-2'],
  ])
  assert.equal(errors.length, 1)
  assert.match(errors[0].message, /not JSON/)
})

test('a watch that dies says why and starts again, and stopping it does not', async () => {
  const { spawn, children } = fakeSpawn()
  const errors = []
  const watch = watchWorkspace({
    spawn,
    command: 'thrum',
    root: '/ws',
    onMessage: () => {},
    onError: (e) => errors.push(e),
    firstRestartMs: 5, // the backoff is a policy, and a test should not wait for it
    maxRestartMs: 10,
  })

  children[0].stderr.write('thrum: the workspace is gone\n')
  children[0].emit('exit', 1, null)
  await quietly()

  assert.equal(errors.length, 1)
  assert.match(errors[0].message, /stopped \(status 1\)/)
  assert.match(errors[0].message, /the workspace is gone/)
  assert.equal(children.length, 2, 'a watch that died should start again')

  watch.stop()
  await quietly()
  assert.equal(children.length, 2, 'a stopped watch should stay stopped')
})
