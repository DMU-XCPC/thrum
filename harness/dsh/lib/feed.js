// The feed: one line per message, from `thrum message list --watch --json`.
//
// The stream is what makes a wake possible without polling the tool, and it is also what
// makes a late start safe: the watch prints the mailbox it finds before it reports
// arrivals, because nothing is re-delivered.

import { createInterface } from 'node:readline'

// Restarting a watch that keeps dying must not become a busy loop, and it must not give
// up either: a host can outlive a `thrum` upgrade.
const FIRST_RESTART_MS = 2000
const MAX_RESTART_MS = 60000

/**
 * Watch one actor's mailbox.
 *
 * @param {object} options
 * @param {Function} options.spawn - `child_process.spawn`, injectable so a test can drive it.
 * @param {string} options.command - the thrum executable.
 * @param {string} options.root - the workspace.
 * @param {string} options.actor - whose mailbox.
 * @param {(message: object) => void} options.onMessage - one message object per line.
 * @param {(error: Error) => void} [options.onError] - a missing tool, a bad line, a death.
 * @param {number} [options.firstRestartMs] - how long to wait before the first retry.
 * @param {number} [options.maxRestartMs] - the ceiling that wait doubles up to.
 * @returns {{ stop: () => void }}
 */
export function watchMailbox({
  spawn,
  command,
  root,
  actor,
  onMessage,
  onError = () => {},
  firstRestartMs = FIRST_RESTART_MS,
  maxRestartMs = MAX_RESTART_MS,
}) {
  let child = null
  let stopped = false
  let restartMs = firstRestartMs
  let timer = null

  const start = () => {
    if (stopped) return
    child = spawn(command, ['--root', root, '--actor', actor, 'inbox', '--watch', '--json'], {
      // The actor is passed twice on purpose: on the command line for a reader of the
      // process table, and in the environment because that is how thrum names an actor.
      env: { ...process.env, THRUM_ACTOR: actor },
      stdio: ['ignore', 'pipe', 'pipe'],
    })

    const lines = createInterface({ input: child.stdout })
    lines.on('line', (line) => {
      if (line.trim() === '') return
      let message
      try {
        message = JSON.parse(line)
      } catch {
        onError(new Error(`thrum message list --watch printed a line that is not a message: ${line}`))
        return
      }
      restartMs = firstRestartMs // it is working, so a later death starts over
      onMessage(message)
    })

    let complaint = ''
    child.stderr?.on('data', (chunk) => {
      complaint = `${complaint}${chunk}`.slice(-2000)
    })

    child.on('error', (error) => {
      onError(new Error(`could not run ${command}: ${error.message}`))
      // A binary that is not there emits `error` and no `exit`, so without this the watch
      // would be logged once and never tried again — the one failure that has to recover
      // on its own, because nothing else will call the tool again.
      if (stopped) return
      timer = setTimeout(start, restartMs)
      restartMs = Math.min(restartMs * 2, maxRestartMs)
    })

    child.on('exit', (code, signal) => {
      if (stopped) return
      const why = signal ? `signal ${signal}` : `status ${code}`
      onError(new Error(`${command} inbox --watch stopped (${why})${complaint ? `: ${complaint.trim()}` : ''}`))
      timer = setTimeout(start, restartMs)
      restartMs = Math.min(restartMs * 2, maxRestartMs)
    })
  }

  start()

  return {
    stop() {
      stopped = true
      if (timer) clearTimeout(timer)
      child?.kill()
    },
  }
}
