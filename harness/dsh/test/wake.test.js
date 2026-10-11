// Waking a session: what is called, in what order, and what happens when it cannot be.

import assert from 'node:assert/strict'
import test from 'node:test'

import { makeWaker, nudgeText } from '../lib/wake.js'

const message = (id, subject = `subject of ${id}`) => ({
  id,
  from: 'bob',
  sent: '2026-10-09T02:10:00Z',
  subject,
})

test('the nudge names the messages and their ids, and not their bodies', () => {
  const one = nudgeText([message('m-1')])
  assert.match(one, /1 message\./)
  assert.match(one, /m-1/)
  assert.match(one, /thrum read <id>/)

  const two = nudgeText([message('m-1'), message('m-2')])
  assert.match(two, /2 messages\./)
  assert.match(two, /m-2/)
  // The body is what a session log is a bad place for, and the mailbox is where it is.
  assert.ok(!two.includes('body'))
})

function harness({ resolved, flushed = true } = {}) {
  const calls = { followup: [], flush: [], resolve: [] }
  const sessionController = {
    async resolveAgent(id) {
      calls.resolve.push(id)
      return resolved ?? { agent: { followup: (m) => calls.followup.push(m) } }
    },
  }
  const sessions = {
    async flush(session) {
      calls.flush.push(session)
      return flushed
    },
  }
  const createUserMessage = ({ content, source }) => ({ id: 'm-nudge', role: 'user', content, source })
  return { calls, sessionController, sessions, createUserMessage }
}

test('a waking session is resolved, given a follow-up, and flushed', async () => {
  const h = harness()
  const wake = makeWaker({ ...h, log: () => {} })

  assert.equal(await wake('s-1', [message('m-1')]), true)
  assert.deepEqual(h.calls.resolve, ['s-1'])
  assert.equal(h.calls.followup.length, 1)

  const sent = h.calls.followup[0]
  assert.equal(sent.role, 'user')
  assert.equal(sent.source.kind, 'thrum-mailbox')
  assert.match(sent.content[0].text, /m-1/)
  assert.equal(h.calls.flush.length, 1, 'a nudge that is only in memory is one a crash takes back')
})

test('a session that cannot be resolved is reported, and is not fatal', async () => {
  const h = harness({ resolved: { error: { code: 'session/not-found' } } })
  const said = []
  const wake = makeWaker({ ...h, log: (m) => said.push(m) })

  assert.equal(await wake('s-gone', [message('m-1')]), false)
  assert.equal(h.calls.followup.length, 0, 'nothing is queued into a session that is not there')
  assert.match(said.join('\n'), /session\/not-found/)
})

test('a flush that is not acknowledged is reported, so the message is not counted as told', async () => {
  const h = harness({ flushed: false })
  const said = []
  const wake = makeWaker({ ...h, log: (m) => said.push(m) })

  assert.equal(await wake('s-1', [message('m-1')]), false)
  assert.match(said.join('\n'), /did not acknowledge/)
})
