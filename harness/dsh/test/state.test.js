// The bindings and the announced set: what is remembered between runs.

import assert from 'node:assert/strict'
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import test from 'node:test'

import { Bindings, statePath } from '../lib/state.js'

const somewhere = () => join(mkdtempSync(join(tmpdir(), 'thrum-mailbox-')), 'state.json')

test('the state file is the harness home, not the workspace', () => {
  assert.equal(statePath('/home/u/.dsh'), '/home/u/.dsh/thrum-mailbox.json')
})

test('a binding is per workspace and per actor, and survives a reload', () => {
  const path = somewhere()
  const first = Bindings.load(path)
  assert.equal(first.sessionFor('/ws', 'alice'), undefined, 'nothing is bound before anything binds it')

  first.bind('/ws', 'alice', 's-1')
  first.bind('/ws', 'bob', 's-2')
  first.bind('/other', 'alice', 's-3') // one actor may work in two workspaces

  const reloaded = Bindings.load(path)
  assert.equal(reloaded.sessionFor('/ws', 'alice'), 's-1')
  assert.equal(reloaded.sessionFor('/ws', 'bob'), 's-2')
  assert.equal(reloaded.sessionFor('/other', 'alice'), 's-3')
  assert.equal(reloaded.sessionFor('/ws', 'nobody'), undefined)

  reloaded.unbind('/ws', 'alice')
  assert.equal(Bindings.load(path).sessionFor('/ws', 'alice'), undefined)
  assert.equal(Bindings.load(path).sessionFor('/ws', 'bob'), 's-2', 'unbinding one leaves the rest')
})

test('rebinding a session keeps what was already announced', () => {
  const path = somewhere()
  const bindings = Bindings.load(path)
  bindings.bind('/ws', 'alice', 's-1')
  bindings.announce('/ws', 'alice', 'm-1')
  bindings.bind('/ws', 'alice', 's-2')
  assert.equal(bindings.sessionFor('/ws', 'alice'), 's-2')
  assert.ok(bindings.announced('/ws', 'alice', 'm-1'), 'a new session is not told again what the old one was')
})

test('the announced set is capped, so the file does not grow forever', () => {
  const path = somewhere()
  const bindings = Bindings.load(path)
  bindings.bind('/ws', 'alice', 's-1')
  for (let i = 0; i < 1200; i++) bindings.announce('/ws', 'alice', `m-${i}`)
  // A message id is unique forever, so forgetting an old one can only cost one repeated
  // nudge — and it must not cost an unbounded file.
  assert.ok(bindings.announced('/ws', 'alice', 'm-1199'))
  assert.ok(!bindings.announced('/ws', 'alice', 'm-0'))
})

test('an unreadable state file is an empty one rather than a broken plugin', () => {
  const path = somewhere()
  writeFileSync(path, 'this is not json')
  assert.equal(Bindings.load(path).sessionFor('/ws', 'alice'), undefined)

  writeFileSync(path, JSON.stringify({ version: 99, bindings: { x: {} } }))
  assert.equal(Bindings.load(path).sessionFor('/ws', 'alice'), undefined)
})

test('the state is written whole or not at all', () => {
  const path = somewhere()
  const bindings = Bindings.load(path)
  bindings.bind('/ws', 'alice', 's-1')
  const parsed = JSON.parse(readFileSync(path, 'utf8'))
  assert.equal(parsed.version, 1)
  assert.equal(parsed.bindings['/ws\u0000alice'].sessionId, 's-1')
})

// Two hosts can share one harness home. A save is read-merge-write, so the second host's
// binding does not erase the first's — an erased binding is a session that is never woken
// and nothing says so.
test('two hosts sharing a home keep each other\'s bindings', () => {
  const path = statePath(mkdtempSync(join(tmpdir(), 'thrum-mailbox-')))
  const first = Bindings.load(path)
  const second = Bindings.load(path)
  first.bind('/ws/one', 'alice', 's-1')
  second.bind('/ws/two', 'bob', 's-2')

  const after = Bindings.load(path)
  assert.deepEqual(
    after.entries().map((e) => e.actor).sort(),
    ['alice', 'bob'],
    'a save adds what another host recorded rather than replacing it',
  )
  assert.equal(after.sessionFor('/ws/one', 'alice'), 's-1')
  assert.equal(after.sessionFor('/ws/two', 'bob'), 's-2')
})

// And what one host has announced is not forgotten by the other host's save.
test('an announcement survives the other host\'s save', () => {
  const path = statePath(mkdtempSync(join(tmpdir(), 'thrum-mailbox-')))
  const first = Bindings.load(path)
  first.bind('/ws/one', 'alice', 's-1')
  first.announce('/ws/one', 'alice', 'm-1')

  const second = Bindings.load(path)
  second.bind('/ws/two', 'bob', 's-2')

  const after = Bindings.load(path)
  assert.equal(after.announced('/ws/one', 'alice', 'm-1'), true)
})
