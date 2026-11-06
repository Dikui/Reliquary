import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import en from '../../../i18n/locales/en-US'
import zh from '../../../i18n/locales/zh-CN'
import * as autoMatch from '../../../utils/entityAutoMatch'
import { loadComponent } from '../componentTestHarness'
import { mount, send, settle } from '../../../components/ui/__tests__/dom'

const control = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup: (props, { emit, slots }) => () => h('select', { value: props.modelValue, onChange: (event: any) => emit('update:modelValue', event.target.value) }, slots.default?.()) })
const empty = { __esModule: true, default: defineComponent({ render: () => null }) }
const target = { id: 'target', canonical_name: 'Target', kind: 'product', revision: 1, attributes: { shared: 'Target value', flag: false, zero: 0, legacy: 'Target legacy', legacy_empty: '', legacy_null: null } }
const sources = [
  { id: 'source-a', canonical_name: 'Source A', kind: 'line', revision: 2, attributes: { shared: 'Source value', inherited: 'Inherited A', source_only: 17, legacy: 'Source legacy', retired: ['Retired A', 'Old A'], flag: true, zero: 9, orphan: 'Unknown field' } },
  { id: 'source-b', canonical_name: 'Source B', kind: 'line', revision: 3, attributes: { shared: 'Second value', source_only: 28, retired: ['Retired B'] } },
]
const library = { id: 'products', entity_schema: { kinds: [{ key: 'product', name: 'Product' }, { key: 'line', name: 'Line' }], fields: [
  { key: 'shared', name: 'Shared', type: 'text' }, { key: 'inherited', name: 'Inherited', type: 'text' },
  { key: 'flag', name: 'Flag', type: 'boolean' }, { key: 'zero', name: 'Zero', type: 'number' },
  { key: 'source_only', name: 'Line size', type: 'number', kinds: ['line'] },
  { key: 'legacy', name: 'Legacy', type: 'text', archived: true },
  { key: 'legacy_empty', name: 'Empty legacy', type: 'text', archived: true },
  { key: 'legacy_null', name: 'Null legacy', type: 'text', archived: true },
  { key: 'retired', name: 'Retired list', type: 'list', archived: true },
] } }

async function fixture(locale = 'en-US') {
  const writes: any[] = []
  let confirmation: any
  // API fakes replace catalog/detail reads and capture the public merge payload.
  // Shared dialog/confirmation controls are replaced to isolate this workflow.
  const ui = {
    Combobox: control, Select: control, Input: control, Textarea: control,
    Button: defineComponent({ props: ['disabled', 'loading'], setup: (props, { attrs, slots }) => () => h('button', { ...attrs, disabled: props.disabled || props.loading }, slots.default?.()) }),
    Dialog: defineComponent({ props: ['modelValue'], setup: (props, { slots }) => () => props.modelValue ? h('section', slots.default?.()) : null }),
    toast: { success() {} }, confirm(options: any) { confirmation = options },
  }
  const attributes = loadComponent(new URL('./ManagedAttributes.vue', import.meta.url), { '@/components/ui': ui, '@/api/entities': {}, '@/api/library': {}, '@/utils/request': {} })
  const component = loadComponent(new URL('./ManagedReorganization.vue', import.meta.url), {
    '@/components/ui': ui, '@/utils/entityAutoMatch': autoMatch,
    './ManagedAttributes.vue': { __esModule: true, default: attributes }, './ManagedNameMatching.vue': empty,
    '@/api/relations': { listEntityRelations: async () => ({ success: true, data: [] }) },
    '@/api/entities': {
      listEntities: async () => ({ success: true, data: sources }),
      getEntity: async (id: string) => ({ success: true, data: { entity: sources.find(source => source.id === id) } }),
      mergeManagedEntity: async (id: string, input: any) => { writes.push({ id, ...input }); return { success: true, data: target } },
    },
  })
  const mounted = mount(component, { entity: target, library })
  ;(mounted.app.config.globalProperties.$i18n as any).locale = locale
  await settle()
  const messages = locale === 'zh-CN' ? zh : en
  await send(mounted.root.querySelectorAll('button').find(node => node.textContent === messages.managed.merge)!, 'click')
  return { ...mounted, writes,
    async select(id: string) { mounted.root.querySelector('select')!.props.onChange({ target: { value: id } }); await settle() },
    async submit() { await send(mounted.root.querySelector('form')!, 'submit'); assert.ok(confirmation); await confirmation.onConfirm(); await settle() },
  }
}

// MEL-08 (small regression): hidden source values are disclosed before confirmation;
// only target-compatible values and the target's unchanged archived values are submitted.
for (const locale of ['en-US', 'zh-CN']) test(`merge discloses excluded source attributes and preserves target archived values (${locale})`, async t => {
  const f = await fixture(locale); t.after(() => f.unmount())
  await f.select('source-a')
  const excluded = f.root.querySelector('[data-excluded-source-attributes]')!
  assert.ok(excluded, 'excluded source values must be visible before confirmation')
  assert.ok(excluded.textContent.includes(locale === 'zh-CN' ? '不带入结果的来源属性' : 'Source attributes excluded from the result'))
  assert.ok(excluded.textContent.includes(locale === 'zh-CN' ? '来源的属性历史仍将保留。' : 'The source attribute history is preserved.'))
  for (const value of ['Line size', '17', 'Legacy', 'Source legacy', 'Retired list', 'Retired A', 'Old A', 'orphan', 'Unknown field']) assert.ok(excluded.textContent.includes(value), value)
  assert.doesNotMatch(excluded.textContent, /convert|转换/i)
  const preserved = f.root.querySelector('[data-preserved-archived-attributes]')!
  assert.ok(preserved.textContent.includes('Legacy'))
  assert.ok(preserved.textContent.includes('Target legacy'))
  assert.ok(preserved.textContent.includes('Empty legacy'))
  assert.ok(preserved.textContent.includes('Null legacy'))
  assert.ok(preserved.textContent.includes('""'))
  assert.ok(preserved.textContent.includes('null'))
  await f.submit()
  assert.equal(f.writes[0].id, 'target')
  assert.equal(f.writes[0].source_id, 'source-a')
  assert.deepEqual(f.writes[0].attributes, { ...target.attributes, inherited: 'Inherited A' })
  assert.deepEqual(sources[0].attributes.retired, ['Retired A', 'Old A'])
})

// MEL-08 (small regression): changing source resets both imported edits and exclusions.
test('switching merge sources discards the previous source attributes and disclosure', async t => {
  const f = await fixture(); t.after(() => f.unmount())
  await f.select('source-a')
  const inherited = f.root.querySelectorAll('input').find(node => node.value === 'Inherited A')!
  inherited.props.onInput({ target: { value: 'Edited A' } }); await settle()
  await f.select('source-b')
  const excluded = f.root.querySelector('[data-excluded-source-attributes]')!
  assert.ok(excluded.textContent.includes('28'))
  assert.ok(excluded.textContent.includes('Retired B'))
  assert.doesNotMatch(f.root.textContent, /Inherited A|Edited A|Retired A|Source legacy|Unknown field/)
  await f.submit()
  assert.equal(f.writes[0].source_id, 'source-b')
  assert.deepEqual(f.writes[0].attributes, target.attributes)
})
