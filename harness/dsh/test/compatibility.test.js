// The package's declared DSH requirement, judged by the harness's own evaluator.
//
// A version mismatch is not a warning at boot: the harness skips the bundle, so the
// tools never appear and the plugin looks absent rather than broken. This test asks the
// same evaluator the harness uses, so an upgrade that outruns the package fails here
// instead of silently unloading the plugin on the next start.

import { test } from 'node:test'
import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

const pluginDir = fileURLToPath(new URL('..', import.meta.url))
const packages =
  process.env.DSH_AGENT_PACKAGES ?? '/usr/lib/node_modules/@deepseek-ai/dsh/node_modules/@deepseek-ai'
const appBoot = join(packages, 'dsh-app-boot', 'lib', 'index.js')
const skip = existsSync(appBoot) ? false : `the harness packages are not installed at ${packages}`

test('the package admits the installed dsh runtime', { skip }, async () => {
  const { evaluatePluginCompatibility, getDshRuntimeVersion, pluginCompatibilityWarning } =
    await import(pathToFileURL(appBoot).href)
  const manifest = JSON.parse(readFileSync(join(pluginDir, 'package.json'), 'utf8'))

  const issue = evaluatePluginCompatibility(manifest, {}, getDshRuntimeVersion())
  assert.equal(issue, undefined, issue && pluginCompatibilityWarning(issue))
})
