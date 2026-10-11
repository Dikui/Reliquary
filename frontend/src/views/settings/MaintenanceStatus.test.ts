import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createSSRApp, defineComponent, h, onServerPrefetch } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import enUS from '../../i18n/locales/en-US'
import zhCN from '../../i18n/locales/zh-CN'
import type { MaintenanceStatus } from '../../api/maintenance'

const require = createRequire(import.meta.url)
const messages = { 'zh-CN': zhCN, 'en-US': enUS }
const source = readFileSync(new URL('./MaintenanceStatus.vue', import.meta.url), 'utf8')
const { descriptor, errors } = parse(source, { filename: 'MaintenanceStatus.vue' })
assert.deepEqual(errors, [])
const script = compileScript(descriptor, { id: 'maintenance-status-test' })
const template = compileTemplate({ id: 'maintenance-status-test', filename: 'MaintenanceStatus.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])

const status: MaintenanceStatus = {
  paused: true, pause_mode: 'snapshot', pause_reason: 'backup',
  backup_at: '2026-11-01T03:00:00Z', backup_status: 'success', backup_path: '/Volumes/backup/latest', backup_size_bytes: 2097152, backup_error: '',
  drill_at: '2026-11-01T04:00:00Z', drill_status: 'failed', drill_error: 'verify: mine/example.md is missing',
  backup_directory: '/Volumes/backup', backup_schedule: '0 3 * * 0', drill_schedule: '0 4 1 * *', schedule_timezone: 'Asia/Shanghai', automatic_enabled: true,
}

async function scenario(locale: keyof typeof messages, getMaintenanceStatus: () => Promise<unknown>, action: (state: any) => Promise<void>) {
  const i18n = createI18n({ legacy: false, locale, messages })
  const modules: Record<string, unknown> = { 'vue-i18n': { useI18n: () => i18n.global }, '@/api/maintenance': { getMaintenanceStatus } }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => { const state = setup(props, context); onServerPrefetch(() => action(state)); return state }
  const app = createSSRApp(component)
  app.use(i18n)
  app.component('t-button', defineComponent({ props: ['disabled'], setup: (props, { attrs, slots }) => () => h('button', { ...attrs, disabled: props.disabled }, slots.default?.()) }))
  app.component('t-alert', defineComponent({ props: ['message'], setup: (props, { attrs }) => () => h('div', attrs, props.message) }))
  const failures: unknown[] = []; const warnings: string[] = []
  app.config.errorHandler = error => failures.push(error); app.config.warnHandler = warning => warnings.push(warning)
  const html = await renderToString(app)
  assert.deepEqual(failures, []); assert.deepEqual(warnings, [])
  return html
}

for (const locale of ['zh-CN', 'en-US'] as const) {
  const copy = messages[locale].settings.maintenance
  test(`${locale}: backup and drill results show directory, time, size, schedules and persistent failure`, async () => {
    const html = await scenario(locale, async () => ({ success: true, data: status }), state => state.load())
    for (const value of [copy.title, copy.directory, copy.scheduleHint, copy.backup, copy.drill, copy.success, copy.failed, copy.paused, status.backup_directory, status.drill_error, '2 MiB', status.backup_schedule, status.drill_schedule, status.schedule_timezone]) assert.ok(html.includes(value), value)
    assert.match(html, /role="alert"/)
    assert.ok(html.includes(new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(status.backup_at!))))
  })
  test(`${locale}: refresh failure retains last known backup and has retry`, async () => {
    let calls = 0
    const html = await scenario(locale, async () => { if (++calls === 1) return { success: true, data: status }; throw new Error('Maintenance unavailable') }, async state => { await state.load(); await state.load() })
    assert.ok(html.includes('Maintenance unavailable')); assert.ok(html.includes(messages[locale].common.retry)); assert.ok(html.includes(status.backup_directory))
  })
  test(`${locale}: no previous run and skipped backup have explicit labels`, async () => {
    const html = await scenario(locale, async () => ({ success: true, data: { ...status, backup_at: null, backup_status: 'skipped', backup_error: 'Wait timed out', drill_at: null, drill_status: 'never', drill_error: '', paused: false } }), state => state.load())
    assert.ok(html.includes(copy.skipped)); assert.ok(html.includes(copy.never)); assert.ok(html.includes('Wait timed out'))
  })
}

test('maintenance load blocks duplicate requests and ignores stale teardown results', async () => {
  let finish: (result: unknown) => void = () => {}; let calls = 0
  const html = await scenario('en-US', () => { calls++; return new Promise(resolve => { finish = resolve }) }, async state => {
    const pending = state.load(); await state.load(); assert.equal(calls, 1); state.generation++; finish({ success: true, data: status }); await pending; assert.equal(state.status.value, null)
  })
  assert.ok(html.includes(messages['en-US'].common.loading))
})
