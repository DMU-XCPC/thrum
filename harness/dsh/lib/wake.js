// Waking a session, and what it is told.
//
// Two calls, and the second is not optional: `followup` puts the message in the session's
// inbox and wakes the driver, and `flush` is what makes it durable. A nudge that is only
// in memory is a nudge that a crash takes back.

/** What one message looks like on the stream, and what the nudge says about it. */
const line = (m) => `- ${m.id} from ${m.from} ${m.sent ? `at ${m.sent} ` : ''}— ${m.subject}`

/**
 * The text a woken session reads.
 *
 * It names the messages and their ids rather than their bodies: the body is what `thrum
 * read` is for, and a session log is a worse place for it than the mailbox is. A batch also
 * suggests a read-only triage subagent, because reading many bodies into the main context is
 * exactly what delegation can keep out of it; one message is cheaper to read directly.
 *
 * @param {object[]} messages - the batch being announced.
 * @param {object} [options]
 * @param {string} [options.actor] - the actor name a triage subagent must read with.
 * @returns {string} the nudge text.
 */
export function nudgeText(messages, { actor } = {}) {
  const count = messages.length === 1 ? '1 message' : `${messages.length} messages`
  const triage = messages.length > 1
    ? [
        '',
        'This is a batch. If this harness can delegate, have a read-only subagent triage it and',
        `bring back only the decisions. It reads with \`--actor ${actor ?? '<name>'}\` and must not`,
        'reply, send, purge, or claim work.',
      ]
    : []
  return [
    `You have mail in this workspace: ${count}.`,
    '',
    ...messages.map(line),
    '',
    'Read them with `thrum message list`, and one with `thrum message read <id>`. A message is process',
    'information and is never in a handoff, so this is the only place it arrives.',
    ...triage,
  ].join('\n')
}

/**
 * Build the function that wakes a session.
 *
 * Everything DSH-specific is passed in, so that this file — which holds the decision of
 * what to do and in what order — can be read and tested without a running harness.
 *
 * @param {object} deps
 * @param {{ resolveAgent: (id: string) => Promise<object> }} deps.sessionController
 * @param {{ flush: (session: object) => Promise<boolean> }} deps.sessions
 * @param {Function} deps.createUserMessage - the harness's message factory.
 * @param {string} [deps.sourceKind] - how the message is labelled in the session log.
 * @param {(message: string) => void} [deps.log]
 */
export function makeWaker({ sessionController, sessions, createUserMessage, sourceKind = 'thrum-mailbox', log = () => {} }) {
  return async function wake(sessionId, messages, actor) {
    const resolved = await sessionController.resolveAgent(sessionId)
    if (resolved.error) {
      // Not fatal, and not silent: the message is still in the mailbox, and the actor
      // will find it the next time it looks.
      log(`could not wake ${sessionId}: ${resolved.error.code ?? resolved.error}`)
      return false
    }
    const message = createUserMessage({
      content: [{ type: 'text', text: nudgeText(messages, { actor }) }],
      source: { kind: sourceKind },
    })
    resolved.agent.followup(message)
    const durable = await sessions.flush(resolved.agent.session)
    if (!durable) {
      log(`woke ${sessionId}, and the session did not acknowledge the flush`)
      return false
    }
    return true
  }
}
