import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import { loadUiComponent } from '../../../components/ui/uiTestLoader'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'

const Dialog = defineComponent({
  props: ['modelValue'],
  setup: (props, { slots }) => () => props.modelValue ? h('section', [slots.default?.(), slots.footer?.()]) : null,
})

test('reprocessing preserves saved overrides and emits edited configuration only after confirmation', async () => {
  const configurations: any[] = [], confirmations: any[] = []
  let cancellations = 0
  let update!: (config: any) => void
  const Settings = defineComponent({
    props: ['config'], emits: ['update:config'],
    setup(props, { emit }) {
      update = config => emit('update:config', config)
      return () => { configurations.push(JSON.parse(JSON.stringify(props.config))); return h('div') }
    },
  })
  const component = loadComponent(new URL('./ReparseConfirmDialog.vue', import.meta.url), {
    '@/components/ui': { Button: loadUiComponent('Button'), Dialog },
    '../settings/LibraryChunkingSettings.vue': { __esModule: true, default: Settings },
  })
  const input = reactive({
    visible: false,
    libraryInfo: { chunking_config: { chunk_size: 512, chunk_overlap: 80, enable_parent_child: true } },
    reparsePreview: { entryId: 'entry', sourceKind: 'article', fileName: 'Source', processOverrides: { chunking_config: {
      chunk_size: 800, chunk_overlap: 0, enable_parent_child: false, separators: [';'], token_limit: 0, languages: ['en'],
    } } },
  })
  const app = renderer.createApp({ render: () => h(component, { ...input, onConfirm: (value: unknown) => confirmations.push(value), onCancel: () => cancellations++, 'onUpdate:visible': (value: boolean) => { input.visible = value } }) })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } }))
  const root = element('root'); app.mount(root)
  try {
    input.visible = true; await settle()
    const config = configurations.at(-1)
    assert.equal(config.chunkSize, 800); assert.equal(config.chunkOverlap, 0)
    assert.equal(config.enableParentChild, false); assert.deepEqual(config.languages, ['en'])
    update({ ...config, chunkSize: 1200, separators: [';', 'custom'] }); await settle()
    assert.equal(confirmations.length, 0)
    await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === zh.reparseConfirm.cancel)!)
    assert.equal(cancellations, 1); assert.equal(confirmations.length, 0)
    input.visible = true; await settle()
    assert.equal(configurations.at(-1).chunkSize, 800)
    update({ ...config, chunkSize: 1200, separators: [';', 'custom'] }); await settle()
    await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === zh.reparseConfirm.confirmReparse)!)
    assert.equal(confirmations.length, 1)
    assert.deepEqual(confirmations[0].processConfig.chunking_config, {
      chunk_size: 1200, chunk_overlap: 0, separators: [';', 'custom'], enable_parent_child: false,
      parent_chunk_size: 4096, child_chunk_size: 384, strategy: 'auto', token_limit: 0, languages: ['en'],
    })
    assert.deepEqual(input.reparsePreview.processOverrides.chunking_config.separators, [';'])
  } finally { app.unmount() }
})
