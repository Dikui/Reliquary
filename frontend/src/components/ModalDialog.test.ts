import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import test from 'node:test'
import { defineComponent, h, ref } from 'vue'
import { createI18n } from 'vue-i18n'
import { memoryDom, settleDOM } from '../../test/memoryDom'
import { loadComponent } from '../views/library/componentTestHarness'
import en from '../i18n/locales/en-US'

const require = createRequire(import.meta.url)
for (const editor of ['correction', 'entity'] as const) {
  test(`${editor}: an initially visible real TDesign dialog enters focus, traps Tab, closes with Escape and restores its trigger`, async () => {
    const host = memoryDom(), open = ref(false), errors: unknown[] = []
    let complete!: (value: unknown) => void
    const pendingSave = () => new Promise(resolve => { complete = resolve })
    const { Dialog } = require('tdesign-vue-next')
    const modules: Record<string, unknown> = {
      'tdesign-vue-next': { MessagePlugin: { success() {} } },
      'vue-router': { onBeforeRouteLeave() {}, onBeforeRouteUpdate() {} },
      '@/api/entities': { createEntity: pendingSave },
      '@/api/stories': { splitFact: pendingSave },
    }
    Object.defineProperty(modules, '@/components/ModalDialog.vue', { get: () => loadComponent(new URL('./ModalDialog.vue', import.meta.url), {}) })
    const component = loadComponent(new URL(editor === 'correction' ? '../views/library/components/GroupingCorrection.vue' : '../views/library/components/EntityEditor.vue', import.meta.url), modules)
    const Root = defineComponent({ setup: () => () => h('div', [
      h('button', { id: 'trigger', onClick: () => { open.value = true } }, 'Open'),
      h('button', { id: 'background' }, 'Background'),
      open.value ? h(component, editor === 'correction'
        ? { action: 'split', story: { id: 'story', title: 'Story' }, fact: { id: 'fact', title: 'Fact' }, onCancel: () => { open.value = false }, onSaved: () => { open.value = false } }
        : { entity: null, onClose: () => { open.value = false }, onSaved: () => { open.value = false } }) : null,
    ]) })
    const app = host.renderer.createApp(Root)
    app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en } }))
    app.component('t-dialog', Dialog)
    app.component('t-input', defineComponent({ props: ['modelValue', 'disabled'], setup: props => () => h('input', { value: props.modelValue, disabled: props.disabled }) }))
    app.component('t-select', defineComponent({ setup: () => () => null }))
    app.component('t-button', defineComponent({ props: ['disabled'], setup: (props, { attrs, slots }) => () => h('button', { ...attrs, disabled: props.disabled }, slots.default?.()) }))
    app.config.errorHandler = cause => errors.push(cause)
    try {
      app.mount(host.appRoot); await settleDOM()
      const trigger = host.body.querySelector('#trigger')
      trigger.focus(); trigger.props.onClick(); await settleDOM()
      assert.deepEqual(errors, [])
      const dialog = host.body.querySelector('[role="dialog"]')
      assert.ok(dialog, 'modal has an accessible dialog role')
      assert.equal(dialog.props['aria-modal'], 'true')
      assert.ok(dialog.contains(host.document.activeElement), 'opening transfers focus out of the background')
      for (let i = 0; i < 12; i++) { host.press('Tab', i % 3 === 0); await settleDOM(); assert.ok(dialog.contains(host.document.activeElement)) }
      host.body.querySelector('#background').focus()
      assert.ok(dialog.contains(host.document.activeElement), 'outside programmatic focus is contained')
      host.press('Escape'); await settleDOM()
      assert.equal(open.value, false)
      assert.equal(host.document.activeElement, trigger)
      trigger.props.onClick(); await settleDOM()
      if (editor === 'entity') {
        const input = host.body.querySelector('#entity-canonical-name')
        input.value = 'New institute'; host.dispatch('input', input); await settleDOM()
        host.press('Escape'); await settleDOM()
        const modals = host.body.querySelectorAll('[role="dialog"]').filter((node: any) => node.getClientRects().length)
        assert.equal(modals.length, 2, 'dirty editor opens its own discard confirmation')
        assert.ok(modals[1].contains(host.document.activeElement))
        host.press('Tab', true); await settleDOM()
        assert.ok(modals[1].contains(host.document.activeElement))
        host.press('Escape'); await settleDOM()
        assert.equal(open.value, true, 'Escape dismisses only the top confirmation')
        assert.ok(modals[0].contains(host.document.activeElement))
        host.dispatch('submit', host.body.querySelector('form'))
      } else {
        const save = host.descendants(host.body).find(node => node.tag === 'button' && host.descendants(node).some(child => child.text === en.groupingCorrection.split))
        assert.ok(save); save.props.onClick()
      }
      await settleDOM()
      assert.equal(typeof complete, 'function')
      host.press('Escape'); await settleDOM()
      assert.equal(open.value, true, 'submitting dialog ignores Escape')
      const busyDialog = host.body.querySelector('[role="dialog"]')
      for (let i = 0; i < 5; i++) { host.press('Tab'); await settleDOM(); assert.ok(busyDialog.contains(host.document.activeElement)) }
      complete({ success: true, data: { id: 'saved', story_id: 'new-story' } }); await settleDOM()
      assert.equal(open.value, false)
      assert.equal(host.document.activeElement, trigger)
      assert.deepEqual(errors, [])
    } finally { app.unmount(); await new Promise(resolve => setTimeout(resolve, 320)); host.restore() }
  })
}
