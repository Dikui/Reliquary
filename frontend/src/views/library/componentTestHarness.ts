import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { compileScript, parse } from '@vue/compiler-sfc'
import { createRenderer, defineComponent, h, nextTick, ref } from 'vue'
import ts from 'typescript'

const require = createRequire(import.meta.url)

export function loadComponent(url: URL, modules: Record<string, unknown>) {
  const filename = url.pathname
  const { descriptor } = parse(readFileSync(url, 'utf8'), { filename })
  const compiled = compileScript(descriptor, { id: filename, inlineTemplate: true })
  const code = ts.transpileModule(compiled.content, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
  const module = { exports: {} as any }
  new Function('require', 'module', 'exports', code)((name: string) => modules[name] ?? require(name), module, module.exports)
  return module.exports.default
}

export interface Element {
  tag: string
  text: string
  props: Record<string, any>
  style: Record<string, string>
  children: Element[]
  parent: Element | null
  querySelectorAll: (selector: string) => Element[]
}
export const element = (tag: string, text = ''): Element => ({ tag, text, props: {}, style: {}, children: [], parent: null, querySelectorAll: () => [] })
export const descendants = (node: Element): Element[] => [node, ...node.children.flatMap(descendants)]
export const visibleText = (node: Element): string => node.tag === '#comment' ? '' : node.text + node.children.map(visibleText).join('')
export const hasClass = (node: Element, value: string): boolean => String(node.props.class || '').split(' ').includes(value)
export const click = async (node: Element) => {
  if (!node.props.disabled) node.props.onClick?.({ preventDefault() {}, stopPropagation() {} })
  await settle()
}
export const settle = async () => { for (let i = 0; i < 12; i++) await nextTick() }

const teleportTarget = element('body')
export const renderer = createRenderer<Element, Element>({
  createElement: tag => element(tag),
  createText: value => element('#text', value),
  createComment: value => element('#comment', value),
  setText(node, value) { node.text = value },
  setElementText(node, value) { node.children = []; node.text = value },
  parentNode: node => node.parent,
  nextSibling(node) {
    const siblings = node.parent?.children || []
    return siblings[siblings.indexOf(node) + 1] || null
  },
  insert(node, parent, anchor = null) {
    if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1)
    node.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    if (index < 0) parent.children.push(node)
    else parent.children.splice(index, 0, node)
  },
  remove(node) {
    if (node.parent) node.parent.children.splice(node.parent.children.indexOf(node), 1)
    node.parent = null
  },
  patchProp(node, key, _oldValue, value) { node.props[key] = value },
  querySelector: () => teleportTarget,
})

export function registerPrimitives(app: ReturnType<typeof renderer.createApp>) {
  app.component('t-icon', defineComponent({ setup: () => () => h('span') }))
  app.component('t-button', defineComponent({
    props: ['disabled', 'loading', 'size', 'theme', 'variant'],
    setup(props, { attrs, slots }) {
      return () => h('button', { ...attrs, type: 'button', disabled: !!props.disabled, 'data-loading': !!props.loading }, [slots.icon?.(), slots.default?.()])
    },
  }))
  app.component('t-drawer', defineComponent({
    props: ['visible', 'header', 'size', 'placement', 'footer', 'closeBtn', 'destroyOnClose'], emits: ['update:visible'],
    setup(props, { attrs, slots, emit }) {
      return () => props.visible ? h('section', { ...attrs, role: 'dialog', 'aria-label': props.header, 'data-size': props.size }, [
        h('button', { 'data-close-drawer': true, onClick: () => emit('update:visible', false) }, 'Close'), slots.default?.(),
      ]) : null
    },
  }))
  app.component('t-popup', defineComponent({
    props: ['modelValue'], emits: ['update:modelValue'],
    setup(props, { slots, emit }) {
      return () => h('div', { 'data-popup': true }, [
        h('div', { 'data-popup-trigger': true, onClick: () => emit('update:modelValue', !props.modelValue) }, slots.default?.()),
        props.modelValue ? h('div', { 'data-popup-content': true }, slots.content?.()) : null,
      ])
    },
  }))
  app.component('t-popconfirm', defineComponent({
    props: ['disabled', 'content'], emits: ['confirm'],
    setup(props, { slots, emit }) {
      const open = ref(false)
      return () => h('div', { 'data-popconfirm': true, onClick: () => { if (!props.disabled) open.value = true } }, [
        slots.default?.(), open.value ? h('div', { role: 'alertdialog' }, [
          h('p', props.content), h('button', { 'data-confirm': true, onClick: () => { open.value = false; emit('confirm') } }, 'Confirm'),
        ]) : null,
      ])
    },
  }))
  for (const name of ['t-tag', 't-tooltip', 't-loading', 't-input', 't-select', 't-option', 't-textarea', 't-dialog', 't-link', 't-alert']) {
    app.component(name, defineComponent({ props: ['visible'], setup: (props, { slots }) => () =>
      name === 't-dialog' && !props.visible ? null : h('div', slots.default?.()) }))
  }
}

export function mockViewport(width: number) {
  const previous = globalThis.window
  const listeners = new Set<(event: MediaQueryListEvent) => void>()
  const query = {
    matches: width <= 767,
    addEventListener(_event: string, listener: (event: MediaQueryListEvent) => void) { listeners.add(listener) },
    removeEventListener(_event: string, listener: (event: MediaQueryListEvent) => void) { listeners.delete(listener) },
  }
  globalThis.window = { matchMedia: () => query } as unknown as Window & typeof globalThis
  return {
    resize(width: number) {
      query.matches = width <= 767
      for (const listener of listeners) listener({ matches: query.matches } as MediaQueryListEvent)
    },
    listenerCount: () => listeners.size,
    restore() { globalThis.window = previous },
  }
}
