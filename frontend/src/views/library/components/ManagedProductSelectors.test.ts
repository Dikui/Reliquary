import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { loadComponent } from '../componentTestHarness'
import { doc, mount, send, settle, type TestElement } from '../../../components/ui/__tests__/dom'
import { loadUiComponent } from '../../../components/ui/uiTestLoader'
import * as dateTime from '../../../utils/dateTime'
import * as autoMatch from '../../../utils/entityAutoMatch'

const empty = { __esModule: true, default: defineComponent({ render: () => null }) }
const control = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup: (props, { emit, slots }) => () => h('select', { value: props.modelValue, onChange: (event: any) => emit('update:modelValue', event.target.value) }, slots.default?.()) })
const ui = {
  Combobox: loadUiComponent('Combobox'), Select: control,
  Button: defineComponent({ props: ['disabled', 'loading'], setup: (props, { attrs, slots }) => () => h('button', { ...attrs, disabled: props.disabled || props.loading }, slots.default?.()) }),
  Dialog: defineComponent({ props: ['modelValue'], setup: (props, { slots }) => () => props.modelValue ? h('section', slots.default?.()) : null }),
  toast: { success() {}, error() {} }, confirm() {},
}
const selected = { id: 'chosen', canonical_name: 'Selected model', kind: 'model', code: 'P1', revision: 1, attributes: {} }
const library = { id: 'products', name: 'Products', entity_mode: 'managed', entity_schema: { kinds: [], fields: [] }, relation_types: [{ name: 'related', enabled: true }] }
const entity = { id: 'self', canonical_name: 'Self', kind: 'model', attributes: {} }
const kinds = ['ManagedRelationEditor', 'ManagedReorganization', 'ManagedAttributes'] as const

async function fixture(kind: typeof kinds[number], delayedReference = false) {
  let resolveReference: (value: any) => void = () => {}
  const detail = delayedReference ? new Promise(resolve => { resolveReference = resolve }) : Promise.resolve({ success: true, data: { entity: selected } })
  const searches: any[] = [], currentLibrary = ref(library)
  // API fakes replace product catalogs and detail reads; the real Combobox handles input/IME.
  const component = loadComponent(new URL(`./${kind}.vue`, import.meta.url), {
    '@/components/ui': ui, './ManagedAttributes.vue': empty, './ManagedNameMatching.vue': empty,
    '@/utils/dateTime': dateTime, '@/utils/entityAutoMatch': autoMatch, '@/utils/request': {},
    '@/api/library': { listLibraries: async () => ({ success: true, data: [library, { ...library, id: 'other', name: 'Other' }] }) },
    '@/api/relations': { listEntityRelations: async () => ({ success: true, data: [] }) },
    '@/api/entities': {
      getEntity: () => detail,
      listEntities: async (input: any) => {
        searches.push(input)
        const data = delayedReference || input.entity === 'Absent' || input.offset ? [] : [entity, selected, ...Array.from({ length: 98 }, (_, i) => ({ id: `model-${i}`, canonical_name: `Model ${i}` }))]
        return { success: true, data }
      },
    },
  })
  const mounted = mount({ setup: () => () => h(component, kind === 'ManagedAttributes'
    ? { libraryId: currentLibrary.value.id, fields: [{ key: 'ref', name: 'Reference', type: 'entity_ref' }], modelValue: { ref: selected.id } }
    : { library: currentLibrary.value, entity, ...(delayedReference ? { relation: { id: 'relation', peer: selected, relation_type: 'related' } } : {}) }) })
  await settle()
  if (kind !== 'ManagedAttributes') await send(mounted.root.querySelector('button')!, 'click')
  const combo = () => mounted.root.querySelector('[role="combobox"]')!
  return { ...mounted, searches, combo, currentLibrary, resolveReference }
}

