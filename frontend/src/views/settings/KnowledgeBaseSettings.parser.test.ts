import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { defineComponent, h, reactive, ref } from 'vue'
import { changedSettings } from '../../utils/settingsPatch'
import { copyModelTiers } from '../../utils/modelTiers'
import { descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../knowledge-base/componentTestHarness'

test('opening the actual parser tab keeps settings clean, editing makes them dirty and cancel restores the snapshot', async () => {
  const previousWindow = globalThis.window
  globalThis.window = { addEventListener() {}, removeEventListener() {} } as any
  const modules: Record<string, any> = {
    'tdesign-vue-next': { MessagePlugin: {}, DialogPlugin: {} },
    'vue-i18n': { useI18n: () => ({ t: (key: string) => key }) },
    '@/utils/settingsPatch': { changedSettings }, '@/utils/modelTiers': { copyModelTiers },
    '@/stores/ui': { useUIStore: () => ({}) },
    'pinia': { storeToRefs: () => ({ showSettingsModal: ref(false) }) },
    '@/stores/resources': { useResourcesStore: () => ({ allModels: [], ensureModels: async () => {} }) },
    '@/stores/editorResources': { useEditorResourcesStore: () => reactive({
      parserEngines: [{ Name: 'anydoc', FileTypes: ['pdf'], Available: true }, { Name: 'mineru', FileTypes: ['pdf'], Available: true }, { Name: 'simple', FileTypes: ['md'], Available: true }],
      ensureParserEngines: async () => {},
    }) },
    '@/api/knowledge-base': {
      getKnowledgeBase: async () => ({ data: { id: 'kb', name: 'Knowledge base', chunking_config: { parser_engine_rules: null } } }),
      listEntryFiles: async () => ({ total: 0 }),
    },
  }
  const stub = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
  for (const match of readFileSync(new URL('./KnowledgeBaseSettings.vue', import.meta.url), 'utf8').matchAll(/from\s+['"]([^'"]+\.vue)['"]/g)) modules[match[1]] = stub
  modules['@/views/knowledge-base/settings/KBParserSettings.vue'] = { __esModule: true,
    default: loadComponent(new URL('../knowledge-base/settings/KBParserSettings.vue', import.meta.url), modules) }
  const app = renderer.createApp(loadComponent(new URL('./KnowledgeBaseSettings.vue', import.meta.url), modules))
  registerPrimitives(app)
  app.component('t-tabs', defineComponent({ emits: ['update:modelValue'], setup: (_, { emit }) => () =>
    h('button', { 'data-parser-tab': true, onClick: () => emit('update:modelValue', 'parser') }, 'Parser') }))
  app.component('t-select', defineComponent({ props: ['value'], emits: ['change'], setup: (props, { emit }) => () =>
    h('select', { value: props.value, onChange: (value: string) => emit('change', value) }) }))
  app.component('t-checkbox', stub.default)
  app.component('t-tab-panel', stub.default)
  app.config.globalProperties.$t = (key: string) => key
  const errors: unknown[] = []
  app.config.errorHandler = error => errors.push(error)
  const root = element('root')
  try {
    app.mount(root); await settle()
    const buttons = () => descendants(root).filter(node => node.tag === 'button' && ['common.cancel', 'common.save'].includes(visibleText(node)))
    const assertDirty = (dirty: boolean) => { assert.equal(buttons().length, 2); assert.deepEqual(buttons().map(node => node.props.disabled), [!dirty, !dirty]) }
    assertDirty(false)
    descendants(root).find(node => node.props['data-parser-tab'])!.props.onClick()
    await settle()
    assert.deepEqual(errors, [])
    const selects = () => descendants(root).filter(node => node.tag === 'select')
    assert.deepEqual(selects().map(node => node.props.value), ['anydoc', 'simple'])
    assertDirty(false)
    selects()[0].props.onChange('mineru'); await settle()
    assertDirty(true)
    buttons()[0].props.onClick(); await settle()
    assertDirty(false)
    assert.deepEqual(selects().map(node => node.props.value), ['anydoc', 'simple'])
    assert.deepEqual(errors, [])
  } finally { app.unmount(); globalThis.window = previousWindow }
})
