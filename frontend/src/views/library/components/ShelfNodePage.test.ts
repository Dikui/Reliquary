import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import * as shelfText from '../../../utils/productShelf'
import * as externalUrl from '../../../utils/externalUrl'
import { click, descendants, element, loadComponent, renderer, settle, visibleText, type Element } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'

const entity = (id: string, kind: string, name = id): any => ({ id, kind, canonical_name: name, library_id: 'library-a', revision: 3, entity_type: 'product', attributes: {}, aliases: [], space_id: 1, created_at: '', updated_at: '' })
const library: any = { id: 'library-a', name: 'Synthetic shelf', shelf_enabled: true, shelf_config: { module_types: [{ key: 'module', name: '客户模块' }], regions: [{ key: 'north', name: '客户区域' }], stages: [{ key: 'early', name: '客户初期' }, { key: 'late', name: '客户后期' }] }, entity_schema: { kinds: [], fields: [{ key: 'pack_model', name: 'Pack 型号', type: 'text', kinds: ['part'] }] } }
const parent = entity('platform', 'platform', '平台原名')
const product = { ...entity('product', 'product', '产品原名'), attributes: { current_stage: 'early', custom: '保留值' } }
const part = { ...entity('part', 'part', '部件原名'), aliases: [' 别名原文 '], attributes: { module_type: 'module', pack_model: 'Pack 原名' } }
const req = entity('drawing', 'document_requirement', '客户图纸')
const conditions = [{ module_type: 'module', part_ids: ['part'] }]
const model = (id = 'model'): any => ({ entity: entity(id, 'model', `型号 ${id}`), recorded: true, as_of: '2026-11-07T12:00:00Z', facts: [{ field: 'belongs_to', value: { object_id: 'product' } }], partial_options: true, shelf: { current_stage: 'early', stage_owner_id: 'product', composition: [{ module_type: 'module', optional: true, parts: [part, entity('alternative', 'part', '另一个部件')] }], sale_statuses: [{ region: 'north', status: 'GA', planned_eom: true, notes: '原始备注', conditions }], checklist: [{ requirement: req, status: 'delivered', conditions }, { requirement: req, status: 'missing', conditions: [{ module_type: 'module', part_ids: ['alternative'] }] }] } })
const node = (isPart = false): any => ({ entity: isPart ? part : product, recorded: true, as_of: '2026-11-07T12:00:00Z', facts: isPart ? [] : [{ field: 'belongs_to', value: { object_id: 'platform' } }], shelf: { composition: [], sale_statuses: [], checklist: [], checklist_summary: { model_count: 1, models_with_gaps: 1, counts: { missing: 1 } }, certifications: [{ entity: entity('cert', 'certification'), standard_name: '标准原文', origin: isPart ? 'self' : 'ancestor', certification: { owner_id: isPart ? 'part' : 'platform', region: '认证地区', progress: '客户进度', agency: '客户机构', issued_date: '原表日期', validity: '长期原文', number: 'TEST-17', cloud_url: 'https://example.invalid/cert', conditions } }], documents: [{ entity: entity('file', 'document_file', '客户文件.pdf'), requirement: req, document: { id: 'version', file_id: 'file', version_label: ' 客户 V3 ', stage: 'draft', cloud_url: 'https://example.invalid/file', parse_status: 'not_parsed', conditions } }] } })
const success = (data: unknown) => Promise.resolve({ success: true, data })
async function mountPage(t: test.TestContext, options: { part?: boolean; locale?: string; api?: Record<string, unknown> } = {}) {
  const requests: any[] = [], submissions: any[] = []
  // API responses replace HTTP; the actual page, semantic tables and controls render.
  const component = loadComponent(new URL('./ShelfNodePage.vue', import.meta.url), {
    ...entryPresentationTestModules, '@/utils/productShelf': shelfText, '@/utils/externalUrl': externalUrl,
    '@/api/shelf': {
      getShelfNode: (_: string, id: string, asOf?: string) => { requests.push({ id, asOf }); return success(id === 'platform' ? { entity: parent, facts: [{ field: 'belongs_to', value: { object_id: 'line' } }] } : id === 'line' ? { entity: entity('line', 'product_line', '产品线原名'), facts: [] } : id === 'product' && options.part ? node() : node(!!options.part)) },
      queryShelf: (_: string, input: object) => {
        requests.push(input)
        const fixed = model('fixed'); fixed.shelf.composition[0] = { module_type: 'module', optional: false, parts: [part] }
        return success({ items: options.part ? [model(), fixed] : [model()], total: options.part ? 2 : 1, truncated: false })
      },
      submitShelfStage: (...args: any[]) => { submissions.push(args); return success({ status: 'confirmed' }) }, ...options.api,
    },
  })
  const selectedID = ref(options.part ? 'part' : 'product')
  const app = renderer.createApp(defineComponent({ setup: () => () => h(component, { library, nodeId: selectedID.value }) }))
  app.use(createI18n({ legacy: false, locale: options.locale || 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount()); await new Promise<void>(resolve => setImmediate(resolve)); await settle()
  return { root, requests, submissions, selectNode: async (id: string) => { selectedID.value = id; await settle() } }
}
const section = (root: Element, id: string) => descendants(root).find(item => item.props.id === id)!

// Small spec, task 17 / stories 23 and 56: matrix cells retain different optional states.
test('上层节点显示下属型号和选配清单矩阵，单元格链接到对应资料', async t => {
  const { root, requests } = await mountPage(t)
  assert.match(visibleText(root), /平台原名.*产品原名/)
  assert.match(visibleText(section(root, 'models')), /型号 model.*客户区域.*GA.*计划退市.*原始备注.*客户初期/)
  assert.match(visibleText(section(root, 'checklist-summary')), /客户图纸.*已交.*客户模块.*部件原名.*缺.*另一个部件/)
  assert.ok(descendants(section(root, 'checklist-summary')).some(item => item.tag === 'a' && String(item.props.href).endsWith('#requirement-drawing')))
  assert.ok(requests.some(item => item.ancestor_id === 'product' && item.as_of === '2026-11-07T12:00:00Z'))
})

// Small spec, task 17: parts show reverse usage, fixed/optional roles and exact attributes.
test('部件页显示使用型号和组成角色，保留别名及附属属性', async t => {
  const { root, requests } = await mountPage(t, { part: true })
  assert.match(visibleText(root), /部件原名.*别名原文.*Pack 原名/)
  assert.match(visibleText(section(root, 'models')), /型号 model.*产品线原名.*客户模块.*选配.*另一个部件.*型号 fixed.*固定组成/)
  assert.ok(requests.some(item => item.part_id === 'part'))
  assert.doesNotMatch(visibleText(root), /当前阶段|可售状态|资料缺口/)
  assert.match(visibleText(section(root, 'node-certifications')), /认证地区.*标准原文.*客户进度.*客户机构.*原表日期.*长期原文.*TEST-17/)
})

// Small spec, task 17 / ADR 0024: shared files keep requirements, versions and safe original links.
test('本层文件保留资料要求和版本，认证保留上层来源与条件', async t => {
  const { root } = await mountPage(t)
  assert.match(visibleText(section(root, 'node-files')), /客户文件.pdf.*客户图纸.*客户模块.*部件原名.*初稿.*客户 V3/)
  assert.ok(descendants(section(root, 'node-files')).some(item => item.props.href === 'https://example.invalid/file'))
  assert.match(visibleText(section(root, 'node-certifications')), /来自上层.*标准原文.*平台原名.*客户模块.*部件原名/)
})

// Small spec, task 17: changing a product stage refreshes inherited model information.
test('产品名称阶段修改确认后刷新下属型号，失败保留原阶段并显示原因', async t => {
  let saved = false, fail = false
  const { root } = await mountPage(t, { api: {
    getShelfNode: (_: string, id: string) => {
      const result = node(); if (saved) result.entity = { ...product, attributes: { ...product.attributes, current_stage: 'late' } }
      return success(id === 'platform' ? { entity: parent, facts: [] } : result)
    },
    queryShelf: () => { const item = model(); item.shelf.current_stage = saved ? 'late' : 'early'; return success({ items: [item], total: 1 }) },
    submitShelfStage: () => { if (fail) return Promise.reject(new Error('修订冲突')); saved = true; return success({ status: 'confirmed' }) },
  } })
  const select = () => descendants(root).find(item => item.tag === 'select')!
  select().props.onChange({ target: { value: 'late' } }); await settle()
  assert.match(visibleText(section(root, 'models')), /客户后期/)
  fail = true
  select().props.onChange({ target: { value: 'early' } }); await settle()
  assert.match(visibleText(root), /修订冲突/)
  assert.equal(select().props.value, 'late')
  assert.match(visibleText(section(root, 'models')), /客户后期/)
})

// Small spec, task 17: summaries must never silently stop at the first service page.
test('上层节点加载全部型号且每页使用同一业务时间', async t => {
  const pageRequests: any[] = []
  const { root } = await mountPage(t, { api: { queryShelf: (_: string, query: any) => {
    pageRequests.push(query)
    return success({ items: query.offset ? [model('last')] : Array.from({ length: 100 }, (_, index) => model(String(index))), total: 101, truncated: true })
  } } })
  assert.match(visibleText(section(root, 'models')), /型号 last/)
  assert.ok(pageRequests.some(item => item.offset === 100 && item.as_of === '2026-11-07T12:00:00Z'))
})

// Small spec, task 17: failed reads are recoverable; unknown history does not imply missing documents.
test('读取失败可以重试，没有快照时明确显示无记录', async t => {
  let fail = true
  const { root } = await mountPage(t, { api: { getShelfNode: () => fail ? Promise.reject(new Error('合成读取失败')) : success({ ...node(), recorded: false, facts: [] }) } })
  assert.match(visibleText(root), /合成读取失败/)
  fail = false
  await click(descendants(root).find(item => item.tag === 'button' && visibleText(item) === '重试')!)
  assert.match(visibleText(root), /货架中没有记录/)
  assert.doesNotMatch(visibleText(root), /缺 1 项/)
})

// Small spec, task 17: labels translate, while customer names retain their spelling.
test('英文节点页翻译栏目并保留客户原名', async t => {
  const { root } = await mountPage(t, { locale: 'en-US' })
  assert.match(visibleText(root), /Descendant models/)
  assert.match(visibleText(root), /Document checklist summary/)
  assert.match(visibleText(root), /产品原名/)
  assert.doesNotMatch(visibleText(root), /shelfNode\.|shelfModel\./)
})

// Small spec, task 30: even an old response cannot display part requirements.
test('部件页不再显示资料清单', async t => {
  const value = node(true)
  value.shelf.checklist = [{ requirement: { ...req, attributes: { requirement: { draft_due_stage: 'early', final_due_stage: 'late', due_text: '客户交期原文' } } }, status: 'no_file', conditions: [] }]
  const { root } = await mountPage(t, { part: true, api: { getShelfNode: () => success(value) } })
  assert.equal(section(root, 'part-checklist'), undefined)
  assert.ok(section(root, 'node-files'))
})

// Small spec, task 17: new node selection invalidates pending reads at the page boundary.
test('切换节点后迟到的旧响应不能覆盖当前节点', async t => {
  let finishOld: (value: unknown) => void = () => {}
  const { root, selectNode } = await mountPage(t, { api: {
    getShelfNode: (_: string, id: string) => id === 'product' ? new Promise(resolve => { finishOld = resolve }) : success({ ...node(), entity: entity('new-product', 'product', '当前产品'), facts: [] }),
    queryShelf: () => success({ items: [], total: 0 }),
  } })
  await selectNode('new-product')
  finishOld({ success: true, data: node() }); await settle()
  assert.match(visibleText(root), /当前产品/)
  assert.doesNotMatch(visibleText(root), /产品原名/)
})

// Small spec, task 17: incomplete server pages cannot masquerade as a complete summary.
test('分页未返回剩余型号时显示可重试错误', async t => {
  const { root } = await mountPage(t, { api: { queryShelf: () => success({ items: [], total: 2, truncated: true }) } })
  assert.match(visibleText(root), /查询结果不完整/)
  assert.ok(descendants(root).some(item => item.tag === 'button' && visibleText(item) === '重试'))
  assert.equal(section(root, 'checklist-summary'), undefined)
})

// Small spec, task 17: the top gap total counts requirements, not repeated model cells.
test('资料缺口按不同要求计数并保留受影响型号数量', async t => {
  const value = node(); value.shelf.checklist_summary = { counts: { missing: 2 }, models_with_gaps: 2 }
  const { root } = await mountPage(t, { api: {
    getShelfNode: (_: string, id: string) => success(id === 'platform' ? { entity: parent, facts: [] } : value),
    queryShelf: () => success({ items: [model('one'), model('two')], total: 2 }),
  } })
  assert.match(visibleText(root), /缺 1 项（2 个型号）/)
})

// Small spec, task 30: certifications follow usage and precede attached files.
test('部件认证在使用型号之后、资料文件之前展示', async t => {
  const { root } = await mountPage(t, { part: true })
  const sections = descendants(root).filter(item => item.tag === 'section').map(item => item.props.id)
  assert.ok(sections.indexOf('models') < sections.indexOf('node-certifications'))
  assert.ok(sections.indexOf('node-certifications') < sections.indexOf('node-files'))
})
