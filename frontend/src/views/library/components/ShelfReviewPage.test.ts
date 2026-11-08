import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import { defineComponent, h, reactive } from 'vue'
import zh from '../../../i18n/locales/zh-CN'
import { click, descendants, element, loadComponent, renderer, settle, visibleText, type Element } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'
import * as shelfReview from '../../../utils/shelfReview'
import * as dateTime from '../../../utils/dateTime'
import { toast, useFeedback } from '../../../components/ui/feedback'

const item = (id: string, conflict = 'new'): any => ({ id, operation: { id, type: 'create_entity', entity: { name: `名称 ${id}`, kind: 'model' } }, before: null, after: { canonical_name: `名称 ${id}`, kind: 'model' }, conflicts: [{ type: conflict }] })
const changeset = (items = [item('one'), item('two')]): any => ({ id: 'set-a', revision: 4, status: 'pending', source: { type: 'agent', agent_name: '合成助手' }, created_at: '2026-11-07T12:00:00Z', items })
const find = (root: Element, tag: string, text: string) => descendants(root).find(node => node.tag === tag && visibleText(node) === text)!
async function mount(t: test.TestContext, initial = changeset(), api: Record<string, unknown> = {}) {
  let draft = structuredClone(initial)
  const saves: any[] = [], confirmations: any[] = []
  const route = reactive({ query: {} as Record<string, string>, hash: '' })
  const modules: Record<string, unknown> = {
    ...entryPresentationTestModules,
    '@/utils/shelfReview': shelfReview, '@/utils/dateTime': dateTime,
    '@/api/shelf': { SALE_STATUSES: ['GA', 'DEV', 'Planning', 'EOM', '待定'], findShelf: async () => ({ success: true, data: { items: [] } }) },
    '@/api/entities': { getEntity: async (id: string) => ({ success: true, data: { entity: { id, revision: 7, kind: 'model', canonical_name: '已有型号' } } }) },
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async (to: any) => { route.query = to.query } }), onBeforeRouteLeave() {}, onBeforeRouteUpdate() {} },
    '@/api/shelf-review': {
      listShelfChangesets: async () => ({ success: true, data: { items: [draft], total: 1, page: 1, page_size: 20 } }),
      getShelfChangeset: async () => ({ success: true, data: structuredClone(draft) }),
      saveShelfReview: async (_: string, _id: string, input: any) => {
        saves.push(JSON.parse(JSON.stringify(input))); draft.revision++
        for (const decision of saves.at(-1).decisions) draft.items.find((value: any) => value.id === decision.item_id).decision = decision
        return { success: true, data: structuredClone(draft) }
      },
      confirmShelfChangeset: async (_: string, _id: string, input: any) => { confirmations.push(input); return { success: true, data: { ...draft, status: 'confirmed' } } },
      ...api,
    },
  }
  // Reka's DOM-backed picker is represented by a semantic input in this renderer.
  modules['@/components/ui'] = { ...modules['@/components/ui'] as object, Combobox: defineComponent({ props: ['modelValue', 'options'], emits: ['update:modelValue'], setup: (props, { attrs, emit }) => () => h('input', { ...attrs, 'data-combobox': true, value: props.modelValue, onInput: (event: any) => emit('update:modelValue', event.target.value) }) }) }
  modules['./ShelfReviewValue.vue'] = { __esModule: true, default: loadComponent(new URL('./ShelfReviewValue.vue', import.meta.url), modules) }
  modules['./ShelfCertificationConflictEditor.vue'] = { __esModule: true, default: loadComponent(new URL('./ShelfCertificationConflictEditor.vue', import.meta.url), modules) }
  const component = loadComponent(new URL('./ShelfReviewPage.vue', import.meta.url), modules)
  const app = renderer.createApp(component, { library: { id: 'library-a', name: '合成货架', entity_schema: { kinds: [{ key: 'model', name: '型号' }] }, shelf_config: { regions: [{ key: 'north', name: '可售区域' }] } } })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } }))
  const root = element('root'); app.mount(root); t.after(() => { app.unmount(); useFeedback().confirmations.forEach(dialog => dialog.cancel()); toast.clear() }); await settle()
  return { root, saves, confirmations, route }
}

