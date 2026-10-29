import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../i18n/locales/en-US'
import { diagnosticUI } from './diagnosticsTestSupport'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../library/componentTestHarness'
import * as reference from '../../utils/bodyReference'
import * as processing from '../../utils/entryProcessingStatus'
import * as classification from '../../types/entryClassification'

test('failed materials retain full detail links, confirm individual retries and batch all matching failures', async () => {
  const calls: any[] = [], notices: string[] = []
  let accept: (value: boolean) => void = () => {}, done = false
  const entry = { id: 'material-long-identifier', title: 'Actual failed material', library_id: 'library-a', library_name: 'Actual library', content_type: 'research', parse_status: 'completed', summary_status: 'failed', error_message: 'Actual model error', updated_at: '2026-10-28T10:00:00Z' }
  const ui = { ...diagnosticUI(), confirm: () => ({ result: new Promise<boolean>(resolve => { accept = resolve }), destroy() {} }), toast: { success: (message: string) => notices.push(message) } }
  const component = loadComponent(new URL('./FailedProcessingEntries.vue', import.meta.url), {
    '@/components/ui': ui, '@/utils/bodyReference': reference, '@/utils/entryProcessingStatus': processing, '@/types/entryClassification': classification,
    '@/api/library': { listEntryFiles: async (_id: string, query: any) => { calls.push(['list', query]); return { success: true, data: done ? [] : [entry], total: done ? 0 : 31 } }, reparseEntry: async (id: string) => { calls.push(['retry', id]); done = true; return { success: true } } },
    '@/components/EntryBatchDialog.vue': { __esModule: true, default: defineComponent({ props: ['visible', 'selection', 'initialAction'], setup: props => () => props.visible ? h('div', { 'data-batch': props.selection, 'data-action': props.initialAction }) : null }) },
  })
  const app = renderer.createApp(component, { libraries: [], libraryId: 'library-a' })
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en } }))
  const root = element('root'); app.mount(root)
  try {
    await settle()
    assert.equal(calls[0][1].parse_status, 'failed')
    assert.deepEqual(calls[0][1].libraries, ['library-a'])
    assert.ok(visibleText(root).includes('Actual model error'))
    assert.ok(visibleText(root).includes('Actual library'))
    assert.ok(descendants(root).some(n => n.tag === 'a' && n.props.href === '/platform/entries?parse_status=failed&libraries=library-a'))
    assert.ok(visibleText(root).includes('Research'))
    assert.ok(descendants(root).some(n => n.tag === 'a' && n.props.href === '/platform/entries/material-long-identifier'))
    await click(descendants(root).find(n => n.tag === 'button' && visibleText(n) === 'Retry all')!)
    const batch = descendants(root).find(n => n.props['data-action'] === 'reprocess')!
    assert.deepEqual(batch.props['data-batch'], { filters: { parse_status: 'failed', libraries: ['library-a'] } })
    await click(descendants(root).find(n => n.tag === 'button' && visibleText(n) === 'Retry')!)
    assert.equal(calls.some(call => call[0] === 'retry'), false)
    accept(true); await settle()
    assert.deepEqual(calls.find(call => call[0] === 'retry'), ['retry', entry.id])
    assert.equal(notices.length, 1)
  } finally { app.unmount() }
})
