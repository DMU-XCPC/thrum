// The browser half of the plugin: the row's configuration page on the Plugins page.
//
// The host half owns the Config schema; this form is the surface that edits its volatile
// fields. The row config slot is keyed by `<package>#<row id>`, which is the Configure
// control's identity on the bundle page.
window.__ModuleLoader__.load({
  id: 'thrum-mailbox-dsh',
  factory: (require) => {
    var module = { exports: {} }
    var exports = module.exports
    Object.defineProperty(exports, Symbol.toStringTag, { value: 'Module' })

    const { jsx, jsxs } = require('react/jsx-runtime')
    const {
      SettingsForm,
      SettingsFormModel,
      SettingsValueField,
      settingsNumberField,
      settingsTextField,
    } = require('@deepseek-ai/dsh-client-ui-primitives')

    /** Dictionary namespace owned by this page. */
    const NS = 'thrumMailbox'
    /** Profile entry id of the host row, which is also its settings namespace. */
    const ENTRY = 'thrum-mailbox'
    /** The row config slot key: package name plus the row id in the bundle patch. */
    const ROW_KEY = 'thrum-mailbox-dsh#thrum-mailbox'

    const en = {
      title: 'Thrum mailbox',
      description: 'Wake this session when the actor it works as receives a message in a thrum workspace.',
      thrum: 'thrum executable',
      thrumHint: 'The command to run. Empty uses `thrum` from PATH.',
      root: 'Default workspace',
      rootHint: 'Used when a session does not pass a root. Empty uses the session working directory.',
      dshHome: 'Harness home',
      dshHomeHint: 'Where bindings are kept. Empty uses $DSH_HOME, then ~/.dsh.',
      sourceKind: 'Nudge source label',
      sourceKindHint: 'The label written on the notification in the session log.',
      coalesceMs: 'Coalescing window (ms)',
      coalesceHint: 'Messages arriving within this window become one notification.',
      helloTimeoutMs: 'Hello timeout (ms)',
      helloHint: 'How long `thrum actor hello` may take while binding.',
      wakeRetryMs: 'First retry delay (ms)',
      wakeRetryHint: 'The delay before retrying a wake that the session did not acknowledge.',
      maxWakeAttempts: 'Wake attempts',
      maxWakeAttemptsHint: 'Retries before a batch waits for new mail or a watch restart.',
      overridden: 'Overridden',
      reset: 'Reset to default',
      readOnly: 'This deployment stores settings read-only.',
      unavailable: 'This plugin is not loaded, so it cannot be configured right now.',
      save: 'Save',
      saving: 'Saving…',
      saveFailed: 'The deployment did not accept these values; they were left for you to correct.',
      invalidNumber: 'Enter a number, or leave blank to use the default.',
      invalidText: 'Leave blank to use the default.',
    }

    const zh = {
      title: 'Thrum 邮箱',
      description: '当当前 actor 在 thrum 工作区收到消息时，唤醒这个会话。',
      thrum: 'thrum 可执行文件',
      thrumHint: '要运行的命令。留空使用 PATH 中的 `thrum`。',
      root: '默认工作区',
      rootHint: '会话没有传入 root 时使用。留空使用会话工作目录。',
      dshHome: 'Harness 主目录',
      dshHomeHint: '绑定状态所在目录。留空使用 $DSH_HOME，再退到 ~/.dsh。',
      sourceKind: '提醒来源标签',
      sourceKindHint: '写入会话日志的提醒标签。',
      coalesceMs: '合并窗口（毫秒）',
      coalesceHint: '在这个窗口内到达的消息会合并成一次提醒。',
      helloTimeoutMs: 'Hello 超时（毫秒）',
      helloHint: '绑定时 `thrum actor hello` 最多等待多久。',
      wakeRetryMs: '首次重试延迟（毫秒）',
      wakeRetryHint: '会话未确认唤醒时，第一次重试前的等待时间。',
      maxWakeAttempts: '唤醒尝试次数',
      maxWakeAttemptsHint: '一批消息在等待新邮件或 watch 重启前最多尝试几次。',
      overridden: '已覆盖',
      reset: '恢复默认',
      readOnly: '本部署的设置为只读。',
      unavailable: '该插件当前未加载，暂时无法配置。',
      save: '保存',
      saving: '保存中…',
      saveFailed: '本部署没有接受这些值，已保留供你修改。',
      invalidNumber: '请填数字；留空表示使用默认值。',
      invalidText: '留空表示使用默认值。',
    }

    const SPECS = [
      settingsTextField('thrum'),
      settingsTextField('root'),
      settingsTextField('dshHome'),
      settingsTextField('sourceKind'),
      settingsNumberField('coalesceMs'),
      settingsNumberField('helloTimeoutMs'),
      settingsNumberField('wakeRetryMs'),
      settingsNumberField('maxWakeAttempts'),
    ]

    const FIELDS = [
      ['thrum', 'thrum', 'thrumHint', false],
      ['root', 'root', 'rootHint', false],
      ['dshHome', 'dshHome', 'dshHomeHint', false],
      ['sourceKind', 'sourceKind', 'sourceKindHint', false],
      ['coalesceMs', 'coalesceMs', 'coalesceHint', true],
      ['helloTimeoutMs', 'helloTimeoutMs', 'helloHint', true],
      ['wakeRetryMs', 'wakeRetryMs', 'wakeRetryHint', true],
      ['maxWakeAttempts', 'maxWakeAttempts', 'maxWakeAttemptsHint', true],
    ]

    function formLabels(t) {
      return {
        unavailable: t('unavailable'),
        readOnly: t('readOnly'),
        saveFailed: t('saveFailed'),
        save: t('save'),
        saving: t('saving'),
      }
    }

    function projection(form) {
      const fields = {}
      for (const [name] of FIELDS) fields[name] = form.field(name)
      return { ...form.shell(), ...fields }
    }

    function Field(props) {
      return jsx(SettingsValueField, {
        id: `thrum-mailbox-${props.name}`,
        label: props.label,
        hint: props.hint,
        disabled: props.disabled,
        numeric: props.numeric,
        ...props.state,
        overriddenLabel: props.t('overridden'),
        resetLabel: props.t('reset'),
        invalidLabel: props.numeric ? props.t('invalidNumber') : props.t('invalidText'),
        onEdit: props.onEdit,
        onReset: props.onReset,
      })
    }

    function Form(props) {
      const { t } = props
      const state = props.useForm((snapshot) => snapshot)
      if (props.view === 'summary') return t('description')
      const disabled = !state.writable
      return jsxs(SettingsForm, {
        state,
        labels: formLabels(t),
        onSave: props.save,
        onDiscard: props.discard,
        children: FIELDS.map(([name, label, hint, numeric]) =>
          jsx(Field, {
            key: name,
            name,
            label: t(label),
            hint: t(hint),
            state: state[name],
            numeric,
            disabled,
            t,
            onEdit: (text) => props.edit(name, text),
            onReset: () => props.resetField(name),
          }),
        ),
      })
    }

    /** Required services (cordis fiber inject). */
    const inject = ['slots', 'locale', 'configForms']

    /**
     * Mount the row's configuration page while the Host serves the entry.
     * @param ctx - the browser plugin context.
     */
    function apply(ctx) {
      const t = ctx.locale.bind(NS)
      ctx.effect(() => ctx.locale.register(NS, { en, zh }), 'thrum-mailbox: dictionaries')

      const form = new SettingsFormModel(ctx.configForms.get(ENTRY), SPECS)
      const store = form.bind(() => projection(form))
      ctx.effect(() => () => form.dispose(), 'thrum-mailbox: form subscription')

      ctx.effect(
        () =>
          ctx.configForms.whileServed([ENTRY], () =>
            ctx.slots.inject('plugins.row.config', () =>
              ctx.slots.register(
                {
                  name: 'plugins.row.config',
                  key: ROW_KEY,
                  locale: NS,
                  inject: () => ({ hooks: { form: store }, ...form.actions() }),
                },
                Form,
              ),
            ),
          ),
        'thrum-mailbox: row configuration page',
      )
    }

    exports.apply = apply
    exports.inject = inject
    return module.exports
  },
})