// Small spec, task 18 / ADR 0023: HTTP mocks replace only the persisted changeset boundary.
test('reviewer handles individual proposals and saves decisions without confirming shelf writes', async t => {
  const { root, saves, confirmations } = await mount(t)
  const proposals = descendants(root).filter(node => node.tag === 'article')
  await click(find(proposals[0]!, 'button', '接受'))
  await click(find(proposals[1]!, 'button', '拒绝'))
  await click(find(root, 'button', '保存进度'))
  assert.deepEqual(saves, [{ revision: 4, decisions: [{ item_id: 'one', action: 'apply' }, { item_id: 'two', action: 'discard' }] }])
  assert.deepEqual(confirmations, [])
  assert.match(visibleText(root), /已处理 2 \/ 2/)
  assert.match(visibleText(root), /进度已保存/)
})

// Small spec, task 18: only the New group supports accepting a whole group.
test('accepting the new group leaves modifications undecided', async t => {
  const { root, saves } = await mount(t, changeset([item('one'), item('two'), item('old', 'modified')]))
  await click(find(root, 'button', '整组接受'))
  await click(find(root, 'button', '保存进度'))
  assert.deepEqual(saves[0].decisions, [{ item_id: 'one', action: 'apply' }, { item_id: 'two', action: 'apply' }])
  assert.equal(descendants(root).filter(node => node.tag === 'details' && node.props.class === 'review-group').length, 9)
})

// Small spec, task 18 / ADR 0023: unresolved required data cannot be confirmed.
test('required omissions disable confirmation until explicitly rejected', async t => {
  const { root, confirmations } = await mount(t, changeset([item('missing', 'required_missing')]))
  assert.equal(find(root, 'button', '确认写入').props.disabled, true)
  await click(find(root, 'button', '确认写入'))
  assert.deepEqual(confirmations, [])
  await click(find(root, 'button', '拒绝'))
  assert.equal(find(root, 'button', '确认写入').props.disabled, false)
})

// Small spec, task 18: a correction preserves spelling and is validated before confirmation.
test('reviewer fills a required name and saves a replacement proposal', async t => {
  const missing = item('missing', 'required_missing'); missing.operation.entity.name = ''
  const { root, saves } = await mount(t, changeset([missing]))
  await click(find(root, 'button', '修改'))
  const input = descendants(root).find(node => node.tag === 'input' && node.props['aria-label'] === '名称')!
  assert.ok(input)
  input.props.onInput({ target: { value: ' 客户原样名称 ' } }); await settle()
  await click(find(root, 'button', '应用修改'))
  assert.equal(find(root, 'button', '确认写入').props.disabled, true)
  await click(find(root, 'button', '保存进度'))
  assert.deepEqual(saves[0].decisions, [{ item_id: 'missing', action: 'replace', replacement: { id: 'missing', type: 'create_entity', entity: { name: ' 客户原样名称 ', kind: 'model' } } }])
})

// Small spec, task 18 / ADR 0023: same-identity decisions use the chosen current revision.
test('suspected identity can be saved as an alias of the explicit candidate', async t => {
  const proposal = item('alias', 'suspected_same')
  proposal.conflicts[0].candidates = [{ name: '名称 alias', other_entity_id: 'existing', other_kind: 'model' }]
  const { root, saves } = await mount(t, changeset([proposal]))
  await click(find(root, 'button', '同一，存为别名'))
  await click(find(root, 'button', '保存进度'))
  assert.deepEqual(saves[0].decisions, [{ item_id: 'alias', action: 'same_entity', target_entity_id: 'existing', target_revision: 7 }])
})

// Small spec, task 18: returning data is distinct from deferring or rejecting it.
test('uncertain agent proposals can be returned and saved for clarification', async t => {
  const { root, saves } = await mount(t, changeset([item('unclear', 'uncertain')]))
  await click(find(root, 'button', '退回提交方'))
  await click(find(root, 'button', '保存进度'))
  assert.deepEqual(saves[0].decisions, [{ item_id: 'unclear', action: 'return_to_submitter' }])
  assert.equal(find(root, 'button', '确认写入').props.disabled, true)
  assert.match(visibleText(root), /提交方可读取并修正/)
})

