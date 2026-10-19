// The bindings, and what has already been announced.
//
// This is a harness's bookkeeping, so it lives under the harness home and never in the
// workspace: a workspace is a shared thing whose var/ belongs to its tooling, and which
// session is listening for which actor is nobody else's business.
//
// The key is (root, actor), because one actor may work in two workspaces on one host and
// one workspace may have several actors.

import { mkdirSync, readFileSync, renameSync, rmdirSync, statSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'

const VERSION = 1

// A message id is unique forever, so forgetting one can only ever cost a repeated nudge.
// The cap is what keeps the file from growing with every message a host ever receives.
const ANNOUNCED_CAP = 1000

/** Where the state file is, given a configured home. */
export function statePath(dshHome) {
  const home = dshHome ?? process.env.DSH_HOME ?? join(homedir(), '.dsh')
  return join(home, 'thrum-mailbox.json')
}

const key = (root, actor) => `${root}\u0000${actor}`

// A save is read-merge-write under a lock, not "write back what I loaded at boot". Two
// hosts can share one harness home, and each of them binds actors at its own moments, so a
// plain write would let the second host's save erase the first host's bindings — and a
// binding that has been erased is a session that is never woken, silently.
//
// The lock is a directory, because creating one is atomic on every filesystem this runs on.
// Not getting it is not a reason to skip the save: the merge is what stops the erasure, and
// the lock only closes the window between reading and writing. A host that died holding the
// lock must not stop the next one, so a lock older than STALE_LOCK_MS is taken away.
const LOCK_WAIT_MS = 1000
const STALE_LOCK_MS = 5000

/** A synchronous pause, which is what a synchronous save path has to work with. */
function sleep(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms)
}

export class Bindings {
  #path
  #state
  // Keys this process has just removed. A merge must not adopt them back from the file it
  // is about to replace, or an unbind would undo itself; a rebind in the same breath
  // forgets the removal again.
  #removed = new Set()

  constructor(path, state = { version: VERSION, bindings: {} }) {
    this.#path = path
    this.#state = state
  }

  /** Load the state, treating anything unreadable as an empty one. */
  static load(path) {
    try {
      const parsed = JSON.parse(readFileSync(path, 'utf8'))
      if (parsed?.version === VERSION && parsed.bindings) return new Bindings(path, parsed)
    } catch {
      // A missing or unreadable file is a host that has never bound an actor, and a
      // malformed one is a file this plugin wrote and can afford to lose: the cost is a
      // session that has to say who it is again.
    }
    return new Bindings(path)
  }

  /** Record that this actor, in this workspace, is worked by this session. */
  bind(root, actor, sessionId) {
    const entry = this.#state.bindings[key(root, actor)] ?? { announced: [] }
    entry.root = root
    entry.actor = actor
    entry.sessionId = sessionId
    this.#state.bindings[key(root, actor)] = entry
    this.#removed.delete(key(root, actor))
    this.#save()
    return entry
  }

  /** The session bound to this actor in this workspace, or undefined. */
  sessionFor(root, actor) {
    return this.#state.bindings[key(root, actor)]?.sessionId
  }

  /** Every binding, for a caller that has to restart a feed. */
  entries() {
    return Object.values(this.#state.bindings).map((entry) => ({
      root: entry.root,
      actor: entry.actor,
      sessionId: entry.sessionId,
    }))
  }

  /** How many message ids are still remembered as announced. */
  announcedCount(root, actor) {
    return (this.#state.bindings[key(root, actor)]?.announced ?? []).length
  }

  /** Whether this message has already been announced to this binding. */
  announced(root, actor, id) {
    return (this.#state.bindings[key(root, actor)]?.announced ?? []).includes(id)
  }

  /** Remember that this message has been announced. */
  announce(root, actor, id) {
    const entry = this.#state.bindings[key(root, actor)]
    if (!entry || entry.announced.includes(id)) return
    entry.announced.push(id)
    if (entry.announced.length > ANNOUNCED_CAP) {
      entry.announced.splice(0, entry.announced.length - ANNOUNCED_CAP)
    }
    this.#save()
  }

  /** Forget a binding, and everything announced under it. */
  unbind(root, actor) {
    delete this.#state.bindings[key(root, actor)]
    this.#removed.add(key(root, actor))
    this.#save()
  }

  #save() {
    mkdirSync(dirname(this.#path), { recursive: true })
    const lock = `${this.#path}.lock`
    const held = this.#lock(lock)
    try {
      this.#merge(this.#read())
      this.#removed.clear()
      // Written the way the workspace writes its own files: a temporary neighbour and a
      // rename, so a crash leaves the old file or the new one and never half of either.
      const temporary = `${this.#path}.tmp${process.pid}`
      writeFileSync(temporary, `${JSON.stringify(this.#state, null, 2)}\n`, { mode: 0o600 })
      renameSync(temporary, this.#path)
    } finally {
      if (held) rmdirSync(lock)
    }
  }

  /** Take the save lock if it can be had, giving up rather than hanging a wake. */
  #lock(path) {
    const until = Date.now() + LOCK_WAIT_MS
    for (;;) {
      try {
        mkdirSync(path)
        return true
      } catch (error) {
        if (error?.code !== 'EEXIST') return false
        try {
          if (Date.now() - statSync(path).mtimeMs > STALE_LOCK_MS) {
            rmdirSync(path)
            continue
          }
        } catch {
          continue // it went away between the two calls, so take it
        }
        if (Date.now() >= until) return false
        sleep(10)
      }
    }
  }

  /** Fold in what another process recorded, so that a save adds rather than replaces. */
  #merge(onDisk) {
    for (const [k, entry] of Object.entries(onDisk?.bindings ?? {})) {
      if (this.#removed.has(k)) continue // this process is the one that dropped it
      // A binding this process has never seen belongs to another host, and dropping it is
      // how a session stops being woken without anybody being told. A binding this process
      // does know is its own: what it holds is what its own last change decided, and that
      // includes the announced list, whose cap only works if the file cannot re-add what
      // the cap has just evicted.
      if (!this.#state.bindings[k]) this.#state.bindings[k] = entry
    }
  }

  #read() {
    try {
      const parsed = JSON.parse(readFileSync(this.#path, 'utf8'))
      if (parsed?.version === VERSION && parsed.bindings) return parsed
    } catch {
      // Nothing to merge: a missing file is a host that has never bound an actor.
    }
    return { version: VERSION, bindings: {} }
  }
}
