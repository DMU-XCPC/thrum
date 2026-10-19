// The feed: one `thrum watch` per workspace, shared by every actor bound there.
//
// The plugin says which actors it is responsible for, thrum confirms the scope and sends
// each delivery with its actor. One process serves them all; a restart replays the scope,
// because the scope lives in the process, not in the plugin.

import { createInterface } from 'node:readline'

// Restarting a watch that keeps dying must not become a busy loop, and it must not give
// up either: a host can outlive a `thrum` upgrade.
const FIRST_RESTART_MS = 2000
const MAX_RESTART_MS = 60000

/**
 * Watch a workspace's mail for every actor the plugin is responsible for.
 *
 * @param {object} options
 * @param {Function} options.spawn - `child_process.spawn`, injectable so a test can drive it.
 * @param {string} options.command - the thrum executable.
 * @param {string} options.root - the workspace.
 * @param {(actor: string, message: object) => void} options.onMessage - one delivery.
 * @param {(error: Error) => void} [options.onError] - a missing tool, a bad line, a death.
 * @param {number} [options.firstRestartMs] - how long to wait before the first retry.
 * @param {number} [options.maxRestartMs] - the ceiling that wait doubles up to.
 * @returns {{ watch: Function, unwatch: Function, scope: Function, has: Function, stop: Function }}
 */
export function watchWorkspace({
  spawn,
  command,
  root,
  onMessage,
  onError = () => {},
  firstRestartMs = FIRST_RESTART_MS,
  maxRestartMs = MAX_RESTART_MS,
}) {
  const actors = new Set()
  const pending = []
  let child = null
  let stdin = null
  let stopped = false
  let restartMs = firstRestartMs
  let timer = null

  const send = (op) => {
    const line = `${JSON.stringify(op)}\n`
    if (stdin?.writable) stdin.write(line)
    else pending.push(line)
  }

  const start = () => {
    if (stopped) return
    child = spawn(command, ['--root', root, 'watch'], {
      env: { ...process.env },
      stdio: ['pipe', 'pipe', 'pipe'],
    })
    stdin = child.stdin
    for (const line of pending.splice(0)) stdin.write(line)
    // The process is new, so it knows no scope; say the current one again.
    if (actors.size > 0) send({ op: 'scope', actors: [...actors] })

    const lines = createInterface({ input: child.stdout })
    lines.on('line', (line) => {
      if (line.trim() === '') return
      let event
      try {
        event = JSON.parse(line)
      } catch {
        onError(new Error(`thrum watch printed a line that is not JSON: ${line}`))
        return
      }
      restartMs = firstRestartMs // it is working, so a later death starts over
      if (event.kind === 'message.delivered' && event.actor && event.message?.id) {
        onMessage(event.actor, event.message)
      } else if (event.kind === 'error') {
        onError(new Error(event.reason ?? 'thrum watch reported an error'))
      }
    })

    let complaint = ''
    child.stderr?.on('data', (chunk) => {
      complaint = `${complaint}${chunk}`.slice(-2000)
    })

    child.on('error', (error) => {
      onError(new Error(`could not run ${command}: ${error.message}`))
      if (stopped) return
      timer = setTimeout(start, restartMs)
      restartMs = Math.min(restartMs * 2, maxRestartMs)
    })

    child.on('exit', (code, signal) => {
      if (stopped) return
      const why = signal ? `signal ${signal}` : `status ${code}`
      onError(new Error(`${command} watch stopped (${why})${complaint ? `: ${complaint.trim()}` : ''}`))
      timer = setTimeout(start, restartMs)
      restartMs = Math.min(restartMs * 2, maxRestartMs)
    })
  }

  start()

  return {
    watch(actor) {
      actors.add(actor)
      send({ op: 'watch', actor })
    },
    unwatch(actor) {
      actors.delete(actor)
      send({ op: 'unwatch', actor })
    },
    scope(list) {
      actors.clear()
      for (const actor of list) actors.add(actor)
      send({ op: 'scope', actors: [...actors] })
    },
    has(actor) {
      return actors.has(actor)
    },
    stop() {
      stopped = true
      if (timer) clearTimeout(timer)
      stdin?.end()
      child?.kill()
    },
  }
}
