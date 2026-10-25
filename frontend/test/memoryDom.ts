import { createRenderer, markRaw, nextTick } from 'vue'

// Native-node adapter for component tests. It runs no browser or layout engine.
export function memoryDom() {
  const previous = new Map<string, PropertyDescriptor | undefined>()
  const descendants = (node: any): any[] => [node, ...node.children.flatMap(descendants)]
  function eventTarget(target: any) {
    const listeners = new Map<string, Set<(event: any) => void>>()
    target.addEventListener = (type: string, listener: (event: any) => void) => { if (!listeners.has(type)) listeners.set(type, new Set()); listeners.get(type)!.add(listener) }
    target.removeEventListener = (type: string, listener: (event: any) => void) => listeners.get(type)?.delete(listener)
    target.listeners = listeners
    return target
  }
  function matches(node: any, selector: string): boolean {
    return selector.split(',').some(part => {
      const value = part.trim()
      if (value === ':disabled') return !!node.props.disabled
      if (value.startsWith('.')) return String(node.props.class || '').split(' ').includes(value.slice(1))
      if (value.startsWith('#')) return node.props.id === value.slice(1)
      const attribute = /^\[([^=\]]+)(?:="([^"]*)")?\]$/.exec(value)
      if (attribute) return attribute[2] === undefined ? node.props[attribute[1]] != null : String(node.props[attribute[1]]) === attribute[2]
      return node.tag === value
    })
  }
  function create(tag: string, text = ''): any {
    const node: any = eventTarget({ tag, tagName: tag.toUpperCase(), nodeType: tag.startsWith('#') ? 3 : 1, text, children: [], parentNode: null, props: {}, style: {}, dataset: {}, offsetWidth: 320, clientWidth: 320, clientHeight: 844, scrollHeight: 844, offsetHeight: 844 })
    node.classList = { add() {}, remove() {}, contains: (value: string) => matches(node, `.${value}`) }
    node.setAttribute = (key: string, value: string) => { node.props[key] = value }
    node.removeAttribute = (key: string) => { delete node.props[key] }
    node.getAttribute = (key: string) => node.props[key]
    node.hasAttribute = (key: string) => node.props[key] != null
    node.getBoundingClientRect = () => ({ width: 320, height: 100, left: 0, top: 0, right: 320, bottom: 100 })
    node.getClientRects = () => { for (let item = node; item; item = item.parentNode) if (item.style.display === 'none' || item.props.hidden) return []; return [node.getBoundingClientRect()] }
    node.focus = () => { documentObject.activeElement = node; dispatch('focusin', node) }
    node.blur = () => { if (documentObject.activeElement === node) documentObject.activeElement = null }
    node.setSelectionRange = () => {}
    node.getRootNode = () => documentObject
    Object.defineProperties(node, {
      ownerDocument: { get: () => documentObject },
      isConnected: { get: () => descendants(body).includes(node) },
      tabIndex: { get: () => node.props.tabindex != null ? Number(node.props.tabindex) : ['button', 'input', 'select', 'textarea'].includes(tag) || (tag === 'a' && node.props.href) ? 0 : -1 },
      options: { get: () => descendants(node).filter(item => item.tag === 'option') },
    })
    node.contains = (child: any) => descendants(node).includes(child)
    node.matches = (selector: string) => matches(node, selector)
    node.querySelectorAll = (selector: string) => descendants(node).slice(1).filter(item => matches(item, selector))
    node.querySelector = (selector: string) => node.querySelectorAll(selector)[0] || null
    node.closest = (selector: string) => { for (let item = node; item; item = item.parentNode) if (matches(item, selector)) return item; return null }
    node.appendChild = (child: any) => { insert(child, node); return child }
    node.removeChild = (child: any) => { remove(child); return child }
    return markRaw(node)
  }
  function remove(node: any) { if (node.parentNode) node.parentNode.children.splice(node.parentNode.children.indexOf(node), 1); node.parentNode = null }
  function insert(node: any, parent: any, anchor?: any) { remove(node); node.parentNode = parent; const index = anchor ? parent.children.indexOf(anchor) : -1; if (index < 0) parent.children.push(node); else parent.children.splice(index, 0, node) }
  const body = create('body'), head = create('head'), documentElement = create('html'), appRoot = create('main')
  insert(appRoot, body)
  const documentObject: any = eventTarget({ body, head, documentElement, activeElement: null, createElement: create, createElementNS: (_ns: string, tag: string) => create(tag), querySelector: (selector: string) => selector === 'body' ? body : selector === 'head' ? head : body.querySelector(selector) })
  const windowObject = eventTarget({ document: documentObject, innerHeight: 844, innerWidth: 390, getComputedStyle: () => ({ visibility: 'visible', transitionDuration: '0s', transitionDelay: '0s', animationDuration: '0s', animationDelay: '0s', transitionProperty: 'none', getPropertyValue: () => '' }) })
  const globals = { document: documentObject, window: windowObject, getComputedStyle: windowObject.getComputedStyle, requestAnimationFrame: (callback: () => void) => setTimeout(callback, 0), cancelAnimationFrame: clearTimeout, HTMLElement: class { static [Symbol.hasInstance](value: any) { return !!value?.tag } }, Element: class { static [Symbol.hasInstance](value: any) { return !!value?.tag } }, Document: class { static [Symbol.hasInstance](value: any) { return value === documentObject } }, ShadowRoot: class {}, SVGElement: class {} }
  for (const [key, value] of Object.entries(globals)) { previous.set(key, Object.getOwnPropertyDescriptor(globalThis, key)); Object.defineProperty(globalThis, key, { configurable: true, writable: true, value }) }
  const renderer = createRenderer<any, any>({
    createElement: create, createText: text => create('#text', text), createComment: text => create('#comment', text),
    setText: (node, value) => { node.text = value }, setElementText: (node, value) => { node.text = value; node.children = [] },
    parentNode: node => node.parentNode, nextSibling: node => node.parentNode?.children[node.parentNode.children.indexOf(node) + 1] || null,
    insert, remove, querySelector: documentObject.querySelector,
    patchProp(node, key, _old, value) { node.props[key] = value; if (key === 'style') Object.assign(node.style, value); if (key === 'value') node.value = value },
  })
  function dispatch(type: string, target: any, details: Record<string, unknown> = {}) {
    let stopped = false, immediate = false
    const event: any = { type, target, defaultPrevented: false, ...details, preventDefault() { this.defaultPrevented = true }, stopPropagation() { stopped = true }, stopImmediatePropagation() { immediate = true; stopped = true } }
    const path = []
    for (let item = target; item; item = item.parentNode) path.push(item)
    path.push(documentObject, windowObject)
    for (const item of path) {
      event.currentTarget = item
      for (const listener of [item.props?.[`on${type[0].toUpperCase()}${type.slice(1)}`], ...(item.listeners?.get(type) || [])].flat().filter(Boolean)) { listener(event); if (immediate) break }
      if (stopped) break
    }
    return event
  }
  function press(key: string, shiftKey = false) {
    const event = dispatch('keydown', documentObject.activeElement || body, { key, code: key, shiftKey })
    if (key === 'Tab' && !event.defaultPrevented) {
      const available = descendants(body).filter(node => node.tabIndex >= 0 && !node.props.disabled && node.getClientRects().length)
      const index = available.indexOf(documentObject.activeElement)
      available[(index + (shiftKey ? -1 : 1) + available.length) % available.length]?.focus()
    }
  }
  return { renderer, document: documentObject, body, appRoot, descendants, press, dispatch,
    restore() { for (const [key, value] of previous) if (value) Object.defineProperty(globalThis, key, value); else Reflect.deleteProperty(globalThis, key) } }
}

export async function settleDOM() { for (let i = 0; i < 8; i++) await nextTick() }
