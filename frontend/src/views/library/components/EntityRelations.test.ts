import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import type { ConfirmOptions } from '../../../components/ui/feedback'
import type { RelationGroup } from '../../../api/relations'
import * as dateTime from '../../../utils/dateTime'
import { mount, send, settle as settleDOM } from '../../../components/ui/__tests__/dom'

const location = { heading_path: ['Background'], paragraph: 2, start: 12, end: 50, mapping: 'exact', body_hash: '' }
const groups = [{ relation_type: 'owns', relations: [{ id: 'relation-a', direction: 'outgoing', relation_type: 'owns', peer: { id: 'peer', canonical_name: 'Product' }, library_id: 'lib', library_name: 'Library', source_count: 1, sources: [{ entry_id: 'entry-a', title: 'Evidence', body_hash: 'hash-a', evidence: [{ locations: [location], speaker: { name: 'Speaker' } }] }] }] }] as RelationGroup[]

test('relation table preserves subject direction and evidence hash, and deletion waits for shared confirmation', async t => {
  let confirmation: ConfirmOptions | undefined
  const deleted: string[] = [], opened: unknown[] = [], notices: string[] = []
  let changed = 0, destroyed = 0, fail = true
  const component = loadComponent(new URL('./EntityRelations.vue', import.meta.url), {
    './ManagedAttributes.vue': { __esModule: true, default: defineComponent({ render: () => null }) },
    './ManagedRelationEditor.vue': { __esModule: true, default: defineComponent({ render: () => null }) },
    '@/api/relations': { deleteEntityRelation: async (id: string) => { deleted.push(id); if (fail) throw new Error('Unavailable'); return { success: true } } },
    '@/components/ui/feedback': { confirm: (options: ConfirmOptions) => { confirmation = options; return { destroy: () => destroyed++ } }, toast: { success: (value: string) => notices.push(value) } },
    '@/components/ui': Object.fromEntries(['Button', 'EmptyState', 'ErrorState'].map(name => [name, defineComponent({ props: ['disabled', 'title'], setup: (props, { attrs, slots }) => () => h(name === 'Button' ? 'button' : 'section', { ...attrs, disabled: props.disabled }, [props.title, slots.default?.()]) })])),
  })
  const rows = ref(groups)
  const app = renderer.createApp(defineComponent({ setup: () => () => h(component, { entityId: 'company', entityName: 'Company', groups: rows.value, onOpenSource: (...args: unknown[]) => opened.push(args), onChanged: () => { changed++ } }) }))
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh, 'en-US': en } }))
  const root = element('root'); app.mount(root); t.after(() => app.unmount())
  const cells = descendants(root).filter(node => node.tag === 'td')
  assert.equal(visibleText(cells[0]!), 'Company'); assert.equal(visibleText(cells[2]!), 'Product')
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === 'Evidence')!)
  assert.deepEqual(opened, [['entry-a', { ...location, body_hash: 'hash-a' }]])
  await click(descendants(root).find(node => node.tag === 'button' && visibleText(node) === zh.ikb18.deleteRelation)!)
  assert.deepEqual(deleted, [])
  assert.match(confirmation!.body!, /Product/)
  assert.ok(confirmation!.body!.includes(zh.ikb18.suppressHint))
  await assert.rejects(() => confirmation!.onConfirm!({ dialog: {} as any }) as Promise<void>, /Unavailable/)
  assert.equal(changed, 0)
  fail = false
  await confirmation!.onConfirm!({ dialog: {} as any })
  assert.deepEqual(deleted, ['relation-a', 'relation-a'])
  assert.equal(changed, 1); assert.equal(notices.length, 1)
  rows.value = []; await settle()
  assert.ok(visibleText(root).includes(zh.ikb18.noRelations))
  assert.equal(descendants(root).some(node => node.tag === 'table'), false)
  assert.equal(destroyed, 0)
})

