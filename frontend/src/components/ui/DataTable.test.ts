import assert from 'node:assert/strict'
import test from 'node:test'
import { h, ref } from 'vue'
import { doc, mount, send, settle } from './__tests__/dom.ts'
import { loadUiComponent } from './uiTestLoader.ts'

test('DataTable keeps multiple checked rows separate from the single current row', async () => {
  const DataTable = loadUiComponent('DataTable'), selected = ref<string[]>([]), focused = ref('a')
  const mounted = mount({ setup: () => () => h(DataTable, {
    rows: [{ id: 'a', title: 'Alpha' }, { id: 'b', title: 'Beta' }], columns: [{ key: 'title', label: 'Title' }],
    label: 'Materials', selectable: true, selected: selected.value, focused: focused.value,
    'onUpdate:selected': (value: string[]) => { selected.value = value }, 'onUpdate:focused': (value: string) => { focused.value = value },
  }) })
  await settle()
  const checkboxes = doc.querySelectorAll('input[type="checkbox"]')
  checkboxes[0]!.checked = true
  await send(checkboxes[0]!, 'change')
  assert.deepEqual(selected.value, ['a', 'b'])
  // The adapter supports attribute selectors; row classes are part of this component's styling contract.
  const renderedRows = mounted.root.querySelectorAll('[data-row-key]')
  assert.ok(renderedRows.every(row => row.props.class.includes('rq-table__row--selected')))
  assert.equal(renderedRows.filter(row => row.props.class.includes('rq-table__row--focused')).length, 1)
  await send(renderedRows[1]!, 'mouseenter')
  assert.equal(focused.value, 'b')
  assert.deepEqual(selected.value, ['a', 'b'])
  renderedRows[1]!.focus()
  await send(renderedRows[1]!, 'keydown', { key: 'ArrowUp' })
  assert.equal(focused.value, 'a')
  assert.equal(doc.activeElement, renderedRows[0])
  await mounted.unmount()
})

test('DataTable extends a checked range and activates the current row without changing its checks', async () => {
  const DataTable = loadUiComponent('DataTable'), selected = ref<string[]>([]), focused = ref('a'), opened: string[] = []
  const mounted = mount({ setup: () => () => h(DataTable, {
    rows: [{ id: 'a' }, { id: 'b' }, { id: 'c' }], columns: [{ key: 'id', label: 'ID' }],
    label: 'Materials', selectable: true, selected: selected.value, focused: focused.value,
    'onUpdate:selected': (value: string[]) => { selected.value = value },
    'onUpdate:focused': (value: string) => { focused.value = value }, onActivate: (row: {id:string}) => opened.push(row.id),
  }) })
  await settle()
  const rows = mounted.root.querySelectorAll('[data-row-key]')
  const first = rows[0]!.querySelector('input')!, last = rows[2]!.querySelector('input')!
  first.checked = true; await send(first, 'change')
  await send(last.parentElement!, 'click', { shiftKey: true })
  last.checked = true; await send(last, 'change')
  assert.deepEqual(selected.value, ['a', 'b', 'c'])
  rows[0]!.focus(); await send(rows[0]!, 'keydown', { key: 'j' })
  assert.equal(focused.value, 'b')
  await send(rows[1]!, 'keydown', { key: 'Enter', isComposing: true })
  assert.deepEqual(opened, [])
  await send(rows[1]!, 'keydown', { key: 'Enter' })
  assert.deepEqual(opened, ['b'])
  assert.deepEqual(selected.value, ['a', 'b', 'c'])
  await mounted.unmount()
})
