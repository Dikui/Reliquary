import assert from 'node:assert/strict'
import test from 'node:test'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '../../../i18n/locales/en-US'
import zh from '../../../i18n/locales/zh-CN'
import { click, descendants, element, loadComponent, registerPrimitives, renderer, settle, visibleText } from '../componentTestHarness'

const time = '2026-10-24T16:00:00Z'
const story = { id: 'origin', title: 'Original story', first_fact_at: time, last_fact_at: time }
const target = { ...story, id: 'target', title: 'Retained story' }
const fact = { id: 'fact', title: 'Original fact', story_id: story.id }
const kept = { ...fact, id: 'kept', title: 'Retained fact', story_id: target.id }

for (const locale of ['en-US', 'zh-CN'] as const) {
  for (const action of ['move', 'split', 'mergeFacts', 'mergeStories'] as const) {
    test(`${locale}: ${action} preserves failed input, blocks duplicate saves and announces its destination`, async () => {
      let calls = 0, fail = true, resolveSave!: (value: unknown) => void
      const inputs: unknown[][] = [], saved: unknown[] = [], notices: string[] = []
      const mutate = (...args: unknown[]) => {
        calls++; inputs.push(args)
        if (fail) return Promise.reject(new Error('offline'))
        return new Promise(resolve => { resolveSave = resolve })
      }
      const component = loadComponent(new URL('./GroupingCorrection.vue', import.meta.url), {
        '@/components/ModalDialog.vue': defineComponent({ setup: (_props, { slots }) => () => h('div', slots.default?.()) }),
        '@/api/stories': {
          listStories: async () => ({ success: true, data: { items: [story, target], total: 2 } }),
          getStory: async () => ({ success: true, data: { story: target, facts: [kept] } }),
          moveFact: mutate, splitFact: mutate, mergeFacts: mutate, mergeStories: mutate,
        },
        'tdesign-vue-next': { MessagePlugin: { success: (message: string) => notices.push(message) } },
      })
      const root = element('root'), app = renderer.createApp(component, { action, story, fact: action === 'mergeStories' ? undefined : fact, onSaved: (value: unknown) => saved.push(value) })
      app.use(createI18n({ legacy: false, locale, messages: { 'en-US': en, 'zh-CN': zh } }))
      registerPrimitives(app)
      for (const name of ['t-select', 't-input']) {
        app.component(name, defineComponent({ props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'], setup: (props, { attrs, emit }) => () => h(name === 't-input' ? 'input' : 'select', { ...attrs, value: props.modelValue, options: props.options, disabled: props.disabled, onChange: (value: string) => emit('update:modelValue', value) }) }))
      }
      app.config.warnHandler = warning => { if (!warning.includes('already been registered')) throw new Error(warning) }
      try {
        app.mount(root); await settle()
        const bundle = locale === 'en-US' ? en : zh
        if (action !== 'split') {
          const select = descendants(root).find(node => node.tag === 'select')!
          const options = select.props.options as { value: string }[]
          assert.equal(options.some(item => item.value === 'origin'), action === 'mergeFacts')
          select.props.onChange('target'); await settle()
          if (action === 'mergeFacts') { descendants(root).filter(node => node.tag === 'select')[1].props.onChange('kept'); await settle() }
        }
        const submit = () => descendants(root).find(node => node.tag === 'button' && visibleText(node) === bundle.groupingCorrection[action])!
        await click(submit())
        assert.equal(calls, 1)
        assert.ok(visibleText(root).includes(bundle.groupingCorrection.failed))
        if (action !== 'split') assert.equal(descendants(root).find(node => node.tag === 'select')!.props.value, 'target')
        fail = false
        await click(submit()); await click(submit())
        assert.equal(calls, 2)
        assert.equal(submit().props.disabled, true)
        const data = action === 'mergeStories' ? { story: target } : { ...kept, id: action === 'mergeFacts' ? 'kept' : 'fact' }
        resolveSave({ success: true, data }); await settle()
        assert.deepEqual(inputs[0], action === 'move' ? ['fact', 'target'] : action === 'split' ? ['fact', 'Original fact'] : action === 'mergeFacts' ? ['fact', 'kept'] : ['origin', 'target'])
        assert.equal(saved.length, 1)
        assert.deepEqual(saved[0], action === 'mergeStories' ? { storyID: 'target' } : { storyID: 'target', factID: action === 'mergeFacts' ? 'kept' : 'fact' })
        assert.deepEqual(notices, [bundle.groupingCorrection.saved])
        if (action.startsWith('merge')) assert.ok(visibleText(root).includes(bundle.groupingCorrection.mergeHint))
      } finally { app.unmount(); await settle() }
    })
  }
}

test('merge target selection retries failed lists, loads further stories and ignores older fact results', async () => {
  let fail = true, finishOld!: (value: unknown) => void
  const pages: unknown[] = []
  const component = loadComponent(new URL('./GroupingCorrection.vue', import.meta.url), {
    '@/components/ModalDialog.vue': defineComponent({ setup: (_props, { slots }) => () => h('div', slots.default?.()) }),
    '@/api/stories': {
      listStories: async (limit: number, offset: number) => {
        pages.push([limit, offset])
        if (fail) throw new Error('offline')
        return { success: true, data: { items: offset ? [{ ...target, id: 'third' }] : [story, target], total: 3 } }
      },
      getStory: (id: string) => id === 'target' ? new Promise(resolve => { finishOld = resolve })
        : Promise.resolve({ success: true, data: { story, facts: [{ ...kept, id: 'current' }, fact] } }),
    },
    'tdesign-vue-next': { MessagePlugin: { success() {} } },
  })
  const root = element('root'), app = renderer.createApp(component, { action: 'mergeFacts', story, fact })
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en } }))
  registerPrimitives(app)
  app.component('t-select', defineComponent({ props: ['modelValue', 'options'], emits: ['update:modelValue'], setup: (props, { attrs, emit }) => () => h('select', { ...attrs, options: props.options, onChange: (value: string) => emit('update:modelValue', value) }) }))
  const button = (label: string) => descendants(root).find(node => node.tag === 'button' && visibleText(node) === label)!
  const selects = () => descendants(root).filter(node => node.tag === 'select')
  try {
    app.mount(root); await settle()
    assert.ok(visibleText(root).includes(en.groupingCorrection.loadFailed))
    fail = false
    await click(button(en.common.retry))
    await click(button(en.common.loadMore))
    assert.deepEqual(pages, [[50, 0], [50, 0], [50, 2]])
    assert.equal(selects()[0].props.options.length, 3)
    selects()[0].props.onChange('target'); await settle()
    selects()[0].props.onChange('origin'); await settle()
    assert.deepEqual(selects()[1].props.options.map((item: { value: string }) => item.value), ['current'])
    finishOld({ success: true, data: { story: target, facts: [kept] } }); await settle()
    assert.deepEqual(selects()[1].props.options.map((item: { value: string }) => item.value), ['current'])
  } finally { app.unmount(); await settle() }
})

