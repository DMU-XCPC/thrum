// A DSH plugin that wakes a session when the actor it works as receives a message.
//
// This file is the only one that knows DSH by name: everything below it takes what it
// needs as an argument, so the decisions can be read and tested without a harness. See
// README.md for what it is, and for what it deliberately is not.

import { spawn } from 'node:child_process'
import z from '@deepseek-ai/schemastery'
import { createUserMessage } from '@deepseek-ai/dsh-llm'
import { defineTool } from '@deepseek-ai/dsh-tools'

import { install } from './lib/install.js'

/** Cordis plugin name. */
export const name = 'thrum-mailbox'

/** The services this row needs before it can mount. */
export const inject = ['tools', 'sessionController', 'sessions']

/**
 * Configuration, with every field live so the Plugins page can edit it.
 *
 * The row config page is the harness's plugin-configuration surface; a field that is not
 * volatile is ordinary Loader configuration and has no form there.
 */
export const Config = z.object({
  thrum: z.string().default('thrum').volatile(),
  root: z.string().default('').volatile(),
  dshHome: z.string().default('').volatile(),
  sourceKind: z.string().default('thrum-mailbox').volatile(),
  coalesceMs: z.number().step(1).min(0).default(250).volatile(),
  helloTimeoutMs: z.number().step(1).min(1).default(10000).volatile(),
  wakeRetryMs: z.number().step(1).min(100).default(2000).volatile(),
  maxWakeAttempts: z.number().step(1).min(1).default(5).volatile(),
})

/**
 * Mount the plugin.
 *
 * @param {object} ctx - the cordis context.
 * @param {object} config
 * @param {string} [config.thrum] - the executable to run; `thrum` on PATH by default.
 * @param {string} [config.root] - a default workspace, when a session does not pass one.
 * @param {string} [config.dshHome] - where the bindings are kept; `$DSH_HOME` by default.
 * @param {string} [config.sourceKind] - how a nudge is labelled in the session log.
 * @param {number} [config.coalesceMs] - how long messages are gathered into one nudge.
 * @param {number} [config.helloTimeoutMs] - how long `thrum actor hello` may take.
 * @param {number} [config.wakeRetryMs] - the first retry delay after a failed wake.
 * @param {number} [config.maxWakeAttempts] - retries before a batch waits for new mail.
 */
export function apply(ctx, config = {}) {
  install(ctx, config, { defineTool, createUserMessage, spawn })
}
