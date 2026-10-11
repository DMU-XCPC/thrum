// A DSH plugin that wakes a session when the actor it works as receives a message.
//
// This file is the only one that knows DSH by name: everything below it takes what it
// needs as an argument, so the decisions can be read and tested without a harness. See
// README.md for what it is, and for what it deliberately is not.

import { spawn } from 'node:child_process'
import { createUserMessage } from '@deepseek-ai/dsh-llm'
import { defineTool } from '@deepseek-ai/dsh-tools'

import { install } from './lib/install.js'

/** Cordis plugin name. */
export const name = 'thrum-mailbox'

/** The services this row needs before it can mount. */
export const inject = ['tools', 'sessionController', 'sessions']

/**
 * Mount the plugin.
 *
 * @param {object} ctx - the cordis context.
 * @param {object} config
 * @param {string} [config.thrum] - the executable to run; `thrum` on PATH by default.
 * @param {string} [config.root] - a default workspace, when a session does not pass one.
 * @param {string} [config.dshHome] - where the bindings are kept; `$DSH_HOME` by default.
 * @param {string} [config.sourceKind] - how a nudge is labelled in the session log.
 */
export function apply(ctx, config = {}) {
  install(ctx, config, { defineTool, createUserMessage, spawn })
}
