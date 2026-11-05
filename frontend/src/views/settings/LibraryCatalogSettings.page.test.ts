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
import type { LibraryCatalogItem } from '../../api/library'

Object.defineProperty(globalThis, 'Document', { configurable: true, value: class {} })

// Vue's native select directive reads the option collection from the DOM host.
Object.defineProperty(TestElement.prototype, 'options', { configurable: true, get() { return this.querySelectorAll('option') } })

const library = (id = 'research', count = 2) => ({ id, name: id, directory: id, color: 2, description: '',
  prompt_overrides: {}, prompt_defaults: { article_processing: 'Read the material', library_summary: 'Keep key facts', article_entities: 'Extract central entities', article_relations: 'Extract supported relations', summary_merge: 'Merge summaries', topic_backfill: 'Choose matching topics' }, prompt_max_chars: 16000,
  entry_count: count, model_tiers: { generation: 'writer', entity_resolution: '', rerank: 'ranker' }, processing_model_tiers: ['generation', 'entity_resolution'],
  chunking_config: { chunk_size: 512, chunk_overlap: 0, separators: ['\n\n'], enable_parent_child: false, parent_chunk_size: 4096, child_chunk_size: 384 },
  content_types: [{ name: 'research', enabled: true, extract_graph: false, position: 0 }], relation_types: [], feed_enabled: true,
} as unknown as LibraryCatalogItem)

async function fixture(items = [library()], fail = false, locale = 'en-US') {
  const requests: any[] = [], events: any[] = [], dialogs: any[] = []
  let shouldFail = fail
  const saved = (event: Event) => events.push((event as CustomEvent).detail)
  window.addEventListener('reliquary:library-settings-saved', saved)
  const component = loadComponent(new URL('./LibraryCatalogSettings.vue', import.meta.url), {
    // Replace the catalog editor with one public schema-edit action.
    './ManagedEntitySettings.vue': { __esModule: true, default: defineComponent({ props: ['modelValue'], setup: props => () => props.modelValue.entity_mode === 'managed' ? h('button', { type: 'button', 'data-add-field': true, onClick: () => props.modelValue.entity_schema.fields.push({ key: 'size', name: 'Size', type: 'number' }) }, 'Add field') : null }) },
    '@/api/library': {
      listLibraries: async () => ({ success: true, data: items }),
      getCatalogLibrary: async (id: string) => ({ success: true, data: structuredClone(items.find(item => item.id === id)) }),
      saveCatalogLibrary: async (id: string, payload: unknown) => { requests.push({ id, payload }); return { success: !shouldFail, message: 'Reranker was deleted' } },
      createCatalogLibrary: async () => { throw new Error('not used') },
      deleteCatalogLibrary: async (id: string) => { requests.push({ delete: id }); items = items.filter(item => item.id !== id); return { success: true } },
      mergeLibraryRelationTypes: async () => ({ success: true }),
    },
    '@/utils/libraryCatalogForm': form, '@/utils/modelTiers': tiers, '@/types/entryClassification': classification,
    '@/components/ModelTierEditor.vue': { __esModule: true, default: defineComponent({ render: () => h('div') }) },
    '@/components/ui': { Button: loadUiComponent('Button'), LibMark: loadUiComponent('LibMark'), Tag: loadUiComponent('Tag'),
      Dialog: defineComponent({ props: ['modelValue'], setup: (props, { slots }) => () => props.modelValue ? h('div', slots.default?.()) : null }),
      toast: { success() {} }, confirm: (options: any) => { dialogs.push(options); return { destroy() {}, update() {} } },
    },
  })
  const root: TestElement = doc.createElement('main')
  doc.body.appendChild(root)
  const app = renderer.createApp(component)
  app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } }))
  app.mount(root)
  const mounted = { root, app, async unmount() { app.unmount(); root.remove(); await settle() } }
  await settle()
  return { ...mounted, requests, events, dialogs, recover() { shouldFail = false },
    async cleanup() { await mounted.unmount(); window.removeEventListener('reliquary:library-settings-saved', saved) },
  }
}
async function input(root: any, selector: string, value: string) {
  const field = root.querySelector(selector)!
  assert.ok(field, selector)
  field.value = value
  await send(field, 'input')
}