// Small spec, task 18: confirmation respects the last saved revision and deferred destination.
test('confirming reviewed items opens the new deferred changeset', async t => {
  const initial = changeset([item('one')]); initial.items[0].decision = { item_id: 'one', action: 'defer' }
  const deferred = { ...changeset([item('one')]), id: 'set-deferred', revision: 1 }
  const { root, confirmations, route } = await mount(t, initial, {
    confirmShelfChangeset: async (_: string, _id: string, input: any) => { confirmations.push(input); return { success: true, data: { ...initial, status: 'confirmed', deferred_changeset_id: 'set-deferred' } } },
    getShelfChangeset: async (_: string, id: string) => ({ success: true, data: id === 'set-deferred' ? deferred : initial }),
  })
  await click(find(root, 'button', '确认写入')); await useFeedback().confirmations.at(-1)!.accept(); await settle()
  assert.deepEqual(confirmations, [{ revision: 4, decisions: [{ item_id: 'one', action: 'defer' }] }])
  assert.equal(route.query.changeset, 'set-deferred')
  assert.match(visibleText(root), /已处理 0 \/ 1/)
})

// Small regression, task 18: editing an accepted proposal must not confirm an earlier value.
test('opening an edit blocks confirmation until the edited proposal is validated', async t => {
  const { root } = await mount(t, changeset([item('one')]))
  await click(find(root, 'button', '接受'))
  await click(find(root, 'button', '修改'))
  assert.equal(find(root, 'button', '确认写入').props.disabled, true)
})

// Small spec, task 18 / ADR 0023: stale writes preserve progress and never retry automatically.
test('a stale progress save preserves decisions and explains how to reload', async t => {
  let calls = 0
  const { root } = await mount(t, changeset([item('one')]), { saveShelfReview: async () => { calls++; throw { status: 409, message: '修订冲突' } } })
  await click(find(root, 'button', '拒绝')); await click(find(root, 'button', '保存进度'))
  assert.match(visibleText(root), /修订冲突/)
  assert.match(visibleText(root), /不会自动覆盖/)
  assert.equal(find(root, 'button', '拒绝').props['aria-pressed'], true)
  assert.equal(calls, 1)
})

// Small spec, task 18: paging cannot leave a previous page's selected changeset under new rows.
test('changing the changeset list page selects a changeset from that page', async t => {
  const first = changeset([item('first')]), last = { ...changeset([item('last')]), id: 'set-last' }
  const { root, route } = await mount(t, first, {
    listShelfChangesets: async (_: string, page: number) => ({ success: true, data: { items: page === 1 ? [first] : [last], total: 21 } }),
    getShelfChangeset: async (_: string, id: string) => ({ success: true, data: id === 'set-last' ? last : first }),
  })
  await click(find(root, 'button', '下一页'))
  assert.match(visibleText(root), /名称 last/)
  assert.equal(route.query.changeset, 'set-last')
})

// Small regression, task 18: refresh must not overwrite decisions made during an in-flight read.
test('refresh blocks new decisions until the saved changeset has been reloaded', async t => {
  let reads = 0, finish!: (value: unknown) => void
  const initial = changeset([item('one')])
  const response = { success: true, data: { items: [initial], total: 1 } }
  const { root } = await mount(t, initial, { listShelfChangesets: async () => ++reads === 1 ? response : new Promise(resolve => { finish = resolve }) })
  await click(find(root, 'button', '刷新'))
  assert.equal(find(root, 'button', '接受').props.disabled, true)
  finish(response); await settle()
  assert.equal(find(root, 'button', '接受').props.disabled, false)
})

// Small spec, task 18 / ADR 0023: acknowledgements name the concrete cross-category owners.
test('same-name acknowledgement saves the exact category and entity confirmations', async t => {
  const proposal = item('same-name', 'cross_kind_same_name')
  proposal.conflicts[0].details = { conflicts: [{ name: '名称 same-name', other_entity_id: 'platform', other_kind: 'platform', other_kind_label: '平台' }] }
  const { root, saves } = await mount(t, changeset([proposal]))
  await click(find(root, 'button', '确认同名，分别保存')); await click(find(root, 'button', '保存进度'))
  assert.deepEqual(saves[0].decisions, [{ item_id: 'same-name', action: 'apply', name_confirmations: [{ name: '名称 same-name', other_entity_id: 'platform', other_kind: 'platform' }] }])
})

