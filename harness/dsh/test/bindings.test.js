import { test } from 'node:test'
import assert from 'node:assert/strict'
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'

import { Bindings, statePath } from '../lib/state.js'

function run(home, ...args) {
  return spawnSync(process.execPath, ['bin/bindings.js', '--dsh-home', home, ...args], {
    encoding: 'utf8',
  })
}

test('bindings lists a binding with its session and feed state', () => {
  const home = mkdtempSync(join(tmpdir(), 'thrum-bindings-'))
  try {
    Bindings.load(statePath(home)).bind('/ws/one', 'alice', 's-1')
    const r = run(home, '--json')
    assert.equal(r.status, 0, r.stderr)
    const rows = JSON.parse(r.stdout)
    assert.equal(rows.length, 1)
    assert.equal(rows[0].actor, 'alice')
    assert.equal(rows[0].session, 's-1')
    assert.equal(rows[0].feed, 'dead')
    assert.equal(rows[0].sessionExists, undefined)
  } finally {
    rmSync(home, { recursive: true, force: true })
  }
})

test('prune removes only a binding whose session the store does not hold', () => {
  const home = mkdtempSync(join(tmpdir(), 'thrum-bindings-'))
  try {
    const b = Bindings.load(statePath(home))
    b.bind('/ws/one', 'alice', 's-gone')
    b.bind('/ws/two', 'bob', 's-live')
    mkdirSync(join(home, 'sessions', 'ws'), { recursive: true })
    writeFileSync(join(home, 'sessions', 'ws', 's-live'), '{}')
    const r = run(home, '--prune')
    assert.equal(r.status, 0, r.stderr)
    assert.match(r.stdout, /pruned 1/)
    assert.equal(Bindings.load(statePath(home)).sessionFor('/ws/one', 'alice'), undefined)
    assert.equal(Bindings.load(statePath(home)).sessionFor('/ws/two', 'bob'), 's-live')
  } finally {
    rmSync(home, { recursive: true, force: true })
  }
})
