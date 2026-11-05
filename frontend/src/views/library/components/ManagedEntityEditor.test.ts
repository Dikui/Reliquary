import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import en from '../../../i18n/locales/en-US'
import { currentNameOverrides } from '../../../utils/entityAutoMatch'
import * as dateTime from '../../../utils/dateTime'
import { loadComponent } from '../componentTestHarness'
import { mount, send, settle } from '../../../components/ui/__tests__/dom'

const empty = { __esModule: true, default: defineComponent({ render: () => null }) }
const control = defineComponent({ props: ['modelValue'], emits: ['update:modelValue'], setup: (props, { emit, slots }) => () => h('input', { value: props.modelValue, onChange: (event: any) => emit('update:modelValue', event.target.value) }, slots.default?.()) })
const ui = {
  Input: control, Select: control, Textarea: control,
  Button: defineComponent({ props: ['disabled'], setup: (props, { attrs, slots }) => () => h('button', { ...attrs, disabled: props.disabled }, slots.default?.()) }),
  Dialog: defineComponent({ props: ['modelValue'], setup: (props, { slots }) => () => props.modelValue ? h('section', slots.default?.()) : null }),
  toast: { success() {}, error() {} }, confirm() {},
}

// Regressions #5 and #6: identity edits avoid snapshots; category edits discard hidden values.
test('product edits only submit changed attributes valid for the chosen category', async t => {
  const writes: any[] = []
  const entity = { id: 'model', canonical_name: 'Model', kind: 'model', revision: 1, attributes: { status: 'active', size: 12 } }
  const component = loadComponent(new URL('./ManagedEntityEditor.vue', import.meta.url), {
    '@/components/ui': ui,
    '@/api/entities': { updateManagedEntity: async (_id: string, data: any) => { writes.push(data); return { success: true, data: entity } } },
    '@/api/operation-logs': { OPERATION_TYPES: [] }, '@/utils/entityAutoMatch': { currentNameOverrides }, '@/utils/dateTime': dateTime,
    './ManagedNameMatching.vue': empty, './ManagedAttributes.vue': empty, './ManagedAttributeHistory.vue': empty,
  })
  const mounted = mount(component, { entity, library: { id: 'products', entity_schema: { kinds: [{ key: 'model', name: 'Model' }, { key: 'line', name: 'Line' }], fields: [{ key: 'status', type: 'text' }, { key: 'size', type: 'number', kinds: ['model'] }] } } })
  const { root } = mounted; t.after(() => mounted.unmount())
  const edit = () => root.querySelectorAll('button').find(n => n.textContent === en.managed.edit)!
  const submit = async () => { root.querySelector('form')!.props.onSubmit({ preventDefault() {} }); await settle() }
  await send(edit(), 'click')
  root.querySelectorAll('input')[0]!.props.onChange({ target: { value: 'Renamed' } }); await settle(); await submit()
  assert.equal(writes[0].name, 'Renamed'); assert.equal('attributes' in writes[0], false)
  await send(edit(), 'click')
  root.querySelectorAll('input')[1]!.props.onChange({ target: { value: 'line' } }); await settle(); await submit()
  assert.deepEqual(writes[1].attributes, { status: 'active' })
  assert.deepEqual(entity.attributes, { status: 'active', size: 12 })
})

// Regression #22: closing an unsaved editor releases only its new uploads.
test('closing the attribute editor releases a newly uploaded attachment', async () => {
  const deleted: string[] = []
  const component = loadComponent(new URL('./ManagedAttributes.vue', import.meta.url), {
    '@/components/ui': ui, '@/api/library': {}, '@/utils/request': {},
    '@/api/entities': {
      uploadEntityAttachment: async () => ({ success: true, data: { id: 'new-upload', name: 'Data.txt' } }),
      deleteEntityAttachment: async (id: string) => { deleted.push(id); return { success: true } },
    },
  })
  const mounted = mount(component, { modelValue: {}, fields: [{ key: 'file', name: 'File', type: 'attachment' }], libraryId: 'products' })
  const { root } = mounted
  const input = root.querySelector('input[type="file"]')!
  await input.props.onChange({ target: { files: [{}], value: 'Data.txt' } }); await settle()
  assert.deepEqual(deleted, [])
  await mounted.unmount(); await settle()
  assert.deepEqual(deleted, ['new-upload'])
})