// Small spec, task 18: absence must be resolved explicitly and never means deletion.
test('disappeared proposals offer keep and retire decisions', async t => {
  const proposal = item('missing', 'disappeared'); proposal.operation = { id: 'missing', type: 'missing_entity', entity_id: 'model', revision: 7 }
  const { root, saves } = await mount(t, changeset([proposal]))
  await click(find(root, 'button', '保留')); await click(find(root, 'button', '保存进度'))
  assert.deepEqual(saves[0].decisions, [{ item_id: 'missing', action: 'keep' }])
  await click(find(root, 'button', '标为退市')); await click(find(root, 'button', '保存进度'))
  assert.deepEqual(saves[1].decisions, [{ item_id: 'missing', action: 'retire' }])
})

// Small regression, task 18 / ADR 0023: certification regions retain customer text.
test('editing a certification region allows text outside configured sale regions', async t => {
  const proposal = item('cert', 'modified'); proposal.operation = { id: 'cert', type: 'create_certification', certification: { name: '合成认证', owner_id: 'model', standard: 'standard', certification_object: '客户对象', region: '客户地区' } }
  const { root, saves } = await mount(t, changeset([proposal]))
  await click(find(root, 'button', '修改'))
  const region = descendants(root).find(node => node.tag === 'input' && node.props['aria-label'] === '地区')!
  assert.ok(region)
  region.props.onInput({ target: { value: ' 新的客户地区 ' } }); await settle()
  await click(find(root, 'button', '保存进度'))
  assert.equal(saves[0].decisions[0].replacement.certification.region, ' 新的客户地区 ')
})

// Small spec, task 25 / ADR 0023: HTTP fixtures replace the batch API. Feedback
// stays visible without expanding a diff and supports the normal acknowledgement.
test('reviewer sees unmapped column content and acknowledges it before confirming', async t => {
  const proposal = {
    id: 'column-f', operation: { id: 'column-f', type: 'import_notice', import_location: { worksheet: '资料清单', cells: ['F3', 'F4', 'F5'] } },
    before: null, after: null,
    conflicts: [{ type: 'unmapped_column', details: { worksheet: '资料清单', column: 'F', header: ' 备注原文 ', nonempty_rows: 17, examples: [' 示例一 ', '0', '示例三'] } }],
  }
  const initial = changeset([proposal]); initial.source = { type: 'import', file_name: 'synthetic.xlsx', import_batch_id: 'batch-25' }
  const { root, saves, confirmations } = await mount(t, initial)
  const group = descendants(root).find(node => node.tag === 'details' && node.props.class === 'review-group' && visibleText(node).includes('未映射列'))!
  assert.ok(group)
  assert.equal(group.props.open, true)
  const feedback = descendants(group).find(node => node.tag === 'dl')!
  assert.ok(feedback)
  for (const value of ['工作表', '资料清单', '列', 'F', '表头原文', ' 备注原文 ', '非空行数', '17', '示例值', ' 示例一 ', '0', '示例三']) assert.ok(visibleText(feedback).includes(value), value)
  assert.equal(find(root, 'button', '确认写入').props.disabled, true)
  await click(find(group, 'button', '知道了'))
  assert.equal(find(root, 'button', '确认写入').props.disabled, false)
  await click(find(root, 'button', '保存进度'))
  assert.deepEqual(saves[0].decisions, [{ item_id: 'column-f', action: 'apply' }])
  await click(find(root, 'button', '确认写入')); await useFeedback().confirmations.at(-1)!.accept(); await settle()
  assert.deepEqual(confirmations[0], { revision: 5, decisions: [{ item_id: 'column-f', action: 'apply' }] })
})

// Small regression, task 25: submitters can attach arbitrary issue details.
// An incomplete notice must not prevent the whole review page from rendering.
test('incomplete column feedback remains reviewable without a rendering error', async t => {
  const proposal = { id: 'column', operation: { id: 'column', type: 'import_notice' }, before: null, after: null, conflicts: [{ type: 'unmapped_column', details: { worksheet: 'Map', column: 'F' } }] }
  const { root } = await mount(t, changeset([proposal]))
  assert.ok(find(root, 'button', '知道了'))
  await click(find(root, 'button', '知道了'))
  assert.equal(find(root, 'button', '确认写入').props.disabled, false)
})

