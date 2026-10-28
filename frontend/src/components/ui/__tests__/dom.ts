import { createRenderer, markRaw, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import enUS from '../../../i18n/locales/en-US.ts'
import zhCN from '../../../i18n/locales/zh-CN.ts'

// A small DOM adapter for component unit tests. No browser is started. Reka's
// actual focus scopes, keyboard handlers, collection and dismissal logic run.
class TestEvent {
  type: string
  target: any
  currentTarget: any
  defaultPrevented = false
  propagationStopped = false
  immediatePropagationStopped = false
  bubbles = true
  constructor(type: string, options: Record<string, any> = {}) { this.type = type; Object.assign(this, options) }
  preventDefault() { this.defaultPrevented = true }
  stopPropagation() { this.propagationStopped = true }
  stopImmediatePropagation() { this.propagationStopped = true; this.immediatePropagationStopped = true }
  composedPath() { const path: any[] = []; for (let node = this.target; node; node = node.parentNode) path.push(node); return path }
}

class EventNode {
  listeners = new Map<string, Set<(event: any) => void>>()
  addEventListener(type: string, callback: any) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set())
    this.listeners.get(type)!.add(callback)
  }
  removeEventListener(type: string, callback: any) { this.listeners.get(type)?.delete(callback) }
  dispatchEvent(event: any): boolean {
    if (!event.target) event.target = this
    event.currentTarget = this
    for (const listener of [...(this.listeners.get(event.type) ?? [])]) {
      listener(event)
      if (event.immediatePropagationStopped) break
    }
    if (event.bubbles && !event.propagationStopped) (this as any).parentNode?.dispatchEvent(event)
    return !event.defaultPrevented
  }
}

