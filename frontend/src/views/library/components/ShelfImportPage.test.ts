import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import { reactive } from 'vue'
import zh from '../../../i18n/locales/zh-CN'
import { click, descendants, element, loadComponent, renderer, settle, visibleText, type Element } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'
import { toast, useFeedback } from '../../../components/ui/feedback'
import * as dateTime from '../../../utils/dateTime'

const plan = () => ({ key: 'map', name: '合成产品地图', file_kind: 'product_map', revision: 3, definition: { sheets: [{ worksheet: '合成表', product_line_id: 'line', header_rows: 1, columns: [{ column: 'A', field: 'platform' }, { column: 'B', field: 'product' }, { column: 'C', field: 'model' }] }] } })
const find = (root: Element, tag: string, text: string) => descendants(root).find(node => node.tag === tag && visibleText(node) === text)!
const field = (root: Element, label: string) => descendants(root).find(node => node.props['aria-label'] === label)!
async function input(root: Element, label: string, value: string) { const node = field(root, label); assert.ok(node, label); (node.props.onInput || node.props.onChange)({ target: { value } }); await settle() }
async function mount(t: test.TestContext, overrides: Record<string, unknown> = {}) {
  const uploads: any[] = [], writes: any[] = [], suggestions: any[] = []
  const route = reactive({ query: { libraries: 'library-a', tab: 'import' } as Record<string, string> })
  const modules: Record<string, unknown> = {
    ...entryPresentationTestModules, '@/utils/dateTime': dateTime,
    '@/api/shelf-import': {
      IMPORT_KINDS: ['product_map', 'certification_progress', 'certification_report', 'overseas_sales', 'atq'],
      listShelfImportPlans: async () => ({ success: true, data: [plan()] }),
      listShelfImportBatches: async () => ({ success: true, data: { items: [], total: 0 } }),
      saveShelfImportPlan: async (_: string, value: any) => { writes.push(JSON.parse(JSON.stringify(value))); return { success: true, data: { id: 'plan-set', status: 'confirmed' } } },
      uploadShelfImport: async (_: string, file: File, key: string, date: string) => { uploads.push({ file, key, date }); return { success: true, data: { id: 'batch-a', status: 'pending' } } },
      suggestShelfImportPlan: async (...args: any[]) => { suggestions.push(args); return { success: true, data: { id: 'suggestion', revision: 1, items: [{ id: 'suggested_plan', operation: { id: 'suggested_plan', type: 'save_import_plan', import_plan: { ...plan().definition, key: 'new_map', name: '模型方案', file_kind: 'product_map', revision: 0 }, issues: [{ type: 'uncertain', message: '请检查列含义' }] }, conflicts: [{ type: 'uncertain', message: '请检查列含义' }] }] } } },
      ...overrides,
    },
    '@/api/entities': { listEntities: async () => ({ success: true, data: [{ id: 'line', canonical_name: '合成产品线', kind: 'product_line' }] }) },
    '@/api/shelf': { SALE_STATUSES: ['GA', 'DEV', 'Planning', 'EOM', '待定'] },
    '@/api/shelf-review': { confirmShelfChangeset: async (_: string, _id: string, value: any) => { writes.push(JSON.parse(JSON.stringify(value))); return { success: true, data: { status: 'confirmed' } } } },
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace: async (to: any) => { route.query = to.query } }), onBeforeRouteLeave() {}, onBeforeRouteUpdate() {} },
  }
  modules['./ShelfImportPlanEditor.vue'] = { __esModule: true, default: loadComponent(new URL('./ShelfImportPlanEditor.vue', import.meta.url), modules) }
  const component = loadComponent(new URL('./ShelfImportPage.vue', import.meta.url), modules)
  const app = renderer.createApp(component, { library: { id: 'library-a', shelf_config: { regions: [{ key: 'north', name: '合成区域' }], markets: [{ key: 'overseas', name: '合成市场' }] }, entity_schema: { fields: [] } } })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } }))
  const root = element('root'); app.mount(root)
  t.after(() => { app.unmount(); useFeedback().confirmations.forEach(dialog => dialog.cancel()); toast.clear() }); await settle()
  return { root, route, uploads, writes, suggestions }
}
async function chooseFile(root: Element, name = 'map_20261028.xlsx') {
  const file = new File(['synthetic workbook'], name)
  field(root, '选择 Excel 文件').props.onChange({ target: { files: [file], value: name } }); await settle(); return file
}
async function savePlan(root: Element) { descendants(root).find(node => node.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle() }

// Small spec, task 30: ATQ market keys and spelling mappings are editable plan data.
test('ATQ 方案编辑市场键和写法映射且没有部件绑定选项', async t => {
 const stored:any={...plan(),file_kind:'atq',definition:{sheets:[{worksheet:'合成表',product_line_id:'line',header_rows:1,columns:[{column:'A',field:'name'},{column:'B',field:'due_stages'},{column:'C',field:'market',key:'overseas'},{column:'D',field:'market',key:'overseas'}]}],market_values:{Mandatory:'required'}}}
 const {root,writes}=await mount(t,{listShelfImportPlans:async()=>({success:true,data:[stored]})})
 await click(find(root,'button','查看'))
 assert.match(visibleText(field(root,`${zh.shelfImport.configuredValue} 1.3`)),/合成市场/)
 assert.doesNotMatch(visibleText(field(root,`${zh.shelfImport.configuredValue} 1.3`)),/合成区域/)
 assert.equal(descendants(root).some(n=>n.props['aria-label']==='部件类型 1'),false)
 await input(root,'市场原文 1','√');await input(root,'市场含义 1','optional')
 await savePlan(root)
 assert.deepEqual(writes[0].market_values,{'√':'optional'})
 assert.equal(writes[0].sheets[0].columns[2].key,'overseas')
 assert.equal(writes[0].sheets[0].columns[3].key,'overseas')
})

// Small spec, task 19 / ADR 0023: HTTP mocks replace the import boundary, not page behavior.
test('uploading a workbook with a confirmed plan opens its pending changeset', async t => {
  const { root, route, uploads, writes } = await mount(t)
  const file = await chooseFile(root)
  await input(root, '导入方案', 'map')
  await click(find(root, 'button', '按此方案试导入'))
  assert.deepEqual(uploads, [{ file, key: 'map', date: '' }])
  assert.equal(writes.length, 0)
  assert.equal(route.query.tab, 'review')
  assert.equal(route.query.changeset, 'batch-a')
  assert.equal(route.query.libraries, 'library-a')
})

// Small spec, task 19: saving an edited plan is a single confirmed changeset with its current revision.
test('editing worksheet and column mappings preserves the original plan revision and names', async t => {
  const { root, writes } = await mount(t)
  await click(find(root, 'button', '查看'))
  await input(root, '工作表 1', ' 客户工作表 ')
  await input(root, '列 1.3', 'D')
  await input(root, '表头行数 1', '2')
  await savePlan(root)
  assert.equal(writes[0].revision, 3)
  assert.equal(writes[0].sheets[0].worksheet, ' 客户工作表 ')
  assert.equal(writes[0].sheets[0].columns[2].column, 'D')
  assert.equal(writes[0].sheets[0].header_rows, 2)
})

// Small spec, task 19 / ADR 0023: model suggestions remain pending until reviewed and corrected.
test('first-time model suggestions can be edited and explicitly confirmed as a plan', async t => {
  const { root, suggestions, writes, uploads } = await mount(t)
  await chooseFile(root)
  await click(find(root, 'button', '新建方案'))
  await input(root, '方案键', 'new_map')
  await click(find(root, 'button', '请求模型建议'))
  assert.equal(suggestions.length, 1)
  assert.equal(writes.length, 0)
  assert.match(visibleText(root), /请检查列含义/)
  assert.equal(find(root, 'button', '保存方案').props.disabled, true)
  await input(root, '工作表 1', ' 人工核对工作表 ')
  const check = descendants(root).find(node => node.tag === 'input' && node.props.type === 'checkbox')!
  check.props.onChange({ target: { checked: true } }); await settle()
  await savePlan(root)
  assert.equal(writes[0].revision, 1)
  assert.equal(writes[0].decisions[0].action, 'replace')
  assert.equal(writes[0].decisions[0].replacement.import_plan.sheets[0].worksheet, ' 人工核对工作表 ')
  assert.deepEqual(writes[0].decisions[0].replacement.issues, [])
  assert.deepEqual(uploads, [])
})

// Small spec, task 19: unknown file dates are corrected explicitly without losing the workbook.
test('failed import retains the workbook and retries with an explicit UTC file date', async t => {
  const submitted: any[] = []
  const { root, route } = await mount(t, { uploadShelfImport: async (_: string, file: File, key: string, date: string) => {
    submitted.push({ file, key, date })
    if (!date) throw { message: 'file_date is required' }
    return { success: true, data: { id: 'existing-batch', status: 'pending' } }
  } })
  const file = await chooseFile(root, 'undated.xlsx'); await input(root, '导入方案', 'map')
  await click(find(root, 'button', '按此方案试导入'))
  assert.match(visibleText(root), /file_date is required/)
  assert.match(visibleText(root), /undated.xlsx/)
  await input(root, '文件生效日期', '2026-10-28')
  await click(find(root, 'button', '按此方案试导入'))
  assert.deepEqual(submitted[1], { file, key: 'map', date: '2026-10-28T00:00:00Z' })
  assert.equal(route.query.changeset, 'existing-batch')
})

// Small spec, task 19: invalid selection never discards a previously valid file.
test('an invalid replacement file leaves the selected workbook available', async t => {
  const { root, uploads } = await mount(t)
  const file = await chooseFile(root)
  await chooseFile(root, 'not-a-workbook.csv')
  assert.match(visibleText(root), /请选择一个非空的 .xlsx 文件/)
  assert.match(visibleText(root), /map_20261028.xlsx/)
  await input(root, '导入方案', 'map'); await click(find(root, 'button', '按此方案试导入'))
  assert.equal(uploads[0].file, file)
})

// Small regression, task 19: a trial import must not silently ignore unsaved mappings.
test('unsaved mapping edits disable trial import', async t => {
  const { root, uploads } = await mount(t)
  await chooseFile(root); await click(find(root, 'button', '查看'))
  await input(root, '工作表 1', 'Changed')
  for (const button of descendants(root).filter(node => node.tag === 'button' && visibleText(node) === '按此方案试导入')) assert.equal(button.props.disabled, true)
  assert.deepEqual(uploads, [])
})

// Small spec, task 19: a stale plan never drops edits or retries an overwrite.
test('a revision conflict retains edited mappings and explains recovery', async t => {
  const { root } = await mount(t, { saveShelfImportPlan: async () => { throw { status: 409, message: 'import plan revision changed' } } })
  await click(find(root, 'button', '查看')); await input(root, '工作表 1', 'Edited')
  await savePlan(root)
  assert.match(visibleText(root), /不会自动覆盖/)
  assert.equal(field(root, '工作表 1').props.value, 'Edited')
})

// Small spec, task 19: users can configure first-time imports without invoking a model.
test('a first-time plan can be entered manually with explicit worksheet mappings', async t => {
  const { root, writes, suggestions } = await mount(t)
  await click(find(root, 'button', '新建方案')); await input(root, '方案键', 'manual_map'); await input(root, '方案名称', ' 原样方案 ')
  await click(find(root, 'button', '添加工作表')); await input(root, '工作表 1', ' Map '); await input(root, '产品线 1', 'line')
  for (const [index, column, name] of [[1, 'A', 'platform'], [2, 'B', 'product'], [3, 'C', 'model']] as const) {
    await click(find(root, 'button', '添加列映射')); await input(root, `列 1.${index}`, column); await input(root, `对应字段 1.${index}`, name)
  }
  await savePlan(root)
  assert.equal(writes[0].name, ' 原样方案 ')
  assert.deepEqual(writes[0].sheets[0].columns, [{ column: 'A', field: 'platform', key: '' }, { column: 'B', field: 'product', key: '' }, { column: 'C', field: 'model', key: '' }])
  assert.deepEqual(suggestions, [])
})

// Small regression, task 19: ignored worksheets have no columns in the server response.
test('an ignored worksheet can be enabled and given a column mapping', async t => {
  const stored = plan(); (stored.definition.sheets as any[]).push({ worksheet: 'Previously ignored', header_rows: 0, ignore: true })
  const { root } = await mount(t, { listShelfImportPlans: async () => ({ success: true, data: [stored] }) })
  await click(find(root, 'button', '查看'))
  // Sheet 1 shows ignore and transpose; the ignored sheet 2 shows only ignore.
  const checkbox = descendants(root).filter(node => node.tag === 'input' && node.props.type === 'checkbox')[2]!
  checkbox.props.onChange({ target: { checked: false } }); await settle()
  await click(descendants(root).filter(node => node.tag === 'button' && visibleText(node) === '添加列映射')[1]!)
  assert.ok(field(root, '列 2.1'))
})

// Small regression, task 19: file dates are instants, but midnight UTC must not display the previous day.
test('batch history labels the effective time in UTC', async t => {
  const { root } = await mount(t, { listShelfImportBatches: async () => ({ success: true, data: { total: 1, items: [{ id: 'batch', status: 'pending', source: { type: 'import', file_name: 'source.xlsx', file_date: '2026-10-28T00:00:00Z' }, created_at: '2026-11-07T12:30:00Z', items: [] }] } }) })
  assert.match(visibleText(root), /生效时间（UTC）/)
  assert.match(visibleText(root), /2026年10月28日 00:00/)
})

// Small spec, task 19: explicit rules retain spelling and cannot silently overwrite a duplicate.
test('duplicate status spellings block plan saving until corrected', async t => {
  const { root, writes } = await mount(t)
  await click(find(root, 'button', '查看'))
  const statusSection = descendants(root).find(node => node.tag === 'section' && node.children.some(child => child.tag === 'h3' && visibleText(child) === '状态原文映射'))!
  await click(find(statusSection, 'button', '添加规则')); await input(root, '状态原文 1', '客户写法'); await input(root, '对应状态 1', 'GA')
  await click(find(statusSection, 'button', '添加规则')); await input(root, '状态原文 2', '客户写法')
  assert.equal(find(root, 'button', '保存方案').props.disabled, true)
  assert.match(visibleText(root), /规则原文不能为空或重复/)
  await input(root, '状态原文 2', ' 另一种写法 '); await input(root, '对应状态 2', 'DEV')
  await savePlan(root)
  assert.deepEqual(writes[0].status_values, { '客户写法': { status: 'GA', notes: '' }, ' 另一种写法 ': { status: 'DEV', notes: '' } })
})

// Small spec, task 19: unavailable model suggestions do not prevent manual configuration.
test('model failure preserves manual inputs for correction', async t => {
  const { root, writes } = await mount(t, { suggestShelfImportPlan: async () => { throw { message: '未配置本库处理模型' } } })
  await chooseFile(root); await click(find(root, 'button', '新建方案')); await input(root, '方案键', 'new_map'); await input(root, '方案名称', ' 手工名称 ')
  await click(find(root, 'button', '请求模型建议'))
  assert.match(visibleText(root), /未配置本库处理模型/)
  assert.equal(field(root, '方案名称').props.value, ' 手工名称 ')
  assert.ok(find(root, 'button', '添加工作表'))
  assert.equal(writes.length, 0)
})

// Small spec, task 19: duplicate clicks cannot submit the workbook twice while a request is pending.
test('an in-flight import blocks duplicate submission', async t => {
  let finish!: (value: unknown) => void
  const submitted: File[] = []
  const { root } = await mount(t, { uploadShelfImport: async (_: string, file: File) => { submitted.push(file); return new Promise(resolve => { finish = resolve }) } })
  await chooseFile(root); await input(root, '导入方案', 'map')
  await click(find(root, 'button', '按此方案试导入')); await click(find(root, 'button', '按此方案试导入'))
  assert.equal(submitted.length, 1)
  assert.equal(field(root, '选择 Excel 文件').props.disabled, true)
  finish({ success: true, data: { id: 'batch', status: 'pending' } }); await settle()
})

// Small spec, task 19: bounded history pages can be reopened from URL state.
test('batch pagination opens the selected batch without losing its library', async t => {
  const { root, route } = await mount(t, { listShelfImportBatches: async (_: string, page: number) => ({ success: true, data: { total: 21, items: [{ id: `batch-${page}`, status: 'pending', source: { type: 'import', file_name: `file-${page}.xlsx` }, items: [] }] } }) })
  await click(find(root, 'button', '下一页'))
  assert.match(visibleText(root), /file-2.xlsx/)
  assert.equal(route.query.import_page, '2')
  await click(find(root, 'button', '待确认 · 未处理 0'))
  assert.equal(route.query.changeset, 'batch-2')
  assert.equal(route.query.libraries, 'library-a')
})

// Small spec, task 19: latest-file labels come from the server across all history pages.
test('plan rows show the latest imported file supplied by the library', async t => {
  const { root } = await mount(t, { listShelfImportPlans: async () => ({ success: true, data: [{ ...plan(), last_file_name: 'latest_20261029.xlsx' }] }) })
  assert.match(visibleText(root), /最近的文件/)
  assert.match(visibleText(root), /latest_20261029.xlsx/)
})

// ADR 0025: a sheet's table range and transpose flag are edited and saved with
// the plan; clearing them removes the keys instead of sending empty values.
test('worksheet range and transpose are saved with the plan', async t => {
  const stored = plan(); Object.assign(stored.definition.sheets[0]!, { transpose: true })
  const { root, writes } = await mount(t, { listShelfImportPlans: async () => ({ success: true, data: [stored] }) })
  await click(find(root, 'button', '查看'))
  await input(root, '表格区域（可选，如 A3:F200） 1', 'b2:d9')
  await savePlan(root)
  assert.equal(writes[0].sheets[0].range, 'B2:D9')
  assert.equal(writes[0].sheets[0].transpose, true)
})

test('cleared range and transpose are omitted from the saved plan', async t => {
  const stored = plan(); Object.assign(stored.definition.sheets[0]!, { range: 'A3:F200', transpose: true })
  const { root, writes } = await mount(t, { listShelfImportPlans: async () => ({ success: true, data: [stored] }) })
  await click(find(root, 'button', '查看'))
  await input(root, '表格区域（可选，如 A3:F200） 1', '')
  // Sheet 1 checkboxes: ignore, then transpose.
  const transpose = descendants(root).filter(node => node.tag === 'input' && node.props.type === 'checkbox')[1]!
  transpose.props.onChange({ target: { checked: false } }); await settle()
  await savePlan(root)
  assert.ok(!('range' in writes[0].sheets[0]) && !('transpose' in writes[0].sheets[0]))
})
