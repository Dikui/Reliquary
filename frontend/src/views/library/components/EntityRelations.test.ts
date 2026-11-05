import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import zh from '../../../i18n/locales/zh-CN'
import en from '../../../i18n/locales/en-US'
import { click, descendants, element, loadComponent, renderer, settle, visibleText } from '../componentTestHarness'
import type { ConfirmOptions } from '../../../components/ui/feedback'
import type { RelationGroup } from '../../../api/relations'

const location = { heading_path: ['Background'], paragraph: 2, start: 12, end: 50, mapping: 'exact', body_hash: '' }
const groups = [{ relation_type: 'owns', relations: [{ id: 'relation-a', direction: 'outgoing', relation_type: 'owns', peer: { id: 'peer', canonical_name: 'Product' }, library_id: 'lib', library_name: 'Library', source_count: 1, sources: [{ entry_id: 'entry-a', title: 'Evidence', body_hash: 'hash-a', evidence: [{ locations: [location], speaker: { name: 'Speaker' } }] }] }] }] as RelationGroup[]

test('relation table preserves subject direction and evidence hash, and deletion waits for shared confirmation', async t => {
  let confirmation: ConfirmOptions | undefined
  const deleted: string[] = [], opened: unknown[] = [], notices: string[] = []
  let changed = 0, destroyed = 0, fail = true
  const component = loadComponent(new URL('./EntityRelations.vue', import.meta.url), {
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
