import { defineComponent, h } from 'vue'
import { loadUiComponent } from '../../components/ui/uiTestLoader'

/** Actual form primitives; overlays are rendered inline by the DOM-free harness. */
export function settingsTestUi(overrides: Record<string, unknown> = {}): Record<string, any> {
  const ui: Record<string, any> = {
    ...Object.fromEntries(['Button', 'Input', 'Textarea', 'Select', 'LibMark', 'Tag', 'Switch', 'Checkbox', 'Field', 'EmptyState', 'ErrorState', 'StatusSquare', 'Skeleton', 'DataTable'].map(name => [name, loadUiComponent(name)])),
    Dialog: defineComponent({ props: ['modelValue', 'title'], setup: (props, { slots }) => () => props.modelValue ? h('section', { role: 'dialog', 'aria-label': props.title }, [props.title, slots.default?.(), slots.footer?.()]) : null }),
    Drawer: defineComponent({ props: ['modelValue', 'title'], setup: (props, { slots }) => () => props.modelValue ? h('section', { role: 'dialog', 'aria-label': props.title }, [props.title, slots.default?.(), slots.footer?.()]) : null }),
    toast: { success() {}, error() {}, warning() {}, info() {} },
    confirm: () => ({ destroy() {}, hide() {} }),
    ...overrides,
  }
  ui.useConfirm = () => ui.confirm
  return ui
}
