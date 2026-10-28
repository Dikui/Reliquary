import assert from 'node:assert/strict'
import test from 'node:test'
import { doc, mount, send, settle } from './__tests__/dom.ts'
import { loadUiComponent } from './uiTestLoader.ts'
import { createRequire } from 'node:module'
import { h, nextTick, ref } from 'vue'
const { confirm, toast } = createRequire(import.meta.url)('./feedback.ts') as typeof import('./feedback.ts')

test('toast service renders live feedback, deduplicates, supports close and expires', async t => {
  const mounted = mount(loadUiComponent('FeedbackHost'))
  const first = toast.success('Saved', 0)
  toast.success('Saved', 0)
  await settle()
  assert.equal(doc.querySelectorAll('[data-toast]').length, 1)
  assert.equal(doc.querySelector('[role="status"]')?.textContent.includes('Saved'), true)
  first.close()
  await settle()
  assert.equal(doc.querySelectorAll('[data-toast]').length, 0)
  t.mock.timers.enable({ apis: ['setTimeout'] })
  toast.error('Failed', 1000)
  await nextTick()
  assert.equal(doc.querySelector('[role="alert"]')?.textContent.includes('Failed'), true)
  t.mock.timers.tick(1000)
  await nextTick()
  assert.equal(doc.querySelectorAll('[data-toast]').length, 0)
  toast.clear()
  t.mock.timers.reset()
  await mounted.unmount()
})

test('confirm waits for an async action, prevents duplicates and retains failed confirmation for retry', async () => {
  const mounted = mount(loadUiComponent('FeedbackHost'))
  let attempts = 0, release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  const dialog = confirm({ header: 'Delete library', body: 'This removes the empty library.', confirmBtn: 'Delete', onConfirm: async () => {
    attempts++
    if (attempts === 1) { await pending; throw new Error('Connection lost') }
  } })
  await settle()
  const button = doc.querySelector('[data-confirm]')!
  await send(button, 'click')
  assert.equal(button.disabled, true)
  await send(button, 'click')
  assert.equal(attempts, 1)
  release()
  await settle()
  assert.ok(doc.querySelector('[role="alert"]'))
  assert.ok(doc.querySelector('[role="dialog"]'))
  await send(button, 'click')
  assert.equal(await dialog.result, true)
  assert.equal(attempts, 2)
  await mounted.unmount()
})

test('confirm cancel, Escape and destroy settle once without calling the action', async () => {
  const mounted = mount(loadUiComponent('FeedbackHost'))
  let actions = 0, cancelled = 0, closed = 0
  const options = { header: 'Confirm', body: 'Continue?', onConfirm: () => { actions++ }, onCancel: () => { cancelled++ }, onClose: () => { closed++ } }
  const first = confirm(options)
  await settle()
  await send(doc.querySelector('[data-cancel]')!, 'click')
  assert.equal(await first.result, false)
  const second = confirm(options)
  await settle()
  await send(doc.activeElement, 'keydown', { key: 'Escape' })
  assert.equal(await second.result, false)
  const third = confirm(options)
  third.destroy(); third.destroy()
  assert.equal(await third.result, false)
  assert.equal(actions, 0)
  assert.equal(cancelled, 1)
  assert.equal(closed, 1)
  await mounted.unmount()
})

test('a stable danger confirmation closes with Escape from the confirm button', async () => {
  const mounted = mount(loadUiComponent('FeedbackHost'))
  const dialog = confirm({ header: 'Delete material', theme: 'danger', confirmBtn: 'Delete' })
  await settle()
  await settle()
  const button = doc.querySelector('[data-confirm]')!
  button.focus()
  await send(button, 'keydown', { key: 'Escape' })
  assert.equal(doc.querySelector('[role="dialog"]'), null)
  assert.equal(await dialog.result, false)
  await mounted.unmount()
})

test('Escape is blocked during submission and closes the same confirmation after a failure', async () => {
  const mounted = mount(loadUiComponent('FeedbackHost'))
  let reject!: (error: Error) => void
  const dialog = confirm({ header: 'Delete material', theme: 'danger', onConfirm: () => new Promise((_resolve, fail) => { reject = fail }) })
  await settle()
  await send(doc.querySelector('[data-confirm]')!, 'click')
  await send(doc.activeElement, 'keydown', { key: 'Escape' })
  assert.ok(doc.querySelector('[role="dialog"]'))
  reject(new Error('Failed'))
  await settle()
  assert.ok(doc.querySelector('[role="alert"]'))
  await send(doc.querySelector('[data-confirm]')!, 'keydown', { key: 'Escape' })
  assert.equal(doc.querySelector('[role="dialog"]'), null)
  assert.equal(await dialog.result, false)
  await mounted.unmount()
})

test('a confirmation owns Escape above an existing shared dialog', async () => {
  const open = ref(true), Dialog = loadUiComponent('Dialog'), Feedback = loadUiComponent('FeedbackHost')
  const mounted = mount({ setup: () => () => h('div', [h(Dialog, { modelValue: open.value, title: 'Edit material', 'onUpdate:modelValue': (value: boolean) => { open.value = value } }, () => h('input')), h(Feedback)]) })
  await settle()
  const confirmation = confirm({ header: 'Discard changes', theme: 'danger' })
  await settle()
  assert.equal(doc.querySelectorAll('[role="dialog"]').length, 2)
  await send(doc.querySelector('[data-confirm]')!, 'keydown', { key: 'Escape' })
  assert.equal(await confirmation.result, false)
  assert.equal(open.value, true)
  assert.equal(doc.querySelectorAll('[role="dialog"]').length, 1)
  await mounted.unmount()
})