// MEL-06 (small regression): every product picker searches the complete server catalog.
for (const kind of kinds) test(`${kind} searches committed input remotely and paginates the same query`, async t => {
  const f = await fixture(kind); t.after(() => f.unmount())
  const input = f.combo(); input.focus()
  const initial = f.searches.length
  await send(input, 'compositionstart'); input.value = '型号'; await send(input, 'input')
  await new Promise(resolve => setTimeout(resolve, 320))
  assert.equal(f.searches.length, initial)
  await send(input, 'compositionend')
  input.value = 'Model'; await send(input, 'input')
  await new Promise(resolve => setTimeout(resolve, 320)); await settle()
  assert.equal(f.searches.length, initial + 1)
  assert.deepEqual(f.searches.at(-1), { library_id: 'products', entity: 'Model', limit: 100, offset: 0 })
  if (kind !== 'ManagedAttributes') assert.equal(doc.querySelectorAll('[role="option"]').some((n: TestElement) => n.textContent.startsWith('Self')), false)
  await send(f.root.querySelectorAll('button').find(n => n.textContent === 'Load more')!, 'click')
  assert.deepEqual(f.searches.at(-1), { library_id: 'products', entity: 'Model', limit: 100, offset: 100 })
})

// MEL-06 (small regression): changing the result page must not erase selected labels.
for (const kind of kinds) test(`${kind} keeps the selected product name outside the search results`, async t => {
  const f = await fixture(kind); t.after(() => f.unmount())
  const input = f.combo(); input.focus(); await settle()
  await send(doc.querySelectorAll('[role="option"]').find((n: TestElement) => n.textContent.startsWith('Selected model'))!, 'click')
  assert.ok(input.value.startsWith('Selected model'))
  input.focus(); input.value = 'Absent'; await send(input, 'input')
  await new Promise(resolve => setTimeout(resolve, 320)); await settle()
  await send(input, 'keydown', { key: 'Escape' })
  assert.ok(input.value.startsWith('Selected model'), `selected label: ${input.value}`)
})

// MEL-06 (small regression): a library change cancels pending text from the previous picker.
for (const kind of kinds) test(`${kind} clears search text when switching libraries`, async t => {
  const f = await fixture(kind); t.after(() => f.unmount())
  let input = f.combo(); input.focus(); input.value = 'Model'; await send(input, 'input')
  await new Promise(resolve => setTimeout(resolve, 320)); await settle()
  input.value = 'Pending old search'; await send(input, 'input')
  if (kind === 'ManagedAttributes') f.root.querySelector('select')!.props.onChange({ target: { value: 'other' } })
  else f.currentLibrary.value = { ...library, id: 'other' }
  await settle()
  if (kind === 'ManagedReorganization') await send(f.root.querySelector('button')!, 'click')
  input = f.combo()
  assert.notEqual(input.value, 'Pending old search')
  await new Promise(resolve => setTimeout(resolve, 320)); await settle()
  assert.deepEqual(f.searches.at(-1), { library_id: 'other', entity: '', limit: 100, offset: 0 })
  assert.equal(f.searches.some(search => search.entity === 'Pending old search'), false)
})

// MEL-06 (small regression): saved references can be outside every loaded page.
test('a saved reference displays its name when detail loading finishes after the catalog', async t => {
  const f = await fixture('ManagedAttributes', true); t.after(() => f.unmount())
  f.resolveReference({ success: true, data: { entity: selected } }); await settle()
  assert.equal(f.combo().value, 'Selected model')
})

// MEL-06 (small regression): editing an existing off-page peer needs no catalog match.
test('an existing relation displays its peer name outside the first catalog page', async t => {
  const f = await fixture('ManagedRelationEditor', true); t.after(() => f.unmount())
  assert.equal(f.combo().value, 'Selected model')
})

// MEL-06 (small regression): asynchronously resolved names must preserve ongoing input.
test('reference name resolution preserves text currently being searched', async t => {
  const f = await fixture('ManagedAttributes', true); t.after(() => f.unmount())
  const input = f.combo(); input.focus(); input.value = 'Model'; await send(input, 'input')
  f.resolveReference({ success: true, data: { entity: selected } }); await settle()
  assert.equal(input.value, 'Model')
  await new Promise(resolve => setTimeout(resolve, 320)); await settle()
  assert.equal(f.searches.at(-1).entity, 'Model')
})
