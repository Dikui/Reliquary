import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import * as shelfText from '../../../utils/productShelf'
import * as externalUrl from '../../../utils/externalUrl'
import * as dates from '../../../utils/dateTime'
import { click, descendants, element, loadComponent, renderer, settle, visibleText, type Element } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'

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
async function mountPage(t: test.TestContext, options: { api?: Record<string, unknown>; locale?: string; value?: any; config?: any } = {}) {
  const requests: any[] = []
  const parents: Record<string, any> = { product: { entity: entity('product', 'product', '产品原名'), facts: [{ field: 'belongs_to', value: { object_id: 'platform' } }] }, platform: { entity: entity('platform', 'platform', '平台原名'), facts: [{ field: 'belongs_to', value: { object_id: 'line' } }] }, line: { entity: entity('line', 'product_line', '产品线原名'), facts: [] } }
  // Synthetic API responses replace HTTP only; the real page and shared controls render.
  const component = loadComponent(new URL('./ShelfModelPage.vue', import.meta.url), {
    ...entryPresentationTestModules, '@/utils/externalUrl': externalUrl, '@/utils/productShelf': shelfText, '@/utils/dateTime': dates,
    '@/api/operation-logs': { OPERATION_TYPES: ['update_entity'] },
    '@/api/entities': { entityHistory: (_: string, page: number) => success({ items: page === 1 ? [{ id: 'log', operation_type: 'update_entity', source: { type: 'agent', agent_name: '合成助手' }, changeset_id: 'change-7', created_at: '2026-11-07T12:00:00Z', actor: 'local', before: {}, after: {} }] : [], total: 21, page, page_size: 20 }) },
    '@/api/shelf': { getShelfNode: (_: string, id: string, asOf?: string, combination?: object) => { requests.push({ id, asOf, combination }); return success(parents[id] || options.value || data(!!combination)) }, ...options.api },
  })
  const app = renderer.createApp(component, { library: options.config || library, nodeId: 'model' })
  app.use(createI18n({ legacy: false, locale: options.locale || 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount()); await settle()
  return { root, requests }
}
const section = (root: Element, id: string) => descendants(root).find(node => node.props.id === id)!

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
