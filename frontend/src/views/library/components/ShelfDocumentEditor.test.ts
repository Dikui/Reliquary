import assert from 'node:assert/strict'
import test from 'node:test'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import { entryPresentationTestModules } from '../entryPresentationTestModules'
// Small spec, task 20. Real upload form with synthetic HTTP responses.
test('云盘上传由用户明确选择未挂，并通过单操作变更集确认', async t => {
  const writes: any[] = []
  const component = loadComponent(new URL('./ShelfDocumentEditor.vue', import.meta.url), {
    ...entryPresentationTestModules,
    './ShelfConditionsEditor.vue': { __esModule: true, default: loadComponent(new URL('./ShelfConditionsEditor.vue', import.meta.url), entryPresentationTestModules) },
    '@/api/shelf-files': { shelfEntities: async () => [], shelfWriteError: (_: unknown, fallback: string) => fallback, writeShelf: async (_: string, ops: any) => { writes.push(ops); return { success: true, data: { status: 'confirmed' } } } },
    '@/api/entities': { uploadEntityAttachment: async () => { throw new Error('not expected') }, deleteEntityAttachment: async () => ({ success: true }) },
  })
  const app = renderer.createApp(component, { library: { id: 'shelf', shelf_config: {} } }); app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount()); await settle()
  const input = (name: string, value: string) => { const field = descendants(root).find(n => n.tag === 'input' && n.props['aria-label'] === name)!; field.props.onInput({ target: { value } }) }
  input('文件名称', ' 客户原名 '); input('云盘链接', 'https://example.invalid/manual'); await settle()
  descendants(root).find(n => n.tag === 'select' && n.props['aria-label'] === '挂在')!.props.onChange({ target: { value: 'unassigned' } }); await settle()
  descendants(root).find(n => n.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle()
  assert.equal(writes[0][0].type, 'add_document_version')
  assert.equal(writes[0][0].document.name, ' 客户原名 ')
  assert.equal(writes[0][0].document.unassigned, true)
  assert.equal(writes[0][0].document.cloud_url, 'https://example.invalid/manual')
  assert.doesNotMatch(visibleText(root), /保存失败/)
})

// Small spec, task 22: two identically named requirements stay distinguishable
// by list, and explicit selection is sent together through one changeset.
test('资料文件按所属清单区分同名要求并一次选择多项', async t => {
  const writes: any[] = []
  const component = loadComponent(new URL('./ShelfDocumentEditor.vue', import.meta.url), {
    ...entryPresentationTestModules,
    './ShelfConditionsEditor.vue': { __esModule: true, default: loadComponent(new URL('./ShelfConditionsEditor.vue', import.meta.url), entryPresentationTestModules) },
    '@/api/shelf-files': { shelfEntities: async () => [{ id: 'model', kind: 'model', canonical_name: '合成型号' }, ...['01', '02'].map(id => ({ id, kind: 'document_requirement', canonical_name: '参数表', attributes: { requirement: { list_name: id + ' 清单' } } }))], shelfWriteError: (_: unknown, fallback: string) => fallback, writeShelf: async (_: string, ops: any) => { writes.push(ops); return { success: true, data: { status: 'confirmed' } } } },
    '@/api/entities': { uploadEntityAttachment: async () => { throw new Error('not expected') }, deleteEntityAttachment: async () => ({ success: true }) },
  })
  const app = renderer.createApp(component, { library: { id: 'shelf', shelf_config: {} } }); app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount()); await settle()
  for (const [name, value] of [['文件名称', '共用参数表'], ['云盘链接', 'https://example.invalid/shared']]) descendants(root).find(n => n.tag === 'input' && n.props['aria-label'] === name)!.props.onInput({ target: { value } })
  descendants(root).find(n => n.tag === 'select' && n.props['aria-label'] === '挂在')!.props.onChange({ target: { value: 'model' } }); await settle()
  assert.match(visibleText(root), /01 清单/); assert.match(visibleText(root), /02 清单/)
  const checkboxes = descendants(root).filter(n => n.tag === 'input' && n.props.type === 'checkbox')
  assert.equal(checkboxes.length, 2)
  for (const checkbox of checkboxes) checkbox.props.onChange({ target: { checked: true } })
  await settle(); descendants(root).find(n => n.tag === 'form')!.props.onSubmit({ preventDefault() {} }); await settle()
  assert.deepEqual(writes[0][0].document.requirement_ids, ['01', '02'])
})
