import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import ts from 'typescript'
import { createImeEnterGuard, installImeEnterGuard } from './imeEnter'

const keyboard = (extra: object = {}) => ({ key: 'Enter', keyCode: 13, isComposing: false, shiftKey: false, ctrlKey: false, ...extra }) as KeyboardEvent

// The document capture listener protects TDesign's @enter and native key modifiers,
// including inputs rendered in dialogs through Teleport.
for (const name of ['all native and TDesign text fields']) {
  test(`${name}: capture prevents all three IME Enter variants without preventing the default confirmation`, () => {
    let time = 100
    const root = new EventTarget()
    const remove = installImeEnterGuard(root as unknown as Document, () => time)
    const target = { matches: (selector: string) => selector.includes('input') }
    let submitted = 0
    root.addEventListener('keydown', (event) => { if (!(event as KeyboardEvent).shiftKey) submitted++ })
    const dispatch = (type: string, props: object = {}, eventTarget: object = target) => {
      const event = new Event(type, { cancelable: true })
      Object.defineProperty(event, 'target', { value: eventTarget })
      Object.assign(event, props)
      root.dispatchEvent(event)
      assert.equal(event.defaultPrevented, false)
    }
    dispatch('keydown', keyboard({ isComposing: true }))
    dispatch('keydown', keyboard({ keyCode: 229 }))
    dispatch('compositionstart'); dispatch('keydown', keyboard())
    dispatch('compositionend'); dispatch('keydown', keyboard())
    assert.equal(submitted, 0)
    time += 60
    dispatch('keydown', keyboard({ shiftKey: true })); assert.equal(submitted, 0)
    dispatch('keydown', keyboard()); assert.equal(submitted, 1)
    // Composition in another text field must not suppress this field's Enter.
    dispatch('compositionstart')
    dispatch('keydown', keyboard(), { matches: () => true }); assert.equal(submitted, 2)
    dispatch('keydown', keyboard({ isComposing: true }), { matches: () => false }); assert.equal(submitted, 3)
    remove()
    dispatch('keydown', keyboard({ isComposing: true })); assert.equal(submitted, 4)
  })
}

test('the application installs the capture guard before mounting and cleans it up on unmount', () => {
  const source = readFileSync(new URL('../main.ts', import.meta.url), 'utf8')
  assert.ok(source.indexOf('installImeEnterGuard(document)') < source.indexOf('app.mount("#app")'))
  assert.match(source, /app\.onUnmount\(removeImeEnterGuard\)/)
})

function sourceHandler(path: string, name: string, bindings: Record<string, unknown>) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
  const ast = ts.createSourceFile('handler.ts', script, ts.ScriptTarget.Latest, true)
  let expression = ''
  for (const statement of ast.statements) {
    if (ts.isFunctionDeclaration(statement) && statement.name?.text === name) expression = statement.getText(ast)
    if (ts.isVariableStatement(statement)) for (const declaration of statement.declarationList.declarations) {
      if (declaration.name.getText(ast) === name) expression = `const ${name} = ${declaration.initializer!.getText(ast)}`
    }
  }
  assert.ok(expression)
  const code = ts.transpileModule(expression, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
  return new Function(...Object.keys(bindings), `${code};return ${name}`)(...Object.values(bindings))
}

for (const component of ['palette', 'workbench']) {
  test(`${component}: the actual submit handler behind capture ignores IME Enter and retains ordinary Enter behavior`, () => {
    let time = 100, actions = 0
    const handler = component === 'palette'
      ? sourceHandler('../components/GlobalCommandPalette.vue', 'onKeyDown', { composing: { value: false }, drawerVisible: { value: false }, primaryActionForSelected: () => actions++ })
      : sourceHandler('../views/library/components/EntryWorkbench.vue', 'onEntityEnter', { entityComposing: { value: false }, entitySearchTimer: null, resetEntityList: () => actions++ })
    const root = new EventTarget()
    const remove = installImeEnterGuard(root as unknown as Document, () => time)
    root.addEventListener('keydown', event => component === 'palette' ? handler(event) : handler('', { e: event }))
    const target = { matches: () => true }
    const dispatch = (type: string, props = {}) => {
      const event = new Event(type, { cancelable: true })
      Object.defineProperty(event, 'target', { value: target })
      Object.assign(event, props)
      root.dispatchEvent(event)
      return event
    }
    assert.equal(dispatch('keydown', keyboard({ isComposing: true })).defaultPrevented, false)
    assert.equal(dispatch('keydown', keyboard({ keyCode: 229 })).defaultPrevented, false)
    dispatch('compositionstart')
    assert.equal(dispatch('keydown', keyboard()).defaultPrevented, false)
    dispatch('compositionend')
    assert.equal(dispatch('keydown', keyboard()).defaultPrevented, false)
    assert.equal(actions, 0)
    time += 60
    dispatch('keydown', keyboard())
    assert.equal(actions, 1)
    remove()
  })
}