function certItem(id: string, worksheet = 'Sheet A'): any {
  return { id, operation: { id, type: 'update_certification', certification_owner: { name: 'Unmatched', kind: 'model' }, certification: { owner_id: '', standard: 'standard_a', certification_object: '', region: '' }, import_location: { worksheet, cells: ['A3', 'B3'] } }, before: null, after: null, conflicts: [{ type: 'required_missing', details: { missing_fields: ['certification_object', 'region'] } }] }
}

// Small spec, task 23: uniform values explicitly replace every row of one sheet,
// including already-filled rows, without confirming writes or touching another sheet.
test('reviewer applies uniform certification fields only to the selected worksheet', async t => {
  const rows = [certItem('a'), certItem('b'), certItem('c', 'Sheet B')]
  rows[1].operation.certification.region = 'Earlier value'
  const initial = { ...changeset(rows), source: { type: 'import', file_name: 'Synthetic.xlsx' } }
  const { root, saves, confirmations } = await mount(t, initial)
  const form = descendants(root).find(node => node.tag === 'form' && node.props['aria-label'] === '整表统一值 · Sheet A')!
  assert.ok(form)
  const otherForm = descendants(root).find(node => node.tag === 'form' && node.props['aria-label'] === '整表统一值 · Sheet B')!
  const otherInput = descendants(otherForm).find(node => node.tag === 'input')!
  otherInput.props.onInput({ target: { value: 'Other pending object' } }); await settle()
  const inputs = descendants(form).filter(node => node.tag === 'input')
  inputs[0]!.props.onInput({ target: { value: 'Container' } })
  inputs[1]!.props.onInput({ target: { value: 'Country' } }); await settle()
  await click(find(form, 'button', '应用到本批该工作表全部 2 行'))
  assert.equal(saves.length, 1)
  assert.deepEqual(saves[0].decisions.map((decision: any) => decision.item_id), ['a', 'b'])
  for (const decision of saves[0].decisions) {
    assert.equal(decision.action, 'replace')
    assert.equal(decision.replacement.certification.certification_object, 'Container')
    assert.equal(decision.replacement.certification.region, 'Country')
  }
  assert.deepEqual(confirmations, [])
  const retained = descendants(root).find(node => node.tag === 'form' && node.props['aria-label'] === '整表统一值 · Sheet B')!
  assert.equal(descendants(retained).find(node => node.tag === 'input')!.props.value, 'Other pending object')
  assert.equal(find(root, 'button', '确认写入').props.disabled, true)
})

// Small spec, task 23: an unknown owner offers all three explicit choices.
test('certification owner conflicts offer mapping correction and separate creation', async t => {
  const proposal = certItem('owner'); proposal.conflicts = [{ type: 'uncertain' }]
  const { root } = await mount(t, changeset([proposal]))
  await click(find(root, 'button', '修改'))
  for (const label of ['对应到已有节点', '改写名称后重新校验', '新建不同节点']) assert.match(visibleText(root), new RegExp(label))
})

// Small spec, task 23: accepting a split only saves independently reviewable
// proposals; refusing the source row does not invoke the split action.
test('reviewer explicitly confirms split suggestions without writing shelf data', async t => {
  const proposal = certItem('split'); proposal.operation.certification.standard = 'One、Two'
  proposal.conflicts = [{ type: 'uncertain', details: { split_suggestions: [{ standard: 'one', year: 2019 }, { standard: 'two', year: 2022 }] } }]
  const { root, saves, confirmations } = await mount(t, changeset([proposal]))
  await click(find(root, 'button', '修改'))
  assert.match(visibleText(root), /one · 2019/)
  await click(find(root, 'button', '确认拆分为 2 条提议'))
  assert.equal(saves[0].decisions[0].action, 'replace')
  assert.deepEqual(saves[0].decisions[0].replacements.map((op: any) => [op.certification.standard, op.certification.year]), [['one', 2019], ['two', 2022]])
  assert.equal(saves[0].decisions[0].replacements[0].id, 'split')
  assert.deepEqual(confirmations, [])
})

