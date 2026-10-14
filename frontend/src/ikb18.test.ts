import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync, readdirSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, compileTemplate, parse } from '@vue/compiler-sfc'
import { createSSRApp, defineComponent, h, onServerPrefetch, ref } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import ts from 'typescript'
import zh from './i18n/locales/zh-CN'
import en from './i18n/locales/en-US'
import * as form from './utils/libraryCatalogForm'
import * as scope from './utils/libraryScope'
import * as references from './utils/bodyReference'
const require = createRequire(import.meta.url)
const empty = { __esModule: true, default: defineComponent({ setup: () => () => null }) }
const library = (id = 'mine', extra = {}) => ({ id, name: '我的资料', display_name: '我的资料', directory: id, template: 'own', system_role: id === 'mine' ? 'mine' : '', description: '', review_criteria: '完整', content_types: [{ name: '笔记', enabled: true, position: 0, extract_graph: false }], relation_types: [{ name: '合作', enabled: true, position: 0 }, { name: '依赖', enabled: true, position: 1 }], feed_enabled: false, entry_count: 0, ...extra })
async function scenario(path: string, props: Record<string, unknown>, modules: Record<string, any>, action: (state: any) => Promise<void>, locale = 'zh-CN') {
  const filename = new URL(path, import.meta.url).pathname
  const { descriptor } = parse(readFileSync(filename, 'utf8'), { filename })
  const script = compileScript(descriptor, { id: filename })
  const template = compileTemplate({ id: filename, filename, source: descriptor.template!.content, ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: script.bindings } })
  assert.deepEqual(template.errors, [])
  const i18n = createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } })
  function evaluate(code: string) {
    const out = { exports: {} as any }, compiled = ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Function('require', 'module', 'exports', compiled)((name: string) => name === 'vue-i18n' ? { useI18n: () => i18n.global } : modules[name] ?? (name.endsWith('.vue') ? empty : require(name)), out, out.exports)
    return out.exports
  }
  const component = evaluate(script.content).default
  component.ssrRender = evaluate(template.code).ssrRender
  const setup = component.setup
  component.setup = (value: any, context: any) => { const state = setup(value, context); onServerPrefetch(() => action(state)); return state }
  const app = createSSRApp(component, props); app.use(i18n)
  for (const name of ['t-select', 't-input', 't-checkbox', 't-switch', 't-button', 't-dialog', 't-loading', 't-alert', 't-tabs', 't-tab-panel', 't-tag', 't-pagination', 't-empty']) {
    app.component(name, defineComponent({ props: ['visible', 'disabled', 'message', 'loading'], setup: (p, { attrs, slots }) => () => name === 't-dialog' && !p.visible ? null : h(name === 't-button' ? 'button' : 'div', { ...attrs, disabled: p.disabled || undefined }, [p.message, slots.default?.(), slots.operation?.()]) }))
  }
  const errors: unknown[] = []; app.config.errorHandler = err => errors.push(err)
  const html = await renderToString(app); assert.deepEqual(errors, []); return html
}
function catalogModules(calls: any[], options: { fail?: boolean } = {}) {
  let current = library(), dialog: any
  return {
    '@/utils/libraryCatalogForm': form,
    'tdesign-vue-next': { DialogPlugin: { confirm: (value: any) => { dialog = value; return { destroy() {}, update() {} } } } },
    '@/api/library': {
      listLibraries: async () => ({ success: true, data: [current] }), getCatalogLibrary: async () => ({ success: true, data: structuredClone(current) }),
      saveCatalogLibrary: async (id: string, body: any) => { calls.push({ id, body }); if (options.fail) return { success: false, message: 'offline' }; current = { ...current, ...body }; return { success: true } },
      createCatalogLibrary: async (body: any) => { calls.push({ create: body }); current = library('new-notes', body); return { success: true, data: current } },
      mergeLibraryRelationTypes: async (...args: any[]) => { calls.push({ merge: args }); return { success: true } }, deleteCatalogLibrary: async (id: string) => { calls.push({ delete: id }); return { success: true } },
    },
    dialog: () => dialog,
  }
}
for (const locale of ['zh-CN', 'en-US']) {
  test(`${locale}: library editor shows immutable identity and saves ordered types without identity fields`, async () => {
    const calls: any[] = [], modules = catalogModules(calls)
    const html = await scenario('./views/settings/LibraryCatalogSettings.vue', {}, modules, async state => {
      await state.load(); assert.equal(state.dirty.value, false)
      state.draft.value.name = '  Notes  '
      state.draft.value.content_types.push({ name: 'SOP', enabled: false, extract_graph: true, position: 4 })
      state.moveType(state.draft.value.content_types, 1, -1)
      state.draft.value.relation_types.splice(1, 1)
      await state.save()
      assert.equal(state.dirty.value, false)
      const body = calls[0].body
      assert.equal(body.name, 'Notes'); assert.equal(body.content_types[0].name, 'SOP'); assert.equal(body.content_types[0].position, 0); assert.equal(body.content_types[0].extract_graph, true)
      assert.equal(body.relation_types.length, 1)
      for (const field of ['directory', 'template', 'system_role', 'source_evidence_rule']) assert.equal(field in body, false)
    }, locale)
    assert.match(html, /mine\//); assert.doesNotMatch(html, /Delete library|删除库/)
  })
  test(`${locale}: new library validates directory and enters created library settings`, async () => {
    const calls: any[] = []
    await scenario('./views/settings/LibraryCatalogSettings.vue', {}, catalogModules(calls), async state => {
      state.creating.value = true; state.newLibrary.value = { template: 'own', directory: 'Bad--Name', name: 'New', description: 'Notes' }
      await state.create(); assert.equal(calls.length, 0); assert.ok(state.error.value)
      state.newLibrary.value.directory = 'new-notes'; await state.create()
      assert.equal(calls[0].create.template, 'own'); assert.equal(state.draft.value.id, 'new-notes'); assert.equal(state.creating.value, false)
    }, locale)
  })
}
test('library validation preserves failed drafts, rejects duplicate and empty types, and confirms relation merging', async () => {
  const calls: any[] = [], modules = catalogModules(calls)
  await scenario('./views/settings/LibraryCatalogSettings.vue', {}, modules, async state => {
    await state.load(); state.draft.value.name = ' '; await state.save(); assert.equal(calls.length, 0)
    state.draft.value.name = 'Notes'; state.draft.value.content_types.push({ name: '笔记', enabled: true, extract_graph: false })
    await state.save(); assert.equal(calls.length, 0)
    state.draft.value.content_types.pop(); state.draft.value = JSON.parse(state.saved.value)
    state.fromType.value = '合作'; state.toType.value = '依赖'; state.merge()
    assert.equal(calls.length, 0); assert.ok(modules.dialog().body.includes('合作 → 依赖'))
    await modules.dialog().onConfirm(); assert.deepEqual(calls[0].merge, ['mine', '合作', '依赖'])
  })
  await scenario('./views/settings/LibraryCatalogSettings.vue', {}, catalogModules([], { fail: true }), async state => {
    await state.load(); state.draft.value.name = 'Draft'; await state.save(); assert.equal(state.draft.value.name, 'Draft'); assert.equal(state.error.value, 'offline'); assert.equal(state.dirty.value, true)
  })
})
test('four templates have descriptions and directory validation matches backend boundaries', () => {
  for (const template of form.LIBRARY_TEMPLATES) { assert.ok(zh.ikb18[`${template}Hint`]); assert.ok(en.ikb18[`${template}Hint`]) }
  for (const directory of ['a', 'mine-2', 'a'.repeat(100)]) assert.equal(form.validDirectory(directory), true)
  for (const directory of ['', 'A', '-a', 'a-', 'a--b', 'a/b', '../mine', 'a'.repeat(101)]) assert.equal(form.validDirectory(directory), false)
  assert.equal(form.validLibraryName('😀'.repeat(255)), true); assert.equal(form.validLibraryName('😀'.repeat(256)), false)
})
test('scope storage remembers multiple libraries and safely handles blocked and malformed storage', () => {
  const old = Object.getOwnPropertyDescriptor(globalThis, 'localStorage'); const values = new Map<string, string>()
  try {
    Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: { getItem: (key: string) => values.get(key), setItem: (key: string, value: string) => values.set(key, value) } })
    scope.saveLibraryScope(['mine', 'references']); assert.deepEqual(scope.readLibraryScope(), ['mine', 'references'])
    assert.deepEqual(scope.scopedLibraryIDs(scope.readLibraryScope(), [{ id: 'mine' }, { id: 'other' }]), ['mine'])
    assert.deepEqual(scope.scopedLibraryIDs([], [{ id: 'mine' }, { id: 'other' }]), ['mine', 'other'])
    values.set(scope.LIBRARY_SCOPE_KEY, '{'); assert.deepEqual(scope.readLibraryScope(), [])
    Object.defineProperty(globalThis, 'localStorage', { configurable: true, get() { throw new Error('blocked') } }); scope.saveLibraryScope(['mine']); assert.deepEqual(scope.readLibraryScope(), [])
  } finally { if (old) Object.defineProperty(globalThis, 'localStorage', old); else delete (globalThis as any).localStorage }
})
test('mounted library selector renders catalog names and returns selected range', async () => {
  const html = await scenario('./components/LibraryScopeSelect.vue', { modelValue: ['mine'] }, { '@/api/library': { listLibraries: async () => ({ success: true, data: [library()] }) } }, async state => {
    await state.load(); assert.deepEqual(state.options.value, [{ value: 'mine', label: '我的资料' }]); state.emit('update:modelValue', ['mine', 'references'])
  })
  assert.match(html, /库范围/)
})
test('mixed emoji and Chinese/English references highlight UTF-16 ranges only after matching body hash', async () => {
  const body = '# Test\n\nA😀中 English🚀结尾\n', start = Array.from('# Test\n\nA').length, end = start + 3
  const location = { start, end, paragraph: 2, body_hash: await references.bodyHash(body), heading_path: ['Test'], mapping: 'exact' }
  const resolved = await references.resolveBodyReference(body, location)
  assert.equal(resolved.matches, true); assert.equal(body.slice(resolved.range!.start, resolved.range!.end), '😀中 ')
  const mismatch = await references.resolveBodyReference(body + 'changed', location); assert.equal(mismatch.matches, false); assert.equal(mismatch.range, null); assert.equal(mismatch.paragraph, 2)
  assert.equal(references.codePointRange(body, -1, 2), null)
  assert.deepEqual(references.parseReference(JSON.stringify(location)), location); assert.equal(references.parseReference('{'), undefined)
  const oldFetch = globalThis.fetch
  try {
    globalThis.fetch = async () => new Response(body)
    const modules = { '@/utils/bodyReference': references, '@/utils/api-base': { getApiBaseUrl: () => 'http://fixture' } }
    const html = await scenario('./components/BodyReference.vue', { entryId: 'a', location }, modules, async state => { await state.load(); assert.equal(state.changed.value, false) })
    assert.match(html, /<mark>😀中 <\/mark>/)
    const degraded = await scenario('./components/BodyReference.vue', { entryId: 'a', location: { ...location, body_hash: 'old' } }, modules, async state => { await state.load(); assert.equal(state.changed.value, true) })
    assert.match(degraded, /正文已变化/); assert.doesNotMatch(degraded, /<mark>/)
  } finally { globalThis.fetch = oldFetch }
})
test('body paragraphs preserve fenced code, list items, CRLF, tables and setext headings', () => {
  const body = '# H\r\n\r\nA😀\r\n\r\n- one\n- two\n\n```md\n# inside\n\ncode\n```\n\n| a |\n| - |\n\nTitle\n=====\n'
  const blocks = references.bodyParagraphs(body); assert.equal(blocks.length, 7); assert.equal(blocks.map(block => block.text).join(''), body); assert.match(blocks[4].text, /# inside\n\ncode/)
})
test('entity relation groups filter libraries, source links retain positions, and deletion requires suppression confirmation', async () => {
  const calls: string[] = [], libraryScope = ref<string[]>([])
  const location = { start: 1, end: 3, paragraph: 2, body_hash: '', heading_path: ['H'], mapping: 'exact' }
  const relation = { id: 'r', relation_type: '合作', direction: 'outgoing', peer: { id: 'p', canonical_name: 'Peer' }, library_id: 'mine', library_name: 'Notes', source_count: 1, sources: [{ entry_id: 'a', title: 'Source', body_hash: 'hash', evidence: [{ locations: [location], speaker: { name: 'Speaker' } }] }] }
  let deleted = false
  const html = await scenario('./views/library/components/EntityRelations.vue', { entityId: 'e' }, {
    '@/composables/useLibraryScope': { useLibraryScope: () => libraryScope },
    '@/api/relations': { listEntityRelations: async () => ({ success: true, data: deleted ? [] : [{ relation_type: '合作', relations: [relation] }] }), deleteEntityRelation: async (id: string) => { calls.push(id); deleted = true; return { success: true } } },
  }, async state => {
    await state.load(); assert.equal(state.filtered.value[0].relation_type, '合作')
    libraryScope.value = ['other']; assert.equal(state.filtered.value.length, 0); libraryScope.value = []
    assert.equal(state.locationFor(relation.sources[0], location).body_hash, 'hash')
    state.deleting.value = relation; assert.equal(calls.length, 0)
  })
  assert.match(html, /Peer/); assert.match(html, /Source/); assert.match(html, /Speaker/); assert.match(html, /抑制记录/); assert.match(html, /段落 2/)
  await scenario('./views/library/components/EntityRelations.vue', { entityId: 'e' }, {
    '@/composables/useLibraryScope': { useLibraryScope: () => ref([]) },
    '@/api/relations': { listEntityRelations: async () => ({ success: true, data: [] }), deleteEntityRelation: async (id: string) => { calls.push(id); return { success: true } } },
  }, async state => { state.deleting.value = relation; await state.remove(); assert.equal(state.deleting.value, null) })
  assert.deepEqual(calls, ['r'])
})
for (const locale of ['zh-CN', 'en-US']) test(`${locale}: pending review shows library and segment reason and filters both tabs by library`, async () => {
  const calls: any[] = []
  const modules = {
    '@/composables/useLibraryScope': { useLibraryScope: () => ref(['mine']) }, '@/utils/security': { isValidURL: () => true },
    '@/api/entry-review': { listPendingEntry: async (input: any) => { calls.push(input); return { success: true, total: 1, data: [{ id: 'a', title: 'Pending', library_name: 'Notes', metadata: { intake: { reason: '第 2/3 段: body incomplete' } } }] } } },
    '@/api/ingest': { listIncompleteEntrys: async (input: any) => { calls.push(input); return { success: true, data: [], total: 0 } } },
  }
  const html = await scenario('./views/library/components/PendingReviews.vue', {}, modules, async state => { await state.load() }, locale)
  assert.match(html, /Notes/); assert.match(html, /第 2\/3 段/); assert.deepEqual(calls[0].libraries, ['mine'])
  const incomplete = await scenario('./views/library/components/PendingReviews.vue', {}, modules, async state => { state.kind.value = 'incomplete'; await state.load() }, locale)
  assert.ok(incomplete.includes(locale === 'zh-CN' ? '按资料 ID 补交图片' : 'Submit missing images by material ID')); assert.deepEqual(calls[1].libraries, ['mine'])
})

test('mounted relation source opens exact body position and suppression deletion waits for confirm', async () => {
  const { loadComponent, renderer, element, descendants, click, registerPrimitives, settle, visibleText } = await import('./views/library/componentTestHarness')
  const location = { start: 2, end: 5, paragraph: 2, body_hash: '', heading_path: ['H'], mapping: 'exact' }
  const relation = { id: 'rel', relation_type: '合作', direction: 'incoming', peer: { id: 'peer', canonical_name: 'Peer' }, library_id: 'mine', library_name: 'Notes', source_count: 1, sources: [{ entry_id: 'source', title: 'Source title', body_hash: 'saved-hash', evidence: [{ locations: [location], speaker: { name: 'Speaker' } }] }] }
  const scopeRef = ref<string[]>([]), opened: any[] = [], deleted: string[] = []
  let exists = true
  const component = loadComponent(new URL('./views/library/components/EntityRelations.vue', import.meta.url), {
    '@/composables/useLibraryScope': { useLibraryScope: () => scopeRef }, '@/components/LibraryScopeSelect.vue': empty,
    '@/api/relations': { listEntityRelations: async () => ({ success: true, data: exists ? [{ relation_type: '合作', relations: [relation] }] : [] }), deleteEntityRelation: async (id: string) => { deleted.push(id); exists = false; return { success: true } } },
  })
  const root = element('root'), app = renderer.createApp(component, { entityId: 'entity', onOpenSource: (...args: any[]) => opened.push(args) })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } })); registerPrimitives(app); app.mount(root)
  try {
    await settle()
    assert.ok(visibleText(root).includes('指向此实体'))
    const button = (label: string) => descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)!
    await click(button('Source title')); assert.deepEqual(opened, [['source', { ...location, body_hash: 'saved-hash' }]])
    scopeRef.value = ['references']; await settle(); assert.ok(!visibleText(root).includes('Source title'))
    scopeRef.value = []; await settle(); await click(button('删除关系'))
    assert.equal(deleted.length, 0); assert.ok(visibleText(root).includes('抑制记录'))
    await click(button('确认')); assert.deepEqual(deleted, ['rel']); assert.ok(!visibleText(root).includes('Source title'))
  } finally { app.unmount() }
})

