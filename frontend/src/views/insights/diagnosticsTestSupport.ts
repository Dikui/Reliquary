import { defineComponent, h } from 'vue'
import { loadUiComponent } from '../../components/ui/uiTestLoader'

export function diagnosticUI() {
  const ui: Record<string, unknown> = Object.fromEntries(['Button', 'Input', 'Select', 'Field', 'Tag', 'StatusSquare', 'LibMark', 'DataTable', 'EmptyState', 'ErrorState', 'Checkbox'].map(name => [name, loadUiComponent(name)]))
  const overlay = defineComponent({
    props: ['modelValue', 'title'], emits: ['update:modelValue'],
    setup: (props, { slots, emit }) => () => props.modelValue ? h('section', { role: 'dialog', 'aria-label': props.title }, [h('h2', props.title), h('button', { onClick: () => emit('update:modelValue', false) }, 'Close'), slots.default?.(), slots.footer?.()]) : null,
  })
  ui.Dialog = overlay; ui.Drawer = overlay
  ui.Tabs = defineComponent({
    props: ['modelValue', 'items', 'label'], emits: ['update:modelValue'],
    setup: (props, { slots, emit }) => () => h('div', [h('div', { role: 'tablist' }, props.items.map((item: any) => h('button', { role: 'tab', onClick: () => emit('update:modelValue', item.value) }, item.label))), slots[String(props.modelValue)]?.()]),
  })
  return ui
}