export class TestElement extends EventNode {
  nodeType = 1
  props: Record<string, any> = {}
  childNodes: TestElement[] = []
  parentNode: any = null
  style: any = { setProperty(name: string, value: string) { this[name] = value }, removeProperty(name: string) { delete this[name] } }
  value = ''
  disabled = false
  hidden = false
  checked = false
  text = ''
  clientWidth = 1000
  clientHeight = 600
  offsetWidth = 200
  offsetHeight = 40
  scrollTop = 0
  constructor(public tagName: string) { super(); markRaw(this); this.tagName = tagName.toUpperCase(); if (tagName.startsWith('#')) this.nodeType = tagName === '#text' ? 3 : 8 }
  get children() { return this.childNodes.filter(node => node.nodeType === 1) }
  get ownerDocument(): any { return doc }
  get parentElement(): TestElement | null { return this.parentNode instanceof TestElement ? this.parentNode : null }
  get firstChild() { return this.childNodes[0] ?? null }
  get nextSibling(): TestElement | null { return this.parentNode?.childNodes?.[this.parentNode.childNodes.indexOf(this) + 1] ?? null }
  get isConnected(): boolean { return this === doc.documentElement || !!this.parentNode?.isConnected }
  get tabIndex(): number { return this.props.tabindex !== undefined ? Number(this.props.tabindex) : ['BUTTON', 'INPUT', 'SELECT', 'TEXTAREA'].includes(this.tagName) && !this.disabled ? 0 : -1 }
  get textContent(): string { return (this.nodeType === 8 ? '' : this.text) + this.childNodes.map(node => node.textContent).join('') }
  set textContent(value: string) { this.text = value; this.childNodes = [] }
  get innerText() { return this.textContent }
  get id() { return this.props.id }
  get type() { return this.props.type }
  get dataset() { return Object.fromEntries(Object.entries(this.props).filter(([key, value]) => key.startsWith('data-') && value != null).map(([key, value]) => [key.slice(5).replace(/-([a-z])/g, (_match, letter: string) => letter.toUpperCase()), value])) }
  appendChild(node: TestElement) { this.insertBefore(node, null); return node }
  insertAdjacentElement(position: string, node: TestElement) { this.insertBefore(node, position === 'afterbegin' ? this.firstChild : null) }
  insertBefore(node: TestElement, anchor: TestElement | null) {
    node.remove(); node.parentNode = this
    const index = anchor ? this.childNodes.indexOf(anchor) : -1
    if (index < 0) this.childNodes.push(node); else this.childNodes.splice(index, 0, node)
    return node
  }
  removeChild(node: TestElement) { this.childNodes.splice(this.childNodes.indexOf(node), 1); node.parentNode = null; return node }
  remove() { this.parentElement?.removeChild(this) }
  setAttribute(name: string, value: unknown) { this.props[name] = String(value) }
  getAttribute(name: string) { return this.props[name] === undefined ? null : String(this.props[name]) }
  hasAttribute(name: string) { return this.props[name] !== undefined && this.props[name] !== null }
  removeAttribute(name: string) { delete this.props[name] }
  contains(node: any): boolean { return node === this || this.children.some(child => child.contains(node)) }
  getRootNode() { return doc }
  matches(selector: string): boolean {
    if (selector.includes(',')) return selector.split(',').some(part => this.matches(part.trim()))
    if (selector.includes(':not')) { const [base, excluded] = selector.split(':not('); return this.matches(base || '*') && !this.matches(excluded!.slice(0, -1)) }
    if (selector.startsWith('#')) return this.id === selector.slice(1)
    const attrs = [...selector.matchAll(/\[([^=\]]+)(?:=["']?([^\]"']+)["']?)?\]/g)]
    if (attrs.some(([, name, value]) => !this.hasAttribute(name!) || value !== undefined && this.getAttribute(name!) !== value)) return false
    const tag = selector.split(/[\[:]/)[0]
    return !tag || tag === '*' || this.tagName === tag.toUpperCase()
  }
  querySelectorAll(selector: string): TestElement[] { return this.children.flatMap(child => [...(child.nodeType === 1 && child.matches(selector) ? [child] : []), ...child.querySelectorAll(selector)]) }
  querySelector(selector: string) { return this.querySelectorAll(selector)[0] ?? null }
  closest(selector: string): TestElement | null { return this.matches(selector) ? this : this.parentElement?.closest(selector) ?? null }
  focus() {
    if (this.disabled || doc.activeElement === this) return
    const previous = doc.activeElement
    doc.activeElement = this
    previous?.dispatchEvent(new TestEvent('focusout', { relatedTarget: this }))
    previous?.dispatchEvent(new TestEvent('blur', { bubbles: false, relatedTarget: this }))
    this.dispatchEvent(new TestEvent('focus', { bubbles: false, relatedTarget: previous }))
    this.dispatchEvent(new TestEvent('focusin', { relatedTarget: previous }))
  }
  click() { if (!this.disabled) this.dispatchEvent(new TestEvent('click', { button: 0 })) }
  select() {}
  scrollIntoView() {}
  getBoundingClientRect() { return { x: 0, y: 0, width: 200, height: 40, top: 0, left: 0, right: 200, bottom: 40 } }
  getClientRects() { return [this.getBoundingClientRect()] }
  compareDocumentPosition(other: TestElement) { const all = [doc.documentElement, ...doc.documentElement.querySelectorAll('*')]; return all.indexOf(this) < all.indexOf(other) ? 4 : 2 }
}

