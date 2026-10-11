// The bindings, and what has already been announced.
//
// This is a harness's bookkeeping, so it lives under the harness home and never in the
// workspace: a workspace is a shared thing whose var/ belongs to its tooling, and which
// session is listening for which actor is nobody else's business.
//
// The key is (root, actor), because one actor may work in two workspaces on one host and
// one workspace may have several actors.

import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
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

export class Bindings {
  #path
  #state

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
    this.#save()
  }

  #save() {
    mkdirSync(dirname(this.#path), { recursive: true })
    // Written the way the workspace writes its own files: a temporary neighbour and a
    // rename, so a crash leaves the old file or the new one and never half of either.
    const temporary = `${this.#path}.tmp${process.pid}`
    writeFileSync(temporary, `${JSON.stringify(this.#state, null, 2)}\n`, { mode: 0o600 })
    renameSync(temporary, this.#path)
  }
}
