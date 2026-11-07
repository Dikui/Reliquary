import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../i18n/locales/zh-CN'
import en from '../../i18n/locales/en-US'
import { TestElement, doc, renderer, send, settle } from '../../components/ui/__tests__/dom'
import { loadUiComponent } from '../../components/ui/uiTestLoader'
import { loadComponent } from '../library/componentTestHarness'
import * as form from '../../utils/libraryCatalogForm'
import * as tiers from '../../utils/modelTiers'
import * as classification from '../../types/entryClassification'
import type { LibraryCatalogItem, LibraryPatch } from '../../api/library'

// MEL-10: small regression tests exercise the real settings page and matching editor.
Object.defineProperty(globalThis, 'Document', { configurable: true, value: class {} })
Object.defineProperty(TestElement.prototype, 'options', { configurable: true, get() { return this.querySelectorAll('option') } })

function library(id: string, entity_mode: 'managed' | 'extracted' = 'managed'): LibraryCatalogItem {
  return {
    id, name: id, display_name: id, directory: id, color: 2, description: '', entity_mode,
    system_role: '', template: 'standard', review_criteria: '', default_review_criteria: '',
    processing_model_tiers: [], source_evidence_rule: '', collect_content: '', exclude_content: '', deduplication_rule: '',
    template_rules: { deduplication: '', source_evidence: '', review: false, process: false, merge_reposts: false, collapse_similar: false, fact_source: false },
    entity_schema: { kinds: [], fields: [] }, product_library_ids: [],
    entry_count: 0, model_tiers: {}, content_types: [], relation_types: [], feed_enabled: true,
    chunking_config: { chunk_size: 512, chunk_overlap: 0, separators: ['\n\n'], enable_parent_child: false, parent_chunk_size: 4096, child_chunk_size: 384 },
  }
}

async function fixture(items = [library('current'), library('other'), library('articles', 'extracted')], locale = 'en-US', saveData?: LibraryCatalogItem) {
  const requests: Array<{ id: string; payload: LibraryPatch }> = []
  // Replace HTTP and unrelated model controls; matching and schema editors run as real SFCs.
  const api = {
    listLibraries: async () => ({ success: true, data: items }),
    getCatalogLibrary: async (id: string) => ({ success: true, data: structuredClone(items.find(item => item.id === id)) }),
    getEntityConversion: async () => ({ success: true, data: { entity_counts: {} } }),
    saveCatalogLibrary: async (id: string, payload: LibraryPatch) => { requests.push({ id, payload: JSON.parse(JSON.stringify(payload)) }); return { success: true, data: saveData } },
  }
  const ui = { Button: loadUiComponent('Button'), LibMark: loadUiComponent('LibMark'), Tag: loadUiComponent('Tag'),
    Dialog: defineComponent({ render: () => null }), toast: { success() {} },
  }
  const fields = loadComponent(new URL('./ManagedFieldSchema.vue', import.meta.url), { '@/components/ui': ui })
  const editor = loadComponent(new URL('./ManagedEntitySettings.vue', import.meta.url), {
    '@/api/library': api, '@/components/ui': ui, './ManagedFieldSchema.vue': { __esModule: true, default: fields },
  })
  const page = loadComponent(new URL('./LibraryCatalogSettings.vue', import.meta.url), {
    '@/api/library': api, '@/components/ui': ui,
    '@/utils/libraryCatalogForm': form, '@/utils/modelTiers': tiers, '@/types/entryClassification': classification,
    './ManagedEntitySettings.vue': { __esModule: true, default: editor },
    '@/components/ModelTierEditor.vue': { __esModule: true, default: defineComponent({ render: () => h('div') }) },
  })
  const root: TestElement = doc.createElement('main')
  doc.body.appendChild(root)
  const app = renderer.createApp(page)
  app.use(createI18n({ legacy: false, locale, messages: { 'en-US': en, 'zh-CN': zh } }))
  app.mount(root)
  await settle()
  const editorRoot = root.querySelector('section')!
  return { root, editorRoot, requests, async cleanup() { app.unmount(); root.remove(); await settle() } }
}

// Small spec, task 02 / ADR 0023: all three lists use the existing draft/save
// workflow, retaining stable identities and the submitted name spelling.
test('shelf settings append, rename and reorder all three lists before saving', async t => {
  const f = await fixture(); t.after(() => f.cleanup())
  const enable = f.editorRoot.querySelectorAll('button').find(button => button.textContent === 'Enable shelf')!
  await send(enable, 'click')
  assert.ok(!f.editorRoot.textContent.includes('Products in this library always participate'))
  for (const key of ['module_types', 'regions', 'stages'] as const) {
    const group = f.editorRoot.querySelector(`[data-shelf-list="${key}"]`)!
    for (const value of [' First value ', 'Second value']) {
      const field = group.querySelectorAll('input').at(-1)!
      field.value = value; await send(field, 'input')
      await send(group.querySelectorAll('button').find(button => button.textContent === 'Add')!, 'click')
    }
    const first = group.querySelectorAll('input')[0]!
    first.value = ' Renamed value '; await send(first, 'input')
    await send(group.querySelectorAll('button').find(button => button.textContent === 'Move down')!, 'click')
  }
  await send(f.root.querySelector('form')!, 'submit')
  const payload = f.requests[0]!.payload
  assert.equal(payload.shelf_enabled, true)
  assert.deepEqual(payload.product_library_ids, [])
  for (const key of ['module_types', 'regions', 'stages'] as const) {
    assert.deepEqual(payload.shelf_config![key]!.map(item => item.name), ['Second value', ' Renamed value '])
    assert.equal(new Set(payload.shelf_config![key]!.map(item => item.key)).size, 2)
  }
})

