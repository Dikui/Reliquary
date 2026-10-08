import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, reactive, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { descendants, element, loadComponent, registerPrimitives, renderer, settle } from '../componentTestHarness'

for (const locale of ['zh-CN', 'en-US'] as const) {
  test(`${locale}: PDF keeps unavailable anydoc as the default and allows an explicit MinerU choice`, async () => {
    const updates: any[] = []
    const engines = [
      { Name: 'simple', FileTypes: ['md', 'png'], Available: true },
      { Name: 'anydoc', FileTypes: ['pdf'], Available: false, UnavailableReason: 'Rebuild with -tags anydoc' },
      { Name: 'mineru', FileTypes: ['pdf', 'png'], Available: true },
      { Name: 'mineru_cloud', FileTypes: ['pdf', 'png'], Available: false },
    ]
    const resources = reactive({ parserEngines: engines, ensureParserEngines: async () => {} })
    const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
    const component = loadComponent(new URL('./KBParserSettings.vue', import.meta.url), {
      'vue-i18n': { useI18n: () => ({ t: i18n.global.t }) },
      '@/stores/editorResources': { useEditorResourcesStore: () => resources },
      '@/stores/ui': { useUIStore: () => ({ openSettings() {} }) },
      'pinia': { storeToRefs: () => ({ showSettingsModal: ref(false) }) },
    })
    const app = renderer.createApp(component, { 'onUpdate:parserEngineRules': (value: any) => updates.push(value) })
    app.config.globalProperties.$t = i18n.global.t
    registerPrimitives(app)
    app.component('t-select', defineComponent({
      props: ['value', 'status'], emits: ['change'],
      setup(props, { slots, emit }) { return () => h('select', { 'data-value': props.value, 'data-status': props.status, onChange: (event: any) => emit('change', event.value) }, slots.default?.()) },
    }))
    app.component('t-option', defineComponent({
      props: ['value', 'label', 'disabled'],
      setup(props) { return () => h('option', { 'data-value': props.value, disabled: props.disabled }, props.label) },
    }))
    const root = element('root')
    app.mount(root)
    await settle()
    const selects = descendants(root).filter(node => node.tag === 'select')
    assert.deepEqual(selects.map(node => node.props['data-value']), ['anydoc', 'simple', 'simple'])
    const anydoc = descendants(selects[0]).find(node => node.tag === 'option' && node.props['data-value'] === 'anydoc')!
    assert.equal(anydoc.props.disabled, true)
    assert.match(anydoc.text, locale === 'zh-CN' ? /不可用/ : /Unavailable/)
    selects[0].props.onChange({ value: 'mineru' })
    await settle()
    const latest = updates.at(-1)
    assert.equal(latest.find((rule: any) => rule.file_types.includes('pdf')).engine, 'mineru')
    assert.equal(latest.find((rule: any) => rule.file_types.includes('md')).engine, 'simple')
    app.unmount()
  })
}
