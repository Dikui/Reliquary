import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createI18n } from 'vue-i18n'
import { loadComponent, renderer, element, descendants, visibleText, click, settle } from '../views/library/componentTestHarness'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import { contentBlocks, formatContent, contentCharacters } from '../utils/modelCallContent'

const request = ' ' + JSON.stringify({ messages: [{ role: 'system', content: 'Keep the full prompt' }, { role: 'user', content: JSON.stringify({ title: 'Fixture', body: '正文 full body\n  whitespace' }) }] }) + ' '
const response = JSON.stringify({ choices: [{ message: { role: 'assistant', content: '{"summary":"Full response"}' } }] })
function environment(locale: string, props: Record<string, unknown>) {
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
  const reads: string[] = [], copies: string[] = [], queries: any[] = []
  const api = { async read(path: string) { reads.push(path); return { id: 'call1', request, response, complete: false } }, async callDetails(query: unknown) { queries.push(query); return { data: [{ id: 'call1', model_name: 'Fixture', attempt: 1 }], total: 1 } } }
  const modules = { 'vue-i18n': { useI18n: () => i18n.global }, '@/api/insights': api, '@/utils/modelCallContent': { contentBlocks, formatContent, contentCharacters }, '@/utils/clipboard': { async copyToClipboard(text: string) { copies.push(text); return true } } }
  const component = loadComponent(new URL('./ModelCallContentViewer.vue', import.meta.url), modules)
  const root = element('root'), errors: unknown[] = [], app = renderer.createApp(component, props)
  app.config.errorHandler = err => errors.push(err); app.mount(root)
  return { root, reads, copies, queries, errors, close: () => app.unmount() }
}
for (const locale of ['zh-CN', 'en-US']) test(`${locale}: viewer renders roles, folded body, formatting, complete copying and partial response`, async () => {
  const env = environment(locale, { callId: 'call1' }); await settle()
  assert.deepEqual(env.errors, []); assert.deepEqual(env.reads, ['model-calls/call1/content'])
  assert.match(visibleText(env.root), /system/); assert.match(visibleText(env.root), /user/)
  const details = descendants(env.root).filter(n => n.tag === 'details')
  const body = details.find(n => String(n.props.class).includes('message-body'))!
  assert.ok(body); assert.equal(body.props.open, undefined); assert.match(visibleText(body), /正文 full body\n  whitespace/)
  assert.match(visibleText(body), new RegExp(String(contentCharacters('正文 full body\n  whitespace'))))
  const bodyCopy = descendants(body).find(n => n.tag === 'button')!; await click(bodyCopy)
  assert.equal(env.copies[0], '正文 full body\n  whitespace')
  const raw = details.at(-1)!; assert.equal(raw.props.open, false)
  await click(descendants(raw).find(n => n.tag === 'button')!); assert.equal(env.copies[1], request)
  const output = descendants(env.root).filter(n => n.tag === 'nav')[0]!.children.filter(n => n.tag === 'button')[1]!
  await click(output); assert.match(visibleText(env.root), /assistant/); assert.match(visibleText(env.root), /"summary": "Full response"/)
  assert.match(visibleText(env.root), locale === 'zh-CN' ? /响应未完整接收/ : /partially received/)
  assert.equal(env.reads.length, 1); env.close()
})
test('timeline viewer selects calls for exactly the selected entry, attempt and span', async () => {
  const env = environment('en-US', { entryId: 'entry1', processingAttempt: 5, spanId: 'span1', direction: 'output' }); await settle()
  assert.deepEqual(env.queries, [{ entry_id: 'entry1', processing_attempt: 5, span_id: 'span1', limit: 1000, offset: 0 }]); assert.match(visibleText(env.root), /Full response/); assert.deepEqual(env.errors, []); env.close()
})
test('formatting preserves raw non-JSON content and body extraction preserves whitespace', () => {
  assert.equal(formatContent(' bad response\n '), ' bad response\n ')
  assert.equal(formatContent('{"a":1}'), '{\n  "a": 1\n}')
  assert.equal(contentBlocks(request)[1]!.body, '正文 full body\n  whitespace')
})
test('both timeline tabs and call statistics expose the shared viewer', () => {
  const timeline = readFileSync(new URL('./EntryProcessingTimeline.vue', import.meta.url), 'utf8')
  const statistics = readFileSync(new URL('../views/insights/ModelCallsPage.vue', import.meta.url), 'utf8')
  assert.match(timeline, /<ModelCallContentViewer[^>]+:span-id="selectedRow.node.span_id"[^>]+:direction="detailTab"/)
  assert.match(statistics, /@click="viewingCall = call.id"/); assert.match(statistics, /<ModelCallContentViewer :call-id="viewingCall"/)
})
