import assert from 'node:assert/strict'
import { shelfConditionText } from '../../../utils/productShelf'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import type { ShelfQueryItem } from '../../../api/shelf'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'

const statuses = [
  { region: 'north', status: 'GA', planned_eom: false, notes: 'Original note', raw: 'Original spelling', conditions: [{ module_type: 'cell', part_ids: ['part-a'] }] },
  { region: 'north', status: 'DEV', planned_eom: false, notes: '', raw: '', conditions: [{ module_type: 'cell', part_ids: ['part-b'] }] },
  { region: 'south', status: 'EOM', planned_eom: true, notes: 'Keep south', raw: '', conditions: [] },
]
const model = (id: string, revision: number) => ({ entity: { id, canonical_name: id, revision }, shelf: { sale_statuses: id === 'model-b' ? [{ ...statuses[0], status: 'DEV', conditions: [] }, statuses[2]] : statuses, composition: [{ module_type: 'cell', optional: true, parts: [{ id: 'part-a', canonical_name: 'Cell A' }, { id: 'part-b', canonical_name: 'Cell B' }] }] } }) as unknown as ShelfQueryItem
async function mountEditor(t: test.TestContext, bulk = false, submit?: (...args: any[]) => Promise<unknown>) {
  const calls: any[][] = [], saved: any[] = []
  const component = loadComponent(new URL('./ShelfStatusEditor.vue', import.meta.url), {
    ...entryPresentationTestModules, '@/utils/productShelf': { shelfConditionText },
    '@/api/shelf': { SALE_STATUSES: ['GA', 'DEV', 'Planning', 'EOM', '待定'], submitShelfStatuses: async (...args: any[]) => { calls.push(args); return submit ? submit(...args) : { success: true, data: { id: 'set-a', revision: 1, status: bulk ? 'pending' : 'confirmed' } } } },
  })
  const app = renderer.createApp(component, { library: { id: 'library-a', shelf_config: { regions: [{ key: 'north', name: 'North' }], module_types: [{ key: 'cell', name: 'Cell' }] } }, models: bulk ? [model('model-a', 4), model('model-b', 7)] : [model('model-a', 4)], region: 'north', bulk, onSaved: (value: unknown) => saved.push(value) })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount()); await settle()
  return { root, calls, saved }
}
// Small spec, task 14 / ADR 0023: the HTTP changeset boundary is the only write seam.
test('editing one conditional record confirms one operation and preserves other records and raw spelling', async t => {
  const { root, calls, saved } = await mountEditor(t)
  assert.match(visibleText(root), /Cell A/)
  const select = descendants(root).find(node => node.tag === 'select' && node.props['aria-label'] === '状态 1')!
  assert.ok(select)
  select.props.onChange({ target: { value: 'EOM' } }); await settle()
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === '确认修改')!)
  assert.equal(calls.length, 1)
  assert.equal(calls[0]![2], true)
  const operations = calls[0]![1]
  assert.equal(operations.length, 1)
  assert.equal(operations[0].type, 'set_sale_statuses')
  assert.equal(operations[0].revision, 4)
  assert.equal(operations[0].entity_id, 'model-a')
  assert.deepEqual(operations[0].sale_statuses, [{ ...statuses[0], status: 'EOM' }, statuses[1], statuses[2]])
  assert.equal(saved.length, 1)
})

// Small spec, task 14: saving bulk edits only proposes changes, preserving each model's conditions.
test('bulk edits create a pending changeset with one revision checked operation per selected model', async t => {
  const { root, calls, saved } = await mountEditor(t, true)
  const select = descendants(root).find(node => node.tag === 'select' && node.props['aria-label'] === '状态 1')!
  select.props.onChange({ target: { value: 'Planning' } }); await settle()
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === '保存草稿变更集')!)
  assert.equal(calls[0]![2], false)
  assert.deepEqual(calls[0]![1].map((operation: any) => [operation.entity_id, operation.revision]), [['model-a', 4], ['model-b', 7]])
  assert.deepEqual(calls[0]![1][0].sale_statuses, [{ ...statuses[0], status: 'Planning', notes: '' }, { ...statuses[1], status: 'Planning' }, statuses[2]])
  assert.deepEqual(calls[0]![1][1].sale_statuses, [{ ...statuses[0], status: 'Planning', conditions: [], notes: '' }, statuses[2]])
  assert.equal(saved[0].status, 'pending')
})
// Small regression, task 14: concurrency errors must not clear input or retry a write automatically.
test('a rejected confirmation keeps the edited value and displays the conflict', async t => {
  const { root, calls, saved } = await mountEditor(t, false, async () => { throw { message: 'Revision conflict', error: { details: { items: [{ conflicts: [{ message: 'Reload model' }] }] } } } })
  const select = descendants(root).find(node => node.tag === 'select' && node.props['aria-label'] === '状态 1')!
  select.props.onChange({ target: { value: 'EOM' } }); await settle()
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === '确认修改')!)
  assert.match(visibleText(root), /Revision conflict.*Reload model/)
  assert.equal(descendants(root).find(node => node.props['aria-label'] === '状态 1')!.props.value, 'EOM')
  assert.equal(calls.length, 1); assert.equal(saved.length, 0)
})

// Small spec, task 14: absence is an explicit removal, never an implicit TBD value.
test('choosing no record removes only that conditional regional record', async t => {
  const { root, calls } = await mountEditor(t)
  descendants(root).find(node => node.props['aria-label'] === '状态 1')!.props.onChange({ target: { value: 'no_record' } }); await settle()
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === '确认修改')!)
  assert.deepEqual(calls[0]![1][0].sale_statuses, [statuses[1], statuses[2]])
})
