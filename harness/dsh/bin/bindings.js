#!/usr/bin/env node
// The plugin's bindings: who is watching what, behind which feed, and for which session.
//
// This is a host-side view for a person; it never writes to a workspace and never messages
// an actor. `--prune` only removes a binding whose session the harness store no longer
// holds, and leaves anything it cannot decide alone.

import { execFileSync } from 'node:child_process'
import { existsSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

import { Bindings, statePath } from '../lib/state.js'

const args = process.argv.slice(2)
const flag = (name) => args.includes(name)
const value = (name) => {
  const at = args.indexOf(name)
  return at >= 0 ? args[at + 1] : undefined
}

const dshHome = value('--dsh-home') ?? process.env.DSH_HOME ?? undefined
const home = dshHome ?? join(process.env.HOME ?? '', '.dsh')
const prune = flag('--prune')
const json = flag('--json')

const bindings = Bindings.load(statePath(dshHome))
const feedRoots = feedProcesses()
const sessions = sessionIndex(home)

const rows = bindings.entries().map(({ root, actor, sessionId }) => ({
  actor,
  session: sessionId,
  root,
  feed: feedRoots.has(root) ? 'alive' : 'dead',
  announced: bindings.announcedCount?.(root, actor) ?? 0,
  sessionExists: sessions === undefined ? undefined : sessions.has(sessionId),
}))
rows.sort((a, b) => `${a.root}\u0000${a.actor}`.localeCompare(`${b.root}\u0000${b.actor}`))

if (json) {
  console.log(JSON.stringify(rows, null, 2))
} else if (rows.length === 0) {
  console.log('no bindings')
} else {
  console.table(
    rows.map((r) => ({
      actor: r.actor,
      session: r.session,
      root: r.root,
      feed: r.feed,
      announced: r.announced,
      session_exists: r.sessionExists === undefined ? 'unknown' : r.sessionExists,
    })),
  )
}

if (prune) {
  let removed = 0
  let skipped = 0
  for (const r of rows) {
    if (r.sessionExists === true) continue
    if (r.sessionExists === undefined) {
      skipped++
      continue
    }
    bindings.unbind(r.root, r.actor)
    removed++
  }
  console.log(`pruned ${removed} binding(s) whose session is gone${skipped ? `, ${skipped} undecidable` : ''}`)
}

/** The roots that have a live feed: `thrum watch` or the older per-actor form. */
function feedProcesses() {
  let out = ''
  try {
    out = execFileSync('pgrep', ['-af', 'thrum (watch|message list --watch)'], { encoding: 'utf8' })
  } catch {
    return new Set()
  }
  const roots = new Set()
  for (const line of out.split('\n')) {
    const m = line.match(/--root\s+(\S+)/)
    if (m) roots.add(m[1])
  }
  return roots
}

/** All session ids the harness store knows, or undefined when there is no store to read. */
function sessionIndex(home) {
  const dir = join(home, 'sessions')
  if (!existsSync(dir)) return undefined
  const ids = new Set()
  for (const entry of readdirSync(dir, { recursive: true })) {
    const id = String(entry).split('/').pop()
    if (id) ids.add(id)
  }
  return ids
}
