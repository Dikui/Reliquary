/** Ignore the Enter that commits an IME candidate, including Safari's event ordering. */
export function createImeEnterGuard(now: () => number = () => performance.now()) {
  let composing = false
  let endedAt = -Infinity
  return {
    start() { composing = true; endedAt = -Infinity },
    end() { composing = false; endedAt = now() },
    shouldIgnore(event: Pick<KeyboardEvent, 'key' | 'keyCode' | 'isComposing'>) {
      const enter = event.key === 'Enter' || event.keyCode === 13
      const justEnded = now() - endedAt < 50
      if (!composing && !event.isComposing && event.keyCode !== 229) endedAt = -Infinity
      return (enter || event.keyCode === 229)
        && (composing || event.isComposing || event.keyCode === 229 || justEnded)
    },
  }
}

/** Capture before TDesign emits enter and before Vue's .prevent key modifiers run. */
export function installImeEnterGuard(root: Document, now?: () => number) {
  const guards = new WeakMap<EventTarget, ReturnType<typeof createImeEnterGuard>>()
  function guard(event: Event) {
    const target = event.target as HTMLElement | null
    if (!target || typeof target.matches !== 'function' || !target.matches('input, textarea, [contenteditable="true"]')) return
    let value = guards.get(target)
    if (!value) { value = createImeEnterGuard(now); guards.set(target, value) }
    return value
  }
  const start = (event: Event) => guard(event)?.start()
  const end = (event: Event) => guard(event)?.end()
  const keydown = (event: Event) => {
    if (guard(event)?.shouldIgnore(event as KeyboardEvent)) event.stopImmediatePropagation()
  }
  root.addEventListener('compositionstart', start, true)
  root.addEventListener('compositionend', end, true)
  root.addEventListener('keydown', keydown, true)
  return () => {
    root.removeEventListener('compositionstart', start, true)
    root.removeEventListener('compositionend', end, true)
    root.removeEventListener('keydown', keydown, true)
  }
}