const empty = { __esModule: true, default: defineComponent({ render: () => null }) }
const control = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup: (props, { emit, attrs }) => () => h('input', { ...attrs, value: props.modelValue, onChange: (event: any) => emit('update:modelValue', event.target.value) }) })
const ui = {
  Select: control,
  Combobox: defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup: (props, { emit }) => () => h('input', { 'data-peer': true, value: props.modelValue, onChange: (event: any) => emit('update:modelValue', event.target.value) }) }),
  Button: defineComponent({ props: ['disabled'], setup: (props, { attrs, slots }) => () => h('button', { ...attrs, disabled: props.disabled }, slots.default?.()) }),
  Dialog: defineComponent({ props: ['modelValue'], setup: (props, { slots }) => () => props.modelValue ? h('section', { role: 'dialog' }, slots.default?.()) : null }),
  EmptyState: defineComponent({ render: () => null }), ErrorState: defineComponent({ render: () => null }), toast: { success() {} },
}
const library = { id: 'products', relation_types: [{ name: 'belongs', enabled: true }] }
const entity = { id: 'model', canonical_name: 'Model' }

// Spec (small), MEL-04: the editor converts local business time for both writes.
for (const editing of [false, true]) test(`relation ${editing ? 'edit' : 'creation'} submits the chosen effective time`, async t => {
  const writes: any[] = []
  const component = loadComponent(new URL('./ManagedRelationEditor.vue', import.meta.url), {
    '@/components/ui': ui, '@/utils/dateTime': dateTime, './ManagedAttributes.vue': empty,
    '@/api/entities': { listEntities: async () => ({ success: true, data: [{ id: 'line', canonical_name: 'Line' }] }) },
    '@/api/relations': {
      createManagedRelation: async (id: string, data: any) => { writes.push({ id, data }); return { success: true } },
      updateManagedRelation: async (id: string, data: any) => { writes.push({ id, data }); return { success: true } },
    },
  })
  const relation = editing ? { id: 'relation', version: 2, relation_type: 'belongs', peer: { id: 'line' }, direction: 'outgoing', valid_from: '2025-01-01T00:00:00Z' } : undefined
  const mounted = mount(component, { library, entity, relation }); t.after(() => mounted.unmount())
  const { root } = mounted
  await send(root.querySelector('button')!, 'click')
  root.querySelector('input[data-peer]')!.props.onChange({ target: { value: 'line' } }); await settleDOM()
  const date = root.querySelector('input[type="datetime-local"]')
  assert.ok(date, 'the relation editor offers an effective-time input')
  date.value = '2025-03-01T09:30'; await send(date, 'input')
  await send(root.querySelector('form')!, 'submit')
  assert.deepEqual(writes[0], { id: editing ? 'relation' : 'products', data: { subject_id: 'model', object_id: 'line', relation_type: 'belongs', attributes: {}, valid_from: new Date('2025-03-01T09:30').toISOString(), ...(editing ? { version: 2 } : {}) } })
  await send(root.querySelector('button')!, 'click')
  root.querySelector('input[data-peer]')!.props.onChange({ target: { value: 'line' } }); await settleDOM()
  await send(root.querySelector('form')!, 'submit')
  assert.equal('valid_from' in writes[1].data, false, 'blank uses server time, including when reopening')
})

// Spec (small), MEL-04: ending remains a confirmation and can be backdated.
test('ending a managed relation confirms the optional business end time', async t => {
  const deleted: unknown[][] = []
  const component = loadComponent(new URL('./EntityRelations.vue', import.meta.url), {
    '@/components/ui': ui, '@/utils/dateTime': dateTime,
    './ManagedAttributes.vue': empty, './ManagedRelationEditor.vue': empty,
    '@/components/ui/feedback': { confirm() { throw new Error('managed ending needs a time input') }, toast: { success() {} } },
    '@/api/relations': { deleteEntityRelation: async (...args: unknown[]) => { deleted.push(args); return { success: true } } },
  })
  const managedGroups = [{ ...groups[0], relations: [{ ...groups[0]!.relations[0], version: 1 }] }]
  const mounted = mount(component, { entityId: 'model', entityName: 'Model', managedLibrary: library, entity, groups: managedGroups }); t.after(() => mounted.unmount())
  const { root } = mounted
  await send(root.querySelectorAll('button').find(button => button.textContent === en.managed.endRelation)!, 'click')
  assert.deepEqual(deleted, [])
  const dialog = root.querySelector('[role="dialog"]')
  assert.ok(dialog, 'ending opens a confirmation with an optional end-time input')
  const date = dialog.querySelector('input[type="datetime-local"]')!
  date.value = '2025-04-01T09:30'; await send(date, 'input')
  await send(dialog.querySelector('form')!, 'submit')
  assert.deepEqual(deleted, [['relation-a', 1, new Date('2025-04-01T09:30').toISOString()]])
  assert.equal(root.querySelector('[role="dialog"]'), null)
})