test('long destination titles use trigger-sized popup layers inside the modal at a 390px viewport', async () => {
  const previousWindow = globalThis.window
  globalThis.window = { innerWidth: 390 } as Window & typeof globalThis
  const component = loadComponent(new URL('./GroupingCorrection.vue', import.meta.url), {
    '@/components/ModalDialog.vue': defineComponent({ setup: (_props, { slots }) => () => h('section', { role: 'dialog' }, slots.default?.()) }),
    '@/api/stories': { listStories: async () => ({ success: true, data: { items: [{ ...target, title: 'Long title '.repeat(200) }], total: 1 } }) },
    'tdesign-vue-next': { MessagePlugin: {} },
  })
  const root = element('root'), app = renderer.createApp(component, { action: 'move', story, fact })
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': en } }))
  registerPrimitives(app)
  app.component('t-select', defineComponent({ props: ['popupProps'], setup: props => () => h('select', { popupProps: props.popupProps }) }))
  try {
    app.mount(root); await settle()
    const popup = descendants(root).find(node => node.tag === 'select')!.props.popupProps
    assert.ok(popup, 'the selector overrides intrinsic option-width sizing')
    const trigger = { getBoundingClientRect: () => ({ width: 310 }) } as HTMLElement
    const oversizedTrigger = { getBoundingClientRect: () => ({ width: 560 }) } as HTMLElement
    for (const style of [popup.overlayStyle, popup.overlayInnerStyle]) {
      assert.equal(style(trigger).width, '310px')
      assert.equal(style(oversizedTrigger).width, '358px')
      assert.equal(style(trigger).boxSizing, 'border-box')
    }
    assert.equal(popup.overlayClassName, 'grouping-target-popup')
    const label = descendants(root).find(node => node.tag === 'label')!
    const dialog = descendants(root).find(node => node.props.role === 'dialog')!
    Object.assign(label, { closest: () => dialog })
    assert.equal(popup.attach(), dialog, 'popup remains in the modal focus boundary without the scrolling body clipping it')
  } finally { app.unmount(); await settle(); globalThis.window = previousWindow }
})