test('page edits real fields, saves color and notifies navigation without waiting for a reload', async t => {
  const f = await fixture(); t.after(() => f.cleanup())
  const save = f.root.querySelector('button[type="submit"]')!
  assert.equal(save.disabled, true)
  const summary = f.root.querySelector('textarea[name="prompt-library_summary"]')!
  assert.equal(summary.value, 'Keep key facts')
  const customize = f.root.querySelectorAll('button').filter(node => node.textContent === 'Customize')[1]!
  await send(customize, 'click')
  await input(f.root, 'textarea[name="prompt-library_summary"]', 'Keep conditions and steps')
  assert.equal(f.root.querySelectorAll('input[type="radio"]').length, 8)
  assert.ok(f.root.textContent.includes('Research'), 'built-in content type is localized')
  assert.ok(f.root.querySelectorAll('span').some(node => String(node.props.class).includes('rq-tag')))
  await input(f.root, 'input[name="library-name"]', 'Research notes')
  await input(f.root, 'textarea[name="library-description"]', 'Edited source')
  await input(f.root, 'input[name="chunk-size"]', '800')
  const orange = f.root.querySelector('input[aria-label="Orange"]')!
  orange.checked = true; await send(orange, 'change')
  const feed = f.root.querySelector('input[name="library-feed"]')!
  feed.checked = false; await send(feed, 'change')
  await input(f.root, 'input[aria-label="Add content type"]', 'Memo')
  await send(f.root.querySelector('input[aria-label="Add content type"]')!, 'keydown', { key: 'Enter' })
  assert.equal(save.disabled, false)
  await send(f.root.querySelector('form')!, 'submit')
  assert.equal(f.requests.length, 1)
  assert.equal(f.requests[0].payload.color, 6)
  assert.equal(f.requests[0].payload.name, 'Research notes')
  assert.equal(f.requests[0].payload.description, 'Edited source')
  assert.deepEqual(f.requests[0].payload.prompt_overrides, { library_summary: 'Keep conditions and steps' })
  assert.equal(f.requests[0].payload.chunking_config.chunk_size, 800)
  assert.equal(f.requests[0].payload.chunking_config.chunk_overlap, 0)
  assert.equal(f.requests[0].payload.feed_enabled, false)
  assert.equal(f.requests[0].payload.content_types[1].name, 'Memo')
  assert.deepEqual(f.events, [{ id: 'research', name: 'Research notes', color: 6 }])
  assert.equal(save.disabled, true)
  await send(f.root.querySelectorAll('button').find(node => node.textContent === 'Restore default')!, 'click')
  assert.equal(summary.value, 'Keep key facts')
  await send(f.root.querySelector('form')!, 'submit')
  assert.deepEqual(f.requests[1].payload.prompt_overrides, {})
})

test('failed save retains editable values and dirty state with a visible error', async t => {
  const f = await fixture([library()], true); t.after(() => f.cleanup())
  await input(f.root, 'input[name="library-name"]', 'Keep my draft')
  await send(f.root.querySelectorAll('button').filter(node => node.textContent === 'Customize')[1]!, 'click')
  await input(f.root, 'textarea[name="prompt-library_summary"]', 'Keep my prompt')
  await send(f.root.querySelector('form')!, 'submit')
  assert.equal(f.root.querySelector('input[name="library-name"]')!.value, 'Keep my draft')
  assert.match(f.root.querySelector('[role="alert"]')!.textContent, /Reranker was deleted/)
  assert.equal(f.root.querySelector('button[type="submit"]')!.disabled, false)
  assert.equal(f.events.length, 0)
  assert.equal(f.root.querySelector('textarea[name="prompt-library_summary"]')!.value, 'Keep my prompt')
})

test('nonempty libraries cannot be deleted and an empty last library returns to a compact empty state', async t => {
  const full = await fixture(); t.after(() => full.cleanup())
  const deletion = full.root.querySelectorAll('button').find(node => node.textContent === 'Delete library')!
  assert.equal(deletion.disabled, true)
  assert.match(full.root.textContent, /contains 2 materials/)
  await full.cleanup()
  const empty = await fixture([library('empty', 0)]); t.after(() => empty.cleanup())
  await send(empty.root.querySelectorAll('button').find(node => node.textContent === 'Delete library')!, 'click')
  assert.equal(empty.requests.length, 0)
  await empty.dialogs[0].onConfirm({ dialog: { update() {} } }); await settle()
  assert.deepEqual(empty.requests, [{ delete: 'empty' }])
  assert.equal(empty.root.querySelector('form'), null)
  assert.ok(empty.root.querySelectorAll('button').some(node => /Create/.test(node.textContent)))
})

