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
  const input = doc.querySelector('input')!
  input.focus()
  const confirmation = confirm({ header: 'Discard changes', theme: 'danger' })
  await settle()
  assert.equal(doc.querySelectorAll('[role="dialog"]').length, 2)
  await send(doc.querySelector('[data-confirm]')!, 'keydown', { key: 'Escape' })
  assert.equal(await confirmation.result, false)
  assert.equal(open.value, true)
  assert.equal(doc.querySelectorAll('[role="dialog"]').length, 1)
  assert.equal(doc.activeElement === input, true)
  await mounted.unmount()
})

for (const close of ['confirm', 'cancel', 'Escape'] as const) {
  test(`shared confirmation restores its opening focus after ${close}`, async t => {
    const mounted = mount(loadUiComponent('FeedbackHost'))
    const trigger = doc.createElement('button')
    doc.body.appendChild(trigger)
    t.after(async () => { trigger.remove(); await mounted.unmount() })
    trigger.focus()
    const dialog = confirm({ header: 'Confirm the action' })
    // The invoking view can lose focus before the portalled dialog mounts.
    doc.body.focus()
    await settle()
    assert.equal(doc.activeElement === trigger, false, 'the dialog takes focus')
    if (close === 'Escape') await send(doc.activeElement, 'keydown', { key: 'Escape' })
    else await send(doc.querySelector(close === 'confirm' ? '[data-confirm]' : '[data-cancel]')!, 'click')
    assert.equal(await dialog.result, close === 'confirm')
    await settle()
    assert.equal(doc.activeElement === trigger, true, 'focus returns to the opening element')
  })
}

test('closing a confirmation does not focus an opening element removed by its action', async t => {
  const mounted = mount(loadUiComponent('FeedbackHost'))
  const trigger = doc.createElement('button')
  doc.body.appendChild(trigger)
  t.after(async () => { trigger.remove(); await mounted.unmount() })
  trigger.focus()
  const focus = t.mock.method(trigger, 'focus')
  const dialog = confirm({ header: 'Delete this item', onConfirm: () => { trigger.remove() } })
  await settle()
  await send(doc.querySelector('[data-confirm]')!, 'click')
  assert.equal(await dialog.result, true)
  await settle()
  assert.equal(focus.mock.callCount(), 0)
})

test('confirmation focus waits for the invoking button to be enabled again', async t => {
  const disabled = ref(false)
  const Feedback = loadUiComponent('FeedbackHost')
  let dialog: ReturnType<typeof confirm>
  const mounted = mount({ setup: () => () => h('div', [
    h('button', { 'data-opener': '', disabled: disabled.value, onClick: () => {
      disabled.value = true
      dialog = confirm({ header: 'Confirm changes' })
      void dialog.result.finally(() => { disabled.value = false })
      doc.body.focus()
    } }, 'Open'),
    h(Feedback),
  ]) })
  t.after(() => mounted.unmount())
  const trigger = doc.querySelector('[data-opener]')!
  trigger.focus()
  await send(trigger, 'click')
  assert.equal(trigger.disabled, true)
  await send(doc.querySelector('[data-cancel]')!, 'click')
  assert.equal(trigger.disabled, false)
  assert.equal(doc.activeElement === trigger, true)
})

test('closing one queued confirmation does not steal focus from the next dialog', async t => {
  const mounted = mount(loadUiComponent('FeedbackHost'))
  const trigger = doc.createElement('button')
  doc.body.appendChild(trigger)
  t.after(async () => { trigger.remove(); await mounted.unmount() })
  trigger.focus()
  const first = confirm({ header: 'First action' })
  const second = confirm({ header: 'Second action' })
  await settle()
  await send(doc.querySelector('[data-cancel]')!, 'click')
  assert.equal(await first.result, false)
  assert.ok(doc.querySelector('[role="dialog"]')?.textContent.includes('Second action'))
  assert.equal(doc.activeElement === trigger, false)
  await send(doc.querySelector('[data-cancel]')!, 'click')
  assert.equal(await second.result, false)
  assert.equal(doc.activeElement === trigger, true)
})
