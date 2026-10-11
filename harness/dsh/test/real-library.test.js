// The plugin's other tests inject a stub `defineTool`, so a definition the real library
// refuses passed every one of them. This test asks the real library instead, in a scratch
// directory where its packages can be resolved, and skips where they are not installed.

import { test } from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, symlinkSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const pluginDir = fileURLToPath(new URL('..', import.meta.url))
const packages =
  process.env.DSH_AGENT_PACKAGES ?? '/usr/lib/node_modules/@deepseek-ai/dsh/node_modules/@deepseek-ai'
const skip = existsSync(packages) ? false : `the harness packages are not installed at ${packages}`

test('the tool definition is one the real harness accepts', { skip }, () => {
  const scratch = mkdtempSync(join(tmpdir(), 'thrum-plugin-'))
  mkdirSync(join(scratch, 'node_modules'), { recursive: true })
  symlinkSync(packages, join(scratch, 'node_modules', '@deepseek-ai'))
  symlinkSync(pluginDir, join(scratch, 'plugin'))

  // The real `defineTool` is what validates the schema, so mounting at all is the assertion.
  const script = `
    import { defineTool } from '@deepseek-ai/dsh-tools'
    import { install } from './plugin/lib/install.js'
    const registered = []
    const ctx = {
      tools: { register: (tool) => { registered.push(tool); return () => {} } },
      sessionController: {},
      sessions: {},
      effect: (fn) => fn(),
      logger: { info: () => {} },
    }
    install(ctx, { dshHome: './home' }, { defineTool, createUserMessage: () => ({}), spawn: () => {} })
    if (registered.length !== 1) throw new Error('the tool was not registered')
    console.log(registered[0].name)
  `
  const out = execFileSync(process.execPath, ['--input-type=module', '-e', script], {
    cwd: scratch,
    encoding: 'utf8',
  })
  assert.equal(out.trim(), 'thrum_watch')
})