// Small spec, task 23: choosing an existing node can explicitly save the
// original spelling as an alias using the current target revision.
test('reviewer queues an optional alias with the corrected certification', async t => {
  const proposal = certItem('owner'); proposal.conflicts = [{ type: 'uncertain' }]
  const { root, saves, confirmations } = await mount(t, changeset([proposal]))
  await click(find(root, 'button', '修改')); await click(find(root, 'button', '对应到已有节点'))
  const picker = descendants(root).find(node => node.props['data-combobox'])!
  picker.props.onInput({ target: { value: 'chosen-model' } })
  const checkbox = descendants(root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!
  checkbox.props.onChange({ target: { checked: true } }); await settle()
  const form = descendants(root).find(node => node.tag === 'form' && node.props.class === 'certification-review')!
  form.props.onSubmit({ preventDefault() {} }); await settle()
  const ops = saves[0].decisions[0].replacements
  assert.deepEqual(ops[0], { id: 'owner', type: 'add_alias', entity_id: 'chosen-model', revision: 7, alias: 'Unmatched' })
  assert.equal(ops[1].certification.owner_id, 'chosen-model')
  assert.deepEqual(confirmations, [])
})

// Small spec, task 23: a new model cannot be proposed without its parent.
test('reviewer must choose a parent before proposing a separate model', async t => {
  const proposal = certItem('owner'); proposal.conflicts = [{ type: 'uncertain' }]
  const { root, saves } = await mount(t, changeset([proposal]))
  await click(find(root, 'button', '修改')); await click(find(root, 'button', '新建不同节点'))
  const form = descendants(root).find(node => node.tag === 'form' && node.props.class === 'certification-review')!
  form.props.onSubmit({ preventDefault() {} }); await settle()
  assert.match(visibleText(form), /新建型号必须指定/); assert.equal(saves.length, 0)
  descendants(form).find(node => node.props['data-combobox'])!.props.onInput({ target: { value: 'parent-product' } }); await settle()
  form.props.onSubmit({ preventDefault() {} }); await settle()
  const ops = saves[0].decisions[0].replacements
  assert.deepEqual(ops[0], { id: 'owner', type: 'create_entity', entity: { name: 'Unmatched', kind: 'model' }, parent_id: 'parent-product' })
  assert.equal(ops[1].certification.owner_id, '$owner')
})

// Small regression, task 23: a failed correction save keeps the selected node
// and alias choice visible, so retry cannot silently lose the user's decision.
test('failed certification saves preserve owner and alias choices for retry', async t => {
  const proposal = certItem('owner'); proposal.conflicts = [{ type: 'uncertain' }]
  let attempts = 0
  const { root } = await mount(t, changeset([proposal]), { saveShelfReview: async () => { attempts++; throw new Error('Synthetic save failure') } })
  await click(find(root, 'button', '修改')); await click(find(root, 'button', '对应到已有节点'))
  descendants(root).find(node => node.props['data-combobox'])!.props.onInput({ target: { value: 'chosen-model' } })
  descendants(root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!.props.onChange({ target: { checked: true } }); await settle()
  descendants(root).find(node => node.tag === 'form' && node.props.class === 'certification-review')!.props.onSubmit({ preventDefault() {} }); await settle()
  assert.match(visibleText(root), /Synthetic save failure/)
  assert.equal(descendants(root).find(node => node.props['data-combobox'])?.props.value, 'chosen-model')
  assert.equal(descendants(root).find(node => node.tag === 'input' && node.props.type === 'checkbox')?.props.checked, true)
  descendants(root).find(node => node.tag === 'form' && node.props.class === 'certification-review')!.props.onSubmit({ preventDefault() {} }); await settle()
  assert.equal(attempts, 2)
})

// Small regression, acceptance round 3: known server conflict messages read in
// the reviewer's language with quoted values kept; unknown messages show as sent.
test('known conflict messages are translated and unknown messages stay as sent', async t => {
  const known = item('known', 'uncertain'); known.conflicts = [{ type: 'uncertain', message: 'uncertain: model identity "Aqua 原文写法" cannot be identified' }]
  const unknown = item('unknown', 'uncertain'); unknown.conflicts = [{ type: 'uncertain', message: 'uncertain: something new' }]
  const { root } = await mount(t, changeset([known, unknown]))
  assert.ok(visibleText(root).includes('“Aqua 原文写法”无法对应到已有条目。'))
  assert.ok(visibleText(root).includes('uncertain: something new'))
  assert.ok(!visibleText(root).includes('cannot be identified'))
})
