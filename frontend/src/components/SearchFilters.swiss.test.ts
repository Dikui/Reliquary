import assert from 'node:assert/strict'
import test from 'node:test'
import * as vue from 'vue'
import { createI18n } from 'vue-i18n'
import { loadUiComponent } from './ui/uiTestLoader'
import { loadComponent, renderer, element, descendants, click, settle, visibleText } from '../views/library/componentTestHarness'
import { CONTENT_TYPES } from '../types/entryClassification'
import * as search from '../utils/search'
import zh from '../i18n/locales/zh-CN'
import en from '../i18n/locales/en-US'
import type { SearchOptions } from '../types/search'

function mountFilters(options: SearchOptions, locale = 'zh-CN') {
  const model = vue.ref(search.defaultSearchFilters())
  const component = loadComponent(new URL('./SearchFilters.vue', import.meta.url), {
    // The renderer supplies native events; DOM-only v-model directives are omitted.
    vue: { ...vue, vModelSelect: {}, vModelText: {} },
    '@/components/ui': Object.fromEntries(['Button', 'Checkbox', 'Select'].map(name => [name, loadUiComponent(name)])),
    '@/types/entryClassification': { CONTENT_TYPES }, '@/utils/search': search,
  })
  const app = renderer.createApp({ render: () => vue.h(component, {
    modelValue: model.value, 'onUpdate:modelValue': (value: typeof model.value) => { model.value = value }, swiss: true,
    options,
  }) })
  app.use(createI18n({ legacy: false, locale, messages: { 'zh-CN': zh, 'en-US': en } }))
  for (const name of ['t-select', 't-date-picker', 't-input-number', 't-switch']) app.component(name, vue.defineComponent({ setup: () => () => null }))
  const root = element('root')
  app.mount(root)
  return { app, root, model }
}

test('Swiss filters retain multiple content types, inclusive date presets and advanced filter values', async context => {
  context.mock.timers.enable({ apis: ['Date'], now: new Date(2026, 9, 28, 12).getTime() })
  const { app, root, model } = mountFilters({ content_types: ['研究', '自定义类型'], entities: [{ id: 'entity', canonical_name: 'Entity', entity_type: 'company' }], topics: [{ id: 'topic', name: 'Topic' }] })
  try {
    const types = descendants(root).filter(node => node.tag === 'input' && node.props.type === 'checkbox')
    types[0]!.props.onChange({ target: { checked: true } })
    types[1]!.props.onChange({ target: { checked: true } })
    const range = descendants(root).find(node => node.props['aria-label'] === zh.searchPage.timeRange)!
    range.props.onChange({ target: { value: '7' } })
    await settle()
    assert.deepEqual(search.buildSearchRequest('query', model.value), { query: 'query', content_types: ['研究', '自定义类型'], published_from: '2026-10-22', published_to: '2026-10-28', collapse_similar: true, limit: 20 })
    range.props.onChange({ target: { value: 'custom' } })
    await settle()
    assert.equal(descendants(root).filter(node => node.tag === 'input' && node.props.type === 'date').length, 2)
    const entitySelect = descendants(root).find(node => node.tag === 'select' && node.props['aria-label'] === zh.search.entities)!
    assert.equal(entitySelect.props.multiple, undefined)
    entitySelect.props.onChange({ target: { value: 'entity' } })
    assert.deepEqual(model.value.entity_ids, ['entity'])
    const stars = descendants(root).find(node => node.tag === 'select' && node.props['aria-label'] === zh.articleJudgments.starLabel)!
    stars.props.onChange({ target: { value: 'false' } })
    assert.equal(model.value.starred, false)
    await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === zh.search.resetFilters)!)
    assert.deepEqual(model.value, search.defaultSearchFilters())
  } finally { app.unmount() }
})

for (const locale of ['zh-CN', 'en-US'] as const) test(`${locale}: empty entity and topic filters show compact explanations and default filters omit reset`, () => {
  const f = mountFilters({ entities: [], topics: [] }, locale)
  try {
    const text = visibleText(f.root)
    assert.ok(text.includes(locale === 'zh-CN' ? '该库还没有实体' : 'This library has no entities yet.'))
    assert.ok(text.includes(locale === 'zh-CN' ? '该库还没有主题' : 'This library has no topics yet.'))
    assert.ok(!descendants(f.root).some(node => node.tag === 'select' && node.props.multiple !== undefined))
    const messages = locale === 'zh-CN' ? zh : en
    assert.ok(!descendants(f.root).some(node => node.tag === 'button' && visibleText(node) === messages.search.resetFilters))
  } finally { f.app.unmount() }
})

test('compact entity and topic pickers preserve multiple IDs, restore their placeholders and support removal', async () => {
  const f = mountFilters({ entities: [{ id: 'e1', canonical_name: 'First', entity_type: 'company' }, { id: 'e2', canonical_name: 'Second', entity_type: 'product' }], topics: [{ id: 't1', name: 'Topic' }] })
  try {
    const entity = descendants(f.root).find(node => node.tag === 'select' && node.props['aria-label'] === zh.search.entities)!
    assert.equal(entity.props.multiple, undefined)
    assert.ok(entity.children.some(node => node.tag === 'option' && node.props.value === ''))
    const event = { target: { value: 'e1' } }
    entity.props.onChange(event)
    assert.equal(event.target.value, '')
    await settle()
    entity.props.onChange({ target: { value: 'e2' } })
    const topic = descendants(f.root).find(node => node.tag === 'select' && node.props['aria-label'] === zh.search.topics)!
    topic.props.onChange({ target: { value: 't1' } })
    await settle()
    assert.deepEqual(f.model.value.entity_ids, ['e1', 'e2'])
    assert.deepEqual(f.model.value.topic_ids, ['t1'])
    const remove = descendants(f.root).find(node => node.tag === 'button' && String(node.props['aria-label']).includes('First'))!
    await click(remove)
    assert.deepEqual(f.model.value.entity_ids, ['e2'])
    const reset = descendants(f.root).find(node => node.tag === 'button' && visibleText(node) === zh.search.resetFilters)!
    await click(reset)
    assert.deepEqual(f.model.value, search.defaultSearchFilters())
    assert.ok(!descendants(f.root).some(node => node.tag === 'button' && visibleText(node) === zh.search.resetFilters))
  } finally { f.app.unmount() }
})
