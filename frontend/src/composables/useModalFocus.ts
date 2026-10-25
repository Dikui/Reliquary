import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue'

const modalStack: symbol[] = []

// Modal keyboard ownership is shared so a discard confirmation suspends its editor.
export function useModalFocus(visible: Ref<boolean>, root: () => HTMLElement | null, busy: () => boolean, close: () => void) {
  const key = Symbol('modal')
  let previous: HTMLElement | null = null, active = false, version = 0
  const isTop = () => active && modalStack.at(-1) === key
  function focusable() {
    return Array.from(root()?.querySelectorAll<HTMLElement>('button, [href], input, select, textarea, [tabindex]') || [])
      .filter(node => node.tabIndex >= 0 && !node.matches(':disabled, [inert]') && node.getClientRects().length > 0 && getComputedStyle(node).visibility !== 'hidden')
  }
  function focusInside() {
    const dialog = root()
    const current = document.activeElement as HTMLElement | null
    if (isTop() && dialog && (!dialog.contains(current) || current?.matches(':disabled'))) (focusable()[0] || dialog).focus()
  }
  function keydown(event: KeyboardEvent) {
    if (!isTop() || event.defaultPrevented || event.isComposing) return
    if (event.key === 'Escape') {
      event.preventDefault(); event.stopImmediatePropagation()
      if (!busy()) close()
    } else if (event.key === 'Tab') {
      const items = focusable(), index = items.indexOf(document.activeElement as HTMLElement)
      if (!items.length || index < 0 || (event.shiftKey ? index === 0 : index === items.length - 1)) {
        event.preventDefault()
        const target = items.length ? items[event.shiftKey ? items.length - 1 : 0] : root()
        target?.focus()
      }
    }
  }
  function deactivate() {
    version++
    if (!active) return
    const restore = isTop()
    active = false
    modalStack.splice(modalStack.indexOf(key), 1)
    document.removeEventListener('keydown', keydown)
    document.removeEventListener('focusin', focusInside)
    if (restore && previous?.isConnected) previous.focus()
  }
  watch(visible, async value => {
    deactivate()
    if (!value) return
    previous = document.activeElement as HTMLElement | null
    active = true; modalStack.push(key)
    document.addEventListener('keydown', keydown)
    document.addEventListener('focusin', focusInside)
    const ticket = version
    await nextTick()
    if (ticket === version) focusInside()
  })
  onBeforeUnmount(deactivate)
  return { focusInside }
}