const win: any = new EventNode()
export const doc: any = new EventNode()
doc.nodeType = 9
doc.isConnected = true
doc.parentNode = win
doc.documentElement = new TestElement('html')
doc.body = new TestElement('body')
doc.documentElement.appendChild(doc.body)
doc.documentElement.parentNode = doc
doc.activeElement = doc.body
doc.defaultView = win
doc.createElement = (tag: string) => new TestElement(tag)
doc.getElementById = (id: string) => doc.documentElement.querySelector(`#${id}`)
doc.querySelectorAll = (selector: string) => doc.documentElement.querySelectorAll(selector)
doc.querySelector = (selector: string) => doc.documentElement.querySelector(selector)
doc.createTreeWalker = (root: TestElement, _show: number, filter: any) => {
  const nodes = root.querySelectorAll('*').filter(node => filter.acceptNode(node) === 1)
  let index = -1
  return { currentNode: root, nextNode() { this.currentNode = nodes[++index]!; return this.currentNode } }
}
const style = (node: TestElement) => ({ ...node.style, display: node.hidden ? 'none' : 'block', visibility: 'visible', animationName: 'none', direction: 'ltr', getPropertyValue: (key: string) => node.style[key] || '' })
Object.assign(win, { document: doc, navigator: { userAgent: 'node-unit-test', platform: 'Linux' }, innerWidth: 1000, innerHeight: 800, getComputedStyle: style, setTimeout, clearTimeout, requestAnimationFrame: (fn: any) => setTimeout(fn, 0), cancelAnimationFrame: clearTimeout })
for (const [key, value] of Object.entries({
  window: win, document: doc, navigator: win.navigator, Node: TestElement, Element: TestElement,
  HTMLElement: TestElement, HTMLInputElement: TestElement, HTMLSelectElement: TestElement, HTMLTextAreaElement: TestElement,
  SVGElement: class {}, ShadowRoot: class {}, CustomEvent: TestEvent, Event: TestEvent,
  KeyboardEvent: TestEvent, MouseEvent: TestEvent, PointerEvent: TestEvent,
  NodeFilter: { SHOW_ELEMENT: 1, FILTER_ACCEPT: 1, FILTER_SKIP: 3 },
  getComputedStyle: style, requestAnimationFrame: win.requestAnimationFrame, cancelAnimationFrame: clearTimeout,
  MutationObserver: class { observe() {} disconnect() {} }, ResizeObserver: class { observe() {} disconnect() {} },
  IntersectionObserver: class { observe() {} disconnect() {} },
  CSS: { escape: (value: string) => value },
})) Object.defineProperty(globalThis, key, { configurable: true, value })
Object.assign(win, { Node: TestElement, Element: TestElement, HTMLElement: TestElement, ShadowRoot: globalThis.ShadowRoot })

export const renderer = createRenderer<TestElement, TestElement>({
  createElement: tag => new TestElement(tag),
  createText: text => { const node = new TestElement('#text'); node.text = text; return node },
  createComment: text => { const node = new TestElement('#comment'); node.text = text; return node },
  setText: (node, text) => { node.text = text },
  setElementText: (node, text) => { node.textContent = text },
  parentNode: node => node.parentElement,
  nextSibling: node => node.nextSibling,
  insert: (node, parent, anchor) => { parent.insertBefore(node, anchor ?? null) },
  remove: node => node.remove(),
  querySelector: selector => doc.querySelector(selector),
  patchProp(node, key, _previous, value) {
    if (key.startsWith('on')) {
      const event = key.slice(2).replace(/Capture$/, '').toLowerCase()
      if (node.props[key]) for (const callback of [node.props[key]].flat()) node.removeEventListener(event, callback)
      if (value) for (const callback of [value].flat()) node.addEventListener(event, callback)
    } else if (key === 'style') Object.assign(node.style, value)
    else if (key === 'value' || key === 'disabled' || key === 'checked') (node as any)[key] = value
    node.props[key] = value
  },
})
export const settle = async () => { for (let i = 0; i < 15; i++) await nextTick(); await new Promise(resolve => setTimeout(resolve, 5)); for (let i = 0; i < 5; i++) await nextTick() }
export const send = async (node: TestElement, type: string, options: Record<string, any> = {}) => { const event = new TestEvent(type, options); node.dispatchEvent(event); await settle(); return event }
export function mount(component: any, props?: any) {
  const root = new TestElement('main'); doc.body.appendChild(root)
  const app = renderer.createApp(component, props)
  app.use(createI18n({ legacy: false, locale: 'en-US', messages: { 'en-US': enUS, 'zh-CN': zhCN } }))
  app.mount(root)
  return { root, app, async unmount() { app.unmount(); root.remove(); await settle() } }
}
