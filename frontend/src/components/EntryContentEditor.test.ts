import assert from 'node:assert/strict'
import test from 'node:test'
import { routerKey } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { loadUiComponent } from './ui/uiTestLoader'
import zh from '../i18n/locales/zh-CN'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../views/library/componentTestHarness'

async function mountEditor(update: (id: string, data: any) => Promise<any>) {
  const previousWindow = globalThis.window
  const listeners = new Map<string, any>()
  globalThis.window = { addEventListener: (key: string, callback: any) => listeners.set(key, callback), removeEventListener: (key: string) => listeners.delete(key) } as any
  let body = { content: '# Full body\n\n' + '完整正文😀 '.repeat(12000) + '\n\n![Chart](../attachments/entry/chart.png)', bodyHash: 'a'.repeat(64) }
  let dialog: any, guard: any
  const saved: any[] = [], copies: string[] = [], notices: string[] = []
  const component = loadComponent(new URL('./EntryContentEditor.vue', import.meta.url), {
    'vue-router': { routerKey },
    '@/api/library': { getEntryEditableBody: async () => body, updateEntryContent: update },
    '@/utils/clipboard': { copyToClipboard: async (value: string) => { copies.push(value); return true } },
    '@/components/ui': { Button: loadUiComponent('Button'), Textarea: loadUiComponent('Textarea'), confirm: (options: any) => { dialog = options; return { destroy() {} } }, toast: { success: (message: string) => notices.push(message) } },
  })
  const root = element('root')
  const app = renderer.createApp(component, { entryId: 'entry', title: 'Original title', indexOnly: false, onSaved: (value: any) => saved.push(value) })
  app.use(createI18n({ legacy: false, locale: 'zh-CN', messages: { 'zh-CN': zh } }))
  app.provide(routerKey, { beforeEach: (callback: any) => { guard = callback; return () => { guard = undefined } } } as any)
  registerPrimitives(app)
  app.mount(root)
  await settle()
  const input = () => descendants(root).find(node => node.tag === 'textarea')!
  const button = (label: string) => descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)!
  return { root, saved, copies, notices, input, button,
    change: async (value: string) => { input().props.onInput({ target: { value } }); await settle() },
    latest: (content: string) => { body = { content, bodyHash: 'b'.repeat(64) } },
    dialog: () => dialog, guard: () => guard(),
    close: () => { app.unmount(); assert.equal(listeners.size, 0); globalThis.window = previousWindow },
  }
}

test('edit and save the complete Markdown with its base hash, preserving entry ID and preventing duplicate submits', async () => {
  const requests: any[] = []
  let complete!: (value: any) => void
  const editor = await mountEditor((id, data) => { requests.push({ id, data }); return new Promise(resolve => { complete = resolve }) })
  try {
    assert.ok(editor.input().props.value.length > 65536)
    const draft = editor.input().props.value + '\n\nAn exact correction.\n'
    assert.equal(editor.button(zh.entryEditor.saveProcess).props.disabled, true)
    await editor.change(draft)
    await click(editor.button(zh.entryEditor.saveProcess))
    await click(editor.button(zh.entryEditor.saveProcess))
    assert.deepEqual(requests, [{ id: 'entry', data: { content: draft, base_body_hash: 'a'.repeat(64) } }])
    assert.equal(editor.input().props.disabled, true)
    complete({ success: true, data: { entry_id: 'entry', status: 'accepted' } })
    await settle()
    assert.deepEqual(editor.saved, [{ id: 'entry', content: draft, status: 'accepted' }])
    assert.deepEqual(editor.notices, [zh.entryEditor.savedPending])
  } finally { editor.close() }
})

test('a body conflict retains the draft, blocks navigation, and reloads only after explicit discard', async () => {
  const editor = await mountEditor(async () => { throw { status: 409, error: { details: { reason: 'body_changed' } } } })
  try {
    const draft = '# My correction\n\nKeep this draft.'
    await editor.change(draft)
    await click(editor.button(zh.entryEditor.saveProcess))
    assert.ok(visibleText(editor.root).includes(zh.entryEditor.conflict))
    assert.equal(editor.input().props.value, draft)
    assert.equal(editor.button(zh.entryEditor.saveProcess).props.disabled, true)
    await click(editor.button(zh.entryEditor.copyDraft))
    assert.deepEqual(editor.copies, [draft])
    const leaving = editor.guard()
    editor.dialog().onCancel()
    assert.equal(await leaving, false)
    assert.equal(editor.input().props.value, draft)
    editor.latest('# Newest body')
    await click(editor.button(zh.entryEditor.reload))
    assert.equal(editor.input().props.value, draft)
    editor.dialog().onConfirm()
    await settle()
    assert.equal(editor.input().props.value, '# Newest body')
    assert.equal(editor.button(zh.entryEditor.saveProcess).props.disabled, true)
    assert.deepEqual(editor.saved, [])
  } finally { editor.close() }
})
