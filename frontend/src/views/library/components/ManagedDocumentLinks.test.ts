import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../../i18n/locales/en-US'
import zh from '../../../i18n/locales/zh-CN'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'

test('ordinary documents link to the selected product library and keep the selection after failure', async t => {
  const writes: unknown[] = [], listLibraries: string[] = []
  let fail = true
  const select = defineComponent({ props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'], setup: (props, { emit, slots }) => () => h('select', { value: props.modelValue, disabled: props.disabled, onChange: (event: any) => emit('update:modelValue', event.target.value) }, slots.default?.() || props.options?.map((option: any) => h('option', { value: option.value }, option.label))) })
  const component = loadComponent(new URL('./ManagedDocumentLinks.vue', import.meta.url), {
    '@/components/ui': { Select: select, Combobox: select, Button: defineComponent({ props: ['disabled'], setup: (props, { slots, attrs }) => () => h('button', { ...attrs, disabled: props.disabled }, slots.default?.()) }) },
    '@/api/library': { listLibraries: async () => ({ success: true, data: [{ id: 'documents', entity_mode: 'extracted' }, { id: 'products-a', name: 'Products A', entity_mode: 'managed', document_relation_types: ['references', 'test_report'] }, { id: 'products-b', name: 'Products B', entity_mode: 'managed', document_relation_types: ['references', 'specification'] }] }) },
    '@/api/entities': {
      listEntities: async (input: any) => { listLibraries.push(input.library_id); return { success: true, data: [{ id: `${input.library_id}-entity`, canonical_name: 'Model', library_id: input.library_id }] } },
      listEntryEntityLinks: async () => ({ success: true, data: [] }),
      setEntryEntityLink: async (...args: unknown[]) => { writes.push(args); if (fail) throw new Error('Conflict: retry'); return { success: true } },
    },
  })
  const root = element('root'), app = renderer.createApp(component, { entryId: 'document', libraryId: 'documents' })
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en, 'zh-CN': zh } })); app.mount(root); t.after(() => app.unmount()); await settle()
  let controls = descendants(root).filter(n => n.tag === 'select')
  controls[0]!.props.onChange({ target: { value: 'products-b' } }); await settle()
  controls = descendants(root).filter(n => n.tag === 'select')
  controls[1]!.props.onChange({ target: { value: 'specification' } }); controls[2]!.props.onChange({ target: { value: 'products-b-entity' } }); await settle()
  const link = () => descendants(root).find(n => n.tag === 'button' && visibleText(n) === 'Link entity')!
  await click(link()); assert.ok(visibleText(root).includes('Conflict: retry'))
  assert.deepEqual(writes[0], ['document', 'products-b-entity', false, 'specification'])
  assert.equal(descendants(root).filter(n => n.tag === 'select')[2]!.props.value, 'products-b-entity')
  fail = false; await click(link())
  assert.deepEqual(writes[1], writes[0]); assert.ok(listLibraries.includes('products-b'))
})
