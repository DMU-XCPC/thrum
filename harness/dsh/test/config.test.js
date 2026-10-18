// The configuration surface: the host schema the Plugins page edits, and the browser
// module that registers the row's config page.

import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import vm from 'node:vm'

const here = fileURLToPath(new URL('.', import.meta.url))
const pkg = JSON.parse(readFileSync(`${here}../package.json`, 'utf8'))

test('the package serves a browser half and names the row config slot it fills', () => {
  assert.equal(pkg.exports['./client'], './client.js')
  assert.equal(pkg.dsh.client.platform, 'web')
  for (const needed of [
    '@deepseek-ai/dsh-client-locale',
    '@deepseek-ai/dsh-client-ui-settings',
    '@deepseek-ai/dsh-client-ui-plugin-manager',
  ]) {
    assert.ok(pkg.dsh.client.inject.includes(needed), `the client half should inject ${needed}`)
  }
})

test('the client bundle registers the form factory without a browser', () => {
  let entry
  const sandbox = {
    window: { __ModuleLoader__: { load: (value) => { entry = value } } },
  }
  vm.createContext(sandbox)
  vm.runInContext(readFileSync(`${here}../client.js`, 'utf8'), sandbox)

  assert.equal(entry.id, 'thrum-mailbox-dsh')
  const stub = {
    SettingsForm: () => {},
    SettingsFormModel: class {},
    SettingsValueField: () => {},
    settingsNumberField: () => ({}),
    settingsTextField: () => ({}),
  }
  const exports = entry.factory((name) =>
    name === 'react/jsx-runtime' ? { jsx: () => {}, jsxs: () => {} } : stub,
  )
  assert.equal(typeof exports.apply, 'function')
  assert.deepEqual(Array.from(exports.inject), ['slots', 'locale', 'configForms'])
})

test('the host Config makes every field live for the Plugins page', async (t) => {
  let Config
  try {
    ;({ Config } = await import('../index.js'))
  } catch (error) {
    t.skip(`the harness packages are not resolvable here: ${error.message}`)
    return
  }

  const expected = {
    thrum: 'thrum',
    root: '',
    dshHome: '',
    sourceKind: 'thrum-mailbox',
    coalesceMs: 250,
    helloTimeoutMs: 10000,
    wakeRetryMs: 2000,
    maxWakeAttempts: 5,
  }
  for (const [field, fallback] of Object.entries(expected)) {
    const schema = Config.dict[field]
    assert.ok(schema, `${field} should be a Config field`)
    assert.equal(schema.meta.default, fallback, `${field} should have a default`)
    assert.equal(schema.meta.volatile, true, `${field} should be live in the settings form`)
  }
})

test('the client half registers the row configuration key on the Plugins page', () => {
  let entry
  const sandbox = {
    window: { __ModuleLoader__: { load: (value) => { entry = value } } },
  }
  vm.createContext(sandbox)
  vm.runInContext(readFileSync(`${here}../client.js`, 'utf8'), sandbox)

  class FormModel {
    constructor(scope) { this.scope = scope }
    bind(project) { return { project } }
    shell() {
      return { available: true, writable: true, dirty: false, invalid: false, saving: false, failed: false }
    }
    field() { return { text: '', overridden: false, invalid: false } }
    actions() { return { edit() {}, resetField() {}, save() {}, discard() {} } }
    dispose() {}
  }
  const stub = {
    SettingsForm: () => {},
    SettingsFormModel: FormModel,
    SettingsValueField: () => {},
    settingsNumberField: () => ({}),
    settingsTextField: () => ({}),
  }

  let registration
  const ctx = {
    locale: { bind: () => (key) => key, register: () => () => {} },
    configForms: {
      get: () => ({}),
      whileServed: (namespaces, register) => register(new Set(namespaces)),
    },
    slots: {
      inject: (_name, callback) => callback(),
      register: (value) => {
        registration = value
        return () => {}
      },
    },
    effect: (callback) => callback(),
  }

  const exported = entry.factory((name) =>
    name === 'react/jsx-runtime' ? { jsx: () => {}, jsxs: () => {} } : stub,
  )
  exported.apply(ctx)

  assert.equal(registration.name, 'plugins.row.config')
  assert.equal(registration.key, 'thrum-mailbox-dsh#thrum-mailbox')
  const injected = registration.inject()
  assert.ok(injected.hooks.form)
  assert.equal(typeof injected.edit, 'function')
  assert.equal(typeof injected.save, 'function')
})
