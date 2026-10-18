// The wiring against a real thrum: the binding says hello, and a real message wakes
// the session. The harness is still a fake, because this test is about the command line
// and the mailbox, not about DSH.
//
// It is skipped unless THRUM_BIN names a current thrum. `make test-harness` must not
// need one: the rest of the suite is a fake thrum on purpose.

import assert from 'node:assert/strict'
import { execFileSync, spawn } from 'node:child_process'
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import test from 'node:test'

import { createUserMessage } from '@deepseek-ai/dsh-llm'
import { defineTool } from '@deepseek-ai/dsh-tools'

import { install } from '../lib/install.js'

const thrum = process.env.THRUM_BIN ? resolve(process.env.THRUM_BIN) : ''
const skip =
  thrum !== '' && existsSync(thrum) ? false : 'set THRUM_BIN to a current thrum to run this test'

test('a real thrum says hello and wakes the bound session', { skip }, async () => {
  const home = mkdtempSync(join(tmpdir(), 'thrum-real-'))
  const root = join(home, 'ws')
  const stateHome = join(home, 'dsh-home')
  const site = 'dsh-test'
  const actor = 'plugin-real'
  const sender = 'plugin-real-send'

  const envFor = (name) => ({ ...process.env, THRUM_ACTOR: name })
  const thrumAt = (args, options = {}) =>
    execFileSync(thrum, ['--root', root, ...args], {
      encoding: 'utf8',
      timeout: 20000,
      ...options,
    })

  execFileSync(thrum, ['create', root, '--site', site], { encoding: 'utf8', timeout: 20000 })
  thrumAt(['actor', 'declare', actor, '--kind', 'agent'], { env: envFor(actor) })
  thrumAt(['actor', 'declare', sender, '--kind', 'agent'], { env: envFor(sender) })

  const registered = []
  const woken = []
  const logs = []
  const agent = {
    sessionId: 's-real',
    session: { id: 's-real', header: { cwd: root } },
    followup(message) {
      woken.push(message)
    },
  }

  let dispose
  const ctx = {
    tools: {
      register(tool) {
        registered.push(tool)
        return () => {}
      },
    },
    sessionController: {
      async resolveAgent(id) {
        return id === agent.sessionId ? { agent } : { error: { code: 'session/not-found' } }
      },
    },
    sessions: {
      async flush(session) {
        return session === agent.session
      },
    },
    effect(callback) {
      dispose = callback()
    },
    logger: {
      info(message) {
        logs.push(message)
      },
    },
  }

  install(ctx, { thrum, dshHome: stateHome }, { defineTool, createUserMessage, spawn })

  try {
    const watch = registered.find((tool) => tool.name === 'thrum_watch')
    assert.ok(watch, 'the watch tool should be registered')
    const result = await watch.execute({ actor, root }, { agent })
    assert.deepEqual(result, { watching: actor, root, mailbox: `${root}/var/msg/${actor}` })
    assert.doesNotMatch(logs.join('\n'), /could not say hello/)

    // `actor list` prints the site last known, which is only there after hello. The JSON
    // form omits the field until a place exists, so the text is the stable check here.
    const list = thrumAt(['actor', 'list'], { env: envFor(actor) })
    assert.match(
      list,
      new RegExp(`${actor}\\s+agent\\s+${site}\\s+`),
      `the binding should have said hello for ${actor}:\n${list}`,
    )

    const id = thrumAt(
      ['message', 'send', actor, '--subject', 'real wake', '--body', '-'],
      { input: 'a message from the real thrum\n', env: envFor(sender) },
    ).trim()

    const deadline = Date.now() + 8000
    while (woken.length === 0 && Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, 50))
    }

    assert.equal(woken.length, 1, 'the real message should wake the bound session')
    assert.match(woken[0].content[0].text, new RegExp(id))
    assert.match(woken[0].content[0].text, /real wake/)
    assert.equal(woken[0].source.kind, 'thrum-mailbox')

    const state = JSON.parse(readFileSync(join(stateHome, 'thrum-mailbox.json'), 'utf8'))
    const binding = Object.values(state.bindings).find((entry) => entry.actor === actor)
    assert.ok(binding, 'the binding should be recorded')
    assert.ok(binding.announced.includes(id), 'a flushed wake should be announced exactly once')
  } finally {
    dispose?.()
    rmSync(home, { recursive: true, force: true })
  }
})
