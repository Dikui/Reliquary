import assert from 'node:assert/strict'
import test from 'node:test'
import { h, ref } from 'vue'
import { click, descendants, element, hasClass, renderer, settle, visibleText } from '../../views/library/componentTestHarness.ts'
import { loadUiComponent as component } from './uiTestLoader.ts'

test('Button preserves its label while busy and prevents repeated actions', async () => {
  const Button = component('Button')
  let calls = 0
  const root = element('root')
  const app = renderer.createApp({ render: () => h(Button, { loading: true, variant: 'primary', onClick: () => calls++ }, () => 'Save') })
  app.mount(root)
  const button = descendants(root).find(node => node.tag === 'button')!
  assert.equal(button.props.type, 'button')
  assert.equal(button.props['aria-busy'], true)
  assert.ok(hasClass(button, 'rq-button--primary'))
  assert.match(visibleText(button), /Save/)
  await click(button)
  assert.equal(calls, 0)
  app.unmount()
})

test('Field associates the native control with its label, hint and error', async () => {
  const Field = component('Field'), Input = component('Input')
  const root = element('root')
  const error = ref('Required'), disabled = ref(true)
  const app = renderer.createApp({ render: () => h(Field, { label: 'Library', hint: 'A name', error: error.value }, () => h(Input, { modelValue: '', disabled: disabled.value })) })
  app.mount(root)
  await settle()
  const nodes = descendants(root), input = nodes.find(node => node.tag === 'input')!
  assert.ok(input.props.id)
  assert.equal(nodes.find(node => node.tag === 'label')?.props.for, input.props.id)
  assert.equal(input.props['aria-invalid'], true)
  for (const id of input.props['aria-describedby'].split(' ')) assert.ok(nodes.some(node => node.props.id === id))
  assert.equal(input.props.disabled, true)
  error.value = ''
  disabled.value = false
  await settle()
  assert.equal(input.props['aria-invalid'], undefined)
  assert.equal(input.props.disabled, false)
  assert.ok(!input.props['aria-describedby'].includes('error'))
  app.unmount()
})

test('native controls emit edited values and retain HTML semantics', async () => {
  for (const [name, tag, property, value] of [
    ['Input', 'input', 'value', 'library'], ['Textarea', 'textarea', 'value', 'notes'],
    ['Select', 'select', 'value', 'b'], ['Checkbox', 'input', 'checked', true],
    ['Switch', 'input', 'checked', true], ['Radio', 'input', 'checked', true],
  ] as const) {
    const updates: unknown[] = [], root = element('root')
    const app = renderer.createApp(component(name), { modelValue: name === 'Checkbox' || name === 'Switch' ? false : '', value: 'b', name: 'sample', 'onUpdate:modelValue': (next: unknown) => updates.push(next) })
    app.mount(root)
    const control = descendants(root).find(node => node.tag === tag)!
    assert.equal(control.props.name, 'sample', name)
    const handler = control.props[property === 'checked' || name === 'Select' ? 'onChange' : 'onInput']
    handler({ target: { [property]: value }, isComposing: false })
    await settle()
    assert.deepEqual(updates, [name === 'Radio' ? 'b' : value], name)
    if (name === 'Switch') assert.equal(control.props.role, 'switch')
    app.unmount()
  }
})

test('text controls defer model changes until IME composition is committed', async () => {
  for (const name of ['Input', 'Textarea']) {
    const updates: string[] = [], root = element('root')
    const app = renderer.createApp(component(name), { modelValue: '', 'onUpdate:modelValue': (value: string) => updates.push(value) })
    app.mount(root)
    const input = descendants(root).find(node => node.tag === (name === 'Input' ? 'input' : 'textarea'))!
    input.props.onCompositionstart()
    input.props.onInput({ target: { value: '研' } })
    assert.deepEqual(updates, [])
    input.props.onCompositionend({ target: { value: '研究' } })
    assert.deepEqual(updates, ['研究'])
    app.unmount()
  }
})

test('display components expose content and status without relying on color', async () => {
  for (const name of ['Tag', 'LibMark', 'StatusSquare', 'EmptyState', 'ErrorState', 'Skeleton']) {
    const root = element('root')
    const view = component(name)
    const app = renderer.createApp({ render: () => h(view, { label: 'Library', title: 'Nothing here', description: 'Try again', color: 2 }, () => 'A label') })
    app.mount(root)
    assert.ok(descendants(root).some(node => node.props['aria-label'] || visibleText(node)), name)
    app.unmount()
  }
  const root = element('root'), values: boolean[] = []
  const app = renderer.createApp(component('Chip'), { modelValue: false, 'onUpdate:modelValue': (value: boolean) => values.push(value) })
  app.mount(root)
  const button = descendants(root).find(node => node.tag === 'button')!
  assert.equal(button.props['aria-pressed'], false)
  await click(button)
  assert.deepEqual(values, [true])
  app.unmount()
})