test('compact global search thumbnails expose the shared image viewer trigger', async () => {
  const html = await scenario('./components/SearchImage.vue', { articleId: 'material', path: 'chart.png', title: 'Chart', compact: true, interactive: true }, {
    '@/utils/protectedFileAccess': { buildProtectedFileRequest: () => ({ url: '/image', headers: {} }) },
  }, async state => { state.imageURL.value = 'blob:fixture'; state.loading.value = false; state.openPreview(); assert.equal(state.previewVisible.value, true) })
  assert.match(html, /<button[^>]*search-image-open/); assert.match(html, /blob:fixture/)
})

test('390 wide material list and search keep controls bounded and advanced filters collapsible', async () => {
  const { compiledSFCStyleRules, styleDeclarationsAtWidth } = await import('./utils/sfcStyleTestHelpers')
  const list = await compiledSFCStyleRules(new URL('./views/library/components/DocumentListView.vue', import.meta.url))
  assert.equal(styleDeclarationsAtWidth(list, '.doc-list-row', 390)['grid-template-columns'], '32px minmax(0, 1fr) 52px')
  assert.equal(styleDeclarationsAtWidth(list, '.doc-list-view', 390)['overflow-x'], 'hidden')
  const search = await compiledSFCStyleRules(new URL('./views/library/SearchView.vue', import.meta.url))
  assert.equal(styleDeclarationsAtWidth(search, '.advanced-filter-controls', 390).display, 'none')
  assert.equal(styleDeclarationsAtWidth(search, '.advanced-filter-controls.expanded', 390).display, 'grid')
  assert.equal(styleDeclarationsAtWidth(search, '.advanced-filter-controls', 1440).display, 'grid')
})

