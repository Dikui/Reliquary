import { nextTick, onScopeDispose, reactive, readonly } from 'vue'

export type ToastTone = 'success' | 'info' | 'warning' | 'error'
export interface ToastOptions { content: string; duration?: number; theme?: ToastTone }
export interface ToastHandle { close: () => void }
export interface ConfirmOptions {
  header: string
  body?: string
  confirmBtn?: string
  cancelBtn?: string
  theme?: 'default' | 'danger'
  closeOnOverlayClick?: boolean
  closeOnEscKeydown?: boolean
  onConfirm?: (context: { dialog: ConfirmHandle }) => void | Promise<void>
  onCancel?: () => void
  onClose?: () => void
}
export interface ConfirmHandle {
  destroy: () => void
  update: (options: Partial<ConfirmOptions>) => void
  result: Promise<boolean>
}
interface ToastRecord { id: number; content: string; theme: ToastTone; close: () => void }
interface Confirmation {
  id: number
  options: ConfirmOptions
  busy: boolean
  failed: boolean
  accept: () => Promise<void>
  cancel: () => void
  close: () => void
  restoreFocus: (event: Event) => void
}
const toasts = reactive<ToastRecord[]>([])
const confirmations = reactive<Confirmation[]>([])
let sequence = 0

function showToast(input: string | ToastOptions, duration?: number): ToastHandle {
  const options = typeof input === 'string' ? { content: input, duration } : input
  const theme = options.theme ?? 'info'
  const existing = toasts.find(item => item.content === options.content && item.theme === theme)
  if (existing) return { close: existing.close }
  const id = ++sequence
  let timer: ReturnType<typeof setTimeout> | undefined
  const close = () => {
    clearTimeout(timer)
    const index = toasts.findIndex(item => item.id === id)
    if (index >= 0) toasts.splice(index, 1)
  }
  toasts.push({ id, content: options.content, theme, close })
  const timeout = options.duration ?? 4000
  if (timeout > 0) timer = setTimeout(close, timeout)
  return { close }
}
const withTone = (theme: ToastTone) => (input: string | ToastOptions, duration?: number) => showToast({ ...(typeof input === 'string' ? { content: input, duration } : input), theme })
export const toast = Object.assign(showToast, {
  success: withTone('success'), info: withTone('info'), warning: withTone('warning'), error: withTone('error'),
  clear: () => { for (const item of [...toasts]) item.close() },
})

export function confirm(options: ConfirmOptions): ConfirmHandle {
  const id = ++sequence
  const opener = typeof document === 'undefined' ? null : document.activeElement as HTMLElement | null
  let settled = false
  let accepting = false
  let resolve!: (confirmed: boolean) => void
  const result = new Promise<boolean>(done => { resolve = done })
  function finish(confirmed: boolean) {
    if (settled) return
    settled = true
    const index = confirmations.findIndex(item => item.id === id)
    if (index >= 0) confirmations.splice(index, 1)
    resolve(confirmed)
  }
  const handle: ConfirmHandle = {
    result,
    destroy: () => finish(accepting),
    update: next => { if (!settled) Object.assign(state.options, next) },
  }
  const state = reactive<Confirmation>({
    id, options: { ...options }, busy: false, failed: false,
    restoreFocus(event) {
      // The service has no DialogTrigger; Reka must not restore its later snapshot.
      event.preventDefault()
      // Let result callbacks re-enable controls and the closing focus scope finish.
      void nextTick(() => setTimeout(() => {
        if (!confirmations.length && opener?.isConnected && typeof opener.focus === 'function') {
          opener.focus({ preventScroll: true })
        }
      }, 0))
    },
    async accept() {
      if (state.busy || settled) return
      state.busy = true
      state.failed = false
      accepting = true
      try {
        await state.options.onConfirm?.({ dialog: handle })
        finish(true)
      } catch {
        if (!settled) state.failed = true
      } finally {
        accepting = false
        state.busy = false
      }
    },
    cancel() {
      if (state.busy || settled) return
      finish(false)
      state.options.onCancel?.()
    },
    close() {
      if (state.busy || settled) return
      finish(false)
      state.options.onClose?.()
    },
  })
  confirmations.push(state)
  return handle
}

/** Mount one FeedbackHost under the app's locale provider. */
export function useFeedback() { return { toasts: readonly(toasts), confirmations: readonly(confirmations) } }

/** Pending confirmations belong to the view that opened them. */
export function useConfirm(): typeof confirm {
  const pending = new Set<ConfirmHandle>()
  onScopeDispose(() => { for (const dialog of pending) dialog.destroy(); pending.clear() })
  return options => {
    const dialog = confirm(options)
    pending.add(dialog)
    void dialog.result.then(() => pending.delete(dialog))
    return dialog
  }
}
