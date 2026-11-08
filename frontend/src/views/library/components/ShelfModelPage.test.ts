import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import * as shelfText from '../../../utils/productShelf'
import * as externalUrl from '../../../utils/externalUrl'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
import * as dates from '../../../utils/dateTime'
import { click, descendants, element, loadComponent, renderer, settle, visibleText, type Element } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'

// Load the production error decoder without importing the browser HTTP client.
const fileApiModule = { exports: {} as any }
new Function('require', 'module', 'exports', ts.transpileModule(readFileSync(new URL('../../../api/shelf-files.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS } }).outputText)(() => ({}), fileApiModule, fileApiModule.exports)
const { shelfWriteError } = fileApiModule.exports

const entity = (id: string, kind: string, name = id) => ({ id, kind, canonical_name: name, library_id: 'library-a', revision: 1, entity_type: 'product', attributes: {}, space_id: 1, created_at: '', updated_at: '' })
const parts = ['Alpha', 'Beta', 'Gamma', 'Delta', 'Epsilon', 'Zeta'].map(name => entity(name.toLowerCase(), 'part', `${name} cell`))
const condition = [{ module_type: 'cell', part_ids: ['alpha'] }]
const library = { id: 'library-a', name: 'Synthetic shelf', shelf_enabled: true, shelf_config: { module_types: [{ key: 'cell', name: '电芯' }], regions: [{ key: 'north', name: '北区' }, { key: 'south', name: '南区' }], stages: [{ key: 'review', name: '评审阶段' }] }, entity_schema: { kinds: [], fields: [{ key: 'energy', name: '能量', type: 'number', unit: 'kWh', product_line_ids: ['line'], kinds: ['model'] }] } }
const doc = { id: 'version', file_id: 'file', owner_id: 'product', cloud_url: 'https://example.invalid/drawing', version_label: 'Customer V2', stage: 'final', parse_status: 'not_parsed', conditions: [] }
const checklist = [
  { requirement: { ...entity('drawing', 'document_requirement', '图纸'), attributes: { requirement: { name_en: 'Drawing', draft_due_stage: 'review', final_due_stage: 'review', stage_label: '原表阶段', due_text: '客户交期原文', owner: '负责人原名' } } }, status: 'delivered', has_file: true, document: doc, document_name: '客户图纸.pdf', origin: 'ancestor', owner_id: 'product', conditions: [] },
  { requirement: entity('manual', 'document_requirement', '手册'), status: 'draft_only', has_file: true, document: { ...doc, id: 'draft', stage: 'draft' }, origin: 'self', owner_id: 'model', conditions: [] },
  { requirement: entity('missing', 'document_requirement', '缺少的资料'), status: 'missing', has_file: false, conditions: condition },
  { requirement: entity('later', 'document_requirement', '后续资料'), status: 'not_due', has_file: false, conditions: [] },
]
const certs = [
  { entity: entity('cert-self', 'certification'), standard_name: '整机标准', origin: 'self', certification: { owner_id: 'model', year: 2026, certification_object: '整机', region: '北区', progress: '客户进度原文', agency: '独立机构', validity: '客户有效期原文', number: 'TEST-001', conditions: [] } },
  { entity: entity('cert-part', 'certification'), standard_name: '部件标准', origin: 'component', certification: { owner_id: 'alpha', certification_object: '电芯', region: '北区', conditions: [] } },
  { entity: entity('cert-upper', 'certification'), standard_name: '系列标准', origin: 'ancestor', certification: { owner_id: 'platform', certification_object: '系列', region: '北区', conditions: condition } },
]
const data = (selected = false): any => ({ entity: entity('model', 'model', 'Synthetic model'), recorded: true, as_of: '2026-11-07T12:00:00Z', facts: [
  { entity_id: 'model', field: 'belongs_to', value: { object_id: 'product' } },
  { entity_id: 'cert-part', field: 'certification', value: certs[1]!.certification, conditions: condition, origin: 'component' },
], shelf: { current_stage: 'review', stage_owner_id: 'product', composition: [{ module_type: 'cell', optional: true, parts }],
  sale_statuses: [{ region: 'north', status: 'GA', planned_eom: true, notes: '状态备注原文', raw: '状态原文', conditions: condition }],
  parameters: { energy: [{ value: { min: 9, max: 54 }, conditions: condition }] },
  certifications: selected ? [certs[0]] : certs,
  checklist: selected ? checklist.filter(row => row.status !== 'missing') : checklist,
  other_documents: selected ? [] : [{ entity: entity('brochure', 'document_file', '产品介绍.pdf'), document: { ...doc, file_id: 'brochure', owner_id: 'alpha', parse_status: 'failed', parse_error: '保留原件' }, owner: parts[0], origin: 'component', conditions: condition }],
} })
const success = (value: unknown) => Promise.resolve({ success: true, data: value })
async function mountPage(t: test.TestContext, options: { api?: Record<string, unknown>; locale?: string; value?: any; config?: any; filesApi?: Record<string, unknown>; relationsApi?: Record<string, unknown>; entityApi?: Record<string, unknown> } = {}) {
  const requests: any[] = [], writes: any[] = []
  const parents: Record<string, any> = { product: { entity: entity('product', 'product', '产品原名'), facts: [{ field: 'belongs_to', value: { object_id: 'platform' } }] }, platform: { entity: entity('platform', 'platform', '平台原名'), facts: [{ field: 'belongs_to', value: { object_id: 'line' } }] }, line: { entity: entity('line', 'product_line', '产品线原名'), facts: [] } }
  // Synthetic API responses replace HTTP only; the real page and shared controls render.
  const modules: Record<string, unknown> = {
    ...entryPresentationTestModules, '@/api/library': {}, '@/utils/request': {}, '@/utils/externalUrl': externalUrl, '@/utils/productShelf': shelfText, '@/utils/dateTime': dates,
    '@/api/shelf-files': { writeShelf: (_: string, ops: any[]) => { writes.push(ops); return success({ status: 'confirmed' }) }, shelfEntities: () => Promise.resolve([...parts, entity('model', 'model'), ...checklist.map(row => row.requirement), entity('existing-file', 'document_file', '已有原件.pdf')]), listShelfVersions: () => success([{ ...doc, owner_id: '', file_id: 'existing-file', is_current: true, stage: 'final' }]), shelfWriteError, ...options.filesApi },
    '@/api/relations': { listEntityRelations: () => success([{ relation_type: 'composed_of', relations: [{ id: 'composition-alpha', relation_type: 'composed_of', version: 4, direction: 'outgoing', attributes: { module_type: 'cell' }, peer: parts[0] }] }]), ...options.relationsApi },
    '@/api/operation-logs': { OPERATION_TYPES: ['update_entity'] },
    '@/api/entities': { uploadEntityAttachment: () => success({ id: 'uploaded-original' }), deleteEntityAttachment: () => success({}), ...options.entityApi, entityHistory: (_: string, page: number) => success({ items: page === 1 ? [{ id: 'log', operation_type: 'update_entity', source: { type: 'agent', agent_name: '合成助手' }, changeset_id: 'change-7', created_at: '2026-11-07T12:00:00Z', actor: 'local', before: {}, after: {} }] : [], total: 21, page, page_size: 20 }) },
    '@/api/shelf': { getShelfNode: (_: string, id: string, asOf?: string, combination?: object) => { requests.push({ id, asOf, combination }); return success(parents[id] || options.value || data(!!combination)) }, ...options.api },
  }
  modules['./ManagedAttributes.vue'] = { __esModule: true, default: loadComponent(new URL('./ManagedAttributes.vue', import.meta.url), modules) }
  modules['./ShelfConditionsEditor.vue'] = { __esModule: true, default: loadComponent(new URL('./ShelfConditionsEditor.vue', import.meta.url), modules) }
  modules['./ShelfParametersEditor.vue'] = { __esModule: true, default: loadComponent(new URL('./ShelfParametersEditor.vue', import.meta.url), modules) }
  modules['./ShelfDocumentEditor.vue'] = { __esModule: true, default: loadComponent(new URL('./ShelfDocumentEditor.vue', import.meta.url), modules) }
  modules['./ShelfModelEditor.vue'] = { __esModule: true, default: loadComponent(new URL('./ShelfModelEditor.vue', import.meta.url), modules) }
  const component = loadComponent(new URL('./ShelfModelPage.vue', import.meta.url), modules)
  const app = renderer.createApp(component, { library: options.config || library, nodeId: 'model' })
  app.use(createI18n({ legacy: false, locale: options.locale || 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount()); await settle()
  return { root, requests, writes }
}
const section = (root: Element, id: string) => descendants(root).find(node => node.props.id === id)!

// Small regression, task 26 / ADR 0023: each region displays its own evidence,
// so a web edit cannot hide the source workbook of an untouched region.
test('可售状态按区域展示网页与导入文件来源', async t => {
  const value = data()
  value.shelf.sale_statuses.push({ region: 'south', status: 'DEV', conditions: [] })
  value.facts.push(
    { entity_id: 'model', field: 'sale_statuses', value: value.shelf.sale_statuses[0], evidence: { source: { type: 'web' }, changeset_id: 'web-change', effective_at: '2026-11-08T12:00:00Z', recorded_at: '2026-11-08T12:00:00Z' } },
    { entity_id: 'model', field: 'sale_statuses', value: value.shelf.sale_statuses[1], evidence: { source: { type: 'import', file_name: 'synthetic-map.xlsx', worksheet: 'Map', cells: ['F3'], import_batch_id: 'import-batch' }, changeset_id: 'import-change', effective_at: '2026-11-01T00:00:00Z', recorded_at: '2026-11-07T12:00:00Z' } },
  )
  for (const locale of ['zh-CN', 'en-US']) {
    const { root } = await mountPage(t, { value, locale })
    const regions = descendants(section(root, 'sale')).filter(node => node.tag === 'details')
    assert.equal(regions.length, 2)
    assert.match(visibleText(regions[0]!), locale === 'zh-CN' ? /网页.*web-change/ : /Web.*web-change/)
    assert.doesNotMatch(visibleText(regions[0]!), /synthetic-map|import-change/)
    assert.match(visibleText(regions[1]!), /synthetic-map.xlsx.*Map.*F3.*import-change/)
    assert.doesNotMatch(visibleText(regions[1]!), /web-change/)
    assert.doesNotMatch(visibleText(section(root, 'sale')), /shelfModel\.|managed\./)
  }
})

// Small spec, task 16 / ADR 0023/0024: unselected options retain conditions and provenance.
test('型号页默认显示适用条件及部件和上层认证来源', async t => {
  const { root } = await mountPage(t)
  const text = visibleText(root)
  assert.match(text, /Synthetic model/); assert.match(text, /产品线原名.*平台原名.*产品原名/)
  assert.match(text, /评审阶段.*沿用产品名称/)
  const summary = descendants(root).find(node => node.tag === 'dl' && node.props.class === 'facts')!
  assert.match(visibleText(summary), /北区 GA.*电芯 ∈ \{Alpha cell\}/)
  for (const name of ['组成', '可售状态', '认证', '资料清单', '参数', '其他文件', '操作历史']) assert.ok(text.includes(name), name)
  assert.match(visibleText(section(root, 'cert')), /整机.*整机标准.*来自部件.*部件标准.*Alpha cell.*来自上层.*系列标准.*平台原名/)
  assert.match(visibleText(section(root, 'cert')), /电芯 ∈ \{Alpha cell\}/)
  assert.match(visibleText(section(root, 'sale')), /状态备注原文.*状态原文.*电芯 ∈ \{Alpha cell\}/)
  assert.match(visibleText(section(root, 'params')), /9 ~ 54 kWh.*电芯 ∈ \{Alpha cell\}/)
})

const changeOption = async (root: Element, module: string, id: string) => {
  const radio = descendants(root).find(node => node.tag === 'input' && node.props.name === `option-${module}` && node.props.value === id)
  assert.ok(radio, `${module}: ${id}`)
  radio.props.onChange(); await settle()
}

// Small spec, task 16 / story 55: each of six alternatives queries the shared service.
test('选择六种部件后显示组合结果并隐藏条件，全部恢复条件', async t => {
  const { root, requests } = await mountPage(t)
  for (const part of parts) {
    await changeOption(root, 'cell', part.id)
    assert.deepEqual(requests.filter(row => row.id === 'model').at(-1).combination, { cell: part.id })
    assert.doesNotMatch(visibleText(section(root, 'cert')), /部件标准|系列标准|∈/)
    assert.doesNotMatch(visibleText(section(root, 'docs')), /缺少的资料|∈/)
    assert.doesNotMatch(visibleText(section(root, 'files')), /产品介绍.pdf|∈/)
    assert.doesNotMatch(visibleText(section(root, 'params')), /∈/)
  }
  await changeOption(root, 'cell', '')
  assert.match(visibleText(section(root, 'cert')), /部件标准.*电芯 ∈ \{Alpha cell\}/)
})

// Small spec, task 16: partial module selection must not pretend to be a complete combination.
test('多个选配模块选齐后才发送完整组合', async t => {
  const value = data()
  value.shelf.composition.push({ module_type: 'controller', optional: true, parts: [entity('control-a', 'part'), entity('control-b', 'part')] })
  const { root, requests } = await mountPage(t, { value })
  await changeOption(root, 'cell', 'beta')
  assert.equal(requests.filter(row => row.id === 'model').at(-1).combination, undefined)
  assert.match(visibleText(section(root, 'cert')), /∈/)
  await changeOption(root, 'controller', 'control-b')
  assert.deepEqual(requests.filter(row => row.id === 'model').at(-1).combination, { cell: 'beta', controller: 'control-b' })
  assert.doesNotMatch(visibleText(section(root, 'cert')), /∈/)
})

// Small spec, task 16 / ADR 0024: checklist states, exact source text and file provenance remain visible.
test('资料清单保留四种状态、应交原文、版本和来源，文件可打开原件', async t => {
  const { root } = await mountPage(t)
  assert.ok(descendants(section(root, 'docs')).some(node => node.props.id === 'requirement-drawing'))
  const text = visibleText(section(root, 'docs'))
  for (const value of ['缺', '只有初稿', '已交', '未到期', '原表阶段', '客户交期原文', 'Customer V2', '产品名称 产品原名', '负责人原名', '客户图纸.pdf']) assert.ok(text.includes(value), value)
  assert.ok(descendants(section(root, 'docs')).some(node => node.tag === 'a' && node.props.href === 'https://example.invalid/drawing'))
  assert.match(visibleText(section(root, 'files')), /产品介绍.pdf.*来自部件.*Alpha cell.*解析失败.*保留原件/)
})

// Small spec, task 16: history pagination keeps the submitted actor and changeset source.
test('操作历史显示具名来源与变更集并可翻页', async t => {
  const { root } = await mountPage(t)
  assert.match(visibleText(section(root, 'history')), /agent · 合成助手.*change-7/)
  await click(descendants(section(root, 'history')).find(node => node.tag === 'button' && visibleText(node) === '下一页')!)
  assert.doesNotMatch(visibleText(section(root, 'history')), /合成助手/)
})

// Small spec, task 16: a newer option read owns the displayed result.
test('快速切换后迟到的组合响应不能覆盖当前组合', async t => {
  let finishOld: (value: unknown) => void = () => {}
  const { root } = await mountPage(t, { api: { getShelfNode: (_: string, id: string, _at?: string, combination?: Record<string, string>) => {
    if (id === 'model' && combination?.cell === 'alpha') return new Promise(resolve => { finishOld = resolve })
    const result = data(!!combination)
    result.facts = result.facts.filter((fact: any) => fact.field !== 'belongs_to')
    result.shelf.certifications = [{ ...certs[0], standard_name: combination ? '当前组合认证' : '默认认证' }]
    return success(result)
  } } })
  await changeOption(root, 'cell', 'alpha')
  assert.match(visibleText(root), /加载货架/)
  await changeOption(root, 'cell', 'beta')
  const old = data(true); old.shelf.certifications = [{ ...certs[0], standard_name: '旧组合认证' }]
  finishOld({ success: true, data: old }); await settle()
  assert.match(visibleText(section(root, 'cert')), /当前组合认证/)
  assert.doesNotMatch(visibleText(root), /旧组合认证/)
})

// Small spec, task 16: unavailable reads recover through the visible retry control.
test('型号读取失败后可重试，未记录与空资料显示明确状态', async t => {
  let fail = true
  const { root } = await mountPage(t, { api: { getShelfNode: () => {
    if (fail) return Promise.reject(new Error('合成读取失败'))
    return success({ ...data(), facts: [], recorded: false })
  } } })
  assert.match(visibleText(root), /合成读取失败/)
  fail = false
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === '重试')!)
  assert.match(visibleText(root), /货架中没有记录/)
  assert.doesNotMatch(visibleText(root), /整机标准/)
})

// Small spec, task 16: owned labels follow the locale, submitted names retain their spelling.
test('英文界面保留客户名称并翻译栏目与清单状态', async t => {
  const { root } = await mountPage(t, { locale: 'en-US' })
  assert.match(visibleText(root), /Model details|Sale status/)
  assert.match(visibleText(section(root, 'docs')), /Delivered.*Draft only|Draft only.*Delivered/)
  assert.match(visibleText(root), /产品原名/)
  assert.doesNotMatch(visibleText(root), /shelfModel\./)
})

// Small spec, task 17: a matrix deep link scrolls to its requirement once;
// later option changes keep the user's reading position.
test('资料行链接首次读取后定位，切换选配不重复滚动', async t => {
  const previousDocument = globalThis.document, previousWindow = globalThis.window
  const targets: string[] = []
  globalThis.document = { getElementById: (id: string) => ({ scrollIntoView: () => targets.push(id) }) } as unknown as Document
  globalThis.window = { location: { hash: '#requirement-drawing' } } as unknown as Window & typeof globalThis
  t.after(() => { globalThis.document = previousDocument; globalThis.window = previousWindow })
  const { root } = await mountPage(t)
  assert.deepEqual(targets, ['requirement-drawing'])
  await changeOption(root, 'cell', 'beta')
  assert.deepEqual(targets, ['requirement-drawing'])
})

// Small spec, task 24 / story 36: all edits start on the model page.
test('型号页提供编辑、挂资料、认证、阶段和组成五个入口', async t => {
  const { root } = await mountPage(t)
  for (const label of ['编辑型号', '挂资料文件', '新增认证', '单独设置', '指定部件']) {
    assert.ok(descendants(root).some(node => node.tag === 'button' && visibleText(node) === label), label)
  }
})

const button = (root: Element, label: string) => {
  const node = descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)
  assert.ok(node, label); return node
}
const control = (root: Element, id: string) => {
  const node = descendants(root).find(node => node.props.id === id)
  assert.ok(node, id); return node
}
const input = async (root: Element, id: string, value: string) => { control(root, id).props.onInput({ target: { value } }); await settle() }
const select = async (root: Element, id: string, value: string) => { control(root, id).props.onChange({ target: { value } }); await settle() }
const submit = async (root: Element) => { const form = descendants(root).find(node => node.tag === 'form'); assert.ok(form); form.props.onSubmit({ preventDefault() {} }); await settle() }

// Small spec, task 24: an atomic edit preserves spelling and unrelated facts.
test('编辑型号确认一项变更并刷新名称和属性', async t => {
  let saved = false
  const value = data(); value.entity.attributes = { custom: '保留', current_stage: 'review' }
  const { root, writes } = await mountPage(t, { value, config: { ...library, entity_schema: { ...library.entity_schema, fields: [{ key: 'custom', name: '自定属性', type: 'text', kinds: ['model'] }] } }, filesApi: { writeShelf: (_: string, ops: any[]) => { writes.push(ops); saved = true; return success({ status: 'confirmed' }) } }, api: { getShelfNode: () => success({ ...value, facts: [], entity: saved ? { ...value.entity, canonical_name: ' 新型号 ', description: ' 新描述 ', code: ' CODE ', aliases: [' 别名一 ', '别名二'], attributes: { custom: '新属性值', current_stage: 'review' } } : value.entity }) } })
  await click(button(root, '编辑型号'))
  await input(root, 'model-name', ' 新型号 '); await input(root, 'model-description', ' 新描述 ')
  await input(root, 'model-code', ' CODE '); await input(root, 'model-aliases', ' 别名一 \n别名二')
  const dialog = descendants(root).find(node => node.props.role === 'dialog')!
  const label = descendants(dialog).find(node => node.tag === 'label' && visibleText(node).includes('自定属性'))!
  await input(root, label.props.for, '新属性值'); await submit(root)
  assert.equal(writes[0].length, 1); assert.equal(writes[0][0].type, 'update_entity')
  assert.deepEqual(writes[0][0].patch, { revision: 1, name: ' 新型号 ', description: ' 新描述 ', code: ' CODE ', aliases: [' 别名一 ', '别名二'], attributes: { custom: '新属性值', current_stage: 'review' } })
  assert.match(visibleText(root), /新型号.*新描述/); assert.match(visibleText(root), /CODE.*别名一.*别名二/); assert.match(visibleText(section(root, 'params')), /自定属性.*新属性值/)
  assert.ok(!descendants(root).some(node => node.props.role === 'dialog'))
})

// Small spec, task 24 / ADR 0024: a missing model stage restores product inheritance.
test('型号阶段可覆盖并恢复沿用产品名称，完整快照保留其他属性', async t => {
  let current = 'review'
  const value = data(); value.entity.attributes = { current_stage: 'review', custom: '保留' }
  const { root, writes } = await mountPage(t, { api: { getShelfNode: () => success({ ...value, facts: [], entity: { ...value.entity, attributes: { ...value.entity.attributes, current_stage: current } }, shelf: { ...value.shelf, current_stage: current || 'review', stage_owner_id: current ? 'model' : 'product' } }) }, filesApi: { writeShelf: (_: string, ops: any[]) => { writes.push(ops); current = ops[0].patch.attributes.current_stage || ''; return success({ status: 'confirmed' }) } } })
  await click(button(root, '单独设置')); await select(root, 'model-stage', ''); await submit(root)
  assert.equal(writes[0].length, 1); assert.deepEqual(writes[0][0].patch.attributes, { custom: '保留' })
  assert.match(visibleText(root), /评审阶段.*沿用产品名称/)
  await click(button(root, '单独设置')); await select(root, 'model-stage', 'review'); await submit(root)
  assert.deepEqual(writes[1][0].patch.attributes, { current_stage: 'review', custom: '保留' })
  const facts = descendants(root).find(node => node.tag === 'dl' && node.props.class === 'facts')!
  assert.doesNotMatch(visibleText(facts), /沿用产品名称/)
})

// Small spec, task 24 / ADR 0023: each add or removal confirms one relationship operation.
test('按模块添加选配部件并按关系修订结束组成', async t => {
  let count = 1
  const { root, writes } = await mountPage(t, { api: { getShelfNode: () => { const result = data(); result.facts = []; result.shelf.composition = [{ module_type: 'cell', optional: count > 1, parts: count > 1 ? parts.slice(0, 2) : [parts[0]] }]; return success(result) } }, filesApi: { writeShelf: (_: string, ops: any[]) => { writes.push(ops); count = ops[0].type === 'save_relation' ? 2 : 1; return success({ status: 'confirmed' }) } } })
  await click(button(root, '指定部件'))
  await select(root, 'composition-module', 'cell'); await select(root, 'composition-part', 'beta'); await submit(root)
  assert.deepEqual(writes[0], [{ id: 'composition', type: 'save_relation', relation: { subject_id: 'model', object_id: 'beta', relation_type: 'composed_of', attributes: { module_type: 'cell' } } }])
  assert.match(visibleText(section(root, 'composition')), /选配 · 2/); assert.ok(!descendants(root).some(node => node.props.role === 'dialog'))
  await click(button(root, '指定部件')); await select(root, 'composition-action', 'remove')
  await select(root, 'composition-module', 'cell'); await select(root, 'composition-part', 'composition-alpha'); await submit(root)
  assert.deepEqual(writes[1], [{ id: 'composition', type: 'end_relation', relation_id: 'composition-alpha', revision: 4 }])
})

// Small spec, task 24 / ADR 0023: certification fields and applicability remain explicit.
test('新增认证保留原文、年份、电芯部件和适用条件，成功后刷新', async t => {
  let saved = false
  const config = { ...library, shelf_config: { ...library.shelf_config, standards: [{ key: 'standard', name: '合成标准', aliases: [] }] } }
  const { root, writes } = await mountPage(t, { config, filesApi: { writeShelf: (_: string, ops: any[]) => { writes.push(ops); saved = true; return success({ status: 'confirmed' }) } }, api: { getShelfNode: () => { const result = data(); result.facts = []; if (saved) result.shelf.certifications.push({ ...certs[0], entity: entity('new-cert', 'certification'), standard_name: '新增合成标准' }); return success(result) } } })
  await click(button(root, '新增认证'))
  for (const [id, value] of [['cert-name', ' 认证原名 '], ['cert-object', ' 整机原文 '], ['cert-region', ' 地区原文 '], ['cert-year', '2026'], ['cert-number', 'SYN-24']]) await input(root, id, value)
  await select(root, 'cert-standard', 'standard'); await select(root, 'cert-cell', 'alpha')
  await click(button(root, '添加条件')); await select(root, 'condition-module-0', 'cell')
  const checkbox = descendants(root).find(n => n.tag === 'input' && n.props.type === 'checkbox' && n.props['aria-label'] === 'Alpha cell')!
  assert.ok(checkbox); checkbox.props.onChange({ target: { checked: true } }); await settle(); await submit(root)
  assert.equal(writes[0].length, 1); assert.equal(writes[0][0].type, 'create_certification')
  assert.deepEqual({ ...writes[0][0].certification }, { name: ' 认证原名 ', owner_id: 'model', standard: 'standard', certification_object: ' 整机原文 ', region: ' 地区原文 ', year: 2026, cell_part_id: 'alpha', number: 'SYN-24', conditions: condition })
  assert.match(visibleText(section(root, 'cert')), /新增合成标准/)
})

// Small regression, task 24: editing a filtered read must not drop facts for other options.
test('选配视图编辑时读取完整属性并能修改带条件的参数区间', async t => {
  let last: any
  const reads: any[] = []
  const full = data(); full.entity.attributes = { custom: '保留', energy: [{ value: { min: 9, max: 54 }, conditions: condition }, { value: 60, conditions: [{ module_type: 'cell', part_ids: ['beta'] }] }] }
  const { root, writes } = await mountPage(t, { api: { getShelfNode: (_: string, id: string, _at?: string, combination?: any) => { reads.push({ combination }); if (id !== 'model') return success({ entity: entity(id, id === 'product' ? 'product' : id === 'platform' ? 'platform' : 'product_line'), facts: id === 'line' ? [] : [{ field: 'belongs_to', value: { object_id: id === 'product' ? 'platform' : 'line' } }] }); const result = structuredClone(full); if (combination) result.entity.attributes.energy = [result.entity.attributes.energy[0]]; return success(result) } }, filesApi: { writeShelf: (_: string, ops: any[]) => { writes.push(ops); last = ops[0].patch; return success({ status: 'confirmed' }) } } })
  await changeOption(root, 'cell', 'alpha'); await click(button(root, '编辑型号'))
  await input(root, 'parameter-energy-0-min', '10'); await input(root, 'parameter-energy-0-max', '50'); await submit(root)
  assert.ok(reads.some(request => request.combination?.cell === 'alpha'))
  assert.deepEqual(last.attributes, { custom: '保留', energy: [{ value: { min: 10, max: 50 }, conditions: condition }, { value: 60, conditions: [{ module_type: 'cell', part_ids: ['beta'] }] }] })
})

// Small spec, task 24 / ADR 0024: uploaded originals receive explicit classification.
test('型号页上传文件并指定资料要求、稿和条件', async t => {
  let saved = false
  const { root, writes } = await mountPage(t, { filesApi: { writeShelf: (_: string, ops: any[]) => { writes.push(ops); saved = true; return success({ status: 'confirmed' }) } }, api: { getShelfNode: () => { const result = data(); result.facts = []; if (saved) result.shelf.other_documents.push({ ...result.shelf.other_documents[0], entity: entity('new-file', 'document_file', ' 合成图纸.pdf ') }); return success(result) } } })
  await click(button(root, '挂资料文件'))
  await input(root, 'shelf-file-name', ' 合成图纸.pdf ')
  const original = control(root, 'shelf-file-original')
  original.props.onChange({ target: { files: [new File(['synthetic'], ' 合成图纸.pdf ', { type: 'application/pdf' })], value: '' } }); await settle()
  const requirement = descendants(root).find(n => n.tag === 'input' && n.props.type === 'checkbox' && visibleText(n.parent!).includes('图纸'))!
  assert.ok(requirement); requirement.props.onChange({ target: { checked: true } }); await settle()
  await select(root, 'shelf-file-stage', 'final')
  await click(button(root, '添加条件')); await select(root, 'condition-module-0', 'cell')
  const applicable = descendants(root).find(node => node.tag === 'input' && node.props.type === 'checkbox' && node.props['aria-label'] === 'Alpha cell')!
  applicable.props.onChange({ target: { checked: true } }); await settle(); await submit(root)
  assert.equal(writes[0].length, 1); assert.equal(writes[0][0].type, 'add_document_version')
  assert.equal(writes[0][0].document.owner_id, 'model'); assert.deepEqual(writes[0][0].document.requirement_ids, ['drawing'])
  assert.equal(writes[0][0].document.stage, 'final'); assert.equal(writes[0][0].document.attachment_id, 'uploaded-original')
  assert.match(visibleText(section(root, 'files')), /合成图纸.pdf/)
  assert.equal(writes[0][0].document.name, ' 合成图纸.pdf '); assert.deepEqual(writes[0][0].document.conditions, condition)
  assert.ok(!descendants(root).some(n => n.props.role === 'dialog'))
})

// Small spec, task 24 / ADR 0024: an existing original is attached without duplication.
test('选择已有文件确认改归属，保留原版本的稿和原件', async t => {
  const { root, writes } = await mountPage(t)
  await click(button(root, '挂资料文件')); await select(root, 'shelf-file-source', 'existing')
  await select(root, 'shelf-existing-file', 'existing-file')
  assert.match(visibleText(root), /终稿/)
  await submit(root)
  assert.deepEqual(writes[0], [{ id: 'document', type: 'classify_document', entity_id: 'existing-file', revision: 1, classification: { owner_id: 'model', requirement_ids: [], certificate_number: '', conditions: [] } }])
})

// Small regression, task 24: server conflicts never discard a local draft or retry against a new revision.
for (const reason of ['名称已被占用', '实体修订过期，请重新读取']) {
  for (const mode of ['model', 'stage', 'composition', 'certification', 'document']) {
    test(`${mode}保存遇到${reason}时显示服务器原因并保留输入`, async t => {
      const submitted: any[] = []
      const { root } = await mountPage(t, { filesApi: { writeShelf: (_: string, ops: any[]) => {
        submitted.push(ops)
        return Promise.reject({ message: '变更集确认失败', error: { details: { items: [{ conflicts: [{ type: reason.includes('修订') ? 'revision' : 'name', message: reason }] }] } } })
      } } })
      const labels: Record<string, string> = { model: '编辑型号', stage: '单独设置', composition: '指定部件', certification: '新增认证', document: '挂资料文件' }
      await click(button(root, labels[mode]!))
      let id = '', value = ''
      if (mode === 'model') { id = 'model-name'; value = ' 未提交的新名 '; await input(root, id, value) }
      if (mode === 'stage') { id = 'model-stage'; value = 'review'; await select(root, id, value) }
      if (mode === 'composition') { await select(root, 'composition-module', 'cell'); id = 'composition-part'; value = 'beta'; await select(root, id, value) }
      if (mode === 'certification') {
        for (const [field, text] of [['cert-name', ' 独立认证 '], ['cert-object', '整机'], ['cert-region', '自定地区']]) await input(root, field, text)
        await select(root, 'cert-standard', 'configured-standard'); id = 'cert-name'; value = ' 独立认证 '
      }
      if (mode === 'document') { id = 'shelf-file-name'; value = ' 保留的文件 '; await input(root, id, value); await input(root, 'shelf-file-cloud', 'https://example.invalid/synthetic') }
      await submit(root)
      assert.equal(submitted.length, 1); assert.equal(submitted[0].length, 1)
      assert.ok(descendants(root).some(node => node.props.role === 'alert' && visibleText(node).includes(reason)))
      assert.equal(control(root, id).props.value, value)
      assert.ok(descendants(root).some(node => node.props.role === 'dialog'))
      await submit(root)
      assert.deepEqual(submitted[1], submitted[0])
    })
  }
}

// Small spec, task 24 / model mockup: a checklist gap can attach its requirement directly.
test('资料缺口的挂文件入口预选该项要求', async t => {
  const { root } = await mountPage(t)
  const row = control(root, 'requirement-missing')
  await click(button(row, '挂资料文件'))
  const requirement = descendants(root).find(node => node.tag === 'input' && node.props.type === 'checkbox' && visibleText(node.parent!).includes('缺少的资料'))
  assert.ok(requirement); assert.equal(requirement.props.checked, true)
})

// Small regression, task 24: switching after a rejected upload releases the unused original.
test('上传确认失败后改挂已有文件会清理未被引用的暂存附件', async t => {
  const removed: string[] = []; let attempts = 0
  const { root } = await mountPage(t, { entityApi: { deleteEntityAttachment: (id: string) => { removed.push(id); return success({}) } }, filesApi: { writeShelf: () => ++attempts === 1 ? Promise.reject(new Error('合成冲突')) : success({ status: 'confirmed' }) } })
  await click(button(root, '挂资料文件')); await input(root, 'shelf-file-name', '合成原件.pdf')
  control(root, 'shelf-file-original').props.onChange({ target: { files: [new File(['synthetic'], '合成原件.pdf')], value: '' } }); await settle(); await submit(root)
  assert.match(visibleText(root), /合成冲突/)
  await select(root, 'shelf-file-source', 'existing'); await select(root, 'shelf-existing-file', 'existing-file'); await submit(root)
  assert.deepEqual(removed, ['uploaded-original'])
})
