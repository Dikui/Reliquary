import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../../i18n/locales/en-US'
import zh from '../../../i18n/locales/zh-CN'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../componentTestHarness'

const entry = (id: string, published_at = '') => ({ id, title: `Title ${id}`, original_url: `https://${id}.example/a`, published_at, created_at: '2026-10-25T00:00:00Z' })

test('duplicate cleanup previews groups and deletes only the listed copies in batches', async () => {
  const listed: string[] = [], deleted: string[][] = []
  const emitted: string[][] = []
  const removable = Array.from({ length: 201 }, (_, i) => entry(`copy${i}`))
  const component = loadComponent(new URL('./DuplicateCleanupDialog.vue', import.meta.url), {
    '@/api/library': {
      listLibraryDuplicates: async (id: string) => { listed.push(id); return { success: true, data: [{ keep: entry('keep', '2026-10-18'), remove: removable }] } },
      batchDeleteEntry: async (ids: string[]) => { deleted.push(ids); return { success: true } },
    },
    'tdesign-vue-next': { MessagePlugin: { success: () => {} } },
  })
  const root = element('root')
  const app = renderer.createApp(defineComponent({
    data: () => ({ visible: false }),
    render() {
      return h(component, {
        visible: this.visible, 'onUpdate:visible': (value: boolean) => { this.visible = value },
        libraries: [{ id: 'lib-a', name: 'A' }, { id: 'lib-b', name: 'B' }], defaultLibraryId: 'lib-b', onDeleted: (ids: string[]) => emitted.push(ids),
      })
    },
  }))
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en, 'zh-CN': zh } }))
  registerPrimitives(app)
  const vm = app.mount(root) as unknown as { visible: boolean }
  vm.visible = true; await settle()
  const button = (label: string) => descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)!
  await click(button(en.duplicateCleanup.find)); await settle()
  assert.deepEqual(listed, ['lib-b'])
  assert.ok(visibleText(root).includes('Title keep'))
  await click(button(en.duplicateCleanup.confirm.replace('{count}', '201'))); await settle()
  assert.deepEqual(deleted.map(ids => ids.length), [200, 1])
  assert.ok(!deleted.flat().includes('keep'))
  assert.deepEqual(emitted, [deleted.flat()])
  assert.equal(vm.visible, false)
  app.unmount()
})
