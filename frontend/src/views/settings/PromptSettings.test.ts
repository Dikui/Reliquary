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
import { normalizeSettingsSection } from '../../config/settingsRoute'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./PromptSettings.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source, { filename: 'PromptSettings.vue' })
const script = compileScript(descriptor, { id: 'prompt-settings-test' })
const template = compileTemplate({ id: 'prompt-settings-test', filename: 'PromptSettings.vue', source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
assert.deepEqual(template.errors, [])

async function scenario(locale: 'zh-CN' | 'en-US', failSave: boolean, action: (state: any, requests: any[], dialog: () => any, controls: { delayNextList: () => () => void }) => Promise<void>, emptyLibraries = false) {
  const requests: any[] = []
  let confirmation: any
  const stored = new Map<string, { key: string; id: number; value: string }>()
  const switches = new Map<string, unknown>()
  let listGate: Promise<void> | undefined
  const controls = { delayNextList() {
    let release!: () => void
    listGate = new Promise<void>(resolve => { release = resolve })
    return release
  } }
  const libraryTemplates = ['article_processing', 'quality_scoring', 'summary_merge', 'fact_grouping', 'story_consolidation', 'output_correction', 'evaluation_questions']
  const globalTemplates = ['evaluation_questions_multi_library', 'model_connection_test', 'model_maintenance_probe']
  function row(id: string, scope?: string) {
    return stored.get(`${scope || 'global'}:${id}`) || { key: `prompt.${id}`, id: 0, value: id === 'article_processing' ? 'Original prompt' : `${scope || 'global'} ${id}` }
  }
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  const originalWindow = globalThis.window
  globalThis.window = { addEventListener() {}, removeEventListener() {} } as any
  const stub = defineComponent({ setup(_, { slots }) { return () => h('div', slots.default?.()) } })
  const modules: Record<string, unknown> = {
    'vue-i18n': { useI18n: () => i18n.global },
    'tdesign-vue-next': {
      MessagePlugin: { success(message: string) { requests.push({ toast: message }) }, error(message: string) { requests.push({ toastError: message }) } },
      DialogPlugin: { confirm(options: any) { confirmation = options; return { destroy() {} } } },
    },
    '@/components/SafeTextarea.vue': { __esModule: true, default: defineComponent({ props: ['modelValue'], setup(props) { return () => h('textarea', props.modelValue) } }) },
    '@/api/library': {
      listLibraries: async () => ({ success: true, data: emptyLibraries ? [] : [
        { id: 'library-1', name: 'First library' }, { id: 'library-2', name: 'Second library' },
      ] }),
    },
    '@/api/system': {
      listPromptSettings: async (scope?: string) => {
        requests.push({ list: scope || 'global' })
        const result = (scope ? libraryTemplates : globalTemplates).map(id => ({ ...row(id, scope) }))
        const gate = listGate
        listGate = undefined
        if (gate) await gate
        return result
      },
      updatePromptSetting: async (id: string, value: string, scope?: string) => {
        requests.push({ key: id, value, scope })
        if (failSave) throw new Error('Save unavailable')
        const updated = { key: `prompt.${id}`, id: 31, value }
        stored.set(`${scope || 'global'}:${id}`, updated)
        return { ...updated }
      },
      resetPromptSetting: async (id: string, scope?: string) => {
        requests.push({ reset: id, scope })
        stored.delete(`${scope || 'global'}:${id}`)
      },
      getSystemSetting: async (key: string) => ({ key, value: switches.get(key) ?? false, value_type: 'bool' }),
      updateSystemSetting: async (key: string, value: unknown) => {
        requests.push({ setting: key, value })
        if (failSave) throw new Error('Switch unavailable')
        switches.set(key, value)
        return { key, value, value_type: 'bool' }
      },
    },
  }
  function evaluate(code: string) {
    const module = { exports: {} as any }
    const compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => modules[name] ?? require(name), module, module.exports)
    return module.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (props: object, context: object) => {
    const state = setup(props, context)
    onServerPrefetch(async () => { await state.initialize(); await action(state, requests, () => confirmation, controls) })
    return state
  }
  const app = createSSRApp(component)
  app.use(i18n)
  for (const name of ['t-button', 't-loading', 't-empty', 't-tag', 't-select', 't-switch']) app.component(name, stub)
  try { return { html: await renderToString(app), requests } }
  finally { globalThis.window = originalWindow }
}

test('prompt settings save and restore defaults within the selected library', async () => {
  assert.equal(normalizeSettingsSection('prompts'), 'prompts')
  const { html, requests } = await scenario('zh-CN', false, async (state, calls, dialog) => {
    assert.equal(state.libraryId.value, 'library-1')
    assert.deepEqual(state.templates.value.map((item: { key: string }) => item.key), ['prompt.article_processing', 'prompt.quality_scoring', 'prompt.summary_merge', 'prompt.fact_grouping', 'prompt.story_consolidation', 'prompt.output_correction', 'prompt.evaluation_questions'])
    state.draft.value = 'Updated prompt\n保留换行'
    await state.save()
    assert.equal(state.dirty.value, false)
    assert.equal(state.selected.value.row.id, 31)
    assert.equal(calls.filter(call => call.key).length, 1)
    await state.load()
    assert.equal(state.draft.value, 'Updated prompt\n保留换行')
    state.restoreDefault()
    await dialog().onConfirm()
    assert.equal(state.draft.value, 'Original prompt')
    assert.equal(state.selected.value.row.id, 0)
    assert.deepEqual(calls.find(call => call.reset), { reset: 'article_processing', scope: 'library-1' })
  })
  assert.match(html, /资料理解/)
  assert.match(html, /系统默认/)
  assert.equal(requests.find(call => call.key).key, 'article_processing')
  assert.equal(requests.find(call => call.key).scope, 'library-1')
  assert.match(requests.find(call => call.toast).toast, /First library/)
  assert.equal(requests.find(call => call.key).value, 'Updated prompt\n保留换行')
})

test('failed prompt saves retain the draft and cancelling navigation keeps edits', async () => {
  const { html } = await scenario('en-US', true, async (state, calls, dialog) => {
    state.draft.value = 'Keep this draft'
    await state.save()
    assert.equal(state.dirty.value, true)
    assert.equal(state.draft.value, 'Keep this draft')
    assert.equal(state.selected.value.row.value, 'Original prompt')
    assert.equal(state.error.value, 'Save unavailable')
    const leave = state.confirmLeave()
    dialog().onCancel()
    assert.equal(await leave, false)
    assert.equal(state.draft.value, 'Keep this draft')
    assert.equal(calls.some(call => call.toast), false)
  })
  assert.match(html, /role="alert"[^>]*>Save unavailable/)
  assert.match(html, /Unsaved changes/)
  assert.match(html, /Keep this draft/)
})


test('switching prompt scope guards drafts and ignores an older library response', async () => {
  await scenario('en-US', false, async (state, calls, dialog, controls) => {
    state.draft.value = 'Keep this library draft'
    const denied = state.selectScope('library-2')
    await Promise.resolve()
    dialog().onCancel()
    await denied
    assert.equal(state.libraryId.value, 'library-1')
    assert.equal(state.draft.value, 'Keep this library draft')

    const release = controls.delayNextList()
    const switching = state.selectScope('library-2')
    await Promise.resolve()
    dialog().onConfirm()
    await Promise.resolve()
    assert.equal(state.libraryId.value, 'library-2')
    await state.selectScope('')
    release()
    await switching
    assert.equal(state.libraryId.value, '')
    assert.deepEqual(state.templates.value.map((item: { key: string }) => item.key), [
      'prompt.evaluation_questions_multi_library', 'prompt.model_connection_test', 'prompt.model_maintenance_probe',
    ])
    assert.equal(state.draft.value, 'global evaluation_questions_multi_library')
    assert.equal(calls.some(call => call.list === 'library-2'), true)
  })
})

test('global prompts remain editable when there are no libraries', async () => {
  const { requests } = await scenario('zh-CN', false, async state => {
    assert.equal(state.libraryId.value, '')
    assert.equal(state.templates.value.length, 3)
    state.draft.value = 'Global evaluation prompt'
    await state.save()
    assert.equal(state.dirty.value, false)
  }, true)
  assert.deepEqual(requests.find(call => call.key), {
    key: 'evaluation_questions_multi_library', value: 'Global evaluation prompt', scope: undefined,
  })
})

test('quality scoring switch is per library and rolls back on failure', async () => {
  const { html, requests } = await scenario('zh-CN', false, async state => {
    assert.equal(state.qualityEnabled.value, false)
    await state.selectTemplate('prompt.quality_scoring')
    await state.setQualityEnabled(true)
    assert.equal(state.qualityEnabled.value, true)
    await state.selectScope('library-2')
    assert.equal(state.qualityEnabled.value, false, 'another library keeps its own switch')
    await state.selectScope('library-1')
    assert.equal(state.qualityEnabled.value, true)
    await state.selectTemplate('prompt.quality_scoring')
  })
  assert.match(html, /本库启用质量评分/)
  assert.deepEqual(requests.find(call => call.setting), { setting: 'prompt.library.library-1.quality_scoring_enabled', value: true })

  await scenario('en-US', true, async state => {
    await state.selectTemplate('prompt.quality_scoring')
    await state.setQualityEnabled(true)
    assert.equal(state.qualityEnabled.value, false)
  })
})
