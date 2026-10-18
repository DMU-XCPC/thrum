// Saying hello: where this actor's mail is, before the watch starts.
//
// `thrum actor hello` is an address, not a claim, and it is what moves an actor's mail to
// this site. It is best effort: a missing or old thrum must not stop the watchdog. The
// mailbox is still the truth, and a watch over it can still wake the session.

// The current CLI answers `<actor> is here`. An older one prints `actor` help and exits 0,
// so the exit status alone is not an answer: without the acknowledgement the hello did not
// happen.
const ACKNOWLEDGEMENT = 'is here'
// `thrum actor hello` may have to start the site daemon first, and the CLI itself waits up
// to five seconds for that. The bound is above that rather than racing it.
const DEFAULT_TIMEOUT_MS = 10000

/**
 * Say hello for one actor, bounded.
 *
 * @param {object} options
 * @param {Function} options.spawn - `child_process.spawn`, injectable so a test can drive it.
 * @param {string} options.command - the thrum executable.
 * @param {string} options.root - the workspace.
 * @param {string} options.actor - whose mail this is.
 * @param {number} [options.timeoutMs] - how long to wait for an answer.
 * @returns {Promise<{ok: boolean, reason?: string}>} whether thrum acknowledged the hello.
 */
export function sayHello({
  spawn,
  command,
  root,
  actor,
  timeoutMs = DEFAULT_TIMEOUT_MS,
}) {
  return new Promise((resolve) => {
    let child
    try {
      child = spawn(command, ['--root', root, '--actor', actor, 'actor', 'hello'], {
        env: { ...process.env, THRUM_ACTOR: actor },
        stdio: ['ignore', 'pipe', 'pipe'],
      })
    } catch (error) {
      resolve({ ok: false, reason: `could not run ${command}: ${error.message}` })
      return
    }

    let output = ''
    let complaint = ''
    let settled = false
    let timer

    const finish = (ok, reason) => {
      if (settled) return
      settled = true
      if (timer) clearTimeout(timer)
      resolve(ok ? { ok: true } : { ok: false, reason })
    }

    child.stdout?.on('data', (chunk) => {
      output = `${output}${chunk}`.slice(-2000)
    })
    child.stderr?.on('data', (chunk) => {
      complaint = `${complaint}${chunk}`.slice(-2000)
    })

    child.on('error', (error) => {
      finish(false, `could not run ${command}: ${error.message}`)
    })

    child.on('close', (code, signal) => {
      const acknowledged = output.includes(`${actor} ${ACKNOWLEDGEMENT}`)
      const why = signal ? `signal ${signal}` : `status ${code}`
      if (acknowledged && code === 0) {
        finish(true)
        return
      }
      const detail = complaint.trim()
      finish(
        false,
        acknowledged
          ? `${why} after acknowledging the hello`
          : `${why}${detail ? `: ${detail}` : `: no "${actor} ${ACKNOWLEDGEMENT}" acknowledgement`}`,
      )
    })

    timer = setTimeout(() => {
      finish(false, `timed out after ${timeoutMs}ms`)
      try {
        child.kill()
      } catch {
        // The process is already gone; the timeout is still the answer.
      }
    }, timeoutMs)
  })
}
