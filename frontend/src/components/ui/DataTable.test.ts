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