test('enabling parent-child chunking fills missing sizes and preserves explicitly configured sizes', async t => {
  const fresh = library('fresh', 0)
  Reflect.deleteProperty(fresh.chunking_config, 'parent_chunk_size')
  Reflect.deleteProperty(fresh.chunking_config, 'child_chunk_size')
  const f = await fixture([fresh]); t.after(() => f.cleanup())
  const toggle = f.root.querySelector('input[type="checkbox"]')!
  toggle.checked = true; await send(toggle, 'change')
  assert.equal(f.root.querySelector('input[min="512"]')!.value, 4096)
  assert.equal(f.root.querySelector('input[min="64"]')!.value, 384)
  await send(f.root.querySelector('form')!, 'submit')
  assert.equal(f.requests.length, 1)
  assert.equal(f.requests[0].payload.chunking_config.parent_chunk_size, 4096)
  assert.equal(f.requests[0].payload.chunking_config.child_chunk_size, 384)
  await input(f.root, 'input[min="512"]', '1536')
  await input(f.root, 'input[min="64"]', '256')
  toggle.checked = false; await send(toggle, 'change')
  toggle.checked = true; await send(toggle, 'change')
  assert.equal(Number(f.root.querySelector('input[min="512"]')!.value), 1536)
  assert.equal(Number(f.root.querySelector('input[min="64"]')!.value), 256)
  await send(f.root.querySelector('form')!, 'submit')
  assert.equal(f.requests[1].payload.chunking_config.parent_chunk_size, 1536)
  assert.equal(f.requests[1].payload.chunking_config.child_chunk_size, 256)
})


test('required numeric fields show associated Chinese errors without native validation and recover on correction', async t => {
  const f = await fixture([library()], false, 'zh-CN'); t.after(() => f.cleanup())
  const form = f.root.querySelector('form')!
  assert.ok(form.hasAttribute('novalidate'))
  const toggle = f.root.querySelector('input[name="parent-child"]')!
  toggle.checked = true; await send(toggle, 'change')
  const names = ['chunk-size', 'chunk-overlap', 'parent-chunk-size', 'child-chunk-size']
  for (const name of names) await input(f.root, `input[name="${name}"]`, '')
  await send(form, 'submit')
  assert.equal(f.requests.length, 0)
  for (const name of names) {
    const field = f.root.querySelector(`input[name="${name}"]`)!
    assert.equal(field.props['aria-invalid'], true)
    assert.equal(field.props['aria-describedby'], `${name}-error`)
    assert.equal(f.root.querySelector(`#${name}-error`)!.textContent, '请输入数值。')
  }
  for (const [name, value] of [['chunk-size', '512'], ['chunk-overlap', '0'], ['parent-chunk-size', '4096'], ['child-chunk-size', '384']]) await input(f.root, `input[name="${name}"]`, value!)
  await input(f.root, 'input[name="token-limit"]', '12.5')
  await send(form, 'submit')
  assert.equal(f.requests.length, 0)
  assert.equal(f.root.querySelector('#token-limit-error')!.textContent, '请输入 0 到 8192 之间的整数。')
  await input(f.root, 'input[name="token-limit"]', '')
  await send(form, 'submit')
  assert.equal(f.requests.length, 1)
  assert.equal(f.requests[0].payload.chunking_config.chunk_overlap, 0)
  assert.equal(f.requests[0].payload.chunking_config.token_limit, 0)
  assert.equal(f.root.querySelector('[aria-invalid="true"]'), null)
})

// MEL-06 (small regression): ordinary saves must not request catalog write locks.
test('renaming a managed library omits unchanged catalog configuration on repeated saves', async t => {
  const products = { ...library(), entity_mode: 'managed', entity_schema: { kinds: [{ key: 'model', name: 'Model', parent_kinds: [] }], fields: [] }, relation_types: [{ name: 'related', enabled: true, position: 0 }] } as LibraryCatalogItem
  const f = await fixture([products]); t.after(() => f.cleanup())
  for (const name of ['Products', 'Renamed products']) {
    await input(f.root, 'input[name="library-name"]', name)
    await send(f.root.querySelector('form')!, 'submit')
    const payload = f.requests.at(-1).payload
    assert.equal(payload.name, name)
    for (const key of ['entity_mode', 'entity_schema', 'relation_types']) assert.equal(key in payload, false, key)
  }
})

// MEL-06 (small regression): failed writes must preserve both the draft and loaded baseline.
test('a failed schema save retries the change and advances the baseline only on success', async t => {
  const products = { ...library(), entity_mode: 'managed', entity_schema: { kinds: [], fields: [] } } as LibraryCatalogItem
  const f = await fixture([products], true); t.after(() => f.cleanup())
  await send(f.root.querySelector('button[data-add-field]')!, 'click')
  await send(f.root.querySelector('form')!, 'submit')
  assert.deepEqual(f.requests[0].payload.entity_schema.fields, [{ key: 'size', name: 'Size', type: 'number' }])
  assert.equal(f.root.querySelector('button[type="submit"]')!.disabled, false)
  f.recover()
  await send(f.root.querySelector('form')!, 'submit')
  assert.deepEqual(f.requests[1].payload.entity_schema, f.requests[0].payload.entity_schema)
  await input(f.root, 'input[name="library-name"]', 'Renamed after schema edit')
  await send(f.root.querySelector('form')!, 'submit')
  assert.equal('entity_schema' in f.requests[2].payload, false)
})
