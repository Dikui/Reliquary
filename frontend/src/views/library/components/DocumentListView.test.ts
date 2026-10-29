import assert from 'node:assert/strict'
import test from 'node:test'
import { createRequire } from 'node:module'
import { h, ref } from 'vue'
import { loadUiComponent } from '../../../components/ui/uiTestLoader'
import { descendants, element, hasClass, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import * as markdown from '../../../utils/markdownPlainText'
import * as processing from '../../../utils/entryProcessingStatus'
import zhCN from '../../../i18n/locales/zh-CN'

const require = createRequire(import.meta.url)
const { createI18n } = require('vue-i18n') as typeof import('vue-i18n')
const ui = Object.fromEntries(['Button', 'DataTable', 'Tag', 'StatusSquare'].map(name => [name, loadUiComponent(name)]))
const Component = loadComponent(new URL('./DocumentListView.vue', import.meta.url), {
  '@/components/EntryUpdateNotice.vue': { __esModule: true, default: loadComponent(new URL('../../../components/EntryUpdateNotice.vue', import.meta.url), {}) },
  '@/utils/entrySourceLabels': { entryChannelLabel: () => '', entryFileFormatLabel: () => '' }, '@/utils/markdownPlainText': markdown, '@/components/ui': ui, '@/utils/entryProcessingStatus': processing,
})

test('list focus previews one entry; J/K moves it and Enter opens the focused entry', async () => {
  const focused = ref('a'), opened: string[] = []
  const root = element('root')
  const app = renderer.createApp({ setup: () => () => h(Component, {
    items: [{ id: 'a', title: 'Alpha', parse_status: 'completed' }, { id: 'b', title: 'Beta', parse_status: 'failed' }],
    mode: 'list', focused: focused.value, selectedIds: new Set(),
    'onUpdate:focused': (id: string) => { focused.value = id }, onOpen: (item: {id:string}) => opened.push(item.id),
  }) })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zhCN } }))
  app.mount(root)
  await settle()
  const rows = () => descendants(root).filter(node => node.props['data-select-id'])
  assert.equal(rows().length, 2)
  const key = async (value: string) => {
    const row = rows().find(node => node.props['data-select-id'] === focused.value)!
    const button = descendants(row).find(node => node.tag === 'button')!
    button.props.onKeydown({ key: value, target: button, currentTarget: button, preventDefault() {}, isComposing: false })
    await settle()
  }
  await key('j')
  assert.equal(focused.value, 'b')
  await key('Enter')
  assert.deepEqual(opened, ['b'])
  await key('k')
  assert.equal(focused.value, 'a')
  assert.equal(rows().filter(node => node.props['aria-current'] === 'true').length, 1)
  assert.ok(hasClass(rows().find(node => node.props['aria-current'] === 'true')!, 'rq-focus-row'), 'the list uses the shared focus palette')
  assert.ok(visibleText(root).includes('Beta'))
  app.unmount()
})