// Small spec, task 02 / ADR 0024: shelf libraries are never offered as targets.
test('automatic matching choices exclude shelves', async t => {
  const f = await fixture([library('current'), { ...library('shelf'), shelf_enabled: true }, library('ordinary')]); t.after(() => f.cleanup())
  const choices = f.editorRoot.querySelectorAll('input[type="checkbox"]')
  assert.deepEqual(choices.map(choice => choice.parentElement!.textContent), ['ordinary'])
})

// Small regression, task 02: enabling installs a server-owned schema, so a
// successful save must adopt the returned library as the next editing baseline.
test('saving shelf settings adopts the structure returned by the server', async t => {
  const saved = { ...library('current'), shelf_enabled: true, entity_schema: { kinds: [], fields: [{ key: 'confirmed_field', name: 'Server confirmed field', type: 'text' as const, kinds: [] }] } }
  const f = await fixture(undefined, 'en-US', saved); t.after(() => f.cleanup())
  await send(f.editorRoot.querySelectorAll('button').find(button => button.textContent === 'Enable shelf')!, 'click')
  await send(f.root.querySelector('form')!, 'submit')
  assert.ok(f.root.querySelectorAll('input').some(input => input.value === 'Server confirmed field'))
  assert.equal(f.root.querySelector('button[type="submit"]')!.disabled, true)
})

test('a managed library can save other product libraries as automatic matching targets', async t => {
  const f = await fixture(); t.after(() => f.cleanup())
  assert.match(f.editorRoot.textContent, /Product libraries for automatic matching/)
  const choices = f.editorRoot.querySelectorAll('input[type="checkbox"]')
  assert.deepEqual(choices.map(choice => choice.parentElement!.textContent), ['other'])
  choices[0]!.checked = true
  await send(choices[0]!, 'change')
  assert.equal(f.root.querySelector('button[type="submit"]')!.disabled, false)
  await send(f.root.querySelector('form')!, 'submit')
  assert.equal(f.requests[0]!.id, 'current')
  assert.deepEqual(f.requests[0]!.payload.product_library_ids, ['other'])
  assert.equal(f.root.querySelector('button[type="submit"]')!.disabled, true)
  choices[0]!.checked = false
  await send(choices[0]!, 'change')
  await send(f.root.querySelector('form')!, 'submit')
  assert.deepEqual(f.requests[1]!.payload.product_library_ids, [])
})

for (const initialMode of ['managed', 'extracted'] as const) {
  test(`${initialMode} libraries retain saved matching targets through mode changes`, async t => {
    const current = { ...library('current', initialMode), product_library_ids: ['other'] }
    const f = await fixture([current, library('other')]); t.after(() => f.cleanup())
    assert.equal(f.editorRoot.querySelector('input[type="checkbox"]')!.checked, true)
    const select = f.editorRoot.querySelector('select')!
    for (const mode of initialMode === 'managed' ? ['extracted', 'managed'] : ['managed', 'extracted']) {
      for (const option of select.querySelectorAll('option')) Reflect.set(option, 'selected', option.value === mode)
      await send(select, 'change')
      assert.equal(f.editorRoot.textContent.includes('Document relation types (comma-separated)'), mode === 'managed')
      assert.equal(f.editorRoot.querySelector('input[type="checkbox"]')!.checked, true)
      assert.match(f.editorRoot.textContent, /Product libraries for automatic matching/)
    }
    const name = f.root.querySelector('input[name="library-name"]')!
    name.value = 'Updated library'; await send(name, 'input')
    await send(f.root.querySelector('form')!, 'submit')
    assert.deepEqual(f.requests[0]!.payload.product_library_ids, ['other'])
  })
}

for (const [locale, heading, selfHint, emptyHint] of [
  ['en-US', 'Product libraries for automatic matching', 'Products in this library always participate in automatic matching.', 'No other product libraries are available. Create a managed library to add matching targets.'],
  ['zh-CN', '自动匹配的产品库', '本库产品始终参与自动匹配。', '暂无其他可选产品库。创建人工维护库后，可在此添加匹配目标。'],
] as const) {
  test(`${locale} explains that managed libraries always match their own products`, async t => {
    const f = await fixture(undefined, locale); t.after(() => f.cleanup())
    assert.ok(f.editorRoot.textContent.includes(selfHint))
  })

  for (const mode of ['managed', 'extracted'] as const) {
    test(`${locale} explains empty matching candidates in ${mode} mode`, async t => {
      const f = await fixture([library('current', mode), library('articles', 'extracted')], locale)
      t.after(() => f.cleanup())
      assert.ok(f.editorRoot.textContent.includes(heading))
      assert.ok(f.editorRoot.textContent.includes(emptyHint))
      assert.equal(f.editorRoot.querySelectorAll('input[type="checkbox"]').length, 0)
      assert.equal(f.editorRoot.textContent.includes(selfHint), mode === 'managed')
    })
  }
}