test('English newline separator labels display literal escapes', async () => {
  const { collectLocaleKeys, getLocaleValueAtPath } = await import('./i18n/localeKeyAudit')
  const keys = [...collectLocaleKeys(en)]
  const double = keys.find(key => key.endsWith('.doubleNewline'))!, single = keys.find(key => key.endsWith('.singleNewline'))!
  const escapedNewline = String.fromCharCode(92) + 'n'
  assert.equal(getLocaleValueAtPath(en, double), 'Double newline (' + escapedNewline + escapedNewline + ')')
  assert.equal(getLocaleValueAtPath(en, single), 'Single newline (' + escapedNewline + ')')
})

for (const locale of ['zh-CN','en-US'] as const) test(`${locale}: historical default description uses current copy without an implicit save`, async()=>{
 const legacy='外部个人的观点：专家、分析师、KOL 的评论与文章。'
 const original=library('opinions',{system_role:'opinions',template:'external_timely',description:legacy})
 const calls:any[]=[]
 const modules=catalogModules(calls)
 modules['@/api/library'].listLibraries=async()=>({success:true,data:[original]})
 modules['@/api/library'].getCatalogLibrary=async()=>({success:true,data:structuredClone(original)})
 await scenario('./views/settings/LibraryCatalogSettings.vue',{},modules,async state=>{
  await state.load()
  assert.equal(state.draft.value.description,(locale==='zh-CN'?zh:en).ikb18.systemOpinionsDescription)
  assert.equal(state.dirty.value,false)
  assert.equal(calls.length,0)
  assert.equal(original.description,legacy)
 },locale)
 assert.equal(form.catalogDescription({...original,description:'自定义说明：'+legacy} as any,'replacement'),'自定义说明：'+legacy)
 assert.equal(form.catalogDescription({...original,system_role:''} as any,'replacement'),legacy)
})
