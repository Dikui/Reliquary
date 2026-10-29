import assert from 'node:assert/strict'
import test from 'node:test'
import * as vue from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import { loadUiComponent } from '../../../components/ui/uiTestLoader'
import { descendants, element, loadComponent, renderer, settle } from '../componentTestHarness'

// Native text/select editing is irrelevant to the shared Switch event boundary.
const component = loadComponent(new URL('./LibraryChunkingSettings.vue', import.meta.url), {
  vue: { ...vue, vModelText: {}, vModelSelect: {} },
  '@/components/ui': Object.fromEntries(['Button', 'Input', 'Switch'].map(name => [name, loadUiComponent(name)])),
})

test('the parent/child switch emits the newly selected value in the processing configuration', async () => {
  const configs: any[] = []
  const app = renderer.createApp(component, {
    config: { chunkSize: 512, chunkOverlap: 80, separators: [';'], enableParentChild: false, parentChunkSize: 4096, childChunkSize: 384 },
    'onUpdate:config': (value: unknown) => configs.push(value),
  })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } }))
  const root = element('root'); app.mount(root)
  try {
    await settle()
    const input = descendants(root).find(node => node.props.role === 'switch')!
    const handlers = Array.isArray(input.props.onChange) ? input.props.onChange : [input.props.onChange]
    for (const handler of handlers) handler({ target: { checked: true } })
    await settle()
    assert.equal(configs.at(-1)?.enableParentChild, true)
    assert.deepEqual(configs.at(-1)?.separators, [';'])
  } finally { app.unmount() }
})
