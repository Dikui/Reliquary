import assert from 'node:assert/strict'
import test from 'node:test'
import { createRequire } from 'node:module'
import { h, ref } from 'vue'
import { loadUiComponent } from '../../../components/ui/uiTestLoader'
import { descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import * as processing from '../../../utils/entryProcessingStatus'
import * as trace from '../../../utils/entryTrace'
import * as externalUrl from '../../../utils/externalUrl'
import * as markdown from '../../../utils/markdownPlainText'
import zhCN from '../../../i18n/locales/zh-CN'
import enUS from '../../../i18n/locales/en-US'
const require = createRequire(import.meta.url)
const { createI18n } = require('vue-i18n') as typeof import('vue-i18n')
const ui = Object.fromEntries(['Button', 'ErrorState', 'LibMark', 'Skeleton', 'StatusSquare', 'Tag'].map(name => [name, loadUiComponent(name)]))
const deferred = () => { let resolve!: (value: unknown) => void; const promise = new Promise(done => { resolve = done }); return { promise, resolve } }

for (const locale of ['zh-CN', 'en-US']) test(`${locale}: preview ignores an old response and recovers a failed read without inventing steps`, async () => {
  const old = deferred(), current = ref({ id: 'a', title: 'Alpha', parse_status: 'failed', summary_status: 'failed' })
  let fail = true
  const Component = loadComponent(new URL('./EntryPreview.vue', import.meta.url), {
    '@/components/ui': ui, '@/utils/entryProcessingStatus': processing, '@/utils/entryTrace': trace,
    '@/utils/externalUrl': externalUrl, '@/utils/markdownPlainText': markdown,
    '@/api/library': {
      getEntryDetails: (id: string) => id === 'a' ? old.promise : Promise.resolve(fail ? { success: false } : { success: true, data: { id, title: 'Beta detail', structured_summary: { overview: '**Verified** overview', points: ['First point'] }, entities: [{ id: 'e', canonical_name: 'Entity' }], source: 'javascript:alert(1)' } }),
      getEntrySpans: async () => ({ success: true, data: { trace: { name: 'pipeline', children: [] } } }),
    },
  })
  const opened: string[] = [], root = element('root')
  const app = renderer.createApp({ setup: () => () => h(Component, { entry: current.value, libraries: [], onOpen: () => opened.push(current.value.id) }) })
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zhCN, 'en-US': enUS } })
  app.use(i18n); app.mount(root); await settle()
  current.value = { id: 'b', title: 'Beta', parse_status: 'failed', summary_status: 'failed' }; await settle()
  old.resolve({ success: true, data: { title: 'Stale title', description: 'Stale summary' } }); await settle()
  assert.ok(visibleText(root).includes(i18n.global.t('entries.previewFailed')))
  assert.ok(!visibleText(root).includes('Stale'))
  assert.ok(visibleText(root).includes(i18n.global.t('entries.noTrace')))
  fail = false
  const retry = descendants(root).find(node => node.tag === 'button' && visibleText(node) === i18n.global.t('common.retry'))!
  retry.props.onClick(); await settle()
  assert.ok(visibleText(root).includes('Verified overview'))
  assert.ok(visibleText(root).includes('First point'))
  assert.ok(visibleText(root).includes('Entity'))
  assert.ok(!descendants(root).some(node => String(node.props.href).startsWith('javascript:')))
  descendants(root).find(node => node.tag === 'button' && visibleText(node).includes(i18n.global.t('entries.open')))!.props.onClick(); await settle()
  assert.deepEqual(opened, ['b'])
  app.unmount()
})
