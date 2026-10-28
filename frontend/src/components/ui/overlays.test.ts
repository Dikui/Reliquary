import assert from 'node:assert/strict'
import test from 'node:test'
import { h, ref } from 'vue'
import { doc, mount, send, settle } from './__tests__/dom.ts'
import { loadUiComponent } from './uiTestLoader.ts'

test('Dialog can place its overlay and content above legacy drawers', async t => {
  const view = loadUiComponent('Dialog')
  const mounted = mount({ setup: () => () => h(view, { modelValue: true, title: 'Search', layer: 2400 }) })
  t.after(() => mounted.unmount())
  await settle()
  const dialog = doc.querySelector('[role="dialog"]')!
  assert.equal(dialog.style.zIndex, 2401)
  const overlay = doc.querySelectorAll('div').find((node: any) => node.props.class === 'rq-overlay')!
  assert.equal(overlay.style.zIndex, 2400)
})

for (const name of ['Dialog', 'Drawer']) test(`${name} traps focus, closes with Escape and restores the opener`, async t => {
  const view = loadUiComponent(name), open = ref(false)
  const mounted = mount({ setup: () => () => h(view, { modelValue: open.value, title: 'Edit library', description: 'Library name', 'onUpdate:modelValue': (value: boolean) => { open.value = value } }, {
    trigger: () => h('button', { id: 'opener' }, 'Open'),
    default: () => [h('input', { id: 'first' }), h('button', { id: 'last' }, 'Save')],
  }) })
  t.after(() => mounted.unmount())
  const opener = doc.getElementById('opener')!
  opener.focus()
  await send(opener, 'click')
  assert.equal(open.value, true)
  const dialog = doc.querySelector('[role="dialog"]')!
  assert.ok(dialog)
  assert.equal(doc.getElementById(dialog.props['aria-labelledby'])?.textContent, 'Edit library')
  const first = doc.getElementById('first')!, last = doc.getElementById('last')!
  last.focus()
  await send(last, 'keydown', { key: 'Tab' })
  assert.ok(dialog.contains(doc.activeElement), 'Tab cannot escape the modal')
  opener.focus()
  assert.ok(dialog.contains(doc.activeElement), `external focus is brought back: ${doc.activeElement?.id}; listeners: ${[...doc.listeners.keys()].join(',')}`)
  first.focus()
  await send(first, 'keydown', { key: 'Escape' })
  assert.equal(open.value, false)
  assert.equal(doc.querySelector('[role="dialog"]'), null)
  assert.equal(doc.activeElement, opener)
  await mounted.unmount()
})

test('Tabs follows arrow keys, skips disabled tabs and exposes the active panel', async () => {
  const Tabs = loadUiComponent('Tabs'), current = ref('a')
  const mounted = mount({ setup: () => () => h(Tabs, { modelValue: current.value, label: 'Views', items: [{ value: 'a', label: 'List' }, { value: 'b', label: 'Unavailable', disabled: true }, { value: 'c', label: 'Table' }], 'onUpdate:modelValue': (value: string) => { current.value = value } }, { a: () => 'List content', c: () => 'Table content' }) })
  await settle()
  const tabs = doc.querySelectorAll('[role="tab"]')
  tabs[0]!.focus()
  await send(tabs[0]!, 'keydown', { key: 'ArrowRight' })
  assert.equal(current.value, 'c')
  assert.equal(doc.activeElement, tabs[2])
  assert.equal(doc.querySelector('[role="tabpanel"][data-state="active"]')?.textContent, 'Table content')
  await mounted.unmount()
})

test('Combobox filters by label and Enter selects the remaining option', async t => {
  const Combobox = loadUiComponent('Combobox'), selected = ref('')
  const mounted = mount({ setup: () => () => h(Combobox, { modelValue: selected.value, 'aria-label': 'Library', options: [{ value: 'a', label: 'Research' }, { value: 'b', label: 'Reports' }], 'onUpdate:modelValue': (value: string) => { selected.value = value } }) })
  t.after(() => mounted.unmount())
  await settle()
  const input = doc.querySelector('[role="combobox"]')!
  input.focus()
  input.value = 'Reports'
  await send(input, 'input')
  assert.deepEqual(doc.querySelectorAll('[role="option"]').map((node: any) => node.textContent), ['Reports'])
  await send(input, 'keydown', { key: 'Enter' })
  assert.equal(selected.value, 'b')
  assert.equal(input.value, 'Reports')
  await send(doc.querySelector('[aria-label="Clear selection"]')!, 'click')
  assert.equal(selected.value, '')
  assert.equal(input.value, '')
  input.value = 'No matching library'
  await send(input, 'input')
  assert.equal(doc.querySelectorAll('[role="option"]').length, 0)
  await send(input, 'keydown', { key: 'Enter' })
  assert.equal(selected.value, '')
  await send(doc.querySelector('[aria-label="Clear selection"]')!, 'click')
  assert.equal(input.value, '')
  assert.equal(doc.querySelector('[aria-label="Clear selection"]'), null)
  await mounted.unmount()
})

test('DropdownMenu skips a disabled action and selects an enabled action with the keyboard', async t => {
  const DropdownMenu = loadUiComponent('DropdownMenu'), selections: string[] = []
  const mounted = mount({ setup: () => () => h(DropdownMenu, { items: [{ value: 'a', label: 'Unavailable', disabled: true }, { value: 'b', label: 'Open' }], onSelect: (value: string) => selections.push(value) }, () => h('button', { id: 'menu-trigger' }, 'Actions')) })
  t.after(() => mounted.unmount())
  const trigger = doc.getElementById('menu-trigger')!
  trigger.focus()
  await send(trigger, 'keydown', { key: 'ArrowDown' })
  const item = doc.querySelectorAll('[role="menuitem"]').find((node: any) => !node.hasAttribute('data-disabled'))!
  assert.equal(item.textContent, 'Open')
  item.focus()
  await send(item, 'keydown', { key: 'Enter' })
  assert.deepEqual(selections, ['b'])
  assert.equal(doc.querySelector('[role="menu"]'), null)
})

test('Tooltip is available on keyboard focus and dismisses on Escape', async t => {
  const Tooltip = loadUiComponent('Tooltip')
  const mounted = mount({ setup: () => () => h(Tooltip, { content: 'More information' }, () => h('button', { id: 'tooltip-trigger' }, 'Help')) })
  t.after(() => mounted.unmount())
  const trigger = doc.getElementById('tooltip-trigger')!
  trigger.focus()
  await settle()
  assert.equal(doc.querySelector('[role="tooltip"]')?.textContent, 'More information')
  await send(trigger, 'keydown', { key: 'Escape' })
  assert.equal(doc.querySelector('[role="tooltip"]'), null)
})
