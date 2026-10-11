// The feed, driven by a child process a test owns.

import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { PassThrough } from 'node:stream'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import { watchMailbox } from '../lib/feed.js'

/** A child that looks enough like the real one, and can be told what to say. */
class FakeChild extends EventEmitter {
  constructor() {
    super()
    this.stdout = new PassThrough()
    this.stderr = new PassThrough()
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

test('a watch runs the command that reports a mailbox as a stream', () => {
  const { spawn, calls } = fakeSpawn()
  const watch = watchMailbox({
    spawn,
    command: 'thrum',
    root: '/ws',
    actor: 'alice',
    onMessage: () => {},
  })
  watch.stop()

  assert.equal(calls.length, 1)
  assert.equal(calls[0].command, 'thrum')
  assert.deepEqual(calls[0].args, ['--root', '/ws', '--actor', 'alice', 'message', 'list', '--watch', '--json'])
  // The actor is in the environment too, because that is how thrum names one.
  assert.equal(calls[0].options.env.THRUM_ACTOR, 'alice')
})

test('each line is one message, and a line that is not one is reported rather than fatal', async () => {
  const { spawn, children } = fakeSpawn()
  const seen = []
  const errors = []
  const watch = watchMailbox({
    spawn,
    command: 'thrum',
    root: '/ws',
    actor: 'alice',
    onMessage: (m) => seen.push(m),
    onError: (e) => errors.push(e),
  })

  children[0].say(JSON.stringify({ id: 'm-1', from: 'bob', subject: 'first' }))
  children[0].say('not json at all')
  children[0].say('')
  children[0].say(JSON.stringify({ id: 'm-2', from: 'bob', subject: 'second' }))
  await quietly()
  watch.stop()

  assert.deepEqual(
    seen.map((m) => m.id),
    ['m-1', 'm-2'],
  )
  assert.equal(errors.length, 1)
  assert.match(errors[0].message, /not a message/)
})

test('a watch that dies says why and starts again, and stopping it does not', async () => {
  const { spawn, children } = fakeSpawn()
  const errors = []
  const watch = watchMailbox({
    spawn,
    command: 'thrum',
    root: '/ws',
    actor: 'alice',
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
